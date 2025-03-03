package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Not-Diamond/go-notdiamond/pkg/model"
)

// ExtractModelFromRequest extracts the model from the request body.
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

	// Check if the model string contains a region (format: model/region)
	parts := strings.Split(modelStr, "/")

	// If it's in provider/model format, extract just the model part
	if len(parts) == 2 && (parts[0] == "openai" || parts[0] == "azure" || parts[0] == "vertex") {
		return parts[1], nil // Return just the model part
	}

	// If it's in model/region format, keep it as is to preserve the region information
	if len(parts) == 2 && parts[0] != "openai" && parts[0] != "azure" && parts[0] != "vertex" {
		return modelStr, nil // Return model/region
	}

	// If it's in provider/model/region format, extract model/region
	if len(parts) == 3 {
		return parts[1] + "/" + parts[2], nil // Return model/region
	}

	return modelStr, nil
}

// ExtractProviderFromRequest extracts the provider from the request URL or model name.
func ExtractProviderFromRequest(req *http.Request) string {
	// First try to extract from URL
	url := req.URL.String()
	if strings.Contains(url, "azure") {
		return "azure"
	} else if strings.Contains(url, "openai.com") {
		return "openai"
	} else if strings.Contains(url, "aiplatform.googleapis.com") ||
		strings.Contains(url, "-aiplatform.googleapis.com") ||
		strings.Contains(url, "aiplatform.googleapiss.com") ||
		strings.Contains(url, "-aiplatform.googleapiss.com") {
		return "vertex"
	}

	// If not found in URL, try to extract from model name in the request body
	if req.Body == nil {
		return ""
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		slog.Error("❌ Error reading request body", "error", err)
		return ""
	}

	// Restore the body for future reads
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("❌ Error unmarshaling request body", "error", err)
		return ""
	}

	modelStr, ok := payload["model"].(string)
	if !ok {
		slog.Error("❌ Model field not found or not a string")
		return ""
	}

	// Check if the model string contains a provider
	parts := strings.Split(modelStr, "/")

	// If it's in provider/model format or provider/model/region format
	if len(parts) >= 2 {
		provider := parts[0]
		if provider == "vertex" || provider == "azure" || provider == "openai" {
			return provider
		}
	}

	return ""
}

// ExtractMessagesFromRequest extracts the messages from the request body.
func ExtractMessagesFromRequest(req *http.Request) []model.Message {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		slog.Error("❌ Failed to read body in ExtractMessagesFromRequest", "error", err)
		return nil
	}

	req.Body = io.NopCloser(bytes.NewBuffer(body))

	provider := ExtractProviderFromRequest(req)
	switch provider {
	case "vertex":
		return extractVertexMessages(body)
	default:
		return extractOpenAIMessages(body)
	}
}

// extractOpenAIMessages extracts messages from OpenAI/Azure format
func extractOpenAIMessages(body []byte) []model.Message {
	var payload struct {
		Messages []model.Message `json:"messages"`
	}
	err := json.Unmarshal(body, &payload)
	if err != nil {
		return nil
	}
	return payload.Messages
}

// extractVertexMessages extracts messages from Vertex AI format
func extractVertexMessages(body []byte) []model.Message {
	var payload struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
	}
	err := json.Unmarshal(body, &payload)
	if err != nil {
		return nil
	}

	// Check if contents field exists in the JSON
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(body, &rawPayload); err != nil {
		return nil
	}
	if _, exists := rawPayload["contents"]; !exists {
		return nil
	}

	messages := make([]model.Message, 0)
	for _, content := range payload.Contents {
		if len(content.Parts) > 0 {
			messages = append(messages, model.Message{
				"role":    content.Role,
				"content": content.Parts[0].Text,
			})
		}
	}
	return messages
}

// TransformToVertexRequest transforms OpenAI format to Vertex AI format
func TransformToVertexRequest(body []byte, model string) ([]byte, error) {
	var openAIPayload struct {
		Messages    []map[string]string    `json:"messages"`
		Temperature float64                `json:"temperature"`
		MaxTokens   int                    `json:"max_tokens"`
		TopP        float64                `json:"top_p"`
		TopK        int                    `json:"top_k"`
		Stream      bool                   `json:"stream"`
		Stop        []string               `json:"stop"`
		Extra       map[string]interface{} `json:"extra,omitempty"`
	}

	if err := json.Unmarshal(body, &openAIPayload); err != nil {
		slog.Error("❌ Failed to unmarshal OpenAI payload",
			"error", err,
			"body", string(body))
		return nil, fmt.Errorf("failed to unmarshal OpenAI payload: %v, body: %s", err, string(body))
	}

	// Convert OpenAI messages to Vertex AI format
	var contents []map[string]interface{}
	for _, msg := range openAIPayload.Messages {
		role := msg["role"]
		content := msg["content"]

		if role == "assistant" {
			role = "model" // Vertex AI uses "model" instead of "assistant"
		} else if role == "system" {
			role = "user" // Vertex AI doesn't support system role, treat as user
		}

		contents = append(contents, map[string]interface{}{
			"role": role,
			"parts": []map[string]interface{}{
				{
					"text": content,
				},
			},
		})
	}

	// Build the request payload according to Vertex AI's format
	type VertexPayload struct {
		Model            string                   `json:"model"`
		Contents         []map[string]interface{} `json:"contents"`
		GenerationConfig map[string]interface{}   `json:"generationConfig"`
		StopSequences    []string                 `json:"stopSequences,omitempty"`
		Extra            map[string]interface{}   `json:"extra,omitempty"`
	}

	// Extract just the model name if it contains a provider prefix or region
	modelName := model
	if strings.Contains(model, "/") {
		parts := strings.Split(model, "/")
		if len(parts) >= 2 {
			// For format: provider/model or model/region
			modelName = parts[1]

			// If it's provider/model/region format, we just want the model part
			if len(parts) > 2 && parts[0] == "vertex" {
				modelName = parts[1]
			}
		}
	}

	// Default to gemini-pro if no model is specified
	if modelName == "" {
		modelName = "gemini-pro"
		slog.Info("⚠️ No model specified, defaulting to gemini-pro")
	}

	slog.Info("🔄 Transforming to Vertex format", "model", modelName)

	vertexPayload := VertexPayload{
		Model:    modelName,
		Contents: contents,
		GenerationConfig: map[string]interface{}{
			"temperature":     openAIPayload.Temperature,
			"maxOutputTokens": openAIPayload.MaxTokens,
			"topP":            openAIPayload.TopP,
			"topK":            openAIPayload.TopK,
		},
		Extra: openAIPayload.Extra,
	}

	// Set defaults if values are not provided
	if openAIPayload.Temperature == 0 {
		vertexPayload.GenerationConfig["temperature"] = 0.7
	}
	if openAIPayload.MaxTokens == 0 {
		vertexPayload.GenerationConfig["maxOutputTokens"] = 1024
	}
	if openAIPayload.TopP == 0 {
		vertexPayload.GenerationConfig["topP"] = 0.95
	}
	if openAIPayload.TopK == 0 {
		vertexPayload.GenerationConfig["topK"] = 40
	}

	if len(openAIPayload.Stop) > 0 {
		vertexPayload.StopSequences = openAIPayload.Stop
	}

	// Initialize Extra if nil
	if vertexPayload.Extra == nil {
		vertexPayload.Extra = make(map[string]interface{})
	}

	// Copy any extra parameters
	for k, v := range openAIPayload.Extra {
		// Skip fields we already handle
		if k == "model" || k == "contents" || k == "generationConfig" || k == "stopSequences" {
			continue
		}
		vertexPayload.Extra[k] = v
		slog.Info("➕ Added extra parameter", "key", k, "value", v)
	}

	result, err := json.Marshal(vertexPayload)
	if err != nil {
		slog.Error("❌ Failed to marshal Vertex payload",
			"error", err,
			"payload", vertexPayload)
		return nil, fmt.Errorf("failed to marshal Vertex payload: %v", err)
	}

	return result, nil
}

// TransformFromVertexResponse transforms Vertex AI response to OpenAI format
func TransformFromVertexResponse(body []byte) ([]byte, error) {
	var vertexResponse struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
				Role string `json:"role"`
			} `json:"content"`
			FinishReason  string `json:"finishReason"`
			SafetyRatings []struct {
				Category    string `json:"category"`
				Probability string `json:"probability"`
			} `json:"safetyRatings"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(body, &vertexResponse); err != nil {
		return nil, err
	}

	openAIResponse := map[string]interface{}{
		"choices": make([]map[string]interface{}, 0, len(vertexResponse.Candidates)),
		"usage": map[string]interface{}{
			"prompt_tokens":     vertexResponse.UsageMetadata.PromptTokenCount,
			"completion_tokens": vertexResponse.UsageMetadata.CandidatesTokenCount,
			"total_tokens":      vertexResponse.UsageMetadata.TotalTokenCount,
		},
	}

	for i, candidate := range vertexResponse.Candidates {
		if len(candidate.Content.Parts) > 0 {
			choice := map[string]interface{}{
				"index": i,
				"message": map[string]interface{}{
					"role":    candidate.Content.Role,
					"content": candidate.Content.Parts[0].Text,
				},
				"finish_reason": strings.ToLower(candidate.FinishReason),
			}
			openAIResponse["choices"] = append(openAIResponse["choices"].([]map[string]interface{}), choice)
		}
	}

	return json.Marshal(openAIResponse)
}

// TransformFromVertexToOpenAI transforms Vertex AI format to OpenAI format
func TransformFromVertexToOpenAI(body []byte) ([]byte, error) {
	if len(body) == 0 {
		slog.Error("❌ Empty body received")
		return nil, fmt.Errorf("empty body received")
	}

	var vertexPayload struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		GenerationConfig map[string]interface{} `json:"generationConfig"`
	}

	if err := json.Unmarshal(body, &vertexPayload); err != nil {
		slog.Error("❌ Failed to unmarshal Vertex payload",
			"error", err,
			"body", string(body))
		return nil, fmt.Errorf("failed to unmarshal Vertex payload: %v", err)
	}

	// Convert Vertex messages to OpenAI format
	messages := make([]map[string]string, 0, len(vertexPayload.Contents))
	for _, content := range vertexPayload.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}
		if len(content.Parts) > 0 {
			message := map[string]string{
				"role":    role,
				"content": content.Parts[0].Text,
			}
			messages = append(messages, message)
		}
	}

	// Build OpenAI payload
	openaiPayload := map[string]interface{}{
		"messages": messages,
	}

	// Map generation config to OpenAI parameters
	if vertexPayload.GenerationConfig != nil {
		if temp, ok := vertexPayload.GenerationConfig["temperature"].(float64); ok {
			openaiPayload["temperature"] = temp
		}
		if maxTokens, ok := vertexPayload.GenerationConfig["maxOutputTokens"].(float64); ok {
			openaiPayload["max_tokens"] = int(maxTokens)
		}
		if topP, ok := vertexPayload.GenerationConfig["topP"].(float64); ok {
			openaiPayload["top_p"] = topP
		}
		// Intentionally skip topK as it's not supported by OpenAI/Azure
	}

	result, err := json.Marshal(openaiPayload)
	if err != nil {
		slog.Error("❌ Failed to marshal OpenAI payload",
			"error", err,
			"payload", openaiPayload)
		return nil, fmt.Errorf("failed to marshal OpenAI payload: %v", err)
	}

	return result, nil
}
