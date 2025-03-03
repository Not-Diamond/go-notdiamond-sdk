package test_region_fallback

import (
	"github.com/Not-Diamond/go-notdiamond/pkg/model"
)

// RegionFallbackWeightedTest demonstrates region fallback with weighted models
var RegionFallbackWeightedTest = model.Config{
	Models: model.WeightedModels{
		"vertex/gemini-pro/us-east4":    0.3, // 30% chance to try us-east4 first
		"vertex/gemini-pro/us-west1":    0.3, // 30% chance to try us-west1 first
		"vertex/gemini-pro/us-central1": 0.2, // 20% chance to try us-central1 first
		"openai/gpt-3.5-turbo":          0.1, // 10% chance to try OpenAI first
		"azure/gpt-35-turbo/eastus":     0.1, // 10% chance to try Azure eastus first
	},
	AzureAPIVersion: "2023-05-15", // Specify Azure API version
}

// RegionFallbackWithTimeoutTest demonstrates region fallback with timeouts
var RegionFallbackWithTimeoutTest = model.Config{
	Models: model.OrderedModels{
		"vertex/gemini-pro/us-east4",
		"vertex/gemini-pro/us-west1",
		"vertex/gemini-pro/us-central1",
	},
	Timeout: map[string]float64{
		"vertex/gemini-pro/us-east4":    10.0, // 10 second timeout for us-east4
		"vertex/gemini-pro/us-west1":    15.0, // 15 second timeout for us-west1
		"vertex/gemini-pro/us-central1": 20.0, // 20 second timeout for us-central1
	},
}
