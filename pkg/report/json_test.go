package report

import (
	"encoding/json"
	"testing"
	"time"
)

func TestJobResultVdbenchOutputMarshaling(t *testing.T) {
	// Test that Vdbench output (stored as JSON-marshaled string in FIOOutput) round-trips properly
	vdbenchOutput := "VDBench output log\nSome performance data\nCompleted successfully"

	// Create JobResult with Vdbench-like output (marshal stdout as JSON string)
	result := JobResult{
		Pod:      "test-pod",
		Job:      "test-pattern",
		Category: "vdbench",
		Status:   "pass",
		ExitCode: 0,
		Duration: time.Second * 30,
	}
	result.FIOOutput, _ = json.Marshal(vdbenchOutput)

	// Test JSON marshaling succeeds
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal JobResult with vdbench output: %v", err)
	}

	// Test JSON unmarshaling round-trip
	var result2 JobResult
	err = json.Unmarshal(data, &result2)
	if err != nil {
		t.Fatalf("Failed to unmarshal JobResult: %v", err)
	}

	// Verify FIOOutput round-trips correctly
	var unmarshaledOutput string
	err = json.Unmarshal(result2.FIOOutput, &unmarshaledOutput)
	if err != nil {
		t.Fatalf("Failed to unmarshal FIOOutput as string: %v", err)
	}

	if unmarshaledOutput != vdbenchOutput {
		t.Errorf("Expected output %q, got %q", vdbenchOutput, unmarshaledOutput)
	}
}
