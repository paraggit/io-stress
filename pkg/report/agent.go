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

	// ExitCleanup and ExitInfra reuse the existing usage and cluster codes so
	// a FIO-only run keeps the same numbers. Object cleanup failures are exit
	// 2; a worker that never produced a result is exit 3.
	ExitCleanup = ExitUsage
	ExitInfra   = ExitCluster

	ClassHarness = "harness"
	ClassStorage = "storage"
	ClassUnknown = "unknown"
	ClassInfra   = "infra"
	ClassCleanup = "cleanup"
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
	Options       *UsedOptions   `json:"options,omitempty"`
}

// UsedOptions is the effective run configuration. JSON names match long flag names.
type UsedOptions struct {
	Config               string   `json:"config,omitempty"`
	Recipe               string   `json:"recipe,omitempty"`
	Kubeconfig           string   `json:"kubeconfig"`
	NumPVC               int      `json:"num-pvc,omitempty"`
	RBDNumPVC            int      `json:"rbd-num-pvc"`
	CephFSNumPVC         int      `json:"cephfs-num-pvc"`
	Namespace            string   `json:"namespace"`
	RBDStorageClass      string   `json:"rbd-storage-class"`
	CephFSStorageClass   string   `json:"cephfs-storage-class"`
	PVCSize              string   `json:"pvc-size"`
	Image                string   `json:"image"`
	Runtime              int      `json:"runtime"`
	BlockSize            string   `json:"bs"`
	Offset               string   `json:"offset"`
	FIOSize              string   `json:"fio-size"`
	SeedSize             string   `json:"seed-size"`
	Prefix               string   `json:"prefix"`
	ResultsDir           string   `json:"results-dir,omitempty"`
	Timeout              string   `json:"timeout"`
	CloneTimeout         string   `json:"clone-timeout"`
	Format               string   `json:"format"`
	NoCleanup            bool     `json:"no-cleanup"`
	DryRun               bool     `json:"dry-run"`
	LifecycleInterval    int      `json:"lifecycle-interval"`
	SkipLifecycle        bool     `json:"skip-lifecycle"`
	SkipFIOStress        bool     `json:"skip-fio-stress"`
	WriteVerify          bool     `json:"write-verify"`
	SetupOnly            bool     `json:"setup-only"`
	ExpandFactor         int      `json:"expand-factor"`
	SnapshotClass        string   `json:"snapshot-class"`
	SustainRuntime       int      `json:"sustain-runtime"`
	MaxParallel          int      `json:"max-parallel"`
	MaxParallelProvision int      `json:"max-parallel-provision"`
	Sequential           bool     `json:"sequential"`
	AppType              []string `json:"app-type,omitempty"`

	Object             bool   `json:"object,omitempty"`
	ObjectOnly         bool   `json:"object-only,omitempty"`
	ObjectProfile      string `json:"object-profile,omitempty"`
	ObjectImage        string `json:"object-image,omitempty"`
	ObjectBuckets      int    `json:"object-buckets,omitempty"`
	ObjectCount        int    `json:"object-count,omitempty"`
	ObjectSizeMix      string `json:"object-size-mix,omitempty"`
	ObjectWorkers      int    `json:"object-workers,omitempty"`
	ObjectKeyLayout    string `json:"object-key-layout,omitempty"`
	ObjectVerify       string `json:"object-verify,omitempty"`
	ObjectVersioned    int    `json:"object-versioned,omitempty"`
	ObjectPhases       string `json:"object-phases,omitempty"`
	ObjectDeleteMode   string `json:"object-delete-mode,omitempty"`
	ObjectStorageClass string `json:"object-storage-class,omitempty"`
	ObjectProvisioning string `json:"object-provisioning,omitempty"`
	ObjectSeed         string `json:"object-seed,omitempty"`
	ObjectResume       bool   `json:"object-resume,omitempty"`
	ObjectInsecureTLS  bool   `json:"object-insecure-tls,omitempty"`
	ObjectForce        bool   `json:"object-force,omitempty"`
}

// objectOnlyOptions is the flag set written for --object-only. Volume and FIO
// flags stay off this object so the summary does not describe a FIO run.
type objectOnlyOptions struct {
	Config               string `json:"config,omitempty"`
	Recipe               string `json:"recipe,omitempty"`
	Kubeconfig           string `json:"kubeconfig"`
	Namespace            string `json:"namespace"`
	Prefix               string `json:"prefix"`
	ResultsDir           string `json:"results-dir,omitempty"`
	Timeout              string `json:"timeout"`
	NoCleanup            bool   `json:"no-cleanup"`
	DryRun               bool   `json:"dry-run"`
	MaxParallel          int    `json:"max-parallel"`
	MaxParallelProvision int    `json:"max-parallel-provision"`

	Object             bool   `json:"object,omitempty"`
	ObjectOnly         bool   `json:"object-only,omitempty"`
	ObjectProfile      string `json:"object-profile,omitempty"`
	ObjectImage        string `json:"object-image,omitempty"`
	ObjectBuckets      int    `json:"object-buckets,omitempty"`
	ObjectCount        int    `json:"object-count,omitempty"`
	ObjectSizeMix      string `json:"object-size-mix,omitempty"`
	ObjectWorkers      int    `json:"object-workers,omitempty"`
	ObjectKeyLayout    string `json:"object-key-layout,omitempty"`
	ObjectVerify       string `json:"object-verify,omitempty"`
	ObjectVersioned    int    `json:"object-versioned,omitempty"`
	ObjectPhases       string `json:"object-phases,omitempty"`
	ObjectDeleteMode   string `json:"object-delete-mode,omitempty"`
	ObjectStorageClass string `json:"object-storage-class,omitempty"`
	ObjectProvisioning string `json:"object-provisioning,omitempty"`
	ObjectSeed         string `json:"object-seed,omitempty"`
	ObjectResume       bool   `json:"object-resume,omitempty"`
	ObjectInsecureTLS  bool   `json:"object-insecure-tls,omitempty"`
	ObjectForce        bool   `json:"object-force,omitempty"`
}

func (o UsedOptions) MarshalJSON() ([]byte, error) {
	if !o.ObjectOnly {
		type full UsedOptions
		return json.Marshal(full(o))
	}
	return json.Marshal(objectOnlyOptions{
		Config:               o.Config,
		Recipe:               o.Recipe,
		Kubeconfig:           o.Kubeconfig,
		Namespace:            o.Namespace,
		Prefix:               o.Prefix,
		ResultsDir:           o.ResultsDir,
		Timeout:              o.Timeout,
		NoCleanup:            o.NoCleanup,
		DryRun:               o.DryRun,
		MaxParallel:          o.MaxParallel,
		MaxParallelProvision: o.MaxParallelProvision,
		Object:               o.Object,
		ObjectOnly:           o.ObjectOnly,
		ObjectProfile:        o.ObjectProfile,
		ObjectImage:          o.ObjectImage,
		ObjectBuckets:        o.ObjectBuckets,
		ObjectCount:          o.ObjectCount,
		ObjectSizeMix:        o.ObjectSizeMix,
		ObjectWorkers:        o.ObjectWorkers,
		ObjectKeyLayout:      o.ObjectKeyLayout,
		ObjectVerify:         o.ObjectVerify,
		ObjectVersioned:      o.ObjectVersioned,
		ObjectPhases:         o.ObjectPhases,
		ObjectDeleteMode:     o.ObjectDeleteMode,
		ObjectStorageClass:   o.ObjectStorageClass,
		ObjectProvisioning:   o.ObjectProvisioning,
		ObjectSeed:           o.ObjectSeed,
		ObjectResume:         o.ObjectResume,
		ObjectInsecureTLS:    o.ObjectInsecureTLS,
		ObjectForce:          o.ObjectForce,
	})
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
		if r.Status != "fail" && r.Status != "infra-fail" && r.Status != "cleanup-fail" {
			continue
		}
		class := ClassifyFailure(r.VolumeMode, r.Error)
		switch r.Status {
		case "infra-fail":
			class = ClassInfra
		case "cleanup-fail":
			class = ClassCleanup
		}
		s.Failures = append(s.Failures, FailureBrief{
			Pod:             r.Pod,
			Job:             r.Job,
			Storage:         r.Storage,
			VolumeMode:      r.VolumeMode,
			ExitCode:        r.ExitCode,
			DurationSeconds: roundSeconds(r.Duration.Seconds()),
			Error:           r.Error,
			Classification:  class,
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
