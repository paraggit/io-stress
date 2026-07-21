package report

import (
	"encoding/json"
	"sync"
	"time"
)

type JobResult struct {
	Pod        string          `json:"pod"`
	Job        string          `json:"job"`
	Category   string          `json:"category"`
	Storage    string          `json:"storage"`
	VolumeMode string          `json:"volumeMode"`
	Status     string          `json:"status"`
	ExitCode   int             `json:"exitCode"`
	Duration   time.Duration   `json:"duration"`
	FIOOutput  json.RawMessage `json:"fioOutput,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type PhaseSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type Summary struct {
	Total     int          `json:"total"`
	Passed    int          `json:"passed"`
	Failed    int          `json:"failed"`
	Skipped   int          `json:"skipped"`
	Phase1    PhaseSummary `json:"phase1"`
	Lifecycle PhaseSummary `json:"lifecycle"`
}

type RunReport struct {
	StartTime time.Time   `json:"startTime"`
	EndTime   time.Time   `json:"endTime"`
	Results   []JobResult `json:"results"`
	Summary   Summary     `json:"summary"`
}

type Collector struct {
	mu      sync.Mutex
	results []JobResult
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Add(r JobResult) {
	c.mu.Lock()
	c.results = append(c.results, r)
	c.mu.Unlock()
}

func (c *Collector) Results() []JobResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]JobResult, len(c.results))
	copy(out, c.results)
	return out
}

func ComputeSummary(results []JobResult) Summary {
	var s Summary
	for _, r := range results {
		s.Total++
		ps := &s.Phase1
		if r.Category == "lifecycle" {
			ps = &s.Lifecycle
		}
		ps.Total++
		switch r.Status {
		case "pass":
			s.Passed++
			ps.Passed++
		case "fail":
			s.Failed++
			ps.Failed++
		case "skip":
			s.Skipped++
			ps.Skipped++
		}
	}
	return s
}
