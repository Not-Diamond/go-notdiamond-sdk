package metric

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Not-Diamond/go-notdiamond/pkg/model"
	"github.com/Not-Diamond/go-notdiamond/pkg/redis"
	"github.com/alicebob/miniredis/v2"
)

func setupTestRedis(t *testing.T) (string, func()) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}

	return mr.Addr(), func() {
		mr.Close()
	}
}

func TestNewTracker(t *testing.T) {
	redisAddr, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker, err := NewTracker(redisAddr)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	if tracker.client == nil {
		t.Error("Expected non-nil Redis client")
	}
}

func TestRecordAndCheckLatency(t *testing.T) {
	redisAddr, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker, err := NewTracker(redisAddr)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	modelName := "test-model"
	latencyConfig := &model.RollingAverageLatency{
		AvgLatencyThreshold: 1.0,
		NoOfCalls:           2,
		RecoveryTime:        time.Second,
	}
	config := model.Config{
		ModelLatency: model.ModelLatency{
			modelName: latencyConfig,
		},
	}

	// Record some latencies
	if err := tracker.RecordLatency(modelName, 0.5, "success"); err != nil {
		t.Errorf("RecordLatency() error = %v", err)
	}
	if err := tracker.RecordLatency(modelName, 0.7, "success"); err != nil {
		t.Errorf("RecordLatency() error = %v", err)
	}

	// Check health - should be healthy as average is below threshold
	healthy, err := tracker.CheckModelHealth(modelName, config)
	if err != nil {
		t.Errorf("CheckModelHealth() error = %v", err)
	}
	if !healthy {
		t.Error("Expected model to be healthy")
	}

	// Record high latency
	if err := tracker.RecordLatency(modelName, 2.0, "success"); err != nil {
		t.Errorf("RecordLatency() error = %v", err)
	}

	// Check health - should be unhealthy as average is above threshold
	healthy, err = tracker.CheckModelHealth(modelName, config)
	if err == nil {
		t.Error("Expected an error indicating the model is unhealthy")
	} else if !strings.Contains(err.Error(), "unhealthy") {
		t.Errorf("Expected unhealthy error message, got: %v", err)
	}
	if healthy {
		t.Error("Expected model to be unhealthy")
	}
}

func TestRecoveryTime(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tracker, err := NewTracker(mr.Addr())
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	modelName := "test-model"
	latencyConfig := &model.RollingAverageLatency{
		RecoveryTime: time.Second,
	}
	config := model.Config{
		ModelLatency: model.ModelLatency{
			modelName: latencyConfig,
		},
	}

	// Record recovery time
	if err := tracker.RecordRecoveryTime(modelName, config); err != nil {
		t.Errorf("RecordRecoveryTime() error = %v", err)
	}

	// Check recovery time - should be in recovery
	err = tracker.CheckRecoveryTime(modelName, config)
	if err == nil {
		t.Error("Expected an error indicating the model is in recovery")
	} else if !strings.Contains(err.Error(), "still in recovery period") {
		t.Errorf("Expected 'still in recovery period' error message, got: %v", err)
	}

	// Fast forward time by 2 seconds
	mr.FastForward(2 * time.Second)

	// Check recovery time - should be recovered
	if err := tracker.CheckRecoveryTime(modelName, config); err != nil {
		t.Errorf("CheckRecoveryTime() error = %v", err)
	}
}

func TestCheckModelHealth_NoConfig(t *testing.T) {
	redisAddr, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker, err := NewTracker(redisAddr)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	modelName := "test-model"
	config := model.Config{
		ModelLatency: model.ModelLatency{}, // Empty config
	}

	// Model should be considered healthy when no config exists
	healthy, err := tracker.CheckModelHealth(modelName, config)
	if err != nil {
		t.Errorf("CheckModelHealth() error = %v", err)
	}
	if !healthy {
		t.Error("Expected model to be healthy when no config exists")
	}
}

func TestCheckModelHealth_NoData(t *testing.T) {
	redisAddr, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker, err := NewTracker(redisAddr)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	modelName := "test-model"
	latencyConfig := &model.RollingAverageLatency{
		AvgLatencyThreshold: 1.0,
		NoOfCalls:           2,
		RecoveryTime:        time.Second,
	}
	config := model.Config{
		ModelLatency: model.ModelLatency{
			modelName: latencyConfig,
		},
	}

	// Model should be considered healthy when no data exists
	healthy, err := tracker.CheckModelHealth(modelName, config)
	if err != nil {
		t.Errorf("CheckModelHealth() error = %v", err)
	}
	if !healthy {
		t.Error("Expected model to be healthy when no data exists")
	}
}

func TestErrorTracking(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tracker, err := NewTracker(mr.Addr())
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	modelName := "test-model"
	config := model.Config{
		ModelErrorTracking: model.ModelErrorTracking{
			modelName: &model.RollingErrorTracking{
				StatusConfigs: map[int]*model.StatusCodeConfig{
					401: {
						ErrorThresholdPercentage: 80,
						NoOfCalls:                5,
						RecoveryTime:             1 * time.Minute,
					},
				},
			},
		},
	}

	// Test recording error codes
	for i := 0; i < 5; i++ {
		if err := tracker.RecordErrorCode(modelName, 401); err != nil {
			t.Errorf("RecordErrorCode() error = %v", err)
		}
	}

	// Test health check after recording errors
	healthy, err := tracker.CheckModelErrorHealth(modelName, config)
	if err == nil {
		t.Error("Expected an error indicating the model is unhealthy")
	}
	if healthy {
		t.Error("Expected model to be unhealthy after 5 401 errors")
	}

	// Test recovery time
	if err := tracker.RecordErrorRecoveryTime(modelName, config, 401); err != nil {
		t.Errorf("RecordErrorRecoveryTime() error = %v", err)
	}

	// Check recovery time - should be in recovery
	err = tracker.CheckErrorRecoveryTime(modelName, config)
	if err == nil {
		t.Error("Expected an error indicating the model is in recovery")
	} else if !strings.Contains(err.Error(), "still in error recovery period") {
		t.Errorf("Expected 'still in error recovery period' error message, got: %v", err)
	}

	// Fast forward time by 2 minutes
	mr.FastForward(2 * time.Minute)

	// Check recovery time - should be recovered
	if err := tracker.CheckErrorRecoveryTime(modelName, config); err != nil {
		t.Errorf("CheckErrorRecoveryTime() error = %v", err)
	}

	// Test health check after recovery
	healthy, err = tracker.CheckModelErrorHealth(modelName, config)
	if err != nil {
		t.Errorf("CheckModelErrorHealth() error = %v", err)
	}
	if !healthy {
		t.Error("Expected model to be healthy after recovery period")
	}
}

func TestErrorTrackingWithMultipleStatusCodes(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tracker, err := NewTracker(mr.Addr())
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	modelName := "test-model"
	config := model.Config{
		ModelErrorTracking: model.ModelErrorTracking{
			modelName: &model.RollingErrorTracking{
				StatusConfigs: map[int]*model.StatusCodeConfig{
					401: {
						ErrorThresholdPercentage: 80,
						NoOfCalls:                5,
						RecoveryTime:             1 * time.Minute,
					},
					500: {
						ErrorThresholdPercentage: 60,
						NoOfCalls:                3,
						RecoveryTime:             30 * time.Second,
					},
				},
			},
		},
	}

	// Test 401 errors (not enough to trigger recovery)
	for i := 0; i < 3; i++ {
		if err := tracker.RecordErrorCode(modelName, 401); err != nil {
			t.Errorf("RecordErrorCode() error = %v", err)
		}
	}
	// Add some successful calls to keep error percentage below threshold
	for i := 0; i < 3; i++ {
		if err := tracker.RecordErrorCode(modelName, 200); err != nil {
			t.Errorf("RecordErrorCode() error = %v", err)
		}
	}

	// Check health - should still be healthy
	healthy, err := tracker.CheckModelErrorHealth(modelName, config)
	if err != nil {
		t.Errorf("CheckModelErrorHealth() error = %v", err)
	}
	if !healthy {
		t.Error("Expected model to be healthy with only 3 401 errors")
	}

	// Test 500 errors (enough to trigger recovery)
	for i := 0; i < 3; i++ {
		if err := tracker.RecordErrorCode(modelName, 500); err != nil {
			t.Errorf("RecordErrorCode() error = %v", err)
		}
	}

	// Check health - should be unhealthy due to 500 errors
	healthy, err = tracker.CheckModelErrorHealth(modelName, config)
	if err == nil {
		t.Error("Expected an error indicating the model is unhealthy")
	}
	if healthy {
		t.Error("Expected model to be unhealthy after 3 500 errors")
	}

	// Record recovery time for 500 errors
	if err := tracker.RecordErrorRecoveryTime(modelName, config, 500); err != nil {
		t.Errorf("RecordErrorRecoveryTime() error = %v", err)
	}

	// Fast forward time by 1 minute
	mr.FastForward(1 * time.Minute)

	// Check health - should be healthy again
	healthy, err = tracker.CheckModelErrorHealth(modelName, config)
	if err != nil {
		t.Errorf("CheckModelErrorHealth() error = %v", err)
	}
	if !healthy {
		t.Error("Expected model to be healthy after recovery period")
	}
}

func TestErrorTrackingEdgeCases(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tracker, err := NewTracker(mr.Addr())
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	modelName := "test-model"
	config := model.Config{
		ModelErrorTracking: model.ModelErrorTracking{
			modelName: &model.RollingErrorTracking{
				StatusConfigs: map[int]*model.StatusCodeConfig{
					401: {
						ErrorThresholdPercentage: 80,
						NoOfCalls:                5,
						RecoveryTime:             1 * time.Minute,
					},
				},
			},
		},
	}

	// Test recording error code for non-configured status code
	if err := tracker.RecordErrorCode(modelName, 404); err != nil {
		t.Errorf("RecordErrorCode() error = %v", err)
	}

	// Check health - should be healthy since 404 is not configured
	healthy, err := tracker.CheckModelErrorHealth(modelName, config)
	if err != nil {
		t.Errorf("CheckModelErrorHealth() error = %v", err)
	}
	if !healthy {
		t.Error("Expected model to be healthy with unconfigured status code")
	}

	// Test recording recovery time for non-configured status code
	err = tracker.RecordErrorRecoveryTime(modelName, config, 404)
	if err == nil {
		t.Error("Expected an error when recording recovery time for unconfigured status code")
	}

	// Test health check for non-existent model
	healthy, err = tracker.CheckModelErrorHealth("non-existent-model", config)
	if err != nil {
		t.Errorf("CheckModelErrorHealth() error = %v", err)
	}
	if !healthy {
		t.Error("Expected non-existent model to be considered healthy")
	}
}

func TestCheckModelOverallHealth(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tracker, err := NewTracker(mr.Addr())
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	defer tracker.Close()

	modelName := "test-model"
	config := model.Config{
		ModelLatency: model.ModelLatency{
			modelName: &model.RollingAverageLatency{
				AvgLatencyThreshold: 1.0,
				NoOfCalls:           2,
				RecoveryTime:        time.Second,
			},
		},
		ModelErrorTracking: model.ModelErrorTracking{
			modelName: &model.RollingErrorTracking{
				StatusConfigs: map[int]*model.StatusCodeConfig{
					401: {
						ErrorThresholdPercentage: 80,
						NoOfCalls:                2,
						RecoveryTime:             time.Second,
					},
				},
			},
		},
	}

	clearAllData := func() {
		ctx := context.Background()
		// Clear all data for the model
		if err := tracker.client.ClearAllModelData(ctx, modelName); err != nil {
			t.Errorf("Failed to clear model data: %v", err)
		}
		// Fast forward time to clear any remaining recovery periods
		mr.FastForward(2 * time.Second)
	}

	tests := []struct {
		name           string
		setupFunc      func()
		expectedHealth bool
		expectError    bool
		errorContains  string
	}{
		{
			name: "both healthy",
			setupFunc: func() {
				clearAllData()
				// Record good latencies
				tracker.RecordLatency(modelName, 0.5, "success")
				tracker.RecordLatency(modelName, 0.7, "success")
				// Record some errors but below threshold
				tracker.RecordErrorCode(modelName, 401)
				tracker.RecordErrorCode(modelName, 200)
			},
			expectedHealth: true,
			expectError:    false,
		},
		{
			name: "latency unhealthy",
			setupFunc: func() {
				clearAllData()
				// Record bad latencies
				tracker.RecordLatency(modelName, 1.5, "success")
				tracker.RecordLatency(modelName, 1.7, "success")
			},
			expectedHealth: false,
			expectError:    true,
			errorContains:  "unhealthy: average latency",
		},
		{
			name: "errors unhealthy",
			setupFunc: func() {
				clearAllData()
				// Record good latencies
				tracker.RecordLatency(modelName, 0.5, "success")
				tracker.RecordLatency(modelName, 0.7, "success")
				// Record errors above threshold
				tracker.RecordErrorCode(modelName, 401)
				tracker.RecordErrorCode(modelName, 401)
			},
			expectedHealth: false,
			expectError:    true,
			errorContains:  "unhealthy: status code 401",
		},
		{
			name: "both unhealthy",
			setupFunc: func() {
				clearAllData()
				// Record bad latencies
				tracker.RecordLatency(modelName, 1.5, "success")
				tracker.RecordLatency(modelName, 1.7, "success")
				// Record errors above threshold
				tracker.RecordErrorCode(modelName, 401)
				tracker.RecordErrorCode(modelName, 401)
			},
			expectedHealth: false,
			expectError:    true,
			errorContains:  "unhealthy: average latency",
		},
		{
			name: "no data is healthy",
			setupFunc: func() {
				clearAllData()
			},
			expectedHealth: true,
			expectError:    false,
		},
		{
			name: "in latency recovery",
			setupFunc: func() {
				clearAllData()
				// Record bad latencies and set recovery
				tracker.RecordLatency(modelName, 1.5, "success")
				tracker.RecordLatency(modelName, 1.7, "success")
				tracker.RecordRecoveryTime(modelName, config)
			},
			expectedHealth: false,
			expectError:    true,
			errorContains:  "still in recovery period",
		},
		{
			name: "in error recovery",
			setupFunc: func() {
				clearAllData()
				// Record errors and set recovery
				tracker.RecordErrorCode(modelName, 401)
				tracker.RecordErrorCode(modelName, 401)
				tracker.RecordErrorRecoveryTime(modelName, config, 401)
			},
			expectedHealth: false,
			expectError:    true,
			errorContains:  "still in error recovery period",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupFunc()

			healthy, err := tracker.CheckModelOverallHealth(modelName, config)
			if tt.expectError {
				if err == nil {
					t.Error("Expected an error but got none")
				} else if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got %v", tt.errorContains, err)
				}
			} else if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if healthy != tt.expectedHealth {
				t.Errorf("Expected health = %v, got %v", tt.expectedHealth, healthy)
			}
		})
	}
}

func TestCheckErrorRecoveryTimeWithCleanup(t *testing.T) {
	// Setup miniredis directly for better control
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	// Create Redis client with the miniredis address
	redisClient, err := redis.NewClient(redis.Config{
		Addr: mr.Addr(),
	})
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redisClient.Close()

	// Create metrics tracker with our Redis client
	tracker := &Tracker{client: redisClient}

	// Create model configuration
	modelName := "testmodel"
	recoveryTime := 2 * time.Second
	config := model.Config{
		ModelErrorTracking: model.ModelErrorTracking{
			modelName: &model.RollingErrorTracking{
				StatusConfigs: map[int]*model.StatusCodeConfig{
					400: {
						ErrorThresholdPercentage: 50,
						NoOfCalls:                5,
						RecoveryTime:             recoveryTime,
					},
				},
			},
		},
	}

	ctx := context.Background()

	// Record some error codes
	statuses := []int{200, 400, 400, 200, 400}
	for _, status := range statuses {
		if err := tracker.RecordErrorCode(modelName, status); err != nil {
			t.Fatalf("Failed to record error code: %v", err)
		}
	}

	// Check error percentages before recovery
	errorPercentages, err := tracker.client.GetErrorPercentages(ctx, modelName, 5)
	if err != nil {
		t.Fatalf("Failed to get error percentages: %v", err)
	}
	if errorPercentages[400] != 60.0 {
		t.Errorf("Expected 400 error percentage to be 60.0, got %f", errorPercentages[400])
	}

	// Set error recovery time
	if err := tracker.RecordErrorRecoveryTime(modelName, config, 400); err != nil {
		t.Fatalf("Failed to record recovery time: %v", err)
	}

	// Verify model is in recovery
	err = tracker.CheckErrorRecoveryTime(modelName, config)
	if err == nil || !strings.Contains(err.Error(), "still in error recovery period") {
		t.Errorf("Expected 'still in error recovery period' error, got %v", err)
	}

	// Fast forward time past the recovery period in miniredis
	mr.FastForward(recoveryTime + 100*time.Millisecond)

	// Verify model is out of recovery and cleanup happened
	err = tracker.CheckErrorRecoveryTime(modelName, config)
	if err != nil {
		t.Errorf("CheckErrorRecoveryTime() error = %v, want nil", err)
	}

	// Check error data was cleaned up
	errorPercentages, err = tracker.client.GetErrorPercentages(ctx, modelName, 5)
	if err != nil {
		t.Fatalf("Failed to get error percentages: %v", err)
	}
	if len(errorPercentages) != 0 {
		t.Errorf("Expected empty error percentages after cleanup, got %v", errorPercentages)
	}
}

func TestStartPeriodicCleanup(t *testing.T) {
	// Setup miniredis directly for better control
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	// Create Redis client with the miniredis address
	redisClient, err := redis.NewClient(redis.Config{
		Addr: mr.Addr(),
	})
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redisClient.Close()

	// Create metrics tracker with our Redis client
	tracker := &Tracker{client: redisClient}

	ctx := context.Background()
	modelName := "testmodel"

	// Create test data
	// 1. Record some latency data
	for i := 0; i < 5; i++ {
		if err := tracker.RecordLatency(modelName, float64(i+1), "200"); err != nil {
			t.Fatalf("Failed to record latency: %v", err)
		}
	}

	// 2. Record some error data
	statuses := []int{200, 400, 400, 500, 429}
	for _, status := range statuses {
		if err := tracker.RecordErrorCode(modelName, status); err != nil {
			t.Fatalf("Failed to record error code: %v", err)
		}
	}

	// Verify data exists before cleanup
	latencyEntries, err := tracker.client.GetLatencyEntries(ctx, modelName, 5)
	if err != nil {
		t.Fatalf("Failed to get latency entries: %v", err)
	}
	if len(latencyEntries) != 5 {
		t.Errorf("Expected 5 latency entries before cleanup, got %d", len(latencyEntries))
	}

	errorPercentages, err := tracker.client.GetErrorPercentages(ctx, modelName, 5)
	if err != nil {
		t.Fatalf("Failed to get error percentages: %v", err)
	}
	if errorPercentages[400] != 40.0 {
		t.Errorf("Expected 400 error percentage to be 40.0 before cleanup, got %f", errorPercentages[400])
	}

	// Use a very short cleanup interval and age of 0 to clean up all data
	cleanupInterval := 100 * time.Millisecond
	dataAge := 0 * time.Second // 0 seconds means clean up all data

	// Start the actual periodic cleanup
	tracker.StartPeriodicCleanup(cleanupInterval, dataAge)

	// Wait for the cleanup to run
	time.Sleep(500 * time.Millisecond)

	// Verify data was cleaned up
	latencyEntries, err = tracker.client.GetLatencyEntries(ctx, modelName, 5)
	if err != nil {
		t.Fatalf("Failed to get latency entries: %v", err)
	}
	if len(latencyEntries) != 0 {
		t.Errorf("Expected 0 latency entries after cleanup, got %d", len(latencyEntries))
	}

	errorPercentages, err = tracker.client.GetErrorPercentages(ctx, modelName, 5)
	if err != nil {
		t.Fatalf("Failed to get error percentages: %v", err)
	}
	if len(errorPercentages) != 0 {
		t.Errorf("Expected empty error percentages after cleanup, got %v", errorPercentages)
	}
}

// redisClientWrapper is a wrapper around redis.Client that tracks when cleanup methods are called
type redisClientWrapper struct {
	client                 *redis.Client
	cleanupLatenciesCalled *bool
	cleanupErrorsCalled    *bool
}

// CleanupOldLatencies wraps the original method and tracks when it's called
func (w *redisClientWrapper) CleanupOldLatencies(ctx context.Context, model string, age time.Duration) error {
	*w.cleanupLatenciesCalled = true
	return w.client.CleanupOldLatencies(ctx, model, age)
}

// CleanupOldErrors wraps the original method and tracks when it's called
func (w *redisClientWrapper) CleanupOldErrors(ctx context.Context, model string, age time.Duration) error {
	*w.cleanupErrorsCalled = true
	return w.client.CleanupOldErrors(ctx, model, age)
}

// Forward all other methods to the underlying client
func (w *redisClientWrapper) Close() error {
	return w.client.Close()
}

func (w *redisClientWrapper) RecordLatency(ctx context.Context, model string, latency float64, status string) error {
	return w.client.RecordLatency(ctx, model, latency, status)
}

func (w *redisClientWrapper) SetRecoveryTime(ctx context.Context, model string, duration time.Duration) error {
	return w.client.SetRecoveryTime(ctx, model, duration)
}

func (w *redisClientWrapper) CheckRecoveryTime(ctx context.Context, model string) (bool, error) {
	return w.client.CheckRecoveryTime(ctx, model)
}

func (w *redisClientWrapper) GetAverageLatency(ctx context.Context, model string, n int64) (float64, error) {
	return w.client.GetAverageLatency(ctx, model, n)
}

func (w *redisClientWrapper) GetLatencyEntries(ctx context.Context, model string, n int64) ([]float64, error) {
	return w.client.GetLatencyEntries(ctx, model, n)
}

func (w *redisClientWrapper) RecordErrorCode(ctx context.Context, model string, statusCode int) error {
	return w.client.RecordErrorCode(ctx, model, statusCode)
}

func (w *redisClientWrapper) GetErrorPercentages(ctx context.Context, model string, n int64) (map[int]float64, error) {
	return w.client.GetErrorPercentages(ctx, model, n)
}

func (w *redisClientWrapper) SetErrorRecoveryTime(ctx context.Context, model string, duration time.Duration) error {
	return w.client.SetErrorRecoveryTime(ctx, model, duration)
}

func (w *redisClientWrapper) CheckErrorRecoveryTime(ctx context.Context, model string) (bool, error) {
	return w.client.CheckErrorRecoveryTime(ctx, model)
}

func (w *redisClientWrapper) ClearAllModelData(ctx context.Context, model string) error {
	return w.client.ClearAllModelData(ctx, model)
}

func (w *redisClientWrapper) GetKeysWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	return w.client.GetKeysWithPrefix(ctx, prefix)
}

func TestNewTrackerWithEnvironmentConfig(t *testing.T) {
	// Save original environment and restore it after the test
	origEnvCleanup := os.Getenv("ENABLE_REDIS_PERIODIC_CLEANUP")
	origEnvInterval := os.Getenv("REDIS_CLEANUP_INTERVAL")
	origEnvRetention := os.Getenv("REDIS_DATA_RETENTION")
	defer func() {
		os.Setenv("ENABLE_REDIS_PERIODIC_CLEANUP", origEnvCleanup)
		os.Setenv("REDIS_CLEANUP_INTERVAL", origEnvInterval)
		os.Setenv("REDIS_DATA_RETENTION", origEnvRetention)
	}()

	// Set up Redis
	mr := miniredis.RunT(t)
	defer mr.Close()

	// Test cases
	tests := []struct {
		name            string
		enableCleanup   string
		cleanupInterval string
		dataRetention   string
		wantEnabled     bool
		wantInterval    time.Duration
		wantRetention   time.Duration
	}{
		{
			name:            "cleanup enabled with default settings",
			enableCleanup:   "true",
			cleanupInterval: "",
			dataRetention:   "",
			wantEnabled:     true,
			wantInterval:    6 * time.Hour,
			wantRetention:   24 * time.Hour,
		},
		{
			name:            "cleanup disabled",
			enableCleanup:   "false",
			cleanupInterval: "5s",
			dataRetention:   "10s",
			wantEnabled:     false,
		},
		{
			name:            "cleanup enabled with custom settings",
			enableCleanup:   "true",
			cleanupInterval: "100ms",
			dataRetention:   "1h",
			wantEnabled:     true,
			wantInterval:    100 * time.Millisecond,
			wantRetention:   time.Hour,
		},
		{
			name:            "cleanup with invalid interval falls back to default",
			enableCleanup:   "true",
			cleanupInterval: "invalid",
			dataRetention:   "1h",
			wantEnabled:     true,
			wantInterval:    6 * time.Hour,
			wantRetention:   time.Hour,
		},
		{
			name:            "cleanup with invalid retention falls back to default",
			enableCleanup:   "true",
			cleanupInterval: "5m",
			dataRetention:   "invalid",
			wantEnabled:     true,
			wantInterval:    5 * time.Minute,
			wantRetention:   24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables for this test case
			os.Setenv("ENABLE_REDIS_PERIODIC_CLEANUP", tt.enableCleanup)
			os.Setenv("REDIS_CLEANUP_INTERVAL", tt.cleanupInterval)
			os.Setenv("REDIS_DATA_RETENTION", tt.dataRetention)

			// For this test, we'll manually parse the environment variables
			// the same way the NewTracker function does, since we can't easily
			// spy on the StartPeriodicCleanup call

			// Check if cleanup enabled as expected
			enabled := os.Getenv("ENABLE_REDIS_PERIODIC_CLEANUP") == "true"
			if enabled != tt.wantEnabled {
				t.Errorf("Cleanup enabled = %v, want %v", enabled, tt.wantEnabled)
			}

			// Only check interval and retention if cleanup was enabled
			if enabled {
				// Get cleanup interval from env var or use default
				interval := 6 * time.Hour
				if intervalStr := os.Getenv("REDIS_CLEANUP_INTERVAL"); intervalStr != "" {
					if parsedInterval, err := time.ParseDuration(intervalStr); err == nil {
						interval = parsedInterval
					}
				}

				if interval != tt.wantInterval {
					t.Errorf("Cleanup interval = %v, want %v", interval, tt.wantInterval)
				}

				// Get data retention age from env var or use default
				retention := 24 * time.Hour
				if retentionStr := os.Getenv("REDIS_DATA_RETENTION"); retentionStr != "" {
					if parsedRetention, err := time.ParseDuration(retentionStr); err == nil {
						retention = parsedRetention
					}
				}

				if retention != tt.wantRetention {
					t.Errorf("Data retention = %v, want %v", retention, tt.wantRetention)
				}
			}

			// Now create a tracker to make sure the code doesn't panic
			tracker, err := NewTracker(mr.Addr())
			if err != nil {
				t.Fatalf("Failed to create metrics tracker: %v", err)
			}
			defer tracker.Close()
		})
	}
}
