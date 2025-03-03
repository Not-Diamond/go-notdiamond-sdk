package test_region_fallback

import (
	"github.com/Not-Diamond/go-notdiamond/pkg/model"
)

// RegionFallbackVertexTest demonstrates region fallback for Vertex AI
// If a request to us-east fails, it will fallback to us-west
var RegionFallbackVertexTest = model.Config{
	Models: model.OrderedModels{
		"vertex/gemini-pro/us-east4",    // Try us-east4 first
		"vertex/gemini-pro/us-west1",    // Fallback to us-west1
		"vertex/gemini-pro/us-central1", // Final fallback to us-central1
	},
}

// RegionFallbackOpenAITest demonstrates model fallback for OpenAI
// Note: OpenAI no longer supports region fallback
var RegionFallbackOpenAITest = model.Config{
	Models: model.OrderedModels{
		"openai/gpt-4o-mini",   // Try gpt-4o-mini first
		"openai/gpt-3.5-turbo", // Fallback to gpt-3.5-turbo
	},
}

// RegionFallbackAzureTest demonstrates region fallback for Azure
var RegionFallbackAzureTest = model.Config{
	Models: model.OrderedModels{
		"azure/gpt-35-turbo/eastus",     // Try eastus first
		"azure/gpt-35-turbo/westus",     // Fallback to westus
		"azure/gpt-35-turbo/westeurope", // Final fallback to westeurope
	},
	AzureAPIVersion: "2023-05-15", // Specify Azure API version
	AzureRegions: map[string]string{
		"eastus":     "https://notdiamond-azure-openai.openai.azure.com",
		"westus":     "https://notdiamond-westus.openai.azure.com",
		"westeurope": "https://custom-westeurope.openai.azure.com",
	},
}

// RegionFallbackMixedTest demonstrates region fallback across different providers
var RegionFallbackMixedTest = model.Config{
	Models: model.OrderedModels{
		"azure/gpt-4o-mini/eastus",   // Fallback to Azure in eastus
		"vertex/gemini-pro/us-east4", // Try Vertex in us-east4 first
		"azure/gpt-4o-mini",
		"openai/gpt-4o-mini",
		"openai/gpt-3.5-turbo", // Fallback to OpenAI (no region)
	},
	AzureAPIVersion: "2023-05-15", // Specify Azure API version
	AzureRegions: map[string]string{
		"eastus":     "https://notdiamond-azure-openai.openai.azure.com",
		"westeurope": "https://custom-westeurope.openai.azure.com",
	},
}
