package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	SchemaVersion = 1

	ExitPass       = 0
	ExitJobsFailed = 1
	ExitUsage      = 2
	ExitCluster    = 3

	ClassHarness = "harness"
	ClassStorage = "storage"
	ClassUnknown = "unknown"
)

// AgentSummary is the small document an agent reads after a run.
// The same bytes are written to summary.json and stdout.
type AgentSummary struct {
	SchemaVersion int            `json:"schemaVersion"`
	Status        string         `json:"status"`
	ExitCode      int            `json:"exitCode"`
	ResultsDir    string         `json:"resultsDir,omitempty"`
	Report        string         `json:"report,omitempty"`
	Summary       Summary        `json:"summary"`
	Failures      []FailureBrief `json:"failures"`
	Error         string         `json:"error,omitempty"`
}

type FailureBrief struct {
	Pod             string  `json:"pod"`
	Job             string  `json:"job"`
	Storage         string  `json:"storage"`
	VolumeMode      string  `json:"volumeMode"`
	ExitCode        int     `json:"exitCode"`
	DurationSeconds float64 `json:"durationSeconds"`
	Error           string  `json:"error"`
	Classification  string  `json:"classification"`
}

// ClassifyFailure labels a job error for an agent.
// harness is the verify false-positive set described in the README:
// bad magic header, bad header length, and crc32c verify failed on Block.
func ClassifyFailure(volumeMode, errText string) string {
	e := strings.ToLower(errText)
	switch {
	case strings.Contains(e, "bad magic header"), strings.Contains(e, "bad header length"):
		return ClassHarness
	case strings.Contains(e, "crc32c") && strings.Contains(e, "verify failed") && strings.EqualFold(volumeMode, "Block"):
		return ClassHarness
	case strings.TrimSpace(errText) == "":
		return ClassUnknown
	default:
		return ClassStorage
	}
}

// BuildAgentSummary builds the agent document from collected jobs.
// reportWritten adds the report.json path. runErr is stored in error when set;
// a job-failure exit with a nil error gets "N job(s) failed".
func BuildAgentSummary(exitCode int, resultsDir string, results []JobResult, runErr error, reportWritten bool) AgentSummary {
	s := AgentSummary{
		SchemaVersion: SchemaVersion,
		Status:        "pass",
		ExitCode:      exitCode,
		ResultsDir:    resultsDir,
		Summary:       ComputeSummary(results),
		Failures:      []FailureBrief{},
	}
	if exitCode != ExitPass {
		s.Status = "fail"
	}
	if reportWritten && resultsDir != "" {
		s.Report = filepath.Join(resultsDir, "report.json")
	}
	for _, r := range results {
		if r.Status != "fail" {
			continue
		}
		s.Failures = append(s.Failures, FailureBrief{
			Pod:             r.Pod,
			Job:             r.Job,
			Storage:         r.Storage,
			VolumeMode:      r.VolumeMode,
			ExitCode:        r.ExitCode,
			DurationSeconds: roundSeconds(r.Duration.Seconds()),
			Error:           r.Error,
			Classification:  ClassifyFailure(r.VolumeMode, r.Error),
		})
	}
	if runErr != nil {
		s.Error = runErr.Error()
	}
	return s
}

func roundSeconds(sec float64) float64 {
	return math.Round(sec*1000) / 1000
}

func marshalSummary(s AgentSummary) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return nil, fmt.Errorf("marshal agent summary: %w", err)
	}
	return buf.Bytes(), nil
}

// EmitAgentSummary writes s to w and, when ResultsDir is set, to summary.json.
// Both copies are the same bytes.
func EmitAgentSummary(w io.Writer, s AgentSummary) error {
	if s.Failures == nil {
		s.Failures = []FailureBrief{}
	}
	data, err := marshalSummary(s)
	if err != nil {
		return err
	}
	var writeErr error
	if s.ResultsDir != "" {
		path := filepath.Join(s.ResultsDir, "summary.json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			writeErr = fmt.Errorf("write %s: %w", path, err)
		}
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write agent summary: %w", err)
	}
	return writeErr
}
