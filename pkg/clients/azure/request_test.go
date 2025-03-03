package azure

import (
	"testing"
)

func TestNewRequest(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		apiKey      string
		wantErr     bool
		checkHeader bool
	}{
		{
			name:        "valid request",
			url:         "https://api.example.com",
			apiKey:      "test-api-key",
			wantErr:     false,
			checkHeader: true,
		},
		{
			name:        "empty URL",
			url:         "",
			apiKey:      "test-api-key",
			wantErr:     true,
			checkHeader: false,
		},
		{
			name:        "invalid URL",
			url:         "://invalid-url",
			apiKey:      "test-api-key",
			wantErr:     true,
			checkHeader: false,
		},
		{
			name:        "empty API key",
			url:         "https://api.example.com",
			apiKey:      "",
			wantErr:     false,
			checkHeader: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := NewRequest(tt.url, tt.apiKey)

			// Check error cases
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If we expected an error, no need to check the request
			if tt.wantErr {
				return
			}

			// Verify request is not nil
			if req == nil {
				t.Error("NewRequest() returned nil request with no error")
				return
			}

			// Check headers if required
			if tt.checkHeader {
				// Verify Content-Type header
				contentType := req.Header.Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("Expected Content-Type header to be 'application/json', got %q", contentType)
				}

				// Verify api-key header
				apiKey := req.Header.Get("api-key")
				if apiKey != tt.apiKey {
					t.Errorf("Expected api-key header to be %q, got %q", tt.apiKey, apiKey)
				}
			}

			// Verify request method
			if req.Method != "POST" {
				t.Errorf("Expected request method to be 'POST', got %q", req.Method)
			}

			// Verify URL
			if req.URL.String() != tt.url {
				t.Errorf("Expected URL to be %q, got %q", tt.url, req.URL.String())
			}
		})
	}
}
