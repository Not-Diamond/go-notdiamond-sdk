package notdiamond

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func validateConfig(config Config) error {
	if err := validateClients(config.Clients); err != nil {
		return err
	}

	if err := validateModels(config.Models); err != nil {
		return err
	}

	return validateStatusCodeRetry(config.StatusCodeRetry)
}

func validateClients(clients []http.Request) error {
	if len(clients) == 0 {
		return errors.New("at least one client must be provided")
	}
	return nil
}

func validateModels(models interface{}) error {
	switch m := models.(type) {
	case OrderedModels:
		return validateOrderedModels(m)
	case WeightedModels:
		return validateWeightedModels(m)
	default:
		return fmt.Errorf("models must be either notdiamond.OrderedModels or map[string]float64, got %T", models)
	}
}

func validateWeightedModels(models map[string]float64) error {
	if len(models) == 0 {
		return errors.New("at least one model must be provided")
	}

	if err := validateWeights(models); err != nil {
		return err
	}

	return validateModelNames(getModelNames(models))
}

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

func validateOrderedModels(models []string) error {
	if len(models) == 0 {
		return errors.New("at least one model must be provided")
	}
	return validateModelNames(models)
}

func validateModelNames(models []string) error {
	for _, model := range models {
		if err := validateModelName(model); err != nil {
			return err
		}
	}
	return nil
}

func validateModelName(model string) error {
	if model == "" {
		return errors.New("empty model name not allowed")
	}

	parts := strings.Split(model, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid model format: %s (expected 'provider/model')", model)
	}

	provider := parts[0]
	if err := validateProvider(provider); err != nil {
		return fmt.Errorf("invalid provider in model %s: %w", model, err)
	}

	return nil
}

func validateProvider(provider string) error {
	switch provider {
	case string(ClientTypeAzure), string(ClientTypeOpenAI):
		return nil
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}
}

func getModelNames(models map[string]float64) []string {
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	return names
}

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
