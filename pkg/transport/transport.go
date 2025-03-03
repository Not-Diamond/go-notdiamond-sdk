package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"bytes"

	http_client "github.com/Not-Diamond/go-notdiamond/pkg/http/client"
	"github.com/Not-Diamond/go-notdiamond/pkg/http/request"
	"github.com/Not-Diamond/go-notdiamond/pkg/metric"
	"github.com/Not-Diamond/go-notdiamond/pkg/model"
	"github.com/Not-Diamond/go-notdiamond/pkg/validation"
)

type Transport struct {
	Base           http.RoundTripper
	client         *http_client.Client
	metricsTracker *metric.Tracker
	config         model.Config
}

// NewTransport creates a new Transport with metrics tracking.
func NewTransport(config model.Config) (*Transport, error) {
	slog.Info("🏁 Initializing Transport")
	if err := validation.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize metrics tracker with Redis configuration from config
	var metricsTracker *metric.Tracker
	var err error
	if config.RedisConfig != nil {
		metricsTracker, err = metric.NewTracker(config.RedisConfig.Addr)
	} else {
		// Use default Redis configuration
		metricsTracker, err = metric.NewTracker("localhost:6379")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics tracker: %w", err)
	}

	baseClient := &http.Client{Transport: http.DefaultTransport}

	ndHttpClient := &http_client.NotDiamondHttpClient{
		Client:         baseClient,
		Config:         config,
		MetricsTracker: metricsTracker,
	}

	client := &http_client.Client{
		Clients:        config.Clients,
		Models:         config.Models,
		ModelProviders: buildModelProviders(config.Models),
		IsOrdered:      isOrderedModels(config.Models),
		HttpClient:     ndHttpClient,
	}

	return &Transport{
		Base:           http.DefaultTransport,
		client:         client,
		metricsTracker: metricsTracker,
		config:         config,
	}, nil
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Extract the original messages and model
	messages := request.ExtractMessagesFromRequest(req)
	extractedModel, err := request.ExtractModelFromRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to extract model: %w", err)
	}
	extractedProvider := request.ExtractProviderFromRequest(req)
	currentModel := extractedProvider + "/" + extractedModel

	// Combine with model messages if they exist
	if modelMessages, exists := t.config.ModelMessages[currentModel]; exists {
		if err := updateRequestWithCombinedMessages(req, modelMessages, messages, extractedModel); err != nil {
			return nil, err
		}
	}

	// Add client to context and proceed with request
	ctx := context.WithValue(req.Context(), http_client.ClientKey, t.client)
	req = req.WithContext(ctx)

	return t.client.HttpClient.Do(req)
}

// updateRequestWithCombinedMessages updates the request with combined messages.
func updateRequestWithCombinedMessages(req *http.Request, modelMessages []model.Message, messages []model.Message, extractedModel string) error {
	combinedMessages, err := http_client.CombineMessages(modelMessages, messages)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"model":    extractedModel,
		"messages": combinedMessages,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req.Body = io.NopCloser(bytes.NewBuffer(jsonData))
	req.ContentLength = int64(len(jsonData))
	return nil
}

func buildModelProviders(models model.Models) map[string]map[string]bool {
	modelProviders := make(map[string]map[string]bool)

	switch m := models.(type) {
	case model.WeightedModels:
		for modelFull := range m {
			parts := strings.Split(modelFull, "/")
			provider, model := parts[0], parts[1]
			if modelProviders[model] == nil {
				modelProviders[model] = make(map[string]bool)
			}
			modelProviders[model][provider] = true
		}
	case model.OrderedModels:
		for _, modelFull := range m {
			parts := strings.Split(modelFull, "/")
			provider, model := parts[0], parts[1]
			if modelProviders[model] == nil {
				modelProviders[model] = make(map[string]bool)
			}
			modelProviders[model][provider] = true
		}
	}
	return modelProviders
}

func isOrderedModels(models model.Models) bool {
	_, ok := models.(model.OrderedModels)
	return ok
}

func ExtractModelFromRequest(req *http.Request) (string, error) {
	if req == nil {
		return "", fmt.Errorf("request is nil")
	}

	if req.Body == nil {
		return "", fmt.Errorf("request body is nil")
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read body: %w", err)
	}

	// Always restore the body for future reads
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	// Handle empty body
	if len(body) == 0 {
		return "", fmt.Errorf("empty request body")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("failed to unmarshal body: %w", err)
	}

	modelStr, ok := payload["model"].(string)
	if !ok {
		return "", fmt.Errorf("model field not found or not a string")
	}

	// If it's in provider/model format, extract just the model part
	parts := strings.Split(modelStr, "/")
	if len(parts) == 2 {
		return parts[1], nil // Return just the model part
	}
	return modelStr, nil
}
