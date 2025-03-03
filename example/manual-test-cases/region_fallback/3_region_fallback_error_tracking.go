package test_region_fallback

import (
	"time"

	"github.com/Not-Diamond/go-notdiamond/pkg/model"
)

// RegionFallbackWithErrorTrackingTest demonstrates region fallback with error tracking
var RegionFallbackWithErrorTrackingTest = model.Config{
	Models: model.OrderedModels{
		"vertex/gemini-pro/us-east4",
		"vertex/gemini-pro/us-west1",
		"vertex/gemini-pro/us-central1",
	},
	// Configure error tracking for specific status codes
	ModelErrorTracking: model.ModelErrorTracking{
		"vertex/gemini-pro/us-east4": &model.RollingErrorTracking{
			StatusConfigs: map[int]*model.StatusCodeConfig{
				429: { // Too Many Requests
					ErrorThresholdPercentage: 50.0,            // Fallback if 50% of requests return 429
					NoOfCalls:                5,               // Consider last 5 calls
					RecoveryTime:             time.Minute * 5, // Wait 5 minutes before trying again
				},
				500: { // Internal Server Error
					ErrorThresholdPercentage: 30.0,            // Fallback if 30% of requests return 500
					NoOfCalls:                10,              // Consider last 10 calls
					RecoveryTime:             time.Minute * 2, // Wait 2 minutes before trying again
				},
			},
		},
	},
	// Configure retries for specific status codes
	StatusCodeRetry: map[string]map[string]int{
		"vertex/gemini-pro/us-east4": {
			"429": 3, // Retry 429 errors 3 times before falling back
			"500": 2, // Retry 500 errors 2 times before falling back
		},
		"vertex/gemini-pro/us-west1": {
			"429": 2, // Retry 429 errors 2 times before falling back
			"500": 1, // Retry 500 errors 1 time before falling back
		},
	},
	// Configure backoff between retries
	Backoff: map[string]float64{
		"vertex/gemini-pro/us-east4": 2.0, // Wait 2 seconds between retries
		"vertex/gemini-pro/us-west1": 1.0, // Wait 1 second between retries
	},
}
