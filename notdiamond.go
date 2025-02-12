package notdiamond

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Not-Diamond/go-notdiamond/pkg/model"
	"github.com/Not-Diamond/go-notdiamond/pkg/validation"
)

type Client struct {
	clients        []http.Request
	models         model.Models
	modelProviders map[string]map[string]bool
	isOrdered      bool
	HttpClient     *NotDiamondHttpClient
}

type contextKey string

const clientKey contextKey = "notdiamondClient"

func Init(config model.Config) (*Client, error) {
	slog.Info("▷ Initializing Client...")

	if err := validation.ValidateConfig(config); err != nil {
		slog.Error("validation", "error", err.Error())
		return nil, err
	}

	isOrdered := false
	if _, ok := config.Models.(model.OrderedModels); ok {
		isOrdered = true
	}

	// Create modelProviders map
	modelProviders := make(map[string]map[string]bool)

	switch models := config.Models.(type) {
	case model.WeightedModels:
		for modelFull := range models {
			parts := strings.Split(modelFull, "/")
			provider, model := parts[0], parts[1]

			if modelProviders[model] == nil {
				modelProviders[model] = make(map[string]bool)
			}
			modelProviders[model][provider] = true
		}
	case model.OrderedModels:
		for _, modelFull := range models {
			parts := strings.Split(modelFull, "/")
			provider, model := parts[0], parts[1]

			if modelProviders[model] == nil {
				modelProviders[model] = make(map[string]bool)
			}
			modelProviders[model][provider] = true
		}
	}
	ndHttpClient, err := NewNotDiamondHttpClient(config)
	if err != nil {
		return nil, err
	}
	client := &Client{
		clients:        config.Clients,
		models:         config.Models,
		modelProviders: modelProviders,
		isOrdered:      isOrdered,
		HttpClient:     ndHttpClient,
	}

	return client, nil
}

// Do Extend and added context to the request
// So the package can be used without manually passing not diamond client to the ctx
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	ctx := context.WithValue(context.Background(), clientKey, c)

	newReq := req.Clone(ctx)
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewBuffer(body)) // Restore original request body
		newReq.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	return c.HttpClient.Do(newReq)
}
