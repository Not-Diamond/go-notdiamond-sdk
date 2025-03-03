package http_client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Not-Diamond/go-notdiamond/pkg/http/request"
	"github.com/Not-Diamond/go-notdiamond/pkg/metric"
	"github.com/Not-Diamond/go-notdiamond/pkg/model"
	"github.com/Not-Diamond/go-notdiamond/pkg/validation"
	"golang.org/x/oauth2/google"
)

type Client struct {
	Clients        []http.Request
	Models         model.Models
	ModelProviders map[string]map[string]bool
	IsOrdered      bool
	HttpClient     *NotDiamondHttpClient
}

type contextKey string

const ClientKey contextKey = "notdiamondClient"

// NotDiamondHttpClient is a type that can be used to represent a NotDiamond HTTP client.
type NotDiamondHttpClient struct {
	*http.Client
	Config         model.Config
	MetricsTracker *metric.Tracker
}

// NewNotDiamondHttpClient creates a new NotDiamond HTTP client.
func NewNotDiamondHttpClient(config model.Config) (*NotDiamondHttpClient, error) {
	var metricsTracker *metric.Tracker
	var err error

	if config.RedisConfig != nil {
		metricsTracker, err = metric.NewTracker(config.RedisConfig.Addr)
	} else {
		metricsTracker, err = metric.NewTracker("localhost:6379")
	}
	if err != nil {
		slog.Error("failed to create metrics tracker", "error", err)
		return nil, err
	}

	return &NotDiamondHttpClient{
		Client:         &http.Client{},
		Config:         config,
		MetricsTracker: metricsTracker,
	}, nil
}

// Do executes a request.
func (c *NotDiamondHttpClient) Do(req *http.Request) (*http.Response, error) {
	slog.Info("🔍 Executing request", "url", req.URL.String())

	// Read and log the initial request body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	// Create a new reader for each operation that needs the body
	req.Body = io.NopCloser(bytes.NewBuffer(body))
	messages := request.ExtractMessagesFromRequest(req.Clone(req.Context()))

	req.Body = io.NopCloser(bytes.NewBuffer(body))
	extractedModel, err := request.ExtractModelFromRequest(req.Clone(req.Context()))
	if err != nil {
		return nil, fmt.Errorf("failed to extract model: %w", err)
	}

	req.Body = io.NopCloser(bytes.NewBuffer(body))
	extractedProvider := request.ExtractProviderFromRequest(req.Clone(req.Context()))

	// Parse model and region if present
	modelParts := strings.Split(extractedModel, "/")
	baseModel := modelParts[0]
	region := ""
	if len(modelParts) > 1 {
		region = modelParts[1]
	}

	// Construct the current model string
	var currentModel string
	if region != "" {
		currentModel = extractedProvider + "/" + baseModel + "/" + region
		slog.Info("🔍 Using model with region", "model", currentModel)
	} else {
		currentModel = extractedProvider + "/" + baseModel
		slog.Info("🔍 Using model without region", "model", currentModel)
	}

	// Restore the original body for future operations
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	var lastErr error
	originalCtx := req.Context()

	if client, ok := originalCtx.Value(ClientKey).(*Client); ok {
		var modelsToTry []string

		// Read and preserve the original request body
		originalBody, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read original request body: %v", err)
		}
		// Restore the body for future reads
		req.Body = io.NopCloser(bytes.NewBuffer(originalBody))

		if client.IsOrdered {
			modelsToTry = client.Models.(model.OrderedModels)
			// Validate that requested model is in the configured list
			modelExists := false

			// Check if the model (without region) exists in the configured list
			baseCurrentModel := extractedProvider + "/" + baseModel
			for _, m := range modelsToTry {
				// Strip region from configured model if present
				configModelParts := strings.Split(m, "/")
				configBaseModel := configModelParts[0] + "/" + configModelParts[1]

				if configBaseModel == baseCurrentModel {
					modelExists = true
					break
				}

				// Also check if the exact model with region matches
				if m == currentModel {
					modelExists = true
					break
				}
			}

			if !modelExists {
				return nil, fmt.Errorf("requested model %s is not in the configured model list", baseCurrentModel)
			}
		} else {
			modelsToTry = getWeightedModelsList(client.Models.(model.WeightedModels))
		}

		// If region is specified, try that specific region first
		if region != "" {
			// Move the requested model to the front of the slice
			for i, m := range modelsToTry {
				if m == currentModel {
					// Remove it from its current position and insert at front
					modelsToTry = append(modelsToTry[:i], modelsToTry[i+1:]...)
					modelsToTry = append([]string{currentModel}, modelsToTry...)
					break
				}
			}
		} else {
			// If no region specified, use default regions for each provider
			// Create a new slice with region-specific models at the front
			regionSpecificModels := []string{}

			// For the current model, add region-specific versions at the front
			if extractedProvider == "vertex" {
				// Default to us-east1 if no region is specified
				regionSpecificModels = append(regionSpecificModels, extractedProvider+"/"+baseModel+"/us-east1")
			} else if extractedProvider == "azure" {
				// Default to eastus for Azure
				regionSpecificModels = append(regionSpecificModels, extractedProvider+"/"+baseModel+"/eastus")
			}

			// Add the base model without region
			regionSpecificModels = append(regionSpecificModels, extractedProvider+"/"+baseModel)

			// Add models from config
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

			modelsToTry = regionSpecificModels
		}

		slog.Info("🔄 Models to try (in order)", "models", strings.Join(modelsToTry, ", "))

		for _, modelFull := range modelsToTry {
			// Reset the request body for each attempt
			req.Body = io.NopCloser(bytes.NewBuffer(originalBody))

			slog.Info("🔍 Trying model", "model", modelFull)
			if resp, err := c.tryWithRetries(modelFull, req, messages, originalCtx); err == nil {
				return resp, nil
			} else {
				lastErr = err
				slog.Error("❌ Attempt failed", "model", modelFull, "error", err.Error())

				// If this was a region-specific model that failed, try the next one
				// This implements the region fallback mechanism
			}
		}
	}

	return nil, fmt.Errorf("all requests failed: %v", lastErr)
}

// getMaxRetriesForStatus gets the maximum retries for a status code.
func (c *NotDiamondHttpClient) getMaxRetriesForStatus(modelFull string, statusCode int) int {
	// Check model-specific status code retries first
	if modelRetries, ok := c.Config.StatusCodeRetry.(map[string]map[string]int); ok {
		if modelConfig, exists := modelRetries[modelFull]; exists {
			if retries, hasCode := modelConfig[strconv.Itoa(statusCode)]; hasCode {
				return retries
			}
		}
	}

	// Check global status code retries
	if globalRetries, ok := c.Config.StatusCodeRetry.(map[string]int); ok {
		if retries, exists := globalRetries[strconv.Itoa(statusCode)]; exists {
			return retries
		}
	}

	// Fall back to default MaxRetries
	if maxRetries, exists := c.Config.MaxRetries[modelFull]; exists {
		return maxRetries
	}
	return 1
}

// tryWithRetries tries a request with retries.
func (c *NotDiamondHttpClient) tryWithRetries(modelFull string, req *http.Request, messages []model.Message, originalCtx context.Context) (*http.Response, error) {
	var lastErr error
	var lastStatusCode int

	// Check model health (both latency and error rate) before starting attempts
	slog.Info("🏥 Checking initial model health", "model", modelFull)
	healthy, healthErr := c.MetricsTracker.CheckModelOverallHealth(modelFull, c.Config)
	if healthErr != nil {
		lastErr = healthErr
		slog.Error("❌ Initial health check failed", "model", modelFull, "error", healthErr.Error())
		return nil, fmt.Errorf("model health check failed: %w", healthErr)
	}
	if !healthy {
		lastErr = fmt.Errorf("model %s is unhealthy", modelFull)
		slog.Info("⚠️ Model is already unhealthy, skipping", "model", modelFull)
		return nil, lastErr
	}
	slog.Info("✅ Initial health check passed", "model", modelFull)

	for attempt := 0; ; attempt++ {
		maxRetries := c.getMaxRetriesForStatus(modelFull, lastStatusCode)
		if attempt >= maxRetries {
			break
		}

		slog.Info(fmt.Sprintf("🔄 Request %d of %d for model %s", attempt+1, maxRetries, modelFull))

		timeout := 100.0
		if t, ok := c.Config.Timeout[modelFull]; ok && t > 0 {
			timeout = t
		}

		ctx, cancel := context.WithTimeout(originalCtx, time.Duration(timeout*float64(time.Second)))
		defer cancel()

		startTime := time.Now()
		var resp *http.Response
		var reqErr error

		if attempt == 0 {
			// We only need the provider from the request
			extractedProvider := request.ExtractProviderFromRequest(req)

			// Extract parts from modelFull (provider/model/region)
			modelFullParts := strings.Split(modelFull, "/")
			modelFullProvider := modelFullParts[0]
			modelFullBase := ""
			if len(modelFullParts) > 1 {
				modelFullBase = modelFullParts[1]
			}

			// Read and preserve the original request body
			originalBody, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			req.Body = io.NopCloser(bytes.NewBuffer(originalBody))

			// Log the original request URL before any modifications
			slog.Info("🔄 Original request URL", "url", req.URL.String())

			// Check if we're switching providers
			if modelFullProvider != extractedProvider {
				slog.Info("🔄 Switching provider", "from", extractedProvider, "to", modelFullProvider)

				// Get client from context
				if client, ok := originalCtx.Value(ClientKey).(*Client); ok {
					// Find the appropriate client request for the new provider
					var foundClientReq *http.Request
					for _, clientReq := range client.Clients {
						url := clientReq.URL.String()
						switch modelFullProvider {
						case "vertex":
							if strings.Contains(url, "aiplatform.googleapis.com") {
								foundClientReq = &clientReq
								break
							}
						case "azure":
							if strings.Contains(url, "azure") {
								foundClientReq = &clientReq
								break
							}
						case "openai":
							if strings.Contains(url, "openai.com") {
								foundClientReq = &clientReq
								break
							}
						}
					}

					if foundClientReq != nil {
						// Create a new request with the appropriate URL and headers
						newReq := foundClientReq.Clone(ctx)

						// Transform the request body for the new provider
						transformedBody, err := transformRequestForProvider(originalBody, modelFullProvider, modelFullBase, client)
						if err != nil {
							return nil, fmt.Errorf("failed to transform request body: %w", err)
						}

						newReq.Body = io.NopCloser(bytes.NewBuffer(transformedBody))
						newReq.ContentLength = int64(len(transformedBody))

						// Update the URL to include the region if present
						if len(modelFullParts) > 2 && modelFullParts[2] != "" {
							if err := updateRequestURL(newReq, modelFullProvider, modelFullBase+"/"+modelFullParts[2], client); err != nil {
								return nil, fmt.Errorf("failed to update URL with region: %w", err)
							}
						} else {
							if err := updateRequestURL(newReq, modelFullProvider, modelFullBase, client); err != nil {
								return nil, fmt.Errorf("failed to update URL: %w", err)
							}
						}

						// Update authentication
						if err := updateRequestAuth(newReq, modelFullProvider, ctx); err != nil {
							return nil, fmt.Errorf("failed to update authentication: %w", err)
						}

						// Log the updated request URL
						slog.Info("🔄 Modified request URL for provider switch", "url", newReq.URL.String())

						// Use a client with the same transport as the original client
						rawClient := &http.Client{
							Transport: c.Client.Transport,
							Timeout:   c.Client.Timeout,
						}
						resp, reqErr = rawClient.Do(newReq)
					} else {
						reqErr = fmt.Errorf("no client found for provider %s", modelFullProvider)
					}
				}
			} else {
				// Same provider, just update the URL with region if needed
				// Update the URL to include the region if present in modelFull
				if len(modelFullParts) > 2 && modelFullParts[2] != "" {
					// Get client from context
					if client, ok := originalCtx.Value(ClientKey).(*Client); ok {
						// Update the request URL with the region
						if err := updateRequestURL(req, modelFullProvider, modelFullBase+"/"+modelFullParts[2], client); err != nil {
							return nil, fmt.Errorf("failed to update URL with region: %w", err)
						}
					}
				}

				// Log the updated request URL after modifications
				slog.Info("🔄 Modified request URL", "url", req.URL.String())

				// Update authentication for the request
				if modelFullProvider == "vertex" {
					if err := updateRequestAuth(req, modelFullProvider, ctx); err != nil {
						return nil, fmt.Errorf("failed to update authentication: %w", err)
					}
					slog.Info("🔑 Updated authentication for Vertex AI")
				}

				// Use a client with the same transport as the original client
				rawClient := &http.Client{
					Transport: c.Client.Transport,
					Timeout:   c.Client.Timeout,
				}
				resp, reqErr = rawClient.Do(req)
			}
		} else {
			if client, ok := originalCtx.Value(ClientKey).(*Client); ok {
				resp, reqErr = tryNextModel(client, modelFull, messages, ctx, req)
			}
		}

		elapsed := time.Since(startTime).Seconds()

		if reqErr != nil {
			cancel()
			lastErr = reqErr
			slog.Error("❌ Request", "failed", lastErr)
			// Record the latency in Redis
			recErr := c.MetricsTracker.RecordLatency(modelFull, elapsed, "failed")
			if recErr != nil {
				slog.Error("error", "recording latency", recErr)
			}
			if attempt < maxRetries-1 && c.Config.Backoff[modelFull] > 0 {
				time.Sleep(time.Duration(c.Config.Backoff[modelFull]) * time.Second)
			}
			continue
		}

		if resp != nil {
			body, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if closeErr != nil {
				return nil, closeErr
			}
			cancel()

			if readErr != nil {
				lastErr = readErr
				continue
			}

			lastStatusCode = resp.StatusCode
			if err := c.MetricsTracker.RecordErrorCode(modelFull, resp.StatusCode); err != nil {
				slog.Error("Failed to record error code", "error", err)
			}

			// Check model health after recording the error code
			slog.Info("🏥 Checking model health after error", "model", modelFull, "status_code", resp.StatusCode)
			healthy, healthErr := c.MetricsTracker.CheckModelOverallHealth(modelFull, c.Config)
			if healthErr != nil {
				slog.Error("❌ Health check failed after error", "model", modelFull, "error", healthErr.Error())
				return nil, fmt.Errorf("model %s health check failed after error: %w", modelFull, healthErr)
			}
			if !healthy {
				slog.Info("⚠️ Model became unhealthy after error", "model", modelFull, "status_code", resp.StatusCode)
				return nil, fmt.Errorf("model %s became unhealthy after error", modelFull)
			}
			slog.Info("✅ Health check passed after error", "model", modelFull)

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				// Record the latency in Redis
				recErr := c.MetricsTracker.RecordLatency(modelFull, elapsed, "success")
				if recErr != nil {
					slog.Error("recording latency", "error", recErr)
				}

				return &http.Response{
					Status:     resp.Status,
					StatusCode: resp.StatusCode,
					Header:     resp.Header,
					Body:       io.NopCloser(bytes.NewBuffer(body)),
				}, nil
			}

			// Special handling for 404 errors with Vertex AI, which might indicate a non-existent region
			if resp.StatusCode == 404 && strings.HasPrefix(modelFull, "vertex/") {
				modelParts := strings.Split(modelFull, "/")
				if len(modelParts) > 2 {
					region := modelParts[2]
					slog.Error("❌ Region not found", "model", modelFull, "region", region, "status_code", resp.StatusCode)
					lastErr = fmt.Errorf("region %s not found for model %s: %d %s",
						region, modelParts[1], resp.StatusCode, http.StatusText(resp.StatusCode))
				} else {
					slog.Error("❌ Model or project not found", "model", modelFull, "status_code", resp.StatusCode)
					lastErr = fmt.Errorf("model or project not found: %d %s",
						resp.StatusCode, http.StatusText(resp.StatusCode))
				}
			} else {
				// Parse error response body if possible
				var errorResponse struct {
					Error struct {
						Message string `json:"message"`
						Type    string `json:"type"`
					} `json:"error"`
				}
				if unmarshalErr := json.Unmarshal(body, &errorResponse); unmarshalErr == nil && errorResponse.Error.Message != "" {
					lastErr = fmt.Errorf("with status %d (%s): %s",
						resp.StatusCode,
						http.StatusText(resp.StatusCode),
						errorResponse.Error.Message)
				} else {
					// Fallback to raw body if can't parse error response
					lastErr = fmt.Errorf("with status %d (%s): %s",
						resp.StatusCode,
						http.StatusText(resp.StatusCode),
						string(body))
				}
			}
			slog.Error("❌ Request", "failed", lastErr)
		}

		if attempt < maxRetries-1 && c.Config.Backoff[modelFull] > 0 {
			time.Sleep(time.Duration(c.Config.Backoff[modelFull]) * time.Second)
		}
	}

	return nil, lastErr
}

// getWeightedModelsList gets a list of models with their cumulative weights.
func getWeightedModelsList(weights model.WeightedModels) []string {
	// Create a slice to store models with their cumulative weights
	type weightedModel struct {
		model            string
		cumulativeWeight float64
	}

	models := make([]weightedModel, 0, len(weights))
	var cumulative float64

	// Calculate cumulative weights
	for model, weight := range weights {
		cumulative += weight
		models = append(models, weightedModel{
			model:            model,
			cumulativeWeight: cumulative,
		})
	}

	// Create result slice with the same models but ordered by weighted random selection
	result := make([]string, len(weights))
	remaining := make([]weightedModel, len(models))
	copy(remaining, models)

	for i := 0; i < len(weights); i++ {
		// Generate random number between 0 and remaining total weight
		r := rand.Float64() * remaining[len(remaining)-1].cumulativeWeight

		// Find the model whose cumulative weight range contains r
		selectedIdx := 0
		for j, m := range remaining {
			if r <= m.cumulativeWeight {
				selectedIdx = j
				break
			}
		}

		// Add selected model to result
		result[i] = remaining[selectedIdx].model

		// Remove selected model and recalculate cumulative weights
		remaining = append(remaining[:selectedIdx], remaining[selectedIdx+1:]...)
		cumulative = 0
		for j := range remaining {
			if j == 0 {
				cumulative = weights[remaining[j].model]
			} else {
				cumulative += weights[remaining[j].model]
			}
			remaining[j].cumulativeWeight = cumulative
		}
	}

	return result
}

// combineMessages combines model messages and user messages.
func CombineMessages(modelMessages []model.Message, userMessages []model.Message) ([]model.Message, error) {
	combinedMessages := make([]model.Message, 0)

	// Find system message from modelMessages if any exists
	var systemMessage model.Message
	for _, msg := range modelMessages {
		if msg["role"] == "system" {
			systemMessage = msg
			break
		}
	}

	// Add the system message if found
	if systemMessage != nil {
		combinedMessages = append(combinedMessages, systemMessage)
	}

	// Add non-system messages from modelMessages
	for _, msg := range modelMessages {
		if msg["role"] != "system" {
			combinedMessages = append(combinedMessages, msg)
		}
	}

	// Add all non-system messages from userMessages
	for _, msg := range userMessages {
		if msg["role"] != "system" {
			combinedMessages = append(combinedMessages, msg)
		}
	}

	if err := validation.ValidateMessageSequence(combinedMessages); err != nil {
		slog.Error("invalid message sequence", "error", err)
		return nil, err
	}

	return combinedMessages, nil
}

func transformRequestForProvider(originalBody []byte, nextProvider, nextModel string, client *Client) ([]byte, error) {
	var jsonData []byte
	var err error

	switch nextProvider {
	case "azure", "openai":
		jsonData, err = transformToOpenAIFormat(originalBody, nextProvider, nextModel)
	case "vertex":
		jsonData, err = transformToVertexFormat(originalBody, nextModel, client)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", nextProvider)
	}

	return jsonData, err
}

// transformToOpenAIFormat transforms the request body to OpenAI/Azure format
func transformToOpenAIFormat(originalBody []byte, provider, modelName string) ([]byte, error) {
	var payload map[string]interface{}

	// Determine if the original payload is from Vertex AI by examining its structure
	var vertexCheck struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
	}

	isVertex := false
	if err := json.Unmarshal(originalBody, &vertexCheck); err == nil {
		if len(vertexCheck.Contents) > 0 {
			isVertex = true
		}
	}

	// If coming from Vertex, transform to OpenAI format
	if isVertex {
		transformed, err := request.TransformFromVertexToOpenAI(originalBody)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(transformed, &payload); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(originalBody, &payload); err != nil {
			return nil, err
		}
	}

	// Update model name based on provider
	if provider == "openai" {
		payload["model"] = modelName
	} else if provider == "azure" {
		delete(payload, "model")
	}

	return json.Marshal(payload)
}

// transformToVertexFormat transforms the request body to Vertex format
func transformToVertexFormat(originalBody []byte, modelName string, client *Client) ([]byte, error) {
	return request.TransformToVertexRequest(originalBody, modelName)
}

// updateRequestURL updates the request URL based on the provider and model
func updateRequestURL(req *http.Request, provider, modelName string, client *Client) error {
	// Extract region if present in modelName (format: modelName/region)
	modelParts := strings.Split(modelName, "/")
	actualModelName := modelParts[0]
	region := ""
	if len(modelParts) > 1 {
		region = modelParts[1]
	}

	slog.Info("🔄 Updating request URL", "provider", provider, "model", actualModelName, "region", region, "original_url", req.URL.String())

	switch provider {
	case "azure":
		req.URL.Path = fmt.Sprintf("/openai/deployments/%s/chat/completions", actualModelName)
		// Use API version from config or fall back to default
		apiVersion := client.HttpClient.Config.AzureAPIVersion
		if apiVersion == "" {
			apiVersion = "2023-05-15"
		}
		req.URL.RawQuery = fmt.Sprintf("api-version=%s", apiVersion)

		// Apply region if specified and exists in AzureRegions map
		if region != "" && client.HttpClient.Config.AzureRegions != nil {
			if endpoint, ok := client.HttpClient.Config.AzureRegions[region]; ok {
				// Use the endpoint from the AzureRegions map
				req.URL.Host = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
				req.URL.Scheme = "https"
				slog.Info("🔄 Using Azure endpoint from AzureRegions map", "region", region, "endpoint", endpoint)
			} else {
				slog.Warn("🔄 Region not found in AzureRegions map, using default endpoint", "region", region)
			}
		}
	case "vertex":
		projectID := client.HttpClient.Config.VertexProjectID

		// Check if project ID is valid
		if projectID == "" {
			return fmt.Errorf("vertex project ID is not set in the configuration")
		}

		// Log the project ID being used
		slog.Info("🔄 Using project ID for Vertex AI", "project_id", projectID)

		// Use specified region or fall back to config location
		location := client.HttpClient.Config.VertexLocation
		if region != "" {
			location = region
		}

		// Log the location being used
		slog.Info("🔄 Using location for Vertex AI", "location", location)

		// Update the host with the region
		req.URL.Host = fmt.Sprintf("%s-aiplatform.googleapis.com", location)
		req.URL.Scheme = "https"

		// Check if the path already contains a location and replace it
		path := req.URL.Path
		if strings.Contains(path, "/locations/") {
			// Extract the existing path components
			pathParts := strings.Split(path, "/")
			for i, part := range pathParts {
				if part == "locations" && i+1 < len(pathParts) {
					// Replace the location in the path
					oldLocation := pathParts[i+1]
					pathParts[i+1] = location
					path = strings.Join(pathParts, "/")
					slog.Info("🔄 Replaced location in path", "old_location", oldLocation, "new_location", location)
					break
				}
			}
			req.URL.Path = path
		} else {
			// If path doesn't already have a location, construct a new path
			newPath := fmt.Sprintf("/v1beta1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
				projectID, location, actualModelName)
			slog.Info("🔄 Constructed new path", "project_id", projectID, "location", location, "model", actualModelName)
			req.URL.Path = newPath
		}

		slog.Info("🔄 Updated Vertex URL", "host", req.URL.Host, "path", req.URL.Path)
	case "openai":
		// No region-specific handling for OpenAI
	}

	slog.Info("🔄 Updated request URL", "new_url", req.URL.String())
	return nil
}

// updateRequestAuth updates the request authentication based on the provider
func updateRequestAuth(req *http.Request, provider string, ctx context.Context) error {
	// Extract API key from either header format
	var apiKey string
	authHeader := req.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		apiKey = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		apiKey = req.Header.Get("api-key")
	}

	switch provider {
	case "openai":
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Del("api-key")
	case "azure":
		req.Header.Set("api-key", apiKey)
		req.Header.Del("Authorization")
	case "vertex":
		credentials, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return fmt.Errorf("error getting credentials: %w", err)
		}
		token, err := credentials.TokenSource.Token()
		if err != nil {
			return fmt.Errorf("error getting token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}
	return nil
}

// tryNextModel tries the next model.
func tryNextModel(client *Client, modelFull string, messages []model.Message, ctx context.Context, originalReq *http.Request) (*http.Response, error) {
	parts := strings.Split(modelFull, "/")

	// Handle model format with region: provider/model/region
	nextProvider := parts[0]
	nextModel := parts[1]
	nextRegion := ""

	// If we have a region specified
	if len(parts) > 2 {
		nextRegion = parts[2]
		nextModel = nextModel + "/" + nextRegion
		slog.Info("🔄 Trying model with region", "provider", nextProvider, "model", nextModel, "region", nextRegion)
	} else {
		slog.Info("🔄 Trying model without region", "provider", nextProvider, "model", nextModel)
	}

	var nextReq *http.Request

	// Find matching client request
	for _, clientReq := range client.Clients {
		url := clientReq.URL.String()
		switch nextProvider {
		case "vertex":
			if strings.Contains(url, "aiplatform.googleapis.com") {
				nextReq = clientReq.Clone(ctx)
				slog.Info("🔄 Fallback to", "model:", modelFull, "| URL:", nextReq.URL.String())
				goto found
			}
		case "azure":
			if strings.Contains(url, "azure") {
				nextReq = clientReq.Clone(ctx)
				slog.Info("🔄 Fallback to", "model:", modelFull, "| URL:", nextReq.URL.String())
				goto found
			}
		case "openai":
			if strings.Contains(url, "openai.com") {
				nextReq = clientReq.Clone(ctx)
				slog.Info("🔄 Fallback to", "model:", modelFull, "| URL:", nextReq.URL.String())
				goto found
			}
		}
	}
found:

	if nextReq == nil {
		slog.Info("❌ No matching client found", "provider", nextProvider)
		return nil, fmt.Errorf("no client found for provider %s", nextProvider)
	}

	// Read and preserve the original request body
	originalBody, err := io.ReadAll(originalReq.Body)
	if err != nil {
		return nil, err
	}
	originalReq.Body = io.NopCloser(bytes.NewBuffer(originalBody))

	// Transform request body for the target provider
	jsonData, err := transformRequestForProvider(originalBody, nextProvider, nextModel, client)
	if err != nil {
		return nil, fmt.Errorf("failed to transform request: %w", err)
	}

	// Create a new request with the transformed body
	// This ensures we're using the correct URL and headers from the target provider's client
	newReq := nextReq.Clone(ctx)
	newReq.Body = io.NopCloser(bytes.NewBuffer(jsonData))
	newReq.ContentLength = int64(len(jsonData))

	// Initialize headers if nil
	if newReq.Header == nil {
		newReq.Header = make(http.Header)
	}
	newReq.Header.Set("Content-Type", "application/json")

	// Update request URL
	if err := updateRequestURL(newReq, nextProvider, nextModel, client); err != nil {
		return nil, fmt.Errorf("failed to update URL: %w", err)
	}

	// Update authentication
	if err := updateRequestAuth(newReq, nextProvider, ctx); err != nil {
		return nil, fmt.Errorf("failed to update authentication: %w", err)
	}

	// Use the client's HTTP client to make the request
	return client.HttpClient.Client.Do(newReq)
}
