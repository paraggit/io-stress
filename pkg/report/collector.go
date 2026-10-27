package report

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

type JobResult struct {
	Pod          string          `json:"pod"`
	Job          string          `json:"job"`
	Category     string          `json:"category"`
	Storage      string          `json:"storage"`
	VolumeMode   string          `json:"volumeMode"`
	Status       string          `json:"status"`
	ExitCode     int             `json:"exitCode"`
	Duration     time.Duration   `json:"duration"`
	FIOOutput    json.RawMessage `json:"fioOutput,omitempty"`
	ObjectOutput *ObjectResult   `json:"objectOutput,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// ObjectResult is one object-workload worker result.
type ObjectResult struct {
	Bucket         string            `json:"bucket"`
	Phase          string            `json:"phase"`
	Worker         int               `json:"worker"`
	Seed           string            `json:"seed"`
	Capabilities   map[string]string `json:"capabilities,omitempty"`
	SkipReason     string            `json:"skipReason,omitempty"`
	ObjectsOK      uint64            `json:"objectsOk"`
	ObjectsFail    uint64            `json:"objectsFail"`
	BytesMoved     uint64            `json:"bytesMoved"`
	Ops            map[string]OpStat `json:"ops"`
	S3Errors       map[string]uint64 `json:"s3Errors"`
	S3ErrorSamples map[string]string `json:"s3ErrorSamples,omitempty"`
	ProbeDetail    map[string]string `json:"probeDetail,omitempty"`
	Retries        uint64            `json:"retries"`
	Mismatches     []Mismatch        `json:"mismatches,omitempty"`
	MismatchMore   uint64            `json:"mismatchMore,omitempty"`
	Error          string            `json:"error,omitempty"`
}

// OpStat is the latency summary for one S3 operation.
type OpStat struct {
	Count uint64  `json:"count"`
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	P99Ms float64 `json:"p99Ms"`
	MaxMs float64 `json:"maxMs"`
}

// Mismatch is one byte-level verification failure, capped per result.
type Mismatch struct {
	Key          string `json:"key"`
	VersionID    string `json:"versionId,omitempty"`
	Size         int64  `json:"size"`
	ExpectedSHA  string `json:"expectedSha256"`
	ActualSHA    string `json:"actualSha256"`
	FirstBadByte int64  `json:"firstBadByte"`
	ExpectedHex  string `json:"expectedHex"`
	ActualHex    string `json:"actualHex"`
	ExpectedETag string `json:"expectedEtag,omitempty"`
	ActualETag   string `json:"actualEtag,omitempty"`
	RequestID    string `json:"requestId,omitempty"`
}

type PhaseSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Slow    int `json:"slow,omitempty"`
	Infra   int `json:"infra,omitempty"`
	Cleanup int `json:"cleanup,omitempty"`
}

type Summary struct {
	Total     int          `json:"total"`
	Passed    int          `json:"passed"`
	Failed    int          `json:"failed"`
	Skipped   int          `json:"skipped"`
	Slow      int          `json:"slow,omitempty"`
	Infra     int          `json:"infra,omitempty"`
	Cleanup   int          `json:"cleanup,omitempty"`
	Phase1    PhaseSummary `json:"phase1"`
	Lifecycle PhaseSummary `json:"lifecycle"`
	Object    PhaseSummary `json:"object,omitempty"`
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
		ps := summaryPhase(r.Category, &s)
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
		case "slow", "retryable":
			s.Slow++
			ps.Slow++
		case "infra-fail":
			s.Infra++
			ps.Infra++
		case "cleanup-fail":
			s.Cleanup++
			ps.Cleanup++
		}
	}
	return s
}

// summaryPhase routes a job category onto a summary bucket.
// lifecycle stays on its own phase, object-* categories stay on the object
// phase, and every other category stays on phase 1.
func summaryPhase(category string, s *Summary) *PhaseSummary {
	switch {
	case category == "lifecycle":
		return &s.Lifecycle
	case strings.HasPrefix(category, "object-"):
		return &s.Object
	default:
		return &s.Phase1
	}
}
