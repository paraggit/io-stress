# Config File & Pattern-Driven FIO Suites Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add YAML/JSON config file support with `generate-config`, nested cluster/tools schema, per-backend PVC counts, and FIO suites expressed as pattern data (FIO-only execution in v1).

**Architecture:** Nested `config.Config` (`Cluster` + `Tools.FIO.Suites`) is the single source of truth. `NewDefault()` embeds today’s hardcoded FIO jobs as `[]Pattern`. `Load` / `WriteSample` handle YAML/JSON. `pkg/fio` converts patterns → `Job` args. CLI loads `--config`, then overlays only `Changed()` flags; `generate-config` writes sample YAML.

**Tech Stack:** Go 1.26, cobra, `gopkg.in/yaml.v3`, `encoding/json`, existing `pkg/fio` / `pkg/workload` / client-go.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-22-config-file-design.md`
- Formats: `.yaml`/`.yml` → YAML; `.json` → JSON; sample always YAML
- Merge order: `NewDefault()` → config file → explicitly set CLI flags
- No `--config`: built-in defaults (behavior equivalent to today)
- Unknown tools `vdbench` / `smallfiles`: warn if non-empty, do not run
- `--num-pvc` sets both RBD and CephFS counts; prefer `--rbd-num-pvc` / `--cephfs-num-pvc`
- Patterns: common fields + FIO `params` map; no per-pattern CLI flags in v1
- Do not implement vdbench/smallfiles runners

## File Structure

| File | Responsibility |
|------|----------------|
| `pkg/config/config.go` | Nested types, `NewDefault` (sans suite bodies), `Validate` |
| `pkg/config/duration.go` | YAML/JSON-friendly `Duration` wrapping `time.Duration` |
| `pkg/config/default_suites.go` | Default `Suites` pattern data (ported from current FIO suites) |
| `pkg/config/file.go` | `Load`, `WriteSample`, format detection |
| `pkg/config/flags.go` | `ApplyChangedFlags(cmd, cfg)` CLI overlay |
| `pkg/config/*_test.go` | Defaults, validate, load, sample, flag overlay |
| `pkg/fio/pattern.go` | `PatternsToJobs`, size/runtime injection |
| `pkg/fio/job.go` | `JobsForVolume` / `ReducedSuite` / `CephFSRWXJobs` from suites |
| `pkg/fio/suite_*.go` | Delete after migration (or leave empty package files deleted) |
| `pkg/workload/*.go` | Nested field access; per-backend PVC create/cleanup |
| `cmd/odf-io-stress/main.go` | `--config`, `generate-config`, new PVC flags |
| `README.md` | Document config + flags |
| `go.mod` | Direct require `gopkg.in/yaml.v3` |

---

### Task 1: Nested config types, Duration, Validate, NewDefault skeleton

**Files:**
- Create: `pkg/config/duration.go`
- Modify: `pkg/config/config.go` (replace flat struct)
- Modify: `pkg/config/config_test.go`
- Create: `pkg/config/default_suites.go` (stub returning empty suites initially — filled in Task 3)

**Interfaces:**
- Produces: nested `Config`, `NewDefault() *Config`, `Validate(cfg *Config) error`, `type Duration time.Duration` with YAML/JSON marshal

- [ ] **Step 1: Write failing tests for nested defaults and validation**

Replace `pkg/config/config_test.go` with:

```go
package config

import (
	"testing"
	"time"
)

func TestNewDefault(t *testing.T) {
	cfg := NewDefault()
	if cfg.Cluster.RBD.NumPVC != 4 {
		t.Errorf("RBD.NumPVC = %d, want 4", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 4 {
		t.Errorf("CephFS.NumPVC = %d, want 4", cfg.Cluster.CephFS.NumPVC)
	}
	if cfg.Cluster.Namespace != "odf-io-stress" {
		t.Errorf("Namespace = %q", cfg.Cluster.Namespace)
	}
	if cfg.Cluster.RBD.StorageClass != "ocs-storagecluster-ceph-rbd" {
		t.Errorf("RBD SC = %q", cfg.Cluster.RBD.StorageClass)
	}
	if cfg.Cluster.CephFS.StorageClass != "ocs-storagecluster-cephfs" {
		t.Errorf("CephFS SC = %q", cfg.Cluster.CephFS.StorageClass)
	}
	if cfg.Tools.FIO.Runtime != 60 {
		t.Errorf("FIO.Runtime = %d, want 60", cfg.Tools.FIO.Runtime)
	}
	if cfg.Tools.FIO.Size != "1G" {
		t.Errorf("FIO.Size = %q", cfg.Tools.FIO.Size)
	}
	if time.Duration(cfg.Cluster.WaitTimeout) != 5*time.Minute {
		t.Errorf("WaitTimeout = %v", cfg.Cluster.WaitTimeout)
	}
	if !cfg.Tools.FIO.Parallel {
		t.Error("Parallel should default true")
	}
	if cfg.Cluster.SustainRuntime != 180 {
		t.Errorf("SustainRuntime = %d, want 180", cfg.Cluster.SustainRuntime)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{"valid defaults", func(c *Config) {}, false},
		{"zero both PVCs", func(c *Config) {
			c.Cluster.RBD.NumPVC = 0
			c.Cluster.CephFS.NumPVC = 0
		}, true},
		{"rbd only ok", func(c *Config) { c.Cluster.CephFS.NumPVC = 0 }, false},
		{"cephfs only ok", func(c *Config) { c.Cluster.RBD.NumPVC = 0 }, false},
		{"empty namespace", func(c *Config) { c.Cluster.Namespace = "" }, true},
		{"empty RBD SC when rbd>0", func(c *Config) { c.Cluster.RBD.StorageClass = "" }, true},
		{"empty RBD SC ok when rbd=0", func(c *Config) {
			c.Cluster.RBD.NumPVC = 0
			c.Cluster.RBD.StorageClass = ""
		}, false},
		{"empty PVC size", func(c *Config) { c.Cluster.PVCSize = "" }, true},
		{"empty FIO image", func(c *Config) { c.Tools.FIO.Image = "" }, true},
		{"zero runtime", func(c *Config) { c.Tools.FIO.Runtime = 0 }, true},
		{"zero expand factor", func(c *Config) { c.Cluster.ExpandFactor = 0 }, true},
		{"empty pattern name", func(c *Config) {
			c.Tools.FIO.Suites.Common = []Pattern{{Name: "", Params: map[string]string{"rw": "read"}}}
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewDefault()
			tt.modify(cfg)
			err := Validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/config/ -count=1`
Expected: FAIL (types / fields missing)

- [ ] **Step 3: Implement Duration helper**

Create `pkg/config/duration.go`:

```go
package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration wraps time.Duration for YAML/JSON duration strings (e.g. "5m").
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}
```

- [ ] **Step 4: Replace flat Config with nested types**

Rewrite `pkg/config/config.go` with types from the spec (`Config`, `Cluster`, `Backend`, `Tools`, `FIO`, `Suites`, `Pattern`). `NewDefault()` returns nested defaults matching today’s values; call `defaultSuites()` for `Tools.FIO.Suites`.

`Validate` rules:
- `RBD.NumPVC + CephFS.NumPVC >= 1` (each may be 0 alone)
- Storage class required only when that backend’s `NumPVC >= 1`
- Namespace, PVCSize, FIO.Image non-empty; FIO.Runtime >= 1; ExpandFactor >= 1
- Every pattern in all suites: `Name != ""`

Create stub `pkg/config/default_suites.go`:

```go
package config

func defaultSuites() Suites {
	return Suites{}
}
```

- [ ] **Step 5: Run config tests**

Run: `go test ./pkg/config/ -count=1`
Expected: PASS

Note: other packages will not compile until Task 4 — that is OK for now; run only `./pkg/config/`.

- [ ] **Step 6: Commit**

```bash
git add pkg/config/config.go pkg/config/duration.go pkg/config/default_suites.go pkg/config/config_test.go
git commit -m "$(cat <<'EOF'
feat(config): nest cluster/tools config types and validation

EOF
)"
```

---

### Task 2: Load / WriteSample (YAML + JSON)

**Files:**
- Create: `pkg/config/file.go`
- Create: `pkg/config/file_test.go`
- Modify: `go.mod` / `go.sum` (direct `gopkg.in/yaml.v3`)

**Interfaces:**
- Consumes: nested `Config`, `NewDefault`
- Produces: `Load(path string) (*Config, error)`, `WriteSample(path string, force bool) error`

- [ ] **Step 1: Add yaml dependency**

Run: `go get gopkg.in/yaml.v3@v3.0.1`

- [ ] **Step 2: Write failing load/sample tests**

Create `pkg/config/file_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `
cluster:
  namespace: from-file
  rbd:
    num_pvc: 2
  cephfs:
    num_pvc: 1
tools:
  fio:
    runtime: 30
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.Namespace != "from-file" {
		t.Errorf("namespace = %q", cfg.Cluster.Namespace)
	}
	if cfg.Cluster.RBD.NumPVC != 2 || cfg.Cluster.CephFS.NumPVC != 1 {
		t.Errorf("pvc counts rbd=%d cephfs=%d", cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC)
	}
	if cfg.Tools.FIO.Runtime != 30 {
		t.Errorf("runtime = %d", cfg.Tools.FIO.Runtime)
	}
	// omitted keys keep defaults
	if cfg.Cluster.PVCSize != "10Gi" {
		t.Errorf("PVCSize default lost: %q", cfg.Cluster.PVCSize)
	}
}

func TestLoadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	content := `{"cluster":{"namespace":"json-ns"},"tools":{"fio":{"runtime":15}}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.Namespace != "json-ns" || cfg.Tools.FIO.Runtime != 15 {
		t.Fatalf("unexpected: ns=%q rt=%d", cfg.Cluster.Namespace, cfg.Tools.FIO.Runtime)
	}
}

func TestLoadUnsupportedExt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.toml")
	_ = os.WriteFile(path, []byte("x=1"), 0644)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for .toml")
	}
}

func TestWriteSampleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.yaml")
	if err := WriteSample(path, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteSample(path, false); err == nil {
		t.Fatal("expected refuse overwrite without force")
	}
	if err := WriteSample(path, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	def := NewDefault()
	if cfg.Cluster.Namespace != def.Cluster.Namespace {
		t.Errorf("namespace mismatch")
	}
	if cfg.Tools.FIO.Runtime != def.Tools.FIO.Runtime {
		t.Errorf("runtime mismatch")
	}
}

func TestWriteSampleStdout(t *testing.T) {
	// path "-" should not error; implementation may write to os.Stdout
	if err := WriteSample("-", false); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 3: Run tests — expect FAIL**

Run: `go test ./pkg/config/ -count=1 -run 'TestLoad|TestWrite'`
Expected: FAIL (undefined Load/WriteSample)

- [ ] **Step 4: Implement Load and WriteSample**

Create `pkg/config/file.go`:

```go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := NewDefault()
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse yaml %s: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse json %s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("unsupported config extension %q (use .yaml, .yml, or .json)", ext)
	}
	return cfg, nil
}

func WriteSample(path string, force bool) error {
	cfg := NewDefault()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal sample: %w", err)
	}
	header := []byte("# odf-io-stress sample config\n# Generate: odf-io-stress generate-config\n\n")
	out := append(header, data...)
	if path == "-" {
		_, err := os.Stdout.Write(out)
		return err
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("refusing to overwrite %s (use --force)", path)
		}
	}
	return os.WriteFile(path, out, 0644)
}
```

- [ ] **Step 5: Run tests — expect PASS**

Run: `go test ./pkg/config/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/config/file.go pkg/config/file_test.go go.mod go.sum
git commit -m "$(cat <<'EOF'
feat(config): load YAML/JSON config and write sample

EOF
)"
```

---

### Task 3: Default FIO suites as pattern data

**Files:**
- Modify: `pkg/config/default_suites.go` (full port)
- Modify: `pkg/config/config_test.go` (assert suite job names present)

**Interfaces:**
- Produces: `defaultSuites() Suites` with all current jobs

- [ ] **Step 1: Write failing test for default suite names**

Add to `pkg/config/config_test.go`:

```go
func TestDefaultSuitesJobNames(t *testing.T) {
	s := NewDefault().Tools.FIO.Suites
	common := names(s.Common)
	for _, n := range []string{
		"unaligned-direct", "unaligned-buffered", "unaligned-randread",
		"obj-boundary-3m", "obj-boundary-5m", "mixed-bs-verify",
		"data-integrity-4k", "seq-write-verify", "high-iodepth-stress",
		"overwrite-frag-stress", "high-concurrency-randrw", "compress-pattern-stress",
	} {
		if !common[n] {
			t.Errorf("common missing %q", n)
		}
	}
	fs := names(s.Filesystem)
	for _, n := range []string{"truncate-write", "fsync-stress", "fdatasync-mixed", "append-write"} {
		if !fs[n] {
			t.Errorf("filesystem missing %q", n)
		}
	}
	block := names(s.Block)
	for _, n := range []string{"trim-write-interleave", "trim-stress", "write-zeroes", "sub-4k-rmw"} {
		if !block[n] {
			t.Errorf("block missing %q", n)
		}
	}
	rwx := names(s.CephFSRWX)
	for _, n := range []string{"rwx-concurrent-write", "rwx-read-while-write"} {
		if !rwx[n] {
			t.Errorf("cephfs_rwx missing %q", n)
		}
	}
	life := names(s.Lifecycle)
	for _, n := range []string{"data-integrity-4k", "high-iodepth-stress"} {
		if !life[n] {
			t.Errorf("lifecycle missing %q", n)
		}
	}
}

func names(patterns []Pattern) map[string]bool {
	m := map[string]bool{}
	for _, p := range patterns {
		m[p.Name] = true
	}
	return m
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./pkg/config/ -count=1 -run TestDefaultSuitesJobNames`
Expected: FAIL (empty suites)

- [ ] **Step 3: Port all jobs into `default_suites.go`**

Source of truth for args (port 1:1 into `params`):
- `pkg/fio/suite_common.go` → `Suites.Common`
- `pkg/fio/suite_fs.go` → `Suites.Filesystem`
- `pkg/fio/suite_block.go` → `Suites.Block`
- `CephFSRWXJobs` in `pkg/fio/job.go` → `Suites.CephFSRWX`
- `ReducedSuite` in `pkg/fio/job.go` → `Suites.Lifecycle`

Rules when porting:
- Do **not** put `name`, `filename`, or `output-format` in `params` (injected at runtime).
- Prefer omitting `size` / `runtime` from `params` so tool defaults apply; set pattern-level `Size` only when the job used a non-default size (e.g. `overwrite-frag-stress` uses `64m`).
- For unaligned jobs that used `cfg.FIOBlockSize` / `cfg.FIOOffset`, put `"bs": "512"` and `"offset": "512"` in params (defaults). Optionally later resolve from `tools.fio.block_size` / `offset` in the converter when params omit them — **required:** converter must fill `bs`/`offset` from `FIO.BlockSize`/`FIO.Offset` when the pattern name starts with `unaligned-` OR when params contain placeholder — simplest approach for parity: in `PatternsToJobs`, if `params` lacks `bs` but `fio.BlockSize` set, do not auto-add globally; instead bake `"512"` into default unaligned patterns and document that `--bs` / config `block_size` updates defaults by rewriting at load time **OR** resolve in converter:

**Required converter behavior (implement in Task 4):** for each pattern, before building args:
1. Start from `params` copy.
2. If `size` not in params: use pattern.Size if set, else `fio.Size` → add `size`.
3. If `runtime` not in params: use pattern.Runtime if set, else `fio.Runtime` → add `runtime`.
4. If pattern is one of the three unaligned jobs (or params has no `bs` but category is `unaligned`): if `bs` missing, set from `fio.BlockSize`; if `offset` missing, set from `fio.Offset`.

Example pattern entry:

```go
{
  Name: "unaligned-direct", Category: "unaligned",
  Params: map[string]string{
    "rw": "randwrite", "ioengine": "libaio", "direct": "1",
    "iodepth": "16", "time_based": "1", "group_reporting": "1",
  },
},
```

Port **every** job listed in the test above. Keep categories matching current Go code (`unaligned`, `boundary`, `integrity`, `stress`, `compression`, `filesystem`, `block`, `cephfs`, `lifecycle`).

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./pkg/config/ -count=1 -run TestDefaultSuites`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/config/default_suites.go pkg/config/config_test.go
git commit -m "$(cat <<'EOF'
feat(config): encode default FIO suites as pattern data

EOF
)"
```

---

### Task 4: Pattern → Job conversion; wire `pkg/fio`

**Files:**
- Create: `pkg/fio/pattern.go`
- Create: `pkg/fio/pattern_test.go`
- Modify: `pkg/fio/job.go`
- Delete: `pkg/fio/suite_common.go`, `pkg/fio/suite_fs.go`, `pkg/fio/suite_block.go`, `pkg/fio/suite_cephfs.go` (after migration)
- Modify: `pkg/fio/job_test.go` (keep name assertions; they should still pass)

**Interfaces:**
- Consumes: `config.FIO`, `config.Pattern`, `config.Suites`
- Produces: `PatternsToJobs(patterns []config.Pattern, fio config.FIO) []Job`; updated `JobsForVolume`, `ReducedSuite`, `CephFSRWXJobs`

- [ ] **Step 1: Write failing converter test**

Create `pkg/fio/pattern_test.go`:

```go
package fio

import (
	"strings"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestPatternsToJobs_InjectsSizeRuntimeAndParams(t *testing.T) {
	fioCfg := config.NewDefault().Tools.FIO
	jobs := PatternsToJobs([]config.Pattern{{
		Name:     "unaligned-direct",
		Category: "unaligned",
		Params: map[string]string{
			"rw": "randwrite", "ioengine": "libaio", "direct": "1",
			"iodepth": "16", "time_based": "1", "group_reporting": "1",
		},
	}}, fioCfg)
	if len(jobs) != 1 {
		t.Fatalf("len=%d", len(jobs))
	}
	args := strings.Join(jobs[0].Args, " ")
	for _, want := range []string{"--rw=randwrite", "--size=1G", "--runtime=60", "--bs=512", "--offset=512"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q; got %v", want, jobs[0].Args)
		}
	}
}

func TestPatternsToJobs_PatternSizeOverride(t *testing.T) {
	fioCfg := config.NewDefault().Tools.FIO
	jobs := PatternsToJobs([]config.Pattern{{
		Name: "overwrite-frag-stress", Category: "stress", Size: "64m",
		Params: map[string]string{"rw": "randwrite", "bs": "4k", "ioengine": "libaio", "direct": "1", "iodepth": "64", "time_based": "1", "group_reporting": "1"},
	}}, fioCfg)
	if !strings.Contains(strings.Join(jobs[0].Args, " "), "--size=64m") {
		t.Errorf("got %v", jobs[0].Args)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./pkg/fio/ -count=1 -run TestPatternsToJobs`
Expected: FAIL

- [ ] **Step 3: Implement PatternsToJobs**

Create `pkg/fio/pattern.go`:

```go
package fio

import (
	"fmt"
	"sort"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func PatternsToJobs(patterns []config.Pattern, fioCfg config.FIO) []Job {
	jobs := make([]Job, 0, len(patterns))
	for _, p := range patterns {
		params := cloneParams(p.Params)
		applyDefaults(params, p, fioCfg)
		args := paramsToArgs(params)
		jobs = append(jobs, Job{Name: p.Name, Category: p.Category, Args: args})
	}
	return jobs
}

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func applyDefaults(params map[string]string, p config.Pattern, fioCfg config.FIO) {
	if _, ok := params["size"]; !ok {
		size := fioCfg.Size
		if p.Size != "" {
			size = p.Size
		}
		if size != "" {
			params["size"] = size
		}
	}
	if _, ok := params["runtime"]; !ok {
		rt := fioCfg.Runtime
		if p.Runtime != nil {
			rt = *p.Runtime
		}
		if rt > 0 {
			params["runtime"] = fmt.Sprintf("%d", rt)
		}
	}
	if p.Category == "unaligned" || len(p.Name) >= 9 && p.Name[:9] == "unaligned" {
		if _, ok := params["bs"]; !ok && fioCfg.BlockSize != "" {
			params["bs"] = fioCfg.BlockSize
		}
		if _, ok := params["offset"]; !ok && fioCfg.Offset != "" {
			params["offset"] = fioCfg.Offset
		}
	}
}

func paramsToArgs(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys) // stable output for tests
	args := make([]string, 0, len(keys))
	for _, k := range keys {
		args = append(args, fmt.Sprintf("--%s=%s", k, params[k]))
	}
	return args
}
```

- [ ] **Step 4: Rewrite job.go suite selectors; delete suite_*.go**

In `pkg/fio/job.go`, replace suite helpers:

```go
func JobsForVolume(storageType string, volumeMode string, cfg *config.Config) []Job {
	s := cfg.Tools.FIO.Suites
	fioCfg := cfg.Tools.FIO
	jobs := PatternsToJobs(s.Common, fioCfg)
	if volumeMode == "Filesystem" {
		jobs = append(jobs, PatternsToJobs(s.Filesystem, fioCfg)...)
	} else {
		jobs = append(jobs, PatternsToJobs(s.Block, fioCfg)...)
	}
	return jobs
}

func ReducedSuite(target string, cfg *config.Config) []Job {
	_ = target
	return PatternsToJobs(cfg.Tools.FIO.Suites.Lifecycle, cfg.Tools.FIO)
}

func CephFSRWXJobs(cfg *config.Config) []Job {
	return PatternsToJobs(cfg.Tools.FIO.Suites.CephFSRWX, cfg.Tools.FIO)
}
```

Delete `suite_common.go`, `suite_fs.go`, `suite_block.go`, `suite_cephfs.go`.

- [ ] **Step 5: Run fio tests**

Run: `go test ./pkg/fio/ -count=1`
Expected: PASS (existing name tests + new converter tests)

- [ ] **Step 6: Commit**

```bash
git add pkg/fio/pattern.go pkg/fio/pattern_test.go pkg/fio/job.go
git rm pkg/fio/suite_common.go pkg/fio/suite_fs.go pkg/fio/suite_block.go pkg/fio/suite_cephfs.go
git commit -m "$(cat <<'EOF'
feat(fio): build jobs from config pattern suites

EOF
)"
```

---

### Task 5: Update workload packages for nested config + per-backend PVCs

**Files:**
- Modify: `pkg/workload/runner.go`
- Modify: `pkg/workload/dryrun.go`
- Modify: `pkg/workload/dryrun_test.go`
- Modify: `pkg/workload/phase1.go`
- Modify: `pkg/workload/phase2.go`
- Modify: `pkg/workload/phase3.go`
- Modify: `pkg/workload/sustain.go`

**Interfaces:**
- Consumes: nested `cfg.Cluster.*`, `cfg.Tools.FIO.*`
- Produces: create/cleanup loops honor separate RBD/CephFS counts

Field map (mechanical replace):

| Old | New |
|-----|-----|
| `cfg.Namespace` | `cfg.Cluster.Namespace` |
| `cfg.NumPVC` (RBD loops) | `cfg.Cluster.RBD.NumPVC` |
| `cfg.NumPVC` (CephFS loops) | `cfg.Cluster.CephFS.NumPVC` |
| `cfg.RBDStorageClass` | `cfg.Cluster.RBD.StorageClass` |
| `cfg.CephFSStorageClass` | `cfg.Cluster.CephFS.StorageClass` |
| `cfg.PVCSize` | `cfg.Cluster.PVCSize` |
| `cfg.Prefix` | `cfg.Cluster.Prefix` |
| `cfg.WaitTimeout` | `cfg.Cluster.WaitTimeout.Duration()` |
| `cfg.NoCleanup` | `cfg.Cluster.NoCleanup` |
| `cfg.DryRun` | `cfg.Cluster.DryRun` |
| `cfg.LifecycleInterval` | `cfg.Cluster.LifecycleInterval` |
| `cfg.SkipLifecycle` | `cfg.Cluster.SkipLifecycle` |
| `cfg.SkipFIOStress` | `cfg.Cluster.SkipFIOStress` |
| `cfg.ExpandFactor` | `cfg.Cluster.ExpandFactor` |
| `cfg.SnapshotClass` | `cfg.Cluster.SnapshotClass` |
| `cfg.MaxParallelPods` | `cfg.Cluster.MaxParallelPods` |
| `cfg.ResultsDir` | `cfg.Cluster.ResultsDir` |
| `cfg.SustainRuntime` | `cfg.Cluster.SustainRuntime` |
| `cfg.FIOImage` | `cfg.Tools.FIO.Image` |
| `cfg.FIORuntime` | `cfg.Tools.FIO.Runtime` |
| `cfg.FIOSize` | `cfg.Tools.FIO.Size` |
| `cfg.OutputFormat` | `cfg.Tools.FIO.OutputFormat` |
| `cfg.Parallel` | `cfg.Tools.FIO.Parallel` |

- [ ] **Step 1: Update dryrun_test for nested fields**

In `pkg/workload/dryrun_test.go`, change `cfg.NumPVC = 2` to:

```go
cfg.Cluster.RBD.NumPVC = 2
cfg.Cluster.CephFS.NumPVC = 2
```

- [ ] **Step 2: Update dryrun.go loops**

Use separate loops: `for i := 1; i <= cfg.Cluster.RBD.NumPVC; i++` and `for i := 1; i <= cfg.Cluster.CephFS.NumPVC; i++`. Update log line accordingly. Use nested field names from the map above.

- [ ] **Step 3: Update runner.go setup + cleanup**

- `totalRBD := cfg.Cluster.RBD.NumPVC`, `totalCephFS := cfg.Cluster.CephFS.NumPVC`
- Cleanup: iterate `1..RBD.NumPVC` for rbd resources and `1..CephFS.NumPVC` for cephfs resources (do not use a single shared count)
- Replace all flat field accesses

- [ ] **Step 4: Update phase1/2/3/sustain.go**

Mechanical nested-field replace per map. In `phase2.go` `storageClassFor`, return `cfg.Cluster.CephFS.StorageClass` / `cfg.Cluster.RBD.StorageClass`.

- [ ] **Step 5: Compile and test workload + fio + config**

Run: `go test ./pkg/... -count=1`
Expected: PASS (workload tests that only need nested fields)

- [ ] **Step 6: Commit**

```bash
git add pkg/workload/
git commit -m "$(cat <<'EOF'
refactor(workload): use nested config and per-backend PVC counts

EOF
)"
```

---

### Task 6: CLI — `--config`, `generate-config`, flag overlay

**Files:**
- Create: `pkg/config/flags.go`
- Create: `pkg/config/flags_test.go` (optional table test with a fake flag set — or test via integration in main; prefer unit test with `pflag`)
- Modify: `cmd/odf-io-stress/main.go`

**Interfaces:**
- Consumes: `Load`, `WriteSample`, nested `Config`
- Produces: `ApplyChangedFlags(flagSet, cfg)`; CLI commands wired

- [ ] **Step 1: Implement ApplyChangedFlags**

Create `pkg/config/flags.go` using `github.com/spf13/pflag`:

```go
package config

import (
	"github.com/spf13/pflag"
)

// ApplyChangedFlags overlays only flags that were explicitly set onto cfg.
func ApplyChangedFlags(fs *pflag.FlagSet, cfg *Config) error {
	var err error
	get := func(name string, apply func()) {
		if err != nil || !fs.Changed(name) {
			return
		}
		apply()
	}

	get("namespace", func() {
		cfg.Cluster.Namespace, err = fs.GetString("namespace")
	})
	get("rbd-num-pvc", func() {
		cfg.Cluster.RBD.NumPVC, err = fs.GetInt("rbd-num-pvc")
	})
	get("cephfs-num-pvc", func() {
		cfg.Cluster.CephFS.NumPVC, err = fs.GetInt("cephfs-num-pvc")
	})
	get("num-pvc", func() {
		var n int
		n, err = fs.GetInt("num-pvc")
		if err == nil {
			cfg.Cluster.RBD.NumPVC = n
			cfg.Cluster.CephFS.NumPVC = n
		}
	})
	get("rbd-storage-class", func() {
		cfg.Cluster.RBD.StorageClass, err = fs.GetString("rbd-storage-class")
	})
	get("cephfs-storage-class", func() {
		cfg.Cluster.CephFS.StorageClass, err = fs.GetString("cephfs-storage-class")
	})
	get("pvc-size", func() {
		cfg.Cluster.PVCSize, err = fs.GetString("pvc-size")
	})
	get("image", func() {
		cfg.Tools.FIO.Image, err = fs.GetString("image")
	})
	get("runtime", func() {
		cfg.Tools.FIO.Runtime, err = fs.GetInt("runtime")
	})
	get("bs", func() {
		cfg.Tools.FIO.BlockSize, err = fs.GetString("bs")
	})
	get("offset", func() {
		cfg.Tools.FIO.Offset, err = fs.GetString("offset")
	})
	get("fio-size", func() {
		cfg.Tools.FIO.Size, err = fs.GetString("fio-size")
	})
	get("prefix", func() {
		cfg.Cluster.Prefix, err = fs.GetString("prefix")
	})
	get("timeout", func() {
		var d time.Duration
		d, err = fs.GetDuration("timeout")
		if err == nil {
			cfg.Cluster.WaitTimeout = Duration(d)
		}
	})
	get("format", func() {
		cfg.Tools.FIO.OutputFormat, err = fs.GetString("format")
	})
	get("no-cleanup", func() {
		cfg.Cluster.NoCleanup, err = fs.GetBool("no-cleanup")
	})
	get("dry-run", func() {
		cfg.Cluster.DryRun, err = fs.GetBool("dry-run")
	})
	get("lifecycle-interval", func() {
		cfg.Cluster.LifecycleInterval, err = fs.GetInt("lifecycle-interval")
	})
	get("skip-lifecycle", func() {
		cfg.Cluster.SkipLifecycle, err = fs.GetBool("skip-lifecycle")
	})
	get("skip-fio-stress", func() {
		cfg.Cluster.SkipFIOStress, err = fs.GetBool("skip-fio-stress")
	})
	get("expand-factor", func() {
		cfg.Cluster.ExpandFactor, err = fs.GetInt("expand-factor")
	})
	get("snapshot-class", func() {
		cfg.Cluster.SnapshotClass, err = fs.GetString("snapshot-class")
	})
	get("sustain-runtime", func() {
		cfg.Cluster.SustainRuntime, err = fs.GetInt("sustain-runtime")
	})
	get("max-parallel", func() {
		cfg.Cluster.MaxParallelPods, err = fs.GetInt("max-parallel")
	})
	get("sequential", func() {
		var seq bool
		seq, err = fs.GetBool("sequential")
		if err == nil && seq {
			cfg.Tools.FIO.Parallel = false
		}
	})
	return err
}
```

Add `"time"` import. If both `--num-pvc` and `--rbd-num-pvc` are set, apply `num-pvc` first then per-backend flags (so per-backend wins) — implement by processing `num-pvc` before the specific flags as shown, **or** process specific after: keep order in the function so `rbd-num-pvc` / `cephfs-num-pvc` run **after** `num-pvc`.

- [ ] **Step 2: Unit test ApplyChangedFlags**

```go
func TestApplyChangedFlags(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("num-pvc", 4, "")
	fs.Int("rbd-num-pvc", 4, "")
	fs.String("namespace", "odf-io-stress", "")
	_ = fs.Parse([]string{"--namespace", "ns2", "--rbd-num-pvc", "1"})
	cfg := NewDefault()
	cfg.Cluster.Namespace = "from-file"
	cfg.Cluster.RBD.NumPVC = 9
	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.Namespace != "ns2" {
		t.Errorf("namespace=%q", cfg.Cluster.Namespace)
	}
	if cfg.Cluster.RBD.NumPVC != 1 {
		t.Errorf("rbd=%d", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 4 { // unchanged
		t.Errorf("cephfs=%d", cfg.Cluster.CephFS.NumPVC)
	}
}
```

- [ ] **Step 3: Rewrite main.go**

Structure:

```go
func main() {
	rootCmd := &cobra.Command{Use: "odf-io-stress", Short: "ODF IO stress testing tool for RBD and CephFS"}

	var (
		configPath string
		genOut     string
		genForce   bool
	)

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the FIO stress test suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.NewDefault()
			if configPath != "" {
				loaded, err := config.Load(configPath)
				if err != nil {
					return err
				}
				cfg = loaded
				if len(cfg.Tools.VDBench) > 0 {
					log.Printf("WARNING: tools.vdbench is set but not supported yet; ignoring")
				}
				if len(cfg.Tools.SmallFiles) > 0 {
					log.Printf("WARNING: tools.smallfiles is set but not supported yet; ignoring")
				}
			}
			if err := config.ApplyChangedFlags(cmd.Flags(), cfg); err != nil {
				return err
			}
			if cfg.Cluster.SustainRuntime == 0 {
				cfg.Cluster.SustainRuntime = cfg.Tools.FIO.Runtime * 3
			}
			if err := config.Validate(cfg); err != nil {
				return err
			}
			if cfg.Cluster.DryRun {
				return workload.DryRun(cfg)
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return workload.Run(ctx, cfg)
		},
	}

	def := config.NewDefault()
	f := runCmd.Flags()
	f.StringVar(&configPath, "config", "", "Path to YAML/JSON config file")
	f.Int("num-pvc", def.Cluster.RBD.NumPVC, "Set both RBD and CephFS PVC counts")
	f.Int("rbd-num-pvc", def.Cluster.RBD.NumPVC, "Number of RBD PVC/pod pairs")
	f.Int("cephfs-num-pvc", def.Cluster.CephFS.NumPVC, "Number of CephFS PVC/pod pairs")
	f.StringP("namespace", "N", def.Cluster.Namespace, "Kubernetes namespace")
	// ... register remaining flags with defaults from def (String/Int/Bool/Duration, not Var bound to cfg)
	// include: rbd-storage-class, cephfs-storage-class, pvc-size, image, runtime, bs, offset,
	// fio-size, prefix, timeout, format, no-cleanup, dry-run, lifecycle-interval,
	// skip-lifecycle, skip-fio-stress, expand-factor, snapshot-class, sustain-runtime,
	// max-parallel, sequential

	genCmd := &cobra.Command{
		Use:   "generate-config",
		Short: "Write a sample YAML config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.WriteSample(genOut, genForce)
		},
	}
	genCmd.Flags().StringVarP(&genOut, "output", "o", "odf-io-stress.yaml", "Output path (`-` for stdout)")
	genCmd.Flags().BoolVar(&genForce, "force", false, "Overwrite existing file")

	rootCmd.AddCommand(runCmd, genCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

Register **all** flags listed in the spec’s CLI→config map using `f.Int` / `f.String` / etc. (not `Var` into a live cfg), so `ApplyChangedFlags` + `Get*` work cleanly.

- [ ] **Step 4: Build and smoke-test CLI**

```bash
go build -o /tmp/odf-io-stress ./cmd/odf-io-stress
/tmp/odf-io-stress generate-config -o /tmp/sample.yaml
/tmp/odf-io-stress run --config /tmp/sample.yaml --dry-run --rbd-num-pvc 1 --cephfs-num-pvc 0 | head
/tmp/odf-io-stress run --help
```

Expected: sample written; dry-run emits only RBD manifests; help shows `--config` and `generate-config`.

- [ ] **Step 5: Full test suite**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/config/flags.go pkg/config/flags_test.go cmd/odf-io-stress/main.go
git commit -m "$(cat <<'EOF'
feat(cli): add --config, generate-config, and flag overlays

EOF
)"
```

---

### Task 7: README update

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README**

Add sections for:
- `generate-config` examples
- `--config` usage and merge rules (defaults → file → flags)
- Per-backend PVC flags (`--rbd-num-pvc`, `--cephfs-num-pvc`, `--num-pvc`)
- Abbreviated config schema (cluster + tools.fio.suites)
- Note that `vdbench` / `smallfiles` are reserved

Update the flags table to match nested semantics / new flags. Keep build/prereq sections.

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
docs: document config file and generate-config usage

EOF
)"
```

---

## Spec coverage checklist

| Spec requirement | Task |
|------------------|------|
| YAML + JSON by extension | Task 2 |
| Sample YAML via `generate-config` | Task 6 |
| `--force` / stdout `-` | Task 2 + 6 |
| Nested cluster + tools schema | Task 1 |
| FIO patterns hybrid fields | Task 3–4 |
| Suites: common/fs/block/cephfs_rwx/lifecycle | Task 3–4 |
| Default suites = today’s jobs | Task 3 |
| No `--config` → built-in defaults | Task 1 + 6 |
| Flag overlay via Changed | Task 6 |
| Separate RBD/CephFS num_pvc | Task 1 + 5 + 6 |
| `--num-pvc` sets both | Task 6 |
| Warn/ignore vdbench/smallfiles | Task 6 |
| Validation rules | Task 1 |
| Workload uses nested fields | Task 5 |
| README | Task 7 |

## Self-review notes

- No TBD placeholders left in tasks.
- `Duration` type used consistently for `wait_timeout`.
- Flag registration uses `Get*` + `Changed` (not live `Var` binding) to avoid clobbering file values.
- Suite file deletion happens only after `PatternsToJobs` + `job.go` wiring (Task 4).
