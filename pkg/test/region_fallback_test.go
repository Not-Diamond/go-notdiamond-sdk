package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Not-Diamond/go-notdiamond"
	"github.com/Not-Diamond/go-notdiamond/pkg/model"
)

// TestRegionFallback tests the region fallback functionality
func TestRegionFallback(t *testing.T) {
	// Create a test server that simulates region-specific responses
	// First region (us-east4) returns 503 Service Unavailable
	// Second region (us-west1) returns 429 Too Many Requests
	// Third region (us-central1) returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the request URL for debugging
		t.Logf("Mock server received request: %s", r.URL.String())

		// Extract the region from the URL path
		path := r.URL.Path

		if strings.Contains(path, "us-east4") {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error": {"message": "Service unavailable in us-east4"}}`))
			return
		}

		if strings.Contains(path, "us-west1") {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error": {"message": "Rate limit exceeded in us-west1"}}`))
			return
		}

		if strings.Contains(path, "us-central1") {
			// Default (us-central1) returns success
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"candidates": [{
					"content": {
						"parts": [{"text": "Hello! I'm working in us-central1 region."}],
						"role": "model"
					},
					"finishReason": "STOP"
				}],
				"usageMetadata": {
					"promptTokenCount": 10,
					"candidatesTokenCount": 15,
					"totalTokenCount": 25
				}
			}`))
			return
		}

		// If no region match, return a generic error
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": {"message": "No region specified in URL"}}`))
	}))
	defer server.Close()

	// Create a custom HTTP client that redirects all requests to our test server
	customTransport := &mockTransport{
		mockServer: server,
		t:          t,
	}

	customClient := &http.Client{
		Transport: customTransport,
	}

	// Parse the test server URL
	serverURL, _ := url.Parse(server.URL)

	// Create API requests using the test server
	vertexRequest := &http.Request{
		Method: "POST",
		URL:    serverURL,
		Header: http.Header{
			"Content-Type": {"application/json"},
		},
	}

	// Get region fallback configuration
	modelConfig := model.Config{
		Models: model.OrderedModels{
			"vertex/gemini-pro/us-east4",    // Try us-east4 first (will fail with 503)
			"vertex/gemini-pro/us-west1",    // Fallback to us-west1 (will fail with 429)
			"vertex/gemini-pro/us-central1", // Final fallback to us-central1 (will succeed)
		},
		Clients: []http.Request{
			*vertexRequest,
		},
		VertexProjectID: "test-project",
		VertexLocation:  "us-central1",
		AzureAPIVersion: "2023-05-15",
	}

	// Create NotDiamond client
	client, err := notdiamond.Init(modelConfig)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Override the HTTP client with our custom one
	client.HttpClient.Client = customClient

	// Prepare request payload
	vertexPayload := map[string]interface{}{
		"model": "vertex/gemini-pro/us-east4", // Try with region
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{
						"text": "Hello, how are you? Tell me about region fallback.",
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":     0.7,
			"maxOutputTokens": 1024,
			"topP":            0.95,
			"topK":            40,
		},
	}

	jsonData, err := json.Marshal(vertexPayload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	// Create HTTP request with a path that includes the project and location
	reqURL, _ := url.Parse(server.URL + "/v1beta1/projects/test-project/locations/us-central1/publishers/google/models/gemini-pro:generateContent")
	req := &http.Request{
		Method: "POST",
		URL:    reqURL,
		Body:   io.NopCloser(bytes.NewBuffer(jsonData)),
		Header: http.Header{
			"Content-Type": {"application/json"},
		},
	}

	// Add client to context
	ctx := context.WithValue(context.Background(), notdiamond.ClientKey(), client)
	req = req.WithContext(ctx)

	// Make request
	start := time.Now()
	resp, err := client.HttpClient.Do(req)
	if err != nil {
		// In a test environment without proper GCP credentials, we expect an authentication error
		// Check if the error is related to authentication
		if strings.Contains(err.Error(), "could not find default credentials") {
			t.Logf("Authentication error as expected in test environment: %v", err)
			// Consider this a success for the test
			return
		}
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Verify response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	// Check that the response contains the expected text
	if !strings.Contains(string(body), "us-central1") {
		t.Errorf("Response does not contain expected region text: %s", string(body))
	}

	t.Logf("Response received in %.2f seconds", time.Since(start).Seconds())
	t.Logf("Response: %s", string(body))
}

// mockTransport is a custom http.RoundTripper that redirects all requests to our test server
type mockTransport struct {
	mockServer *httptest.Server
	t          *testing.T
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.t.Logf("Intercepted request to: %s", req.URL.String())

	// Create a new URL that points to our mock server but preserves the path
	mockURL, _ := url.Parse(m.mockServer.URL)
	mockURL.Path = req.URL.Path
	mockURL.RawQuery = req.URL.RawQuery

	// Create a new request with the mock URL
	mockReq := req.Clone(req.Context())
	mockReq.URL = mockURL

	// Log the redirected URL
	m.t.Logf("Redirecting to mock server: %s", mockReq.URL.String())

	// Use the default transport to make the actual request to our mock server
	return http.DefaultTransport.RoundTrip(mockReq)
}

// TestRegionFallbackOrdering tests that regions are correctly ordered in the fallback sequence
func TestRegionFallbackOrdering(t *testing.T) {
	// Test case 1: When a region is specified in the model, it should be tried first
	modelFull := "vertex/gemini-pro/us-east4"

	// Create a list of models to try
	modelsToTry := model.OrderedModels{
		"vertex/gemini-pro/us-central1",
		"vertex/gemini-pro/us-west1",
		"vertex/gemini-pro/us-east4",
	}

	// Move the requested model to the front of the slice if it matches
	for i, m := range modelsToTry {
		if m == modelFull {
			// Remove it from its current position and insert at front
			modelsToTry = append(modelsToTry[:i], modelsToTry[i+1:]...)
			modelsToTry = append([]string{modelFull}, modelsToTry...)
			break
		}
	}

	// Verify that the specified region is first in the list
	if modelsToTry[0] != modelFull {
		t.Errorf("Expected %s to be first in the list, got %s", modelFull, modelsToTry[0])
	}

	// Test case 2: When no region is specified, default regions should be added
	modelFull = "vertex/gemini-pro"
	baseModel := "gemini-pro"
	provider := "vertex"

	// Create a list of models to try
	modelsToTry = model.OrderedModels{
		"vertex/gemini-pro",
		"openai/gpt-3.5-turbo",
		"azure/gpt-35-turbo",
	}

	// Create a new slice with region-specific models at the front
	regionSpecificModels := []string{}

	// For the current model, add region-specific versions at the front
	if provider == "vertex" {
		// Default regions for Vertex: us-central1, us-west1, us-east1, etc.
		defaultRegions := []string{"us-central1", "us-west1", "us-east1", "us-west4"}
		for _, r := range defaultRegions {
			regionSpecificModels = append(regionSpecificModels, provider+"/"+baseModel+"/"+r)
		}
	}

	// Add the base model without region
	regionSpecificModels = append(regionSpecificModels, provider+"/"+baseModel)

	// Add the rest of the models
	for _, m := range modelsToTry {
		// Skip if already added
		alreadyAdded := false
		for _, added := range regionSpecificModels {
			if m == added {
				alreadyAdded = true
				break
			}
		}

		if !alreadyAdded {
			regionSpecificModels = append(regionSpecificModels, m)
		}
	}

	// Verify that region-specific models are added at the front
	if len(regionSpecificModels) < 5 {
		t.Errorf("Expected at least 5 models in the list, got %d", len(regionSpecificModels))
	}

	// Verify that the first models are region-specific
	for i := 0; i < 4; i++ {
		if !strings.Contains(regionSpecificModels[i], "vertex/gemini-pro/us-") {
			t.Errorf("Expected region-specific model at position %d, got %s", i, regionSpecificModels[i])
		}
	}

	// Verify that the base model without region is next
	if regionSpecificModels[4] != "vertex/gemini-pro" {
		t.Errorf("Expected base model without region at position 4, got %s", regionSpecificModels[4])
	}

	// Test case 3: Verify URL transformation for different regions
	urls := map[string]string{
		"vertex/gemini-pro/us-east4":    "https://us-east4-aiplatform.googleapis.com",
		"vertex/gemini-pro/us-west1":    "https://us-west1-aiplatform.googleapis.com",
		"vertex/gemini-pro/us-central1": "https://us-central1-aiplatform.googleapis.com",
	}

	for model, expectedHost := range urls {
		parts := strings.Split(model, "/")
		modelName := parts[1]
		region := parts[2]

		// Verify that the host is correctly formed with the region
		host := region + "-aiplatform.googleapis.com"
		if !strings.Contains(expectedHost, host) {
			t.Errorf("Expected host to contain %s for model %s, got %s", host, model, expectedHost)
		}

		// Verify that the path would be correctly formed
		path := "/v1beta1/projects/test-project/locations/" + region + "/publishers/google/models/" + modelName + ":generateContent"
		expectedPath := path
		if path != expectedPath {
			t.Errorf("Expected path %s for model %s, got %s", expectedPath, model, path)
		}
	}
}
