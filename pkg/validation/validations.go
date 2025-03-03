package validation

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Not-Diamond/go-notdiamond/pkg/model"
)

// ValidateConfig validates the configuration for the NotDiamond client.
func ValidateConfig(config model.Config) error {
	if err := validateClients(config.Clients); err != nil {
		return err
	}

	if err := validateModels(config.Models); err != nil {
		return err
	}

	return validateStatusCodeRetry(config.StatusCodeRetry)
}

// validateClients validates the clients for the NotDiamond client.
func validateClients(clients []http.Request) error {
	if len(clients) == 0 {
		return errors.New("at least one client must be provided")
	}
	return nil
}

// validateModels validates the models for the NotDiamond client.
func validateModels(models interface{}) error {
	switch m := models.(type) {
	case model.OrderedModels:
		return validateOrderedModels(m)
	case model.WeightedModels:
		return validateWeightedModels(m)
	default:
		return fmt.Errorf("models must be either notdiamond.OrderedModels or map[string]float64, got %T", models)
	}
}

// validateWeightedModels validates the weighted models for the NotDiamond client.
func validateWeightedModels(models map[string]float64) error {
	if len(models) == 0 {
		return errors.New("at least one model must be provided")
	}

	if err := validateWeights(models); err != nil {
		return err
	}

	return validateModelNames(getModelNames(models))
}

// validateWeights validates the weights for the NotDiamond client.
func validateWeights(models map[string]float64) error {
	var totalWeight float64
	for modelName, weight := range models {
		if weight <= 0 {
			return fmt.Errorf("model %s has invalid weight: %f (must be positive)", modelName, weight)
		}
		totalWeight += weight
	}

	// Use a small epsilon value to handle floating point imprecision
	const epsilon = 0.000001
	if totalWeight < 1.0-epsilon || totalWeight > 1.0+epsilon {
		return fmt.Errorf("model weights must sum to 1.0, got %f", totalWeight)
	}
	return nil
}

// validateOrderedModels validates the ordered models for the NotDiamond client.
func validateOrderedModels(models []string) error {
	if len(models) == 0 {
		return errors.New("at least one model must be provided")
	}
	return validateModelNames(models)
}

// validateModelNames validates the model names for the NotDiamond client.
func validateModelNames(models []string) error {
	for _, model := range models {
		if err := validateModelName(model); err != nil {
			return err
		}
	}
	return nil
}

// validateModelName validates the model name for the NotDiamond client.
func validateModelName(model string) error {
	if model == "" {
		return errors.New("empty model name not allowed")
	}

	parts := strings.Split(model, "/")

	// Handle provider/model format
	if len(parts) == 2 {
		provider := parts[0]
		if err := validateProvider(provider); err != nil {
			return fmt.Errorf("invalid provider in model %s: %w", model, err)
		}
		return nil
	}

	// Handle provider/model/region format
	if len(parts) == 3 {
		provider := parts[0]
		if err := validateProvider(provider); err != nil {
			return fmt.Errorf("invalid provider in model %s: %w", model, err)
		}
		// We don't validate the region as it can be any string
		return nil
	}

	return fmt.Errorf("invalid model format: %s (expected 'provider/model' or 'provider/model/region')", model)
}

// validateProvider validates the provider for the NotDiamond client.
func validateProvider(provider string) error {
	switch provider {
	case string(model.ClientTypeAzure), string(model.ClientTypeOpenai), string(model.ClientTypeVertex):
		return nil
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}
}

// getModelNames gets the model names for the NotDiamond client.
func getModelNames(models map[string]float64) []string {
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	return names
}

// validateStatusCodeRetry validates the status code retry for the NotDiamond client.
func validateStatusCodeRetry(retry interface{}) error {
	if retry == nil {
		return nil
	}

	switch r := retry.(type) {
	case map[string]map[string]int:
		// Per model validation
		for model, statusCodes := range r {
			if err := validateModelName(model); err != nil {
				return fmt.Errorf("invalid model in status code retry: %w", err)
			}
			if err := validateStatusCodes(statusCodes); err != nil {
				return fmt.Errorf("invalid status codes for model %s: %w", model, err)
			}
		}
	case map[string]int:
		// Global validation
		return validateStatusCodes(r)
	default:
		return fmt.Errorf("status code retry must be either map[string]map[string]int or map[string]int, got %T", retry)
	}
	return nil
}

// validateStatusCodes validates the status codes for the NotDiamond client.
func validateStatusCodes(codes map[string]int) error {
	for code, retries := range codes {
		statusCode, err := strconv.Atoi(code)
		if err != nil {
			return fmt.Errorf("invalid status code %s", code)
		}
		if statusCode < 100 || statusCode > 599 {
			return fmt.Errorf("status code %d out of valid range (100-599)", statusCode)
		}
		if retries < 0 {
			return fmt.Errorf("negative retry count %d for status code %s", retries, code)
		}
	}
	return nil
}

// ValidateMessageSequence ensures messages alternate properly between roles
func ValidateMessageSequence(messages []model.Message) error {
	if len(messages) == 0 {
		return nil
	}

	lastRole := ""
	for i, msg := range messages {
		currentRole := msg["role"]

		// First message can be either system or user
		if i == 0 {
			if currentRole != "system" && currentRole != "user" {
				return fmt.Errorf("first message must be either 'system' or 'user', got '%s'", currentRole)
			}
			lastRole = currentRole
			continue
		}

		// After a system message, only user message is allowed
		if lastRole == "system" && currentRole != "user" {
			return fmt.Errorf("message after 'system' must be 'user', got '%s'", currentRole)
		}

		// After a user message, only assistant message is allowed
		if lastRole == "user" && currentRole != "assistant" {
			return fmt.Errorf("message after 'user' must be 'assistant', got '%s'", currentRole)
		}

		// After an assistant message, only user message is allowed
		if lastRole == "assistant" && currentRole != "user" {
			return fmt.Errorf("message after 'assistant' must be 'user', got '%s'", currentRole)
		}

		lastRole = currentRole
	}
	return nil
}
