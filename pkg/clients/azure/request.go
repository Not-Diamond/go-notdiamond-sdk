package azure

import (
	"errors"
	"net/http"
)

// NewRequest creates a new request for the Azure API.
func NewRequest(url string, apiKey string) (*http.Request, error) {
	if url == "" {
		return nil, errors.New("url cannot be empty")
	}

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", apiKey)

	return req, nil
}
