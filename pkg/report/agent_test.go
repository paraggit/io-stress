package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		name       string
		volumeMode string
		errText    string
		want       string
	}{
		{"magic header", "Block", "verify: bad magic header 0, wanted acca", ClassHarness},
		{"header length", "Filesystem", "verify: bad header length 4096, wanted 262144", ClassHarness},
		{"crc32c on block", "Block", "crc32c: verify failed at offset 4096", ClassHarness},
		{"crc32c on filesystem", "Filesystem", "crc32c: verify failed at offset 4096", ClassStorage},
		{"io error", "Block", "fio: error=Input/output error", ClassStorage},
		{"empty", "Block", "", ClassUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyFailure(tc.volumeMode, tc.errText); got != tc.want {
				t.Fatalf("ClassifyFailure() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildAgentSummary_JobFailure(t *testing.T) {
	results := []JobResult{
		{
			Pod:        "odf-io-rbd-0",
			Job:        "phase3-verify",
			Storage:    "rbd",
			VolumeMode: "Block",
			Status:     "fail",
			ExitCode:   1,
			Duration:   1500 * time.Millisecond,
			Error:      "verify: bad magic header 0, wanted acca",
		},
		{
			Pod:        "odf-io-rbd-1",
			Job:        "seq-write",
			Storage:    "rbd",
			VolumeMode: "Filesystem",
			Status:     "pass",
			ExitCode:   0,
			Duration:   time.Second,
		},
		{
			Pod:      "odf-io-rbd-2",
			Job:      "clone-bound",
			Category: "lifecycle",
			Status:   "slow",
			Duration: 30 * time.Second,
		},
	}

	s := BuildAgentSummary(ExitJobsFailed, "results/run1", results, nil, true)

	if s.SchemaVersion != 1 || s.Status != "fail" || s.ExitCode != ExitJobsFailed {
		t.Fatalf("summary identity = %+v", s)
	}
	if s.ResultsDir != "results/run1" || s.Report != "results/run1/report.json" {
		t.Fatalf("paths = dir %q report %q", s.ResultsDir, s.Report)
	}
	if s.Error != "" {
		t.Fatalf("error = %q, want empty", s.Error)
	}
	if s.Summary.Failed != 1 || s.Summary.Passed != 1 || s.Summary.Slow != 1 || s.Summary.Total != 3 {
		t.Fatalf("counts = %+v", s.Summary)
	}
	if len(s.Failures) != 1 {
		t.Fatalf("failures = %d, want 1", len(s.Failures))
	}
	f := s.Failures[0]
	if f.Pod != "odf-io-rbd-0" || f.Job != "phase3-verify" || f.Storage != "rbd" || f.VolumeMode != "Block" {
		t.Fatalf("failure identity = %+v", f)
	}
	if f.ExitCode != 1 || f.DurationSeconds != 1.5 || f.Classification != ClassHarness {
		t.Fatalf("failure detail = %+v", f)
	}
	if f.Error != "verify: bad magic header 0, wanted acca" {
		t.Fatalf("failure error = %q", f.Error)
	}
}

func TestBuildAgentSummary_UsageAndCluster(t *testing.T) {
	usage := BuildAgentSummary(ExitUsage, "", nil, errors.New("namespace must not be empty"), false)
	if usage.Status != "fail" || usage.ExitCode != ExitUsage || usage.Error != "namespace must not be empty" {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.Report != "" || len(usage.Failures) != 0 {
		t.Fatalf("usage should have no report and no failures: %+v", usage)
	}

	cluster := BuildAgentSummary(ExitCluster, "results/run1", nil, errors.New("create k8s client: denied"), false)
	if cluster.ExitCode != ExitCluster || cluster.Report != "" || cluster.ResultsDir != "results/run1" {
		t.Fatalf("cluster = %+v", cluster)
	}
	if cluster.Error != "create k8s client: denied" {
		t.Fatalf("cluster error = %q", cluster.Error)
	}
}

func TestBuildAgentSummary_PassOmitsError(t *testing.T) {
	s := BuildAgentSummary(ExitPass, "results/ok", []JobResult{
		{Pod: "p", Job: "j", Status: "pass", Duration: 2 * time.Second},
	}, nil, true)
	if s.Status != "pass" || s.ExitCode != ExitPass || s.Error != "" || len(s.Failures) != 0 {
		t.Fatalf("pass summary = %+v", s)
	}
	if s.Report != "results/ok/report.json" {
		t.Fatalf("report = %q", s.Report)
	}
}

func TestEmitAgentSummary_FileMatchesStdout(t *testing.T) {
	dir := t.TempDir()
	s := BuildAgentSummary(ExitJobsFailed, dir, []JobResult{
		{Pod: "p", Job: "j", Status: "fail", VolumeMode: "Filesystem", Error: "io error", Duration: time.Second},
	}, errors.New("1 job(s) failed"), true)

	var stdout bytes.Buffer
	if err := EmitAgentSummary(&stdout, s); err != nil {
		t.Fatal(err)
	}

	onDisk, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != string(onDisk) {
		t.Fatalf("stdout and summary.json differ\nstdout:\n%s\nfile:\n%s", stdout.String(), onDisk)
	}

	var decoded AgentSummary
	if err := json.Unmarshal(onDisk, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Failures[0].Classification != ClassStorage {
		t.Fatalf("classification = %q", decoded.Failures[0].Classification)
	}
	if !bytes.Contains(onDisk, []byte(`"failures": [`)) {
		t.Fatalf("failures encoded as null or omitted: %s", onDisk)
	}
}
