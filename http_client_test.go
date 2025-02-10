package notdiamond

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCombineMessages(t *testing.T) {
	tests := []struct {
		name          string
		modelMessages []Message
		userMessages  []Message
		expected      []Message
	}{
		{
			name: "both model and user messages",
			modelMessages: []Message{
				{"role": "system", "content": "You are a helpful assistant"},
			},
			userMessages: []Message{
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"},
			},
			expected: []Message{
				{"role": "system", "content": "You are a helpful assistant"},
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"},
			},
		},
		{
			name:          "empty model messages",
			modelMessages: []Message{},
			userMessages: []Message{
				{"role": "user", "content": "Hello"},
			},
			expected: []Message{
				{"role": "user", "content": "Hello"},
			},
		},
		{
			name: "empty user messages",
			modelMessages: []Message{
				{"role": "system", "content": "You are a helpful assistant"},
			},
			userMessages: []Message{},
			expected: []Message{
				{"role": "system", "content": "You are a helpful assistant"},
			},
		},
		{
			name:          "both empty messages",
			modelMessages: []Message{},
			userMessages:  []Message{},
			expected:      []Message{},
		},
		{
			name: "multiple model messages",
			modelMessages: []Message{
				{"role": "system", "content": "You are a helpful assistant"},
				{"role": "system", "content": "Respond in English"},
			},
			userMessages: []Message{
				{"role": "user", "content": "Hello"},
			},
			expected: []Message{
				{"role": "system", "content": "You are a helpful assistant"},
				{"role": "system", "content": "Respond in English"},
				{"role": "user", "content": "Hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := combineMessages(tt.modelMessages, tt.userMessages)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("combineMessages() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTryWithRetries(t *testing.T) {
	tests := []struct {
		name           string
		modelFull      string
		maxRetries     map[string]int
		timeout        map[string]float64
		backoff        map[string]float64
		modelMessages  map[string][]Message
		messages       []Message
		setupTransport func() *mockTransport
		expectedCalls  int
		expectError    bool
		errorContains  string
	}{
		{
			name:      "successful first attempt",
			modelFull: "openai/gpt-4",
			maxRetries: map[string]int{
				"openai/gpt-4": 3,
			},
			timeout: map[string]float64{
				"openai/gpt-4": 10.0,
			},
			messages: []Message{
				{"role": "user", "content": "Hello"},
			},
			setupTransport: func() *mockTransport {
				return &mockTransport{
					responses: []*http.Response{
						{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
						},
					},
				}
			},
			expectedCalls: 1,
			expectError:   false,
		},
		{
			name:      "retry success on third attempt",
			modelFull: "openai/gpt-4",
			maxRetries: map[string]int{
				"openai/gpt-4": 3,
			},
			timeout: map[string]float64{
				"openai/gpt-4": 10.0,
			},
			backoff: map[string]float64{
				"openai/gpt-4": 0.1,
			},
			messages: []Message{
				{"role": "user", "content": "Hello"},
			},
			setupTransport: func() *mockTransport {
				return &mockTransport{
					errors: []error{
						fmt.Errorf("network error"),
						fmt.Errorf("network error"),
					},
					responses: []*http.Response{
						nil,
						nil,
						{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
						},
					},
				}
			},
			expectedCalls: 3,
			expectError:   false,
		},
		{
			name:      "all attempts fail",
			modelFull: "openai/gpt-4",
			maxRetries: map[string]int{
				"openai/gpt-4": 2,
			},
			timeout: map[string]float64{
				"openai/gpt-4": 10.0,
			},
			backoff: map[string]float64{
				"openai/gpt-4": 0.1,
			},
			messages: []Message{
				{"role": "user", "content": "Hello"},
			},
			setupTransport: func() *mockTransport {
				return &mockTransport{
					errors: []error{
						fmt.Errorf("persistent error"),
						fmt.Errorf("persistent error"),
					},
				}
			},
			expectedCalls: 2,
			expectError:   true,
			errorContains: "persistent error",
		},
		{
			name:      "non-200 status code",
			modelFull: "openai/gpt-4",
			maxRetries: map[string]int{
				"openai/gpt-4": 1,
			},
			timeout: map[string]float64{
				"openai/gpt-4": 10.0,
			},
			messages: []Message{
				{"role": "user", "content": "Hello"},
			},
			setupTransport: func() *mockTransport {
				return &mockTransport{
					responses: []*http.Response{
						{
							StatusCode: 429,
							Body:       io.NopCloser(bytes.NewBufferString(`{"error": "rate limit"}`)),
						},
					},
				}
			},
			expectedCalls: 1,
			expectError:   true,
			errorContains: "429",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := tt.setupTransport()
			req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`))
			metrics, err := NewMetricsTracker(":memory:" + tt.name)
			if err != nil {
				log.Fatalf("Failed to open database connection: %v", err)
			}
			httpClient := &NotDiamondHttpClient{
				Client: &http.Client{Transport: transport},
				config: &Config{
					MaxRetries:          tt.maxRetries,
					Timeout:             tt.timeout,
					Backoff:             tt.backoff,
					ModelMessages:       tt.modelMessages,
					NoOfCalls:           10,
					RecoveryTime:        10 * time.Minute,
					AvgLatencyThreshold: 3.0,
				},
				metricsTracker: metrics,
			}

			ctx := context.WithValue(context.Background(), NotdiamondClientKey, &Client{
				clients:    []http.Request{*req},
				HttpClient: httpClient,
			})

			resp, err := httpClient.tryWithRetries(tt.modelFull, req, tt.messages, ctx)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error containing %q but got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Error("expected response but got nil")
				}
			}

			if transport.callCount != tt.expectedCalls {
				t.Errorf("expected %d calls but got %d", tt.expectedCalls, transport.callCount)
			}
		})
	}
}

func TestGetWeightedModelsList(t *testing.T) {
	tests := []struct {
		name    string
		weights map[string]float64
		want    []string
	}{
		{
			name: "two models",
			weights: map[string]float64{
				"openai/gpt-4": 0.6,
				"azure/gpt-4":  0.4,
			},
			want: []string{"openai/gpt-4", "azure/gpt-4"},
		},
		{
			name: "three models",
			weights: map[string]float64{
				"openai/gpt-4":       0.6,
				"azure/gpt-4":        0.4,
				"openai/gpt-4o-mini": 0.2,
			},
			want: []string{"openai/gpt-4", "azure/gpt-4", "openai/gpt-4o-mini"},
		},
		{
			name:    "empty map",
			weights: map[string]float64{},
			want:    []string{},
		},
		{
			name: "single model",
			weights: map[string]float64{
				"openai/gpt-4": 1.0,
			},
			want: []string{"openai/gpt-4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getWeightedModelsList(tt.weights)

			sort.Strings(got)
			sort.Strings(tt.want)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getWeightedModelsList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTryNextModel(t *testing.T) {
	tests := []struct {
		name          string
		modelFull     string
		messages      []Message
		setupClient   func() (*Client, *mockTransport)
		expectedURL   string
		expectedBody  map[string]interface{}
		expectedError string
		mockResponse  *http.Response
		mockError     error
		checkRequest  func(t *testing.T, req *http.Request)
	}{
		{
			name:      "successful azure request",
			modelFull: "azure/gpt-4",
			messages: []Message{
				{"role": "user", "content": "Hello"},
			},
			setupClient: func() (*Client, *mockTransport) {
				req, _ := http.NewRequest("POST", "https://myresource.azure.openai.com", nil)
				req.Header.Set("api-key", "test-key")
				transport := &mockTransport{
					responses: []*http.Response{
						{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBufferString(`{"choices":[{"message":{"content":"Hello"}}]}`)),
						},
					},
				}

				metrics, err := NewMetricsTracker(":memory:" + "successful_azure_request")
				if err != nil {
					log.Fatalf("Failed to open database connection: %v", err)
				}
				return &Client{
					clients: []http.Request{*req},
					HttpClient: &NotDiamondHttpClient{
						Client: &http.Client{
							Transport: transport,
						},
						config: &Config{
							ModelMessages: map[string][]Message{
								"azure/gpt-4": {
									{"role": "user", "content": "Hello"},
								},
							},
						},
						metricsTracker: metrics,
					},
				}, transport
			},
			expectedURL: "https://myresource.azure.openai.com/openai/deployments/gpt-4/chat/completions?api-version=2023-05-15",
			checkRequest: func(t *testing.T, req *http.Request) {
				if req.Header.Get("api-key") != "test-key" {
					t.Errorf("Expected api-key header to be 'test-key', got %q", req.Header.Get("api-key"))
				}
				if req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("Expected Content-Type header to be 'application/json', got %q", req.Header.Get("Content-Type"))
				}
				if req.URL.String() != "https://myresource.azure.openai.com/openai/deployments/gpt-4/chat/completions?api-version=2023-05-15" {
					t.Errorf("Expected URL %q, got %q", "https://myresource.azure.openai.com/openai/deployments/gpt-4/chat/completions?api-version=2023-05-15", req.URL.String())
				}
			},
		},
		{
			name:      "successful openai request",
			modelFull: "openai/gpt-4",
			messages: []Message{
				{"role": "user", "content": "Hello"},
			},
			setupClient: func() (*Client, *mockTransport) {
				req, _ := http.NewRequest("POST", "https://api.openai.com", nil)
				req.Header.Set("api-key", "test-key")
				transport := &mockTransport{
					responses: []*http.Response{
						{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBufferString(`{"choices":[{"message":{"content":"Hello"}}]}`)),
						},
					},
				}
				metrics, err := NewMetricsTracker(":memory:" + "successful_openai_request")
				if err != nil {
					log.Fatalf("Failed to open database connection: %v", err)
				}
				return &Client{
					clients: []http.Request{*req},
					HttpClient: &NotDiamondHttpClient{
						Client: &http.Client{
							Transport: transport,
						},
						config: &Config{
							ModelMessages: map[string][]Message{
								"openai/gpt-4": {
									{"role": "user", "content": "Hello"},
								},
							},
						},
						metricsTracker: metrics,
					},
				}, transport
			},
			checkRequest: func(t *testing.T, req *http.Request) {
				if req.Header.Get("Authorization") != "Bearer test-key" {
					t.Errorf("Expected Authorization header to be 'Bearer test-key', got %q", req.Header.Get("Authorization"))
				}
				if req.Header.Get("api-key") != "" {
					t.Errorf("Expected api-key header to be empty, got %q", req.Header.Get("api-key"))
				}
				if req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("Expected Content-Type header to be 'application/json', got %q", req.Header.Get("Content-Type"))
				}
			},
		},
		{
			name:      "provider not found",
			modelFull: "unknown/gpt-4",
			messages: []Message{
				{"role": "user", "content": "Hello"},
			},
			setupClient: func() (*Client, *mockTransport) {
				req, _ := http.NewRequest("POST", "https://api.openai.com", nil)
				transport := &mockTransport{}
				metrics, err := NewMetricsTracker(":memory:" + "provider_not_found")
				if err != nil {
					log.Fatalf("Failed to open database connection: %v", err)
				}

				return &Client{
					clients: []http.Request{*req},
					HttpClient: &NotDiamondHttpClient{
						Client: &http.Client{
							Transport: transport,
						},
						config: &Config{
							ModelMessages: map[string][]Message{
								"unknown/gpt-4": {
									{"role": "user", "content": "Hello"},
								},
							},
						},
						metricsTracker: metrics,
					},
				}, transport
			},
			expectedError: "no client found for provider unknown",
		},
		{
			name:      "http client error",
			modelFull: "openai/gpt-4",
			messages: []Message{
				{"role": "user", "content": "Hello"},
			},
			setupClient: func() (*Client, *mockTransport) {
				req, _ := http.NewRequest("POST", "https://api.openai.com", nil)
				req.Header.Set("api-key", "test-key")
				transport := &mockTransport{
					errors: []error{fmt.Errorf("network error")},
				}
				metrics, err := NewMetricsTracker(":memory:" + "http_client_error")
				if err != nil {
					log.Fatalf("Failed to open database connection: %v", err)
				}
				return &Client{
					clients: []http.Request{*req},
					HttpClient: &NotDiamondHttpClient{
						Client: &http.Client{
							Transport: transport,
						},
						config: &Config{
							ModelMessages: map[string][]Message{
								"openai/gpt-4": {
									{"role": "user", "content": "Hello"},
								},
							},
						},
						metricsTracker: metrics,
					},
				}, transport
			},
			expectedError: "network error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, transport := tt.setupClient()
			ctx := context.Background()

			resp, err := tryNextModel(client, tt.modelFull, tt.messages, ctx)

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("Expected error containing %q but got nil", tt.expectedError)
				} else if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing %q but got %q", tt.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if resp == nil {
				t.Fatal("Expected response but got nil")
			}

			if tt.checkRequest != nil && transport.lastRequest != nil {
				tt.checkRequest(t, transport.lastRequest)
			}
		})
	}
}

func TestExtractMessagesFromRequest(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		expected []Message
	}{
		{
			name: "valid messages",
			payload: []byte(`{
				"messages": [
					{"role": "user", "content": "Hello"},
					{"role": "assistant", "content": "Hi there"}
				]
			}`),
			expected: []Message{
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"},
			},
		},
		{
			name:     "empty messages array",
			payload:  []byte(`{"messages": []}`),
			expected: []Message{},
		},
		{
			name:     "invalid json",
			payload:  []byte(`{invalid json}`),
			expected: nil,
		},
		{
			name:     "missing messages field",
			payload:  []byte(`{"other": "field"}`),
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "http://example.com", bytes.NewBuffer(tt.payload))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			got := extractMessagesFromRequest(req)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("extractMessagesFromRequest() = %v, want %v", got, tt.expected)
			}

			body := make([]byte, len(tt.payload))
			n, err := req.Body.Read(body)
			if err != nil && err.Error() != "EOF" {
				t.Errorf("Failed to read request body after extraction: %v", err)
			}
			if n != len(tt.payload) {
				t.Errorf("Request body length after extraction = %d, want %d", n, len(tt.payload))
			}
		})
	}
}

func TestExtractProviderFromRequest(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "OpenAI URL",
			url:      "https://api.openai.com/v1/chat/completions",
			expected: "openai",
		},
		{
			name:     "Azure URL",
			url:      "https://myresource.azure.openai.com/openai/deployments/gpt-4/chat/completions",
			expected: "azure",
		},
		{
			name:     "Invalid URL",
			url:      "https://api.example.com/v1/chat/completions",
			expected: "",
		},
		{
			name:     "Empty URL",
			url:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", tt.url, nil)
			if err != nil && tt.url != "" {
				t.Fatalf("Failed to create request: %v", err)
			}

			got := extractProviderFromRequest(req)
			if got != tt.expected {
				t.Errorf("extractProviderFromRequest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractModelFromRequest(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		expected string
	}{
		{
			name: "valid model",
			payload: []byte(`{
				"model": "gpt-4",
				"messages": [{"role": "user", "content": "Hello"}]
			}`),
			expected: "gpt-4",
		},
		{
			name: "missing model field",
			payload: []byte(`{
				"messages": [{"role": "user", "content": "Hello"}]
			}`),
			expected: "",
		},
		{
			name:     "invalid json",
			payload:  []byte(`{invalid json}`),
			expected: "",
		},
		{
			name: "model is not string",
			payload: []byte(`{
				"model": 123,
				"messages": [{"role": "user", "content": "Hello"}]
			}`),
			expected: "",
		},
		{
			name:     "empty payload",
			payload:  []byte{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "http://example.com", bytes.NewBuffer(tt.payload))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			got := extractModelFromRequest(req)
			if got != tt.expected {
				t.Errorf("extractModelFromRequest() = %v, want %v", got, tt.expected)
			}

			// Verify that the request body can still be read after extraction
			body := make([]byte, len(tt.payload))
			n, err := req.Body.Read(body)
			if err != nil && err.Error() != "EOF" {
				t.Errorf("Failed to read request body after extraction: %v", err)
			}
			if n != len(tt.payload) {
				t.Errorf("Request body length after extraction = %d, want %d", n, len(tt.payload))
			}
		})
	}
}

func TestGetMaxRetriesForStatus(t *testing.T) {
	tests := []struct {
		name            string
		modelFull       string
		statusCode      int
		maxRetries      map[string]int
		statusCodeRetry interface{}
		expected        int
	}{
		{
			name:       "model specific status code retry",
			modelFull:  "openai/gpt-4",
			statusCode: 429,
			statusCodeRetry: map[string]map[string]int{
				"openai/gpt-4": {
					"429": 5,
				},
			},
			maxRetries: map[string]int{
				"openai/gpt-4": 3,
			},
			expected: 5,
		},
		{
			name:       "global status code retry",
			modelFull:  "openai/gpt-4",
			statusCode: 429,
			statusCodeRetry: map[string]int{
				"429": 4,
			},
			maxRetries: map[string]int{
				"openai/gpt-4": 3,
			},
			expected: 4,
		},
		{
			name:       "fallback to model max retries",
			modelFull:  "openai/gpt-4",
			statusCode: 429,
			statusCodeRetry: map[string]int{
				"500": 5,
			},
			maxRetries: map[string]int{
				"openai/gpt-4": 3,
			},
			expected: 3,
		},
		{
			name:            "default to 1 when no config exists",
			modelFull:       "openai/gpt-4",
			statusCode:      429,
			statusCodeRetry: map[string]int{},
			maxRetries:      map[string]int{},
			expected:        1,
		},
		{
			name:       "model specific takes precedence over global",
			modelFull:  "openai/gpt-4",
			statusCode: 429,
			statusCodeRetry: map[string]map[string]int{
				"openai/gpt-4": {
					"429": 5,
				},
			},
			maxRetries: map[string]int{
				"openai/gpt-4": 3,
			},
			expected: 5,
		},
		{
			name:       "different status code in model specific",
			modelFull:  "openai/gpt-4",
			statusCode: 429,
			statusCodeRetry: map[string]map[string]int{
				"openai/gpt-4": {
					"500": 5,
				},
			},
			maxRetries: map[string]int{
				"openai/gpt-4": 3,
			},
			expected: 3,
		},
		{
			name:       "different model in model specific",
			modelFull:  "openai/gpt-4",
			statusCode: 429,
			statusCodeRetry: map[string]map[string]int{
				"azure/gpt-4": {
					"429": 5,
				},
			},
			maxRetries: map[string]int{
				"openai/gpt-4": 3,
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &NotDiamondHttpClient{
				config: &Config{
					MaxRetries:      tt.maxRetries,
					StatusCodeRetry: tt.statusCodeRetry,
				},
			}

			got := client.getMaxRetriesForStatus(tt.modelFull, tt.statusCode)
			if got != tt.expected {
				t.Errorf("getMaxRetriesForStatus() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDo(t *testing.T) {
	tests := []struct {
		name          string
		setupClient   func() (*NotDiamondHttpClient, *mockTransport)
		expectedCalls int
		expectError   bool
		errorContains string
	}{
		{
			name: "successful first attempt with ordered models",
			setupClient: func() (*NotDiamondHttpClient, *mockTransport) {
				transport := &mockTransport{
					responses: []*http.Response{
						{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
						},
					},
				}
				metrics, err := NewMetricsTracker(":memory:" + "successful first attempt with ordered models")
				if err != nil {
					log.Fatalf("Failed to open database connection: %v", err)
				}
				client := &NotDiamondHttpClient{
					Client: &http.Client{Transport: transport},
					config: &Config{
						MaxRetries: map[string]int{"openai/gpt-4": 3},
						Timeout:    map[string]float64{"openai/gpt-4": 30.0},
					},
					metricsTracker: metrics,
				}

				return client, transport
			},
			expectedCalls: 1,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, transport := tt.setupClient()

			// Create a request with the necessary context
			req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions",
				bytes.NewBufferString(`{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`))

			// Create NotDiamondClient and add it to context
			notDiamondClient := &Client{
				HttpClient: client,
				models:     OrderedModels{"openai/gpt-4", "azure/gpt-4"},
				isOrdered:  true,
			}
			ctx := context.WithValue(context.Background(), NotdiamondClientKey, notDiamondClient)
			req = req.WithContext(ctx)

			// Make the request
			resp, err := client.Do(req)

			// Verify error expectations
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error containing %q but got %q", tt.errorContains, err.Error())
				}
				return
			}

			// Verify success case
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if resp == nil {
				t.Error("expected response but got nil")
				return
			}

			// Verify number of calls made
			if transport.callCount != tt.expectedCalls {
				t.Errorf("expected %d calls but got %d", tt.expectedCalls, transport.callCount)
			}
		})
	}
}

type mockTransport struct {
	responses   []*http.Response
	errors      []error
	lastRequest *http.Request
	callCount   int
	currentIdx  int
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.lastRequest = req
	m.callCount++

	if m.currentIdx < len(m.responses) || m.currentIdx < len(m.errors) {
		resp := (*http.Response)(nil)
		if m.currentIdx < len(m.responses) {
			resp = m.responses[m.currentIdx]
		}

		err := error(nil)
		if m.currentIdx < len(m.errors) {
			err = m.errors[m.currentIdx]
		}

		m.currentIdx++
		return resp, err
	}

	// Default case: return the last configured response/error
	if len(m.responses) > 0 {
		return m.responses[len(m.responses)-1], nil
	}
	if len(m.errors) > 0 {
		return nil, m.errors[len(m.errors)-1]
	}
	return nil, fmt.Errorf("no response configured")
}
