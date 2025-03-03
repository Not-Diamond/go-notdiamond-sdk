package validation

import (
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Not-Diamond/go-notdiamond/pkg/model"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  model.Config
		wantErr bool
	}{
		{
			name: "valid config with ordered models",
			config: model.Config{
				Clients: []http.Request{*&http.Request{}},
				Models: model.OrderedModels{
					"openai/gpt-4",
					"azure/gpt-4",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with weighted models",
			config: model.Config{
				Clients: []http.Request{*&http.Request{}},
				Models: model.WeightedModels{
					"openai/gpt-4": 0.6,
					"azure/gpt-4":  0.4,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid - no clients",
			config: model.Config{
				Models: model.OrderedModels{"openai/gpt-4"},
			},
			wantErr: true,
		},
		{
			name: "invalid - no models",
			config: model.Config{
				Clients: []http.Request{*&http.Request{}},
			},
			wantErr: true,
		},
		{
			name: "invalid - wrong model type",
			config: model.Config{
				Clients: []http.Request{*&http.Request{}},
				Models:  model.CustomInvalidType{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateWeightedModels(t *testing.T) {
	tests := []struct {
		name    string
		models  map[string]float64
		wantErr bool
	}{
		{
			name: "valid weights summing to 1.0",
			models: map[string]float64{
				"openai/gpt-4": 0.6,
				"azure/gpt-4":  0.4,
			},
			wantErr: false,
		},
		{
			name: "valid weights with three models",
			models: map[string]float64{
				"openai/gpt-4": 0.4,
				"azure/gpt-4":  0.3,
				"azure/gpt-3":  0.3,
			},
			wantErr: false,
		},
		{
			name:    "invalid - empty models",
			models:  map[string]float64{},
			wantErr: true,
		},
		{
			name: "invalid - weights sum > 1.0",
			models: map[string]float64{
				"openai/gpt-4": 0.6,
				"azure/gpt-4":  0.5,
			},
			wantErr: true,
		},
		{
			name: "invalid - weights sum < 1.0",
			models: map[string]float64{
				"openai/gpt-4": 0.3,
				"azure/gpt-4":  0.3,
			},
			wantErr: true,
		},
		{
			name: "invalid - negative weight",
			models: map[string]float64{
				"openai/gpt-4": -0.2,
				"azure/gpt-4":  1.2,
			},
			wantErr: true,
		},
		{
			name: "invalid - zero weight",
			models: map[string]float64{
				"openai/gpt-4": 0.0,
				"azure/gpt-4":  1.0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWeightedModels(tt.models)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateWeightedModels() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateOrderedModels(t *testing.T) {
	tests := []struct {
		name        string
		models      []string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid models",
			models: []string{
				"openai/gpt-4",
				"azure/gpt-4",
			},
			wantErr: false,
		},
		{
			name:        "empty models slice",
			models:      []string{},
			wantErr:     true,
			errContains: "at least one model must be provided",
		},
		{
			name: "invalid model in list",
			models: []string{
				"openai/gpt-4",
				"invalid/model/format/toomany",
				"azure/gpt-4",
			},
			wantErr:     true,
			errContains: "invalid model format: invalid/model/format/toomany",
		},
		{
			name: "unknown provider in list",
			models: []string{
				"openai/gpt-4",
				"unknown/model",
			},
			wantErr:     true,
			errContains: "invalid provider in model unknown/model: unknown provider: unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOrderedModels(tt.models)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateOrderedModels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("validateOrderedModels() error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}

func TestValidateModelNames(t *testing.T) {
	tests := []struct {
		name        string
		models      []string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid models",
			models: []string{
				"openai/gpt-4",
				"azure/gpt-4",
			},
			wantErr: false,
		},
		{
			name:    "empty models slice",
			models:  []string{},
			wantErr: false,
		},
		{
			name: "invalid model in list",
			models: []string{
				"openai/gpt-4",
				"invalid/model/format/toomany",
			},
			wantErr:     true,
			errContains: "invalid model format: invalid/model/format/toomany",
		},
		{
			name: "unknown provider in list",
			models: []string{
				"openai/gpt-4",
				"unknown/model",
			},
			wantErr:     true,
			errContains: "invalid provider in model unknown/model: unknown provider: unknown",
		},
		{
			name: "empty model name in list",
			models: []string{
				"openai/gpt-4",
				"",
			},
			wantErr:     true,
			errContains: "empty model name not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelNames(tt.models)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateModelNames() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("validateModelNames() error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}

func TestValidateModelName(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid openai model",
			model:   "openai/gpt-4",
			wantErr: false,
		},
		{
			name:    "valid azure model",
			model:   "azure/gpt-4",
			wantErr: false,
		},
		{
			name:        "empty model name",
			model:       "",
			wantErr:     true,
			errContains: "empty model name not allowed",
		},
		{
			name:        "missing provider",
			model:       "gpt-4",
			wantErr:     true,
			errContains: "invalid model format: gpt-4 (expected 'provider/model' or 'provider/model/region')",
		},
		{
			name:        "too many parts",
			model:       "openai/gpt-4/extra/toomany",
			wantErr:     true,
			errContains: "invalid model format: openai/gpt-4/extra/toomany (expected 'provider/model' or 'provider/model/region')",
		},
		{
			name:        "unknown provider",
			model:       "unknown/gpt-4",
			wantErr:     true,
			errContains: "invalid provider in model unknown/gpt-4: unknown provider: unknown",
		},
		{
			name:        "empty provider",
			model:       "/gpt-4",
			wantErr:     true,
			errContains: "invalid provider in model /gpt-4: unknown provider: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelName(tt.model)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateModelName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("validateModelName() error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}

func TestValidateProvider(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid azure provider",
			provider: "azure",
			wantErr:  false,
		},
		{
			name:     "valid openai provider",
			provider: "openai",
			wantErr:  false,
		},
		{
			name:        "invalid provider",
			provider:    "unknown",
			wantErr:     true,
			errContains: "unknown provider: unknown",
		},
		{
			name:        "empty provider",
			provider:    "",
			wantErr:     true,
			errContains: "unknown provider: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProvider(tt.provider)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != err.Error() {
				t.Errorf("validateProvider() error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}

func TestGetModelNames(t *testing.T) {
	tests := []struct {
		name     string
		models   map[string]float64
		expected []string
	}{
		{
			name: "multiple models",
			models: map[string]float64{
				"openai/gpt-4": 0.6,
				"azure/gpt-4":  0.4,
			},
			expected: []string{"openai/gpt-4", "azure/gpt-4"},
		},
		{
			name: "single model",
			models: map[string]float64{
				"openai/gpt-4": 1.0,
			},
			expected: []string{"openai/gpt-4"},
		},
		{
			name:     "empty map",
			models:   map[string]float64{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getModelNames(tt.models)

			// Sort both slices since map iteration order is not guaranteed
			sort.Strings(got)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("getModelNames() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestValidateStatusCodeRetry(t *testing.T) {
	tests := []struct {
		name    string
		retry   interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			retry:   nil,
			wantErr: false,
		},
		{
			name: "valid per-model config",
			retry: map[string]map[string]int{
				"openai/gpt-4": {
					"429": 3,
					"500": 2,
				},
				"azure/gpt-4": {
					"429": 5,
				},
			},
			wantErr: false,
		},
		{
			name: "valid global config",
			retry: map[string]int{
				"429": 3,
				"500": 2,
			},
			wantErr: false,
		},
		{
			name: "invalid model name",
			retry: map[string]map[string]int{
				"invalid-model": {
					"429": 3,
				},
			},
			wantErr: true,
			errMsg:  "invalid model in status code retry: invalid model format",
		},
		{
			name: "invalid status code in per-model config",
			retry: map[string]map[string]int{
				"openai/gpt-4": {
					"999": 3,
				},
			},
			wantErr: true,
			errMsg:  "invalid status codes for model openai/gpt-4: status code 999 out of valid range (100-599)",
		},
		{
			name: "invalid status code in global config",
			retry: map[string]int{
				"999": 3,
			},
			wantErr: true,
			errMsg:  "status code 999 out of valid range (100-599)",
		},
		{
			name:    "invalid type",
			retry:   "invalid",
			wantErr: true,
			errMsg:  "status code retry must be either map[string]map[string]int or map[string]int, got string",
		},
		{
			name: "negative retry count in per-model config",
			retry: map[string]map[string]int{
				"openai/gpt-4": {
					"429": -1,
				},
			},
			wantErr: true,
			errMsg:  "invalid status codes for model openai/gpt-4: negative retry count -1 for status code 429",
		},
		{
			name: "negative retry count in global config",
			retry: map[string]int{
				"429": -1,
			},
			wantErr: true,
			errMsg:  "negative retry count -1 for status code 429",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStatusCodeRetry(tt.retry)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateStatusCodeRetry() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateStatusCodeRetry() error message = %v, want to contain %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateStatusCodes(t *testing.T) {
	tests := []struct {
		name    string
		codes   map[string]int
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid status codes",
			codes: map[string]int{
				"200": 3,
				"429": 5,
				"500": 2,
			},
			wantErr: false,
		},
		{
			name: "invalid status code format",
			codes: map[string]int{
				"abc": 3,
			},
			wantErr: true,
			errMsg:  "invalid status code abc",
		},
		{
			name: "status code too low",
			codes: map[string]int{
				"99": 3,
			},
			wantErr: true,
			errMsg:  "status code 99 out of valid range (100-599)",
		},
		{
			name: "status code too high",
			codes: map[string]int{
				"600": 3,
			},
			wantErr: true,
			errMsg:  "status code 600 out of valid range (100-599)",
		},
		{
			name: "negative retry count",
			codes: map[string]int{
				"200": -1,
			},
			wantErr: true,
			errMsg:  "negative retry count -1 for status code 200",
		},
		{
			name:    "empty map",
			codes:   map[string]int{},
			wantErr: false,
		},
		{
			name: "mixed valid and invalid",
			codes: map[string]int{
				"200": 3,
				"abc": 2,
			},
			wantErr: true,
			errMsg:  "invalid status code abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStatusCodes(tt.codes)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateStatusCodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && err.Error() != tt.errMsg {
				t.Errorf("validateStatusCodes() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateModels(t *testing.T) {
	tests := []struct {
		name    string
		models  interface{}
		wantErr bool
	}{
		{
			name:    "valid ordered models",
			models:  model.OrderedModels{"openai/gpt-4"},
			wantErr: false,
		},
		{
			name: "valid weighted models",
			models: model.WeightedModels{
				"openai/gpt-4": 1.0,
			},
			wantErr: false,
		},
		{
			name:    "invalid type - string",
			models:  "invalid",
			wantErr: true,
		},
		{
			name:    "invalid type - int",
			models:  42,
			wantErr: true,
		},
		{
			name:    "invalid type - nil",
			models:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModels(tt.models)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateModels() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMessageSequence(t *testing.T) {
	tests := []struct {
		name        string
		messages    []model.Message
		wantErr     bool
		errContains string
	}{
		{
			name:     "empty messages",
			messages: []model.Message{},
			wantErr:  false,
		},
		{
			name: "valid system->user->assistant sequence",
			messages: []model.Message{
				{"role": "system", "content": "You are a helpful assistant"},
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"},
			},
			wantErr: false,
		},
		{
			name: "valid user->assistant->user sequence",
			messages: []model.Message{
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"},
				{"role": "user", "content": "How are you?"},
			},
			wantErr: false,
		},
		{
			name: "invalid first message role",
			messages: []model.Message{
				{"role": "assistant", "content": "Hi there"},
			},
			wantErr:     true,
			errContains: "first message must be either 'system' or 'user'",
		},
		{
			name: "invalid sequence after system",
			messages: []model.Message{
				{"role": "system", "content": "You are a helpful assistant"},
				{"role": "assistant", "content": "Hi there"},
			},
			wantErr:     true,
			errContains: "message after 'system' must be 'user'",
		},
		{
			name: "invalid sequence after user",
			messages: []model.Message{
				{"role": "user", "content": "Hello"},
				{"role": "user", "content": "How are you?"},
			},
			wantErr:     true,
			errContains: "message after 'user' must be 'assistant'",
		},
		{
			name: "invalid sequence after assistant",
			messages: []model.Message{
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"},
				{"role": "assistant", "content": "How can I help?"},
			},
			wantErr:     true,
			errContains: "message after 'assistant' must be 'user'",
		},
		{
			name: "valid complex sequence",
			messages: []model.Message{
				{"role": "system", "content": "You are a helpful assistant"},
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"},
				{"role": "user", "content": "How are you?"},
				{"role": "assistant", "content": "I'm doing well, thanks!"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMessageSequence(tt.messages)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMessageSequence() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("ValidateMessageSequence() error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}
