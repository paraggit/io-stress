package workload

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

func TestFinalize_JobFailureWritesReportAndSummary(t *testing.T) {
	dir := t.TempDir()
	results := []report.JobResult{
		{
			Pod: "odf-io-rbd-0", Job: "phase3-verify", Storage: "rbd", VolumeMode: "Block",
			Status: "fail", ExitCode: 1, Duration: 2 * time.Second,
			Error: "verify: bad magic header 0, wanted acca",
		},
		{Pod: "odf-io-rbd-1", Job: "seq-write", Status: "pass", Duration: time.Second},
	}
	var stdout bytes.Buffer
	err := finalize(&stdout, dir, results, nil, finalizeOpts{writeReport: true, printHuman: false})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != report.ExitJobsFailed || !exit.Emitted {
		t.Fatalf("err = %v, want ExitError code %d emitted", err, report.ExitJobsFailed)
	}

	if _, statErr := os.Stat(filepath.Join(dir, "report.json")); statErr != nil {
		t.Fatal(statErr)
	}
	onDisk, readErr := os.ReadFile(filepath.Join(dir, "summary.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if stdout.String() != string(onDisk) {
		t.Fatalf("stdout and summary.json differ")
	}
	var summary report.AgentSummary
	if err := json.Unmarshal(onDisk, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Status != "fail" || summary.ExitCode != report.ExitJobsFailed {
		t.Fatalf("summary = %+v", summary)
	}
	if len(summary.Failures) != 1 || summary.Failures[0].Classification != report.ClassHarness {
		t.Fatalf("failures = %+v", summary.Failures)
	}
	if summary.Report != filepath.Join(dir, "report.json") {
		t.Fatalf("report = %q", summary.Report)
	}
}

func TestFinalize_Pass(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer
	err := finalize(&stdout, dir, []report.JobResult{
		{Pod: "p", Job: "j", Status: "pass"},
	}, nil, finalizeOpts{writeReport: true, printHuman: false})
	if err != nil {
		t.Fatal(err)
	}
	var summary report.AgentSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Status != "pass" || summary.ExitCode != report.ExitPass || len(summary.Failures) != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestEmitUsage_PrintsJSON(t *testing.T) {
	var stdout bytes.Buffer
	code := EmitUsage(&stdout, errors.New("runtime must be >= 1, got 0"))
	if code != report.ExitUsage {
		t.Fatalf("code = %d, want %d", code, report.ExitUsage)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(">=")) || bytes.Contains(stdout.Bytes(), []byte(`\u003e`)) {
		t.Fatalf("error was HTML-escaped: %s", stdout.Bytes())
	}
	var summary report.AgentSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.ExitCode != report.ExitUsage || summary.Error != "runtime must be >= 1, got 0" || summary.Status != "fail" {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestFinalize_InfraAndCleanup(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer
	err := finalize(&stdout, dir, []report.JobResult{
		{Pod: "w", Job: "write", Category: "object-write", Status: "infra-fail", Error: "OOMKilled"},
	}, nil, finalizeOpts{writeReport: true})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != report.ExitInfra {
		t.Fatalf("infra err = %v", err)
	}

	stdout.Reset()
	err = finalize(&stdout, dir, []report.JobResult{
		{Pod: "obc", Job: "teardown", Category: "object-teardown", Status: "cleanup-fail", Error: "finalizers=[noobaa.io/obc]"},
	}, nil, finalizeOpts{writeReport: true})
	if !errors.As(err, &exit) || exit.Code != report.ExitCleanup {
		t.Fatalf("cleanup err = %v", err)
	}
	var summary report.AgentSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Failures) != 1 || summary.Failures[0].Classification != report.ClassCleanup {
		t.Fatalf("failures = %+v", summary.Failures)
	}

	err = finalize(&stdout, dir, nil, &UsageError{Err: errors.New("projected bytes exceed free capacity")}, finalizeOpts{})
	if !errors.As(err, &exit) || exit.Code != report.ExitUsage {
		t.Fatalf("usage err = %v", err)
	}
}

func TestFinalize_ClusterBeforeJobs(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer
	err := finalize(&stdout, dir, nil, errors.New("no pods became Ready"), finalizeOpts{})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != report.ExitCluster {
		t.Fatalf("err = %v, want cluster exit", err)
	}
	var summary report.AgentSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Error != "no pods became Ready" || summary.Report != "" || summary.ExitCode != report.ExitCluster {
		t.Fatalf("summary = %+v", summary)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "report.json")); !os.IsNotExist(statErr) {
		t.Fatalf("report.json should not exist, stat err = %v", statErr)
	}
}
