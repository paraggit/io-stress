package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCollectorConcurrent(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Add(JobResult{
				Pod:    "pod-1",
				Job:    "job",
				Status: "pass",
			})
		}(i)
	}
	wg.Wait()
	results := c.Results()
	if len(results) != 100 {
		t.Errorf("got %d results, want 100", len(results))
	}
}

func TestComputeSummary(t *testing.T) {
	results := []JobResult{
		{Job: "a", Category: "stress", Status: "pass"},
		{Job: "b", Category: "stress", Status: "fail"},
		{Job: "c", Category: "stress", Status: "skip"},
		{Job: "d", Category: "lifecycle", Status: "pass"},
		{Job: "e", Category: "lifecycle", Status: "fail"},
	}
	s := ComputeSummary(results)
	if s.Total != 5 {
		t.Errorf("Total = %d, want 5", s.Total)
	}
	if s.Passed != 2 {
		t.Errorf("Passed = %d, want 2", s.Passed)
	}
	if s.Failed != 2 {
		t.Errorf("Failed = %d, want 2", s.Failed)
	}
	if s.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", s.Skipped)
	}
	if s.Lifecycle.Total != 2 {
		t.Errorf("Lifecycle.Total = %d, want 2", s.Lifecycle.Total)
	}
	if s.Phase1.Total != 3 {
		t.Errorf("Phase1.Total = %d, want 3", s.Phase1.Total)
	}
}

func TestWriteJSON(t *testing.T) {
	dir := t.TempDir()
	rpt := &RunReport{
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Results: []JobResult{
			{Pod: "p1", Job: "j1", Status: "pass"},
		},
		Summary: ComputeSummary([]JobResult{{Status: "pass"}}),
	}
	if err := WriteJSON(dir, rpt); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded RunReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Results) != 1 {
		t.Errorf("decoded %d results, want 1", len(decoded.Results))
	}
}

func TestWriteJobFile(t *testing.T) {
	dir := t.TempDir()
	r := JobResult{
		Pod:       "rbd-pod-1",
		Job:       "unaligned-direct",
		Status:    "pass",
		FIOOutput: json.RawMessage(`{"key":"value"}`),
	}
	if err := WriteJobFile(dir, r); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rbd-pod-1-unaligned-direct.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("job file not created: %v", err)
	}
}
