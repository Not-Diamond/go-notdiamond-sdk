# 💎 Not Diamond Go SDK [![Go Report Card](https://goreportcard.com/badge/github.com/Not-Diamond/notdiamond-golang)](https://goreportcard.com/report/github.com/Not-Diamond/notdiamond-golang)

One line statement to improve reliability and uptime of LLM requests. [Documentation](https://docs.notdiamond.ai/docs/fallbacks-and-timeouts/)

> **Note**
> Currently supports **OpenAI** and **Azure** models.

## ✨ Features:

- Fallback to other models if one fails
- Load balance requests between multiple models
- Max retries and timeout for each model
- Exponential backoff strategy
- Retry based on HTTP status codes
- Average rolling latency fallback

## 📦 Installation

```
go get github.com/Not-Diamond/go-notdiamond
```

## 🚀 Basic Usage

Error handling intentionally ommited in the example for simplicity.

```go
// Get keys
openaiApiKey := ''
azureApiKey := ''
azureEndpoint := ''

// Create requests
openaiRequest := NewOpenAIRequest("https://api.openai.com/v1/chat/completions", openaiApiKey)
azureRequest := NewOpenAIRequest(azureEndpoint, azureApiKey)

// Create config
config := notdiamond.Config{
	Clients: []http.Request{ *openaiRequest, *azureRequest },
	Models:  notdiamond.OrderedModels{ "azure/gpt-4o-mini", "openai/gpt-4o-mini" },
	MaxRetries: map[string]int{
		"azure/gpt-4o-mini":  2,
		"openai/gpt-4o-mini": 2,
	},
}

// Initialize client
notdiamondClient := notdiamond.Init(config)

// Prepare Payload
messages := []map[string]string{ {"role": "user", "content": "Hello, how are you?"} }
payload := map[string]interface{}{ "model": "gpt-4o-mini", "messages": messages }
jsonData := json.Marshal(payload)

// Create request
req := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", "Bearer "+openaiApiKey)

// Do request via notdiamondClient, not httpClient
resp := notdiamondClient.Do(req)
defer resp.Body.Close()
body := io.ReadAll(resp.Body)

// Final response
fmt.Println(string(body))
```

## Load Balancing

You can configure load balancing between models using weights:

```go
config := notdiamond.Config{
	// ... other config ...
	Models: notdiamond.WeightedModels{
		"azure/gpt-4": 0.3, // 30% of requests
		"openai/gpt-4": 0.7, // 70% of requests
	},
}
```

## Max Retries

Configure custom max retries for each model:

```go
// Default max retries is 1
config := notdiamond.Config{
	// ... other config ...
	MaxRetries: map[string]int{
		"azure/gpt-4":  3, // 3 retries
		"openai/gpt-4": 2, // 2 retries
	},
}
```

## Timeout

Configure custom timeout (in seconds) for each model:

```go
// Default timeout is 100 seconds
config := notdiamond.Config{
	// ... other config ...
	Timeout: map[string]float64{
		"azure/gpt-4":  10.0, // 10 seconds
		"openai/gpt-4": 5.0,  // 5 seconds
	},
}
```

## Exponential Backoff

Configure custom backoff times (in seconds) for each model:

```go
// Default backoff is 1 second
config := notdiamond.Config{
	// ... other config ...
	Backoff: map[string]float64{
		"azure/gpt-4":  0.5,  // Start with 0.5s, then 1s, 2s, etc.
		"openai/gpt-4": 1.0,  // Start with 1s, then 2s, 4s, etc.
	},
}
```

## Model-Specific Messages

You can configure system messages that will be prepended to user messages for specific models:

```go
config := notdiamond.Config{
	// ... other config ...
	ModelMessages: map[string][]map[string]string{
		"azure/gpt-4": {
			{"role": "system", "content": "You are a helpful assistant."},
		},
		"openai/gpt-4": {
			{"role": "system", "content": "Respond concisely."},
		},
	},
}
```

## Status Code Retries

You can configure specific retry behavior for different HTTP status codes, either globally or per model:

```go
config := notdiamond.Config{
	// ... other config ...
	StatusCodeRetry: map[string]map[string]int{
		"openai/gpt-4": {
			"429": 3, // Retry rate limit errors 3 times
			"500": 2, // Retry internal server errors 2 times
		},
	},
}
```
