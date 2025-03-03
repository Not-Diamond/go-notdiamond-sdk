package vertex

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2/google"
)

// For testing purposes, we make this function variable
var findDefaultCredentials = google.FindDefaultCredentials

// NewRequest creates a new request for the Vertex AI API.
func NewRequest(projectID string, location string) (*http.Request, error) {
	if projectID == "" {
		return nil, errors.New("projectID cannot be empty")
	}
	if location == "" {
		return nil, errors.New("location cannot be empty")
	}

	// Construct the full endpoint URL
	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/publishers/google/models/gemini-pro:generateContent",
		location, projectID, location)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}

	// Set default headers
	req.Header.Set("Content-Type", "application/json")

	// Get credentials from the environment
	ctx := context.Background()
	credentials, err := findDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("error getting credentials: %w", err)
	}

	// Get an OAuth2 token
	token, err := credentials.TokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("error getting token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	return req, nil
}
