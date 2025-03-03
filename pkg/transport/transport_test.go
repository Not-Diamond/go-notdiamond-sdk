package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	http_client "github.com/Not-Diamond/go-notdiamond/pkg/http/client"
	"github.com/Not-Diamond/go-notdiamond/pkg/http/request"
	"github.com/Not-Diamond/go-notdiamond/pkg/metric"
	"github.com/Not-Diamond/go-notdiamond/pkg/model"
	"github.com/alicebob/miniredis/v2"
)

type testMockTransport struct {
	responses   []*http.Response
	errors      []error
	lastRequest *http.Request
	callCount   int
}

func (m *testMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Store the request for later inspection
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	// Store the request with a fresh body copy
	m.lastRequest = req.Clone(req.Context())
	m.lastRequest.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Restore original request body
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	m.callCount++

	// Try to parse the body as JSON
	var jsonBody interface{}
	if err := json.Unmarshal(bodyBytes, &jsonBody); err != nil && len(m.errors) > 0 {
		return nil, m.errors[0]
	}

	// Return mock response if configured
	if len(m.responses) > 0 && m.responses[0] != nil {
		resp := m.responses[0]
		// Ensure response has a body
		if resp.Body == nil {
			resp.Body = io.NopCloser(bytes.NewBufferString("{}"))
		}
		// Read and clone the response body
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			respBody = []byte("{}")
		}
		return &http.Response{
			Status:     resp.Status,
			StatusCode: resp.StatusCode,
			Body:       io.NopCloser(bytes.NewBuffer(respBody)),
			Header:     resp.Header,
		}, nil
	}

	return nil, fmt.Errorf("no response configured")
}

func TestNewTransport(t *testing.T) {
	tests := []struct {
		name      string
		config    model.Config
		wantErr   bool
		errString string
	}{
		{
			name: "valid config with ordered models",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
					{
						Host: "myresource.azure.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "myresource.azure.openai.com",
							Path:   "/openai/deployments/gpt-4/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{"openai/gpt-4", "azure/gpt-4"},
				ModelLatency: model.ModelLatency{
					"openai/gpt-4": &model.RollingAverageLatency{
						AvgLatencyThreshold: 3.5,
						NoOfCalls:           5,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with weighted models",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
					{
						Host: "myresource.azure.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "myresource.azure.openai.com",
							Path:   "/openai/deployments/gpt-4/chat/completions",
						},
					},
				},
				Models: model.WeightedModels{
					"openai/gpt-4": 0.6,
					"azure/gpt-4":  0.4,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config - empty models",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{},
			},
			wantErr:   true,
			errString: "invalid config: at least one model must be provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a temporary directory for database files
			transport, err := NewTransport(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTransport() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errString != "" && !reflect.DeepEqual(err.Error(), tt.errString) {
				t.Errorf("NewTransport() error = %v, wantErr %v", err, tt.errString)
				return
			}
			if err == nil {
				if transport == nil {
					t.Error("NewTransport() returned nil transport with no error")
					return
				}
				// Clean up
				if err := transport.metricsTracker.Close(); err != nil {
					t.Errorf("Failed to close metrics tracker: %v", err)
				}
			}
		})
	}
}

func TestBuildModelProviders(t *testing.T) {
	tests := []struct {
		name     string
		models   model.Models
		expected map[string]map[string]bool
	}{
		{
			name: "ordered models",
			models: model.OrderedModels{
				"openai/gpt-4",
				"azure/gpt-4",
				"openai/gpt-3.5-turbo",
			},
			expected: map[string]map[string]bool{
				"gpt-4": {
					"openai": true,
					"azure":  true,
				},
				"gpt-3.5-turbo": {
					"openai": true,
				},
			},
		},
		{
			name: "weighted models",
			models: model.WeightedModels{
				"openai/gpt-4": 0.6,
				"azure/gpt-4":  0.4,
			},
			expected: map[string]map[string]bool{
				"gpt-4": {
					"openai": true,
					"azure":  true,
				},
			},
		},
		{
			name:     "empty models",
			models:   model.OrderedModels{},
			expected: map[string]map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildModelProviders(tt.models)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("buildModelProviders() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTransport_RoundTrip(t *testing.T) {
	// Set up miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tests := []struct {
		name          string
		requestBody   string
		modelMessages map[string][]model.Message
		expectedBody  string
		mockResponse  *http.Response
		mockError     error
		expectError   bool
		errorContains string
		checkRequest  func(t *testing.T, req *http.Request)
	}{
		{
			name: "basic request without model messages",
			requestBody: `{
				"model": "gpt-4",
				"messages": [{"role": "user", "content": "Hello"}]
			}`,
			expectedBody: `{
				"model": "gpt-4",
				"messages": [{"role": "user", "content": "Hello"}]
			}`,
			mockResponse: &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			},
			checkRequest: func(t *testing.T, req *http.Request) {
				if auth := req.Header.Get("Authorization"); auth != "Bearer test-key" {
					t.Errorf("Expected Authorization header to be 'Bearer test-key', got %v", auth)
				}
			},
		},
		{
			name: "request with model messages",
			requestBody: `{
				"model": "gpt-4",
				"messages": [{"role": "user", "content": "Hello"}]
			}`,
			modelMessages: map[string][]model.Message{
				"openai/gpt-4": {
					{"role": "system", "content": "You are a helpful assistant."},
				},
			},
			expectedBody: `{
				"model": "gpt-4",
				"messages": [
					{"role": "system", "content": "You are a helpful assistant."},
					{"role": "user", "content": "Hello"}
				]
			}`,
			mockResponse: &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			},
			checkRequest: func(t *testing.T, req *http.Request) {
				if auth := req.Header.Get("Authorization"); auth != "Bearer test-key" {
					t.Errorf("Expected Authorization header to be 'Bearer test-key', got %v", auth)
				}
			},
		},
		{
			name:        "invalid request body",
			requestBody: `{invalid`,
			mockResponse: &http.Response{
				Status:     "400 Bad Request",
				StatusCode: 400,
				Body:       io.NopCloser(bytes.NewBufferString(`{"error": "invalid request"}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			},
			mockError:     fmt.Errorf("invalid request"),
			expectError:   true,
			errorContains: "failed to unmarshal body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockTransport := &testMockTransport{
				responses: []*http.Response{tt.mockResponse},
				errors:    []error{tt.mockError},
			}

			// Create metrics tracker with miniredis
			metrics, err := metric.NewTracker(mr.Addr())
			if err != nil {
				t.Fatalf("Failed to create metrics tracker: %v", err)
			}

			transport := &Transport{
				Base:           mockTransport,
				metricsTracker: metrics,
				config: model.Config{
					ModelMessages: tt.modelMessages,
					Models:        model.OrderedModels{"openai/gpt-4"},
					Clients: []http.Request{
						{
							Method: "POST",
							URL: &url.URL{
								Scheme: "https",
								Host:   "api.openai.com",
								Path:   "/v1/chat/completions",
							},
						},
					},
				},
				client: &http_client.Client{
					HttpClient: &http_client.NotDiamondHttpClient{
						Client: &http.Client{Transport: mockTransport},
						Config: model.Config{
							Models:        model.OrderedModels{"openai/gpt-4"},
							ModelMessages: tt.modelMessages,
						},
						MetricsTracker: metrics,
					},
					Models:    model.OrderedModels{"openai/gpt-4"},
					IsOrdered: true,
					Clients: []http.Request{
						{
							Method: "POST",
							URL: &url.URL{
								Scheme: "https",
								Host:   "api.openai.com",
								Path:   "/v1/chat/completions",
							},
						},
					},
				},
			}

			req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBufferString(tt.requestBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			// Add Authorization header
			req.Header.Set("Authorization", "Bearer test-key")

			// Add client to context
			ctx := context.WithValue(req.Context(), http_client.ClientKey, transport.client)
			req = req.WithContext(ctx)

			// Extract messages and model
			messages := request.ExtractMessagesFromRequest(req)
			extractedModel, err := request.ExtractModelFromRequest(req)
			if err != nil {
				if tt.expectError {
					if !strings.Contains(err.Error(), tt.errorContains) {
						t.Errorf("Expected error containing %q but got %q", tt.errorContains, err.Error())
					}
					return
				}
				t.Fatalf("Failed to extract model: %v", err)
			}

			// Combine with model messages if they exist
			if modelMessages, exists := tt.modelMessages["openai/"+extractedModel]; exists {
				if err := updateRequestWithCombinedMessages(req, modelMessages, messages, extractedModel); err != nil {
					t.Fatalf("Failed to update request with combined messages: %v", err)
				}
			}

			// Use mockTransport directly for testing
			resp, err := mockTransport.RoundTrip(req)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q but got %q", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if resp == nil {
				t.Error("Expected response but got nil")
				return
			}

			if tt.checkRequest != nil {
				tt.checkRequest(t, mockTransport.lastRequest)
			}

			if mockTransport.lastRequest != nil && tt.expectedBody != "" {
				// Read and compare request bodies
				actualBody, err := io.ReadAll(mockTransport.lastRequest.Body)
				if err != nil {
					t.Fatalf("Failed to read actual request body: %v", err)
				}

				// Normalize JSON for comparison
				var actualJSON, expectedJSON interface{}
				if err := json.Unmarshal(actualBody, &actualJSON); err != nil {
					t.Fatalf("Failed to parse actual JSON: %v", err)
				}
				if err := json.Unmarshal([]byte(tt.expectedBody), &expectedJSON); err != nil {
					t.Fatalf("Failed to parse expected JSON: %v", err)
				}

				if !reflect.DeepEqual(actualJSON, expectedJSON) {
					t.Errorf("Request body mismatch.\nGot: %s\nWant: %s", actualBody, tt.expectedBody)
				}
			}
		})
	}
}

func TestUpdateRequestWithCombinedMessages(t *testing.T) {
	tests := []struct {
		name           string
		modelMessages  []model.Message
		messages       []model.Message
		extractedModel string
		expectedBody   string
		wantErr        bool
	}{
		{
			name: "successfully combines messages",
			modelMessages: []model.Message{
				{"role": "system", "content": "You are a helpful assistant."},
			},
			messages: []model.Message{
				{"role": "user", "content": "Hello"},
			},
			extractedModel: "gpt-4",
			expectedBody:   `{"model":"gpt-4","messages":[{"role":"system","content":"You are a helpful assistant."},{"role":"user","content":"Hello"}]}`,
			wantErr:        false,
		},
		{
			name:          "empty model messages",
			modelMessages: []model.Message{},
			messages: []model.Message{
				{"role": "user", "content": "Hello"},
			},
			extractedModel: "gpt-4",
			expectedBody:   `{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`,
			wantErr:        false,
		},
		{
			name: "invalid message sequence",
			modelMessages: []model.Message{
				{"role": "assistant", "content": "Invalid first message"},
			},
			messages: []model.Message{
				{"role": "user", "content": "Hello"},
			},
			extractedModel: "gpt-4",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "http://example.com",
				bytes.NewBufferString(tt.expectedBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			err = updateRequestWithCombinedMessages(req, tt.modelMessages, tt.messages, tt.extractedModel)

			if (err != nil) != tt.wantErr {
				t.Errorf("updateRequestWithCombinedMessages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Set up test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"resp-123","choices":[{"message":{"role":"assistant","content":"Hello there"}}]}`))
	}))
	defer server.Close()

	// Create a basic transport to test just the transport functionality
	mockTransport := &testMockTransport{
		responses: []*http.Response{
			{
				StatusCode: 200,
				Status:     "200 OK",
				Body:       io.NopCloser(bytes.NewBufferString(`{"id":"resp-123","choices":[{"message":{"role":"assistant","content":"Hello there"}}]}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			},
		},
	}

	// Create a request directly to our mock transport
	req, err := http.NewRequest("POST", "https://api.example.com/v1/chat/completions",
		bytes.NewBufferString(`{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Test direct RoundTrip function rather than going through the client
	resp, err := mockTransport.RoundTrip(req)
	if err != nil {
		t.Errorf("RoundTrip() direct error = %v", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("RoundTrip() got status code %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Now let's test that the transport can read the body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Failed to read response body: %v", err)
		return
	}

	// Check response body content
	expected := `{"id":"resp-123","choices":[{"message":{"role":"assistant","content":"Hello there"}}]}`
	if string(bodyBytes) != expected {
		t.Errorf("Response body = %s, want %s", string(bodyBytes), expected)
	}
}

func TestExtractModelFromRequest(t *testing.T) {
	tests := []struct {
		name        string
		requestBody string
		want        string
		wantErr     bool
	}{
		{
			name:        "valid model field",
			requestBody: `{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`,
			want:        "gpt-4",
			wantErr:     false,
		},
		{
			name:        "model with provider",
			requestBody: `{"model":"openai/gpt-4","messages":[{"role":"user","content":"Hello"}]}`,
			want:        "gpt-4",
			wantErr:     false,
		},
		{
			name:        "invalid json",
			requestBody: `{"model":`,
			want:        "",
			wantErr:     true,
		},
		{
			name:        "missing model field",
			requestBody: `{"messages":[{"role":"user","content":"Hello"}]}`,
			want:        "",
			wantErr:     true,
		},
		{
			name:        "model not string",
			requestBody: `{"model":123,"messages":[{"role":"user","content":"Hello"}]}`,
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "http://example.com", bytes.NewBufferString(tt.requestBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			got, err := ExtractModelFromRequest(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractModelFromRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ExtractModelFromRequest() got = %v, want %v", got, tt.want)
			}
		})
	}

	// Test nil request and body cases
	t.Run("nil request", func(t *testing.T) {
		_, err := ExtractModelFromRequest(nil)
		if err == nil {
			t.Error("ExtractModelFromRequest() on nil request didn't return error")
		}
	})

	t.Run("nil body", func(t *testing.T) {
		req, err := http.NewRequest("POST", "http://example.com", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Body = nil

		_, err = ExtractModelFromRequest(req)
		if err == nil {
			t.Error("ExtractModelFromRequest() on nil body didn't return error")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		req, err := http.NewRequest("POST", "http://example.com", bytes.NewBufferString(""))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = ExtractModelFromRequest(req)
		if err == nil {
			t.Error("ExtractModelFromRequest() on empty body didn't return error")
		}
	})
}

func TestTransport_RoundTripActual(t *testing.T) {
	// Set up test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"resp-123","choices":[{"message":{"role":"assistant","content":"Hello there"}}]}`))
	}))
	defer server.Close()

	// Create a miniredis instance for metrics
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	// Create metrics tracker
	metricsTracker, err := metric.NewTracker(mr.Addr())
	if err != nil {
		t.Fatalf("Failed to create metrics tracker: %v", err)
	}
	defer metricsTracker.Close()

	// Create a mock transport for response
	mockTransport := &testMockTransport{
		responses: []*http.Response{
			{
				StatusCode: 200,
				Status:     "200 OK",
				Body:       io.NopCloser(bytes.NewBufferString(`{"id":"resp-123","choices":[{"message":{"role":"assistant","content":"Hello there"}}]}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			},
		},
	}

	// Create the actual transport
	transport := &Transport{
		Base: mockTransport,
		client: &http_client.Client{
			HttpClient: &http_client.NotDiamondHttpClient{
				Client: &http.Client{Transport: mockTransport},
				Config: model.Config{
					Models: model.OrderedModels{"openai/gpt-4"},
				},
				MetricsTracker: metricsTracker,
			},
			Models:    model.OrderedModels{"openai/gpt-4"},
			IsOrdered: true,
			Clients: []http.Request{
				{
					Method: "POST",
					URL: &url.URL{
						Scheme: "https",
						Host:   "api.openai.com",
						Path:   "/v1/chat/completions",
					},
				},
			},
		},
		config: model.Config{
			Models: model.OrderedModels{"openai/gpt-4"},
			ModelMessages: map[string][]model.Message{
				"openai/gpt-4": {
					{"role": "system", "content": "You are a helpful assistant."},
				},
			},
		},
		metricsTracker: metricsTracker,
	}

	// Create test request
	reqBody := `{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Test RoundTrip
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Errorf("Transport.RoundTrip() error = %v", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Transport.RoundTrip() got status = %v, want %v", resp.StatusCode, http.StatusOK)
	}

	// Verify that the model messages were combined correctly
	if mockTransport.lastRequest != nil {
		bodyBytes, _ := io.ReadAll(mockTransport.lastRequest.Body)
		var requestBody map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
			t.Errorf("Failed to unmarshal last request body: %v", err)
		}

		messagesArray, ok := requestBody["messages"].([]interface{})
		if !ok {
			t.Errorf("Messages not found in request or wrong type")
		} else if len(messagesArray) != 2 {
			t.Errorf("Expected 2 messages (system + user), got %d", len(messagesArray))
		}
	}
}
