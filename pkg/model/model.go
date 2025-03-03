package model

import (
	"net/http"
	"time"

	"github.com/Not-Diamond/go-notdiamond/pkg/redis"
)

// Models is a type that can be used to represent a list of models.
type Models interface {
	isModels()
}

// Message is a map of strings.
type Message map[string]string

// OrderedModels is a type that can be used to represent a list of models.
type OrderedModels []string

func (OrderedModels) isModels() {}

// WeightedModels is a type that can be used to represent a list of models.
type WeightedModels map[string]float64

func (WeightedModels) isModels() {}

// ClientType is a type that can be used to represent a client type.
type clientType string

const (
	ClientTypeAzure  clientType = "azure"
	ClientTypeOpenai clientType = "openai"
	ClientTypeVertex clientType = "vertex"
)

// RollingAverageLatency is a type that can be used to represent a rolling average latency.
type RollingAverageLatency struct {
	AvgLatencyThreshold float64
	NoOfCalls           int
	RecoveryTime        time.Duration
}

// ModelLatency is a type that can be used to represent a model latency.
type ModelLatency map[string]*RollingAverageLatency

// CustomInvalidType is a type that can be used to represent a custom invalid type.
type CustomInvalidType struct{}

func (CustomInvalidType) isModels() {}

type ModelLimits struct {
	MaxNoOfCalls    int
	MaxRecoveryTime time.Duration
}

// StatusCodeConfig represents the configuration for a specific status code
type StatusCodeConfig struct {
	ErrorThresholdPercentage float64       // Percentage of this error code that triggers fallback
	NoOfCalls                int           // Number of calls to consider for the error percentage
	RecoveryTime             time.Duration // Time to wait before retrying the model
}

// RollingErrorTracking is a type that can be used to represent error code tracking configuration.
type RollingErrorTracking struct {
	StatusConfigs map[int]*StatusCodeConfig // Configuration for each status code to track
}

// ModelErrorTracking is a type that can be used to represent model error tracking configuration.
type ModelErrorTracking map[string]*RollingErrorTracking

// Config is the configuration for the NotDiamond client.
type Config struct {
	Clients            []http.Request
	Models             Models
	MaxRetries         map[string]int
	Timeout            map[string]float64
	ModelMessages      map[string][]Message
	Backoff            map[string]float64
	StatusCodeRetry    interface{}
	ModelLatency       ModelLatency
	ModelErrorTracking ModelErrorTracking // Configuration for error code tracking
	ModelLimits        ModelLimits
	RedisConfig        *redis.Config // Redis configuration for metrics tracking
	VertexProjectID    string
	VertexLocation     string
	AzureAPIVersion    string            // Azure API version to use for requests
	AzureRegions       map[string]string // Map of region names to Azure endpoints
}
