package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps the Redis client with our specific operations
type Client struct {
	rdb *redis.Client
}

// Config holds Redis connection configuration
type Config struct {
	Addr     string
	Password string
	DB       int
}

// NewClient creates a new Redis client with the given configuration
func NewClient(cfg Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Test connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %v", err)
	}

	return &Client{rdb: rdb}, nil
}

// Close closes the Redis connection
func (c *Client) Close() error {
	return c.rdb.Close()
}

// RecordLatency records a model's latency with timestamp
func (c *Client) RecordLatency(ctx context.Context, model string, latency float64, status string) error {
	// Key format: latency:model
	key := fmt.Sprintf("latency:%s", model)

	// Create metric entry
	entry := map[string]interface{}{
		"timestamp": time.Now().UTC(),
		"latency":   latency,
		"status":    status,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal latency entry: %v", err)
	}

	// Add to sorted set with timestamp as score for easy retrieval/cleanup
	score := float64(time.Now().UTC().Unix())
	if err := c.rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: string(data)}).Err(); err != nil {
		return fmt.Errorf("failed to record latency: %v", err)
	}

	// Also increment the counter for this model
	counterKey := fmt.Sprintf("latency:%s:counter", model)
	if err := c.rdb.Incr(ctx, counterKey).Err(); err != nil {
		return fmt.Errorf("failed to increment counter: %v", err)
	}

	return nil
}

// SetRecoveryTime sets a recovery time for a model with automatic expiration
func (c *Client) SetRecoveryTime(ctx context.Context, model string, duration time.Duration) error {
	key := fmt.Sprintf("latency:%s:recovery", model)
	latencyKey := fmt.Sprintf("latency:%s", model)
	counterKey := fmt.Sprintf("latency:%s:counter", model)

	// Clean up existing data
	if err := c.rdb.Del(ctx, latencyKey, counterKey).Err(); err != nil {
		return fmt.Errorf("failed to clean latency data: %v", err)
	}

	// Store recovery time with expiration
	recoveryEnd := time.Now().UTC().Add(duration)
	return c.rdb.Set(ctx, key, recoveryEnd.Format(time.RFC3339), duration).Err()
}

// CheckRecoveryTime checks if a model is still in recovery period
func (c *Client) CheckRecoveryTime(ctx context.Context, model string) (bool, error) {
	key := fmt.Sprintf("latency:%s:recovery", model)

	// Check if recovery key exists
	exists, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check recovery time: %v", err)
	}

	return exists == 1, nil
}

// GetAverageLatency calculates average latency for last N calls
func (c *Client) GetAverageLatency(ctx context.Context, model string, n int64) (float64, error) {
	key := fmt.Sprintf("latency:%s", model)
	counterKey := fmt.Sprintf("latency:%s:counter", model)

	// Get the current count
	count, err := c.rdb.Get(ctx, counterKey).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get counter: %v", err)
	}

	// Get last N entries
	entries, err := c.rdb.ZRevRange(ctx, key, 0, count-1).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get latency entries: %v", err)
	}

	if len(entries) == 0 {
		return 0, nil
	}

	// Only consider the last N entries if we have more than N
	if int64(len(entries)) > n {
		entries = entries[:n]
	}

	var totalLatency float64
	for _, entry := range entries {
		var metric map[string]interface{}
		if err := json.Unmarshal([]byte(entry), &metric); err != nil {
			return 0, fmt.Errorf("failed to unmarshal latency entry: %v", err)
		}
		totalLatency += metric["latency"].(float64)
	}

	return totalLatency / float64(len(entries)), nil
}

// CleanupOldLatencies removes latency entries older than the specified duration
func (c *Client) CleanupOldLatencies(ctx context.Context, model string, age time.Duration) error {
	key := fmt.Sprintf("latency:%s", model)
	min := "-inf"
	max := fmt.Sprintf("%d", time.Now().Add(-age).Unix())

	// Remove old entries
	if err := c.rdb.ZRemRangeByScore(ctx, key, min, max).Err(); err != nil {
		return fmt.Errorf("failed to remove old latencies: %v", err)
	}

	return nil
}

// GetLatencyEntries retrieves the last N latency entries for a model
func (c *Client) GetLatencyEntries(ctx context.Context, model string, n int64) ([]float64, error) {
	key := fmt.Sprintf("latency:%s", model)
	counterKey := fmt.Sprintf("latency:%s:counter", model)

	// Get the current count
	count, err := c.rdb.Get(ctx, counterKey).Int64()
	if err == redis.Nil {
		return []float64{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get counter: %v", err)
	}

	// Get last N entries
	entries, err := c.rdb.ZRevRange(ctx, key, 0, count-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get latency entries: %v", err)
	}

	// Only consider the last N entries if we have more than N
	if int64(len(entries)) > n {
		entries = entries[:n]
	}

	latencies := make([]float64, 0, len(entries))
	for _, entry := range entries {
		var metric map[string]interface{}
		if err := json.Unmarshal([]byte(entry), &metric); err != nil {
			return nil, fmt.Errorf("failed to parse latency entry: %v", err)
		}
		latencies = append(latencies, metric["latency"].(float64))
	}

	return latencies, nil
}

// RecordErrorCode records a model's error code with timestamp
func (c *Client) RecordErrorCode(ctx context.Context, model string, statusCode int) error {
	// Key format: errors:model
	key := fmt.Sprintf("errors:%s", model)

	// Create metric entry
	entry := map[string]interface{}{
		"timestamp":   time.Now().UTC(),
		"status_code": statusCode,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal error entry: %v", err)
	}

	// Add to sorted set with timestamp as score for easy retrieval/cleanup
	score := float64(time.Now().UTC().Unix())
	if err := c.rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: string(data)}).Err(); err != nil {
		return fmt.Errorf("failed to record error: %v", err)
	}

	// Also increment the counter for this model
	counterKey := fmt.Sprintf("errors:%s:counter", model)
	if err := c.rdb.Incr(ctx, counterKey).Err(); err != nil {
		return fmt.Errorf("failed to increment counter: %v", err)
	}

	return nil
}

// GetErrorPercentages calculates the percentage of error codes in the last N calls
func (c *Client) GetErrorPercentages(ctx context.Context, model string, n int64) (map[int]float64, error) {
	// Validate input
	if n < 0 {
		return nil, fmt.Errorf("number of calls cannot be negative: %d", n)
	}
	if n == 0 {
		return make(map[int]float64), nil
	}

	key := fmt.Sprintf("errors:%s", model)
	recoveryKey := fmt.Sprintf("errors:%s:recovery", model)
	counterKey := fmt.Sprintf("errors:%s:counter", model)

	// Check if we have any entries
	exists, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to check if key exists: %v", err)
	}
	if exists == 0 {
		return make(map[int]float64), nil
	}

	// Get the last recovery time if it exists
	var lastRecoveryTime time.Time
	recoveryTimeStr, err := c.rdb.Get(ctx, recoveryKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get recovery time: %v", err)
	}
	if err == nil {
		lastRecoveryTime, err = time.Parse(time.RFC3339, recoveryTimeStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse recovery time: %v", err)
		}
		// If recovery time is in the past, clean up all associated data
		if lastRecoveryTime.Before(time.Now().UTC()) {
			// Clean up all error data
			if err := c.rdb.Del(ctx, recoveryKey, key, counterKey).Err(); err != nil {
				slog.Info("Failed to clean up expired error data", "error", err)
			}
			return make(map[int]float64), nil
		}
	}

	// Get all entries to ensure we have enough valid ones after filtering
	entries, err := c.rdb.ZRevRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get error entries: %v", err)
	}

	if len(entries) == 0 {
		return make(map[int]float64), nil
	}

	// Count occurrences of each status code
	statusCodeCounts := make(map[int]int)
	validEntries := 0

	for _, entry := range entries {
		var metric map[string]interface{}
		if err := json.Unmarshal([]byte(entry), &metric); err != nil {
			return nil, fmt.Errorf("failed to parse error entry: %v", err)
		}

		// Parse the timestamp
		timestamp, err := time.Parse(time.RFC3339, metric["timestamp"].(string))
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %v", err)
		}

		// Only consider entries after the last recovery time
		if !lastRecoveryTime.IsZero() && timestamp.Before(lastRecoveryTime) {
			continue
		}

		statusCode := int(metric["status_code"].(float64))
		statusCodeCounts[statusCode]++
		validEntries++

		// Stop if we have enough entries
		if validEntries >= int(n) {
			break
		}
	}

	// Calculate percentages with all valid entries we have
	statusCodePercentages := make(map[int]float64)
	if validEntries > 0 {
		for statusCode, count := range statusCodeCounts {
			statusCodePercentages[statusCode] = float64(count) / float64(validEntries) * 100
		}
	}

	return statusCodePercentages, nil
}

// CleanupOldErrors removes error entries older than the specified duration
func (c *Client) CleanupOldErrors(ctx context.Context, model string, age time.Duration) error {
	key := fmt.Sprintf("errors:%s", model)
	counterKey := fmt.Sprintf("errors:%s:counter", model)
	min := "-inf"
	max := fmt.Sprintf("%d", time.Now().Add(-age).Unix())

	// Remove old entries
	if err := c.rdb.ZRemRangeByScore(ctx, key, min, max).Err(); err != nil {
		return fmt.Errorf("failed to remove old errors: %v", err)
	}

	// Check if any entries remain
	count, err := c.rdb.ZCard(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to check remaining entries: %v", err)
	}

	// If no entries remain, reset the counter
	if count == 0 {
		if err := c.rdb.Del(ctx, counterKey).Err(); err != nil {
			return fmt.Errorf("failed to reset counter: %v", err)
		}
	}

	return nil
}

// SetErrorRecoveryTime sets a recovery time for a model with automatic expiration
func (c *Client) SetErrorRecoveryTime(ctx context.Context, model string, duration time.Duration) error {
	key := fmt.Sprintf("errors:%s:recovery", model)
	errorsKey := fmt.Sprintf("errors:%s", model)
	counterKey := fmt.Sprintf("errors:%s:counter", model)

	// Clean up existing data
	if err := c.rdb.Del(ctx, errorsKey, counterKey).Err(); err != nil {
		return fmt.Errorf("failed to clean error data: %v", err)
	}

	// Store recovery time with expiration
	recoveryEnd := time.Now().UTC().Add(duration)
	return c.rdb.Set(ctx, key, recoveryEnd.Format(time.RFC3339), duration).Err()
}

// CheckErrorRecoveryTime checks if a model is still in error recovery period
func (c *Client) CheckErrorRecoveryTime(ctx context.Context, model string) (bool, error) {
	key := fmt.Sprintf("errors:%s:recovery", model)

	// Check if recovery key exists
	exists, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check error recovery time: %v", err)
	}

	return exists == 1, nil
}

// ClearAllModelData deletes all data associated with a model
func (c *Client) ClearAllModelData(ctx context.Context, model string) error {
	// Delete all keys associated with the model
	keys := []string{
		fmt.Sprintf("latency:%s:recovery", model),
		fmt.Sprintf("errors:%s:recovery", model),
		fmt.Sprintf("latency:%s", model),
		fmt.Sprintf("latency:%s:counter", model),
		fmt.Sprintf("errors:%s", model),
		fmt.Sprintf("errors:%s:counter", model),
	}
	return c.rdb.Del(ctx, keys...).Err()
}

// GetKeysWithPrefix returns all Redis keys that match the given prefix pattern
func (c *Client) GetKeysWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	// Use the KEYS command with a pattern
	// Note: This is okay for maintenance operations like cleanup, but should be used carefully in production
	// with large datasets as it can block Redis
	keys, err := c.rdb.Keys(ctx, prefix).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys with prefix %s: %v", prefix, err)
	}
	return keys, nil
}
