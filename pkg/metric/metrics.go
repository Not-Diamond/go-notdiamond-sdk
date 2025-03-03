package metric

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Not-Diamond/go-notdiamond/pkg/model"
	"github.com/Not-Diamond/go-notdiamond/pkg/redis"
	"github.com/pkg/errors"
)

// Tracker manages metrics for model calls using Redis
type Tracker struct {
	client *redis.Client
}

// NewTracker initializes a new Redis client for tracking metrics
func NewTracker(redisAddr string) (*Tracker, error) {
	cfg := redis.Config{
		Addr:     redisAddr,
		Password: "", // Add password if needed
		DB:       0,  // Default DB
	}

	client, err := redis.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client: %v", err)
	}

	tracker := &Tracker{client: client}

	// Only start periodic cleanup if enabled by environment variable
	if enabledStr := os.Getenv("ENABLE_REDIS_PERIODIC_CLEANUP"); enabledStr == "true" {
		// Get cleanup interval from env var or use default
		interval := 6 * time.Hour
		if intervalStr := os.Getenv("REDIS_CLEANUP_INTERVAL"); intervalStr != "" {
			if parsedInterval, err := time.ParseDuration(intervalStr); err == nil {
				interval = parsedInterval
			} else {
				slog.Warn("Invalid REDIS_CLEANUP_INTERVAL value, using default", "value", intervalStr, "default", interval)
			}
		}

		// Get data retention age from env var or use default
		retention := 24 * time.Hour
		if retentionStr := os.Getenv("REDIS_DATA_RETENTION"); retentionStr != "" {
			if parsedRetention, err := time.ParseDuration(retentionStr); err == nil {
				retention = parsedRetention
			} else {
				slog.Warn("Invalid REDIS_DATA_RETENTION value, using default", "value", retentionStr, "default", retention)
			}
		}

		// Start the periodic cleanup
		go tracker.StartPeriodicCleanup(interval, retention)
	}

	return tracker, nil
}

// Close closes the Redis connection
func (mt *Tracker) Close() error {
	return mt.client.Close()
}

// RecordLatency records a call's latency for a given model
func (mt *Tracker) RecordLatency(model string, latency float64, status string) error {
	ctx := context.Background()
	err := mt.client.RecordLatency(ctx, model, latency, status)
	if err != nil {
		return fmt.Errorf("RecordLatency failed: %v", err)
	}
	return nil
}

// RecordRecoveryTime records the recovery time for a given model
func (mt *Tracker) RecordRecoveryTime(model string, config model.Config) error {
	ctx := context.Background()
	duration := config.ModelLatency[model].RecoveryTime
	return mt.client.SetRecoveryTime(ctx, model, duration)
}

// CheckRecoveryTime checks if the model has recovered from a previous unhealthy state
func (mt *Tracker) CheckRecoveryTime(model string, config model.Config) error {
	ctx := context.Background()

	inRecovery, err := mt.client.CheckRecoveryTime(ctx, model)
	if err != nil {
		return fmt.Errorf("failed to check recovery time: %v", err)
	}

	if inRecovery {
		return errors.Errorf("Model %s is still in recovery period", model)
	}

	// Clean up old latency data when recovery period ends
	age := 24 * time.Hour // Keep last 24 hours of data
	if err := mt.client.CleanupOldLatencies(ctx, model, age); err != nil {
		slog.Error("Failed to cleanup old latency data", "error", err)
	}

	return nil
}

// CheckModelHealth returns true if the model is healthy based on its average latency and recovery time
func (mt *Tracker) CheckModelHealth(model string, config model.Config) (bool, error) {
	ctx := context.Background()

	latencyConfig, ok := config.ModelLatency[model]
	if !ok {
		return true, nil // No latency config means model is considered healthy
	}

	// First check if the model is in recovery period
	inRecovery, err := mt.client.CheckRecoveryTime(ctx, model)
	if err != nil {
		return false, fmt.Errorf("failed to check recovery time: %v", err)
	}
	if inRecovery {
		return false, fmt.Errorf("model %s is still in recovery period", model)
	}

	// Get the latency entries first to check if we have enough data
	entries, err := mt.client.GetLatencyEntries(ctx, model, int64(latencyConfig.NoOfCalls))
	if err != nil {
		return false, fmt.Errorf("failed to get latency entries: %v", err)
	}

	// If we don't have enough data points yet, consider the model healthy
	if len(entries) < latencyConfig.NoOfCalls {
		slog.Info("Not enough data points yet",
			"model", model,
			"current", len(entries),
			"required", latencyConfig.NoOfCalls)
		return true, nil
	}

	// Calculate average from the entries we already have
	var totalLatency float64
	for _, latency := range entries {
		totalLatency += latency
	}
	avgLatency := totalLatency / float64(len(entries))

	// If average latency is above threshold, set recovery time and mark as unhealthy
	if avgLatency > latencyConfig.AvgLatencyThreshold {
		if err := mt.RecordRecoveryTime(model, config); err != nil {
			slog.Error("Failed to record recovery time", "error", err)
		}
		return false, fmt.Errorf("model %s is unhealthy: average latency %.2fs exceeds threshold %.2fs (over last %d calls)",
			model, avgLatency, latencyConfig.AvgLatencyThreshold, len(entries))
	}

	return true, nil
}

// RecordErrorCode records a status code for a given model
func (mt *Tracker) RecordErrorCode(model string, statusCode int) error {
	ctx := context.Background()
	slog.Info("📝 Recording error code", "model", model, "status_code", statusCode)
	err := mt.client.RecordErrorCode(ctx, model, statusCode)
	if err != nil {
		return fmt.Errorf("RecordErrorCode failed: %v", err)
	}
	return nil
}

// RecordErrorRecoveryTime records the error recovery time for a given model
func (mt *Tracker) RecordErrorRecoveryTime(model string, config model.Config, statusCode int) error {
	ctx := context.Background()
	errorConfig, ok := config.ModelErrorTracking[model]
	if !ok {
		return fmt.Errorf("no error tracking config found for model %s", model)
	}

	statusConfig, ok := errorConfig.StatusConfigs[statusCode]
	if !ok {
		return fmt.Errorf("no status code config found for code %d", statusCode)
	}

	duration := statusConfig.RecoveryTime
	return mt.client.SetErrorRecoveryTime(ctx, model, duration)
}

// CheckErrorRecoveryTime checks if the model has recovered from a previous error state
func (mt *Tracker) CheckErrorRecoveryTime(model string, config model.Config) error {
	ctx := context.Background()

	inRecovery, err := mt.client.CheckErrorRecoveryTime(ctx, model)
	if err != nil {
		return fmt.Errorf("failed to check error recovery time: %v", err)
	}

	if inRecovery {
		return errors.Errorf("Model %s is still in error recovery period", model)
	}

	// Model has just come out of recovery, log this event
	slog.Info("🔄 Model has recovered from error state", "model", model)

	// Clean up old error data when recovery period ends
	age := 24 * time.Hour // Keep last 24 hours of data
	if err := mt.client.CleanupOldErrors(ctx, model, age); err != nil {
		slog.Error("Failed to cleanup old error data", "error", err)
	}

	return nil
}

// CheckModelErrorHealth returns true if the model is healthy based on its error rates
func (mt *Tracker) CheckModelErrorHealth(model string, config model.Config) (bool, error) {
	ctx := context.Background()

	errorConfig, ok := config.ModelErrorTracking[model]
	if !ok {
		slog.Info("ℹ️ No error tracking config found", "model", model)
		return true, nil // No error config means model is considered healthy
	}

	// First check if the model is in error recovery period
	inRecovery, err := mt.client.CheckErrorRecoveryTime(ctx, model)
	if err != nil {
		return false, fmt.Errorf("failed to check error recovery time: %v", err)
	}
	if inRecovery {
		slog.Info("⏳ Model is in error recovery period", "model", model)
		return false, fmt.Errorf("model %s is still in error recovery period", model)
	}

	// For each configured status code, check its error percentage
	for statusCode, statusConfig := range errorConfig.StatusConfigs {
		// Get the error percentages for this status code
		errorPercentages, err := mt.client.GetErrorPercentages(ctx, model, int64(statusConfig.NoOfCalls))
		if err != nil {
			return false, fmt.Errorf("failed to get error percentages: %v", err)
		}

		percentage := errorPercentages[statusCode]
		slog.Info("📊 Error tracking status",
			"model", model,
			"status_code", statusCode,
			"percentage", percentage,
			"threshold", statusConfig.ErrorThresholdPercentage,
			"calls_considered", statusConfig.NoOfCalls,
			"will_trigger_recovery", percentage >= statusConfig.ErrorThresholdPercentage)

		if percentage >= statusConfig.ErrorThresholdPercentage {
			slog.Info("🚨 Error threshold exceeded - marking model as unhealthy",
				"model", model,
				"status_code", statusCode,
				"current", percentage,
				"threshold", statusConfig.ErrorThresholdPercentage,
				"calls_considered", statusConfig.NoOfCalls)

			if err := mt.RecordErrorRecoveryTime(model, config, statusCode); err != nil {
				slog.Error("Failed to record error recovery time", "error", err)
			}
			return false, fmt.Errorf("model %s is unhealthy: status code %d error percentage %.2f%% exceeds threshold %.2f%% (over last %d calls)",
				model, statusCode, percentage, statusConfig.ErrorThresholdPercentage, statusConfig.NoOfCalls)
		}
	}

	slog.Info("✅ Error health check passed", "model", model)
	return true, nil
}

// CheckModelOverallHealth checks both latency and error health of a model
func (mt *Tracker) CheckModelOverallHealth(model string, config model.Config) (bool, error) {
	// Check latency health first
	latencyHealthy, latencyErr := mt.CheckModelHealth(model, config)
	// Check error health next
	errorHealthy, errorErr := mt.CheckModelErrorHealth(model, config)

	// Handle different combinations of health states
	switch {
	case !latencyHealthy && !errorHealthy:
		// If either has an actual error (not just recovery), prioritize that
		if strings.Contains(latencyErr.Error(), "average latency") {
			return false, latencyErr
		}
		if strings.Contains(errorErr.Error(), "status code") {
			return false, errorErr
		}
		// If error is in recovery and latency has actual error, return latency error
		if strings.Contains(errorErr.Error(), "recovery period") && !strings.Contains(latencyErr.Error(), "recovery period") {
			return false, latencyErr
		}
		// If latency is in recovery and error has actual error, return error
		if strings.Contains(latencyErr.Error(), "recovery period") && !strings.Contains(errorErr.Error(), "recovery period") {
			return false, errorErr
		}
		// If both are in recovery, return error recovery
		if strings.Contains(errorErr.Error(), "error recovery period") {
			return false, errorErr
		}
		// Default to latency recovery message
		return false, latencyErr
	case !latencyHealthy:
		return false, latencyErr
	case !errorHealthy:
		return false, errorErr
	default:
		return true, nil
	}
}

// StartPeriodicCleanup starts a background goroutine that periodically cleans up
// old latency and error data for all models it finds in Redis.
// cleanupInterval: how often to run the cleanup
// dataAge: how old data should be before it's cleaned up
func (mt *Tracker) StartPeriodicCleanup(cleanupInterval, dataAge time.Duration) {
	ticker := time.NewTicker(cleanupInterval)
	ctx := context.Background()

	slog.Info("🧹 Starting periodic Redis data cleanup",
		"interval", cleanupInterval.String(),
		"data_retention", dataAge.String())

	go func() {
		for range ticker.C {
			// Get all known models from Redis latency keys
			latencyKeys, err := mt.client.GetKeysWithPrefix(ctx, "latency:*")
			if err != nil {
				slog.Error("Failed to get latency keys for cleanup", "error", err)
				continue
			}

			// Extract model names from the latency keys
			models := make(map[string]bool)
			for _, key := range latencyKeys {
				// Parse model name from key format "latency:model"
				parts := strings.Split(key, ":")
				if len(parts) >= 2 && parts[0] == "latency" && !strings.Contains(parts[1], "recovery") && !strings.Contains(parts[1], "counter") {
					models[parts[1]] = true
				}
			}

			// Get all known models from Redis error keys
			errorKeys, err := mt.client.GetKeysWithPrefix(ctx, "errors:*")
			if err != nil {
				slog.Error("Failed to get error keys for cleanup", "error", err)
				continue
			}

			// Extract model names from the error keys
			for _, key := range errorKeys {
				// Parse model name from key format "errors:model"
				parts := strings.Split(key, ":")
				if len(parts) >= 2 && parts[0] == "errors" && !strings.Contains(parts[1], "recovery") && !strings.Contains(parts[1], "counter") {
					models[parts[1]] = true
				}
			}

			// Clean up data for each model
			for model := range models {
				slog.Info("🧹 Cleaning up old data for model", "model", model)

				// Clean up old latency data
				if err := mt.client.CleanupOldLatencies(ctx, model, dataAge); err != nil {
					slog.Error("Failed to clean up old latency data", "model", model, "error", err)
				}

				// Clean up old error data
				if err := mt.client.CleanupOldErrors(ctx, model, dataAge); err != nil {
					slog.Error("Failed to clean up old error data", "model", model, "error", err)
				}
			}

			slog.Info("🧹 Completed periodic cleanup", "models_processed", len(models))
		}
	}()
}
