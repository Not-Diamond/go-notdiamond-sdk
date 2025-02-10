package notdiamond

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetricsTracker_RecordAndHealth(t *testing.T) {

	model := "openai/gpt-4o"
	metrics, err := NewMetricsTracker(":memory:" + t.Name())
	if err != nil {
		log.Fatalf("Failed to open database connection: %v", err)
	}
	// Record several latencies. For example, 1, 2, 3, and 4 seconds.
	latencies := []float64{1.0, 2.0, 3.0, 4.0} // average = 2.5 seconds
	for _, l := range latencies {
		err := metrics.RecordLatency(model, l)
		if err != nil {
			t.Errorf("RecordLatency error: %v", err)
		}
	}

	// Use thresholds: average_latency threshold = 3.0 sec, no_of_calls = 10, recovery_time = 10 minutes.
	config := &Config{
		NoOfCalls:           10,
		RecoveryTime:        10 * time.Minute,
		AvgLatencyThreshold: 3.0,
	}

	healthy, err := metrics.CheckModelHealth(model, config)
	if err != nil {
		t.Errorf("CheckModelHealth error: %v", err)
	}
	if !healthy {
		t.Errorf("Expected model %q to be healthy (avg=2.5 < threshold=3.0)", model)
	}

	// Record two high latency calls (e.g. 10 seconds each), which should push the average above the threshold.
	highLatencies := []float64{10.0, 10.0}
	for _, l := range highLatencies {
		err := metrics.RecordLatency(model, l)
		if err != nil {
			t.Errorf("RecordLatency error: %v", err)
		}
	}

	// Use thresholds: average_latency threshold = 3.0 sec, no_of_calls = 10, recovery_time = 10 minutes.
	config = &Config{
		NoOfCalls:           10,
		RecoveryTime:        10 * time.Minute,
		AvgLatencyThreshold: 3.0,
	}
	healthy, err = metrics.CheckModelHealth(model, config)
	if err != nil {
		t.Errorf("CheckModelHealth error: %v", err)
	}

	if healthy {
		t.Errorf("Expected model %q to be unhealthy (average latency too high)", model)
	}
}

// TestNewMetricsTracker verifies that a new metrics tracker is created and that the model_metrics table exists.
func TestNewMetricsTracker(t *testing.T) {
	// Use a temporary directory to isolate database files.
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_new")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	defer func(mt *MetricsTracker) {
		err := mt.Close()
		if err != nil {

		}
	}(mt)

	cols, err := mt.db.Columns("model_metrics")
	if err != nil {
		t.Fatalf("Failed to get columns for model_metrics: %v", err)
	}
	// When using MakeTables(true, ...), expect columns: id, timestamp, plus the columns we specified ("model" and "latency").
	expectedCols := []string{"id", "timestamp", "model", "latency"}
	for _, col := range expectedCols {
		found := false
		for _, c := range cols {
			if c == col {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected column %q in model_metrics, not found in %v", col, cols)
		}
	}
}

// TestRecordLatency verifies that RecordLatency inserts a record into the model_metrics table.
func TestRecordLatency(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_record")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	defer func(mt *MetricsTracker) {
		err := mt.Close()
		if err != nil {

		}
	}(mt)

	if err := mt.RecordLatency("model_record", 123.45); err != nil {
		t.Fatalf("RecordLatency() failed: %v", err)
	}

	// Query the table to verify a record exists.
	rows, err := mt.db.Query("SELECT latency FROM model_metrics WHERE model = ?", "model_record")
	if err != nil {
		t.Fatalf("Query() failed: %v", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {

		}
	}(rows)

	count := 0
	for rows.Next() {
		count++
	}
	if count == 0 {
		t.Errorf("Expected at least one record for model_record, got none")
	}
}

// TestCheckModelHealth_NoRecords verifies that CheckModelHealth returns healthy when no records exist.
func TestCheckModelHealth_NoRecords(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_no_records")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	defer func(mt *MetricsTracker) {
		err := mt.Close()
		if err != nil {

		}
	}(mt)

	config := &Config{
		AvgLatencyThreshold: 100,
		NoOfCalls:           5,
		RecoveryTime:        time.Minute,
	}
	healthy, err := mt.CheckModelHealth("nonexistent_model", config)
	if err != nil {
		t.Fatalf("CheckModelHealth() failed: %v", err)
	}
	if !healthy {
		t.Errorf("Expected model to be healthy when no records exist")
	}
}

// TestCheckModelHealth_UnderThreshold verifies that a model with low recorded latencies is considered healthy.
func TestCheckModelHealth_UnderThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_under")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	defer func(mt *MetricsTracker) {
		err := mt.Close()
		if err != nil {

		}
	}(mt)

	// Insert two records with low latency.
	if err := mt.RecordLatency("model_under", 50); err != nil {
		t.Fatalf("RecordLatency() failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // Ensure distinct timestamps.
	if err := mt.RecordLatency("model_under", 50); err != nil {
		t.Fatalf("RecordLatency() failed: %v", err)
	}

	config := &Config{
		AvgLatencyThreshold: 100,
		NoOfCalls:           5,
		RecoveryTime:        time.Minute,
	}
	healthy, err := mt.CheckModelHealth("model_under", config)
	if err != nil {
		t.Fatalf("CheckModelHealth() failed: %v", err)
	}
	if !healthy {
		t.Errorf("Expected model to be healthy with average latency below threshold")
	}
}

// TestCheckModelHealth_OverThreshold_NotRecovered verifies that a recent high-latency record makes the model unhealthy.
func TestCheckModelHealth_OverThreshold_NotRecovered(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_over_not_recovered")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	defer func(mt *MetricsTracker) {
		err := mt.Close()
		if err != nil {

		}
	}(mt)

	// Insert a record with high latency (current timestamp).
	if err := mt.RecordLatency("model_over", 200); err != nil {
		t.Fatalf("RecordLatency() failed: %v", err)
	}

	config := &Config{
		AvgLatencyThreshold: 100,
		NoOfCalls:           5,
		RecoveryTime:        time.Minute, // 1 minute recovery period
	}
	healthy, err := mt.CheckModelHealth("model_over", config)
	if err != nil {
		t.Fatalf("CheckModelHealth() failed: %v", err)
	}
	if healthy {
		t.Errorf("Expected model to be unhealthy due to high latency and insufficient recovery time")
	}
}

// TestCheckModelHealth_OverThreshold_Recovered verifies that a record older than the recovery time makes the model healthy.
func TestCheckModelHealth_OverThreshold_Recovered(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_over_recovered")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	defer func(mt *MetricsTracker) {
		err := mt.Close()
		if err != nil {

		}
	}(mt)

	// Manually insert a record with a timestamp older than the recovery time.
	oldTime := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano)
	err = mt.db.Exec("INSERT INTO model_metrics(timestamp, model, latency) VALUES(?, ?, ?)", oldTime, "model_recovered", 200)
	if err != nil {
		t.Fatalf("Manual insert failed: %v", err)
	}

	config := &Config{
		AvgLatencyThreshold: 100,
		NoOfCalls:           5,
		RecoveryTime:        time.Minute,
	}
	healthy, err := mt.CheckModelHealth("model_recovered", config)
	if err != nil {
		t.Fatalf("CheckModelHealth() failed: %v", err)
	}
	if !healthy {
		t.Errorf("Expected model to be healthy since recovery time has elapsed")
	}
}

// TestCheckModelHealth_MaxNoOfCalls verifies that config.NoOfCalls is clamped to 10 when set too high.
func TestCheckModelHealth_MaxNoOfCalls(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_maxcalls")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	defer func(mt *MetricsTracker) {
		err := mt.Close()
		if err != nil {

		}
	}(mt)

	// Insert 12 records with high latency.
	for i := 0; i < 12; i++ {
		if err := mt.RecordLatency("model_max", 200); err != nil {
			t.Fatalf("RecordLatency() failed: %v", err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	config := &Config{
		AvgLatencyThreshold: 150, // Threshold lower than the inserted latency.
		NoOfCalls:           15,  // Should be clamped to 10.
		RecoveryTime:        time.Minute,
	}
	healthy, err := mt.CheckModelHealth("model_max", config)
	if err != nil {
		t.Fatalf("CheckModelHealth() failed: %v", err)
	}
	if healthy {
		t.Errorf("Expected model to be unhealthy with high average latency using maximum of 10 calls")
	}
}

// TestCheckModelHealth_RecoveryTimeClamped verifies that a RecoveryTime above one hour is clamped.
func TestCheckModelHealth_RecoveryTimeClamped(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_recovery_clamped")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	defer func(mt *MetricsTracker) {
		err := mt.Close()
		if err != nil {

		}
	}(mt)

	// Insert a record with high latency and a timestamp older than 1 hour.
	oldTime := time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339Nano)
	err = mt.db.Exec("INSERT INTO model_metrics(timestamp, model, latency) VALUES(?, ?, ?)", oldTime, "model_clamped", 200)
	if err != nil {
		t.Fatalf("Manual insert failed: %v", err)
	}

	// Set RecoveryTime to 2 hours; it should be clamped to 1 hour.
	config := &Config{
		AvgLatencyThreshold: 100,
		NoOfCalls:           5,
		RecoveryTime:        2 * time.Hour,
	}
	healthy, err := mt.CheckModelHealth("model_clamped", config)
	if err != nil {
		t.Fatalf("CheckModelHealth() failed: %v", err)
	}
	if !healthy {
		t.Errorf("Expected model to be healthy since RecoveryTime is clamped to 1 hour and the record is older than 1 hour")
	}

	// Now insert a recent high-latency record.
	recentTime := time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339Nano)
	err = mt.db.Exec("INSERT INTO model_metrics(timestamp, model, latency) VALUES(?, ?, ?)", recentTime, "model_clamped", 200)
	if err != nil {
		t.Fatalf("Manual insert failed: %v", err)
	}
	healthy, err = mt.CheckModelHealth("model_clamped", config)
	if err != nil {
		t.Fatalf("CheckModelHealth() failed: %v", err)
	}
	if healthy {
		t.Errorf("Expected model to be unhealthy due to a recent high-latency record")
	}
}

// TestCloseMetricsTracker verifies that after closing the metrics tracker, operations fail.
func TestCloseMetricsTracker(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	mt, err := NewMetricsTracker("test_metrics_close")
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}

	if err := mt.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Attempting to record latency after Close should fail.
	err = mt.RecordLatency("model_close", 100)
	if err == nil {
		t.Errorf("Expected error when calling RecordLatency after Close, got nil")
	}
}

// TestDropMetricsTracker verifies that Drop closes the database and removes the underlying file.
func TestDropMetricsTracker(t *testing.T) {
	tmpDir := t.TempDir()
	DataFolder, _ = filepath.Abs(tmpDir)

	dbPath := "test_metrics_drop"
	mt, err := NewMetricsTracker(dbPath)
	if err != nil {
		t.Fatalf("NewMetricsTracker() failed: %v", err)
	}
	dbFile := mt.db.Schema

	if err := mt.Drop(); err != nil {
		t.Fatalf("Drop() failed: %v", err)
	}
	// Verify that the database file no longer exists.
	if _, err := os.Stat(dbFile); !os.IsNotExist(err) {
		t.Errorf("Expected database file %q to be removed after Drop(), but it exists", dbFile)
	}
}
