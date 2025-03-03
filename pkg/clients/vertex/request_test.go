package vertex

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// mockTokenSource implements oauth2.TokenSource for testing
type mockTokenSource struct{}

func (m *mockTokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
	}, nil
}

// mockFindDefaultCredentials is our test replacement for google.FindDefaultCredentials
func mockFindDefaultCredentials(ctx context.Context, scopes ...string) (*google.Credentials, error) {
	return &google.Credentials{
		TokenSource: &mockTokenSource{},
	}, nil
}

func TestNewRequest(t *testing.T) {
	// Override the default credentials function with our mock
	originalFindDefaultCredentials := findDefaultCredentials
	findDefaultCredentials = mockFindDefaultCredentials
	defer func() { findDefaultCredentials = originalFindDefaultCredentials }()

	tests := []struct {
		name        string
		projectID   string
		location    string
		wantErr     bool
		errContains string
		checkURL    bool
		expectedURL string
	}{
		{
			name:      "valid request",
			projectID: "test-project",
			location:  "us-central1",
			wantErr:   false,
			checkURL:  true,
			expectedURL: "https://us-central1-aiplatform.googleapis.com/v1beta1/projects/test-project/locations/us-central1/" +
				"publishers/google/models/gemini-pro:generateContent",
		},
		{
			name:        "empty project ID",
			projectID:   "",
			location:    "us-central1",
			wantErr:     true,
			errContains: "projectID cannot be empty",
		},
		{
			name:        "empty location",
			projectID:   "test-project",
			location:    "",
			wantErr:     true,
			errContains: "location cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := NewRequest(tt.projectID, tt.location)

			// Check error cases
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			// Verify request is not nil
			if req == nil {
				t.Error("NewRequest() returned nil request with no error")
				return
			}

			// Check headers
			contentType := req.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type header to be 'application/json', got %q", contentType)
			}

			auth := req.Header.Get("Authorization")
			expectedAuth := "Bearer test-token"
			if auth != expectedAuth {
				t.Errorf("Expected Authorization header to be %q, got %q", expectedAuth, auth)
			}

			// Verify request method
			if req.Method != "POST" {
				t.Errorf("Expected request method to be 'POST', got %q", req.Method)
			}

			// Verify URL if needed
			if tt.checkURL {
				if req.URL.String() != tt.expectedURL {
					t.Errorf("Expected URL to be %q, got %q", tt.expectedURL, req.URL.String())
				}
			}
		})
	}
}
