package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Not-Diamond/go-notdiamond/pkg/clients/azure"
	"github.com/Not-Diamond/go-notdiamond/pkg/clients/openai"
	"github.com/Not-Diamond/go-notdiamond/pkg/clients/vertex"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/Not-Diamond/go-notdiamond/pkg/http/response"
	"github.com/Not-Diamond/go-notdiamond/pkg/transport"

	"example/config"
)

func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Create API requests
	openaiRequest, err := openai.NewRequest("https://api.openai.com/v1/chat/completions", cfg.OpenAIAPIKey)
	if err != nil {
		log.Fatalf("Failed to create openai request: %v", err)
	}
	azureRequest, err := azure.NewRequest(cfg.AzureEndpoint, cfg.AzureAPIKey)
	if err != nil {
		log.Fatalf("Failed to create azure request: %v", err)
	}
	vertexRequest, err := vertex.NewRequest(cfg.VertexProjectID, cfg.VertexLocation)
	if err != nil {
		log.Fatalf("Failed to create vertex request: %v", err)
	}

	// Get model configuration
	modelConfig := config.GetModelConfig()
	modelConfig.Clients = []http.Request{
		*openaiRequest,
		*azureRequest,
		*vertexRequest,
	}
	modelConfig.RedisConfig = &cfg.RedisConfig

	// Set up Azure regions
	modelConfig.AzureRegions = map[string]string{
		"eastus":     cfg.AzureEndpoint,
		"westeurope": "https://custom-westeurope.openai.azure.com",
	}

	// Create transport with configuration
	transport, err := transport.NewTransport(modelConfig)
	if err != nil {
		log.Fatalf("Failed to create transport: %v", err)
	}

	// Create HTTP client with our transport
	client := &http.Client{
		Transport: transport,
	}

	// Prepare request payload
	useVertex := true // Toggle this to switch between Vertex AI and OpenAI

	var jsonData []byte
	var req *http.Request

	if useVertex {
		vertexPayload := map[string]interface{}{
			"model": "gemini-pro",
			"contents": []map[string]interface{}{
				{
					"role": "user",
					"parts": []map[string]interface{}{
						{
							"text": "Hello, how are you??",
						},
					},
				},
			},
			"generationConfig": map[string]interface{}{
				"temperature":     0.7,
				"maxOutputTokens": 1024,
				"topP":            0.95,
				"topK":            40,
			},
		}
		jsonData, err = json.Marshal(vertexPayload)
		if err != nil {
			log.Fatalf("Failed to marshal payload: %v", err)
		}
		req, err = http.NewRequest("POST", vertexRequest.URL.String(), io.NopCloser(bytes.NewBuffer(jsonData)))
		if err != nil {
			log.Fatalf("Failed to create request: %v", err)
		}
		// Copy headers from the vertex request
		for key, values := range vertexRequest.Header {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	} else {
		openaiPayload := map[string]interface{}{
			"model": "gpt-4o-mini", // Non-existent model to trigger fallback
			"messages": []map[string]interface{}{
				{
					"role":    "user",
					"content": "Hello, how are you??",
				},
			},
		}
		jsonData, err = json.Marshal(openaiPayload)
		if err != nil {
			log.Fatalf("Failed to marshal payload: %v", err)
		}
		req, err = http.NewRequest("POST", azureRequest.URL.String(), io.NopCloser(bytes.NewBuffer(jsonData)))
		if err != nil {
			log.Fatalf("Failed to create request: %v", err)
		}
		// Copy headers from the openai request
		for key, values := range azureRequest.Header {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}

	// Make request with transport client
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	result, err := response.Parse(body, start)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("🤖 Model: %s\n", result.Model)
	fmt.Printf("⏱️  Time: %.2fs\n", result.TimeTaken.Seconds())
	fmt.Printf("💬 Response: %s\n", result.Response)
}
