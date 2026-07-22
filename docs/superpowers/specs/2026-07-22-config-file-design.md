# Config File & Pattern-Driven FIO Suites

**Date:** 2026-07-22  
**Status:** Approved for implementation planning

## Problem

Users must pass many CLI flags for each run. FIO job suites are hardcoded in Go, so changing IO patterns requires a code change. The tool should grow toward multiple IO engines (FIO today; vdbench/smallfiles later) without rewriting the CLI each time.

## Goals

1. Accept a config file (YAML or JSON) for `run`.
2. Provide `generate-config` to emit a sample YAML config with current defaults.
3. Structure config by IO tool, with pattern lists under each tool.
4. Separate RBD and CephFS PVC counts.
5. Keep `run` without `--config` working via built-in defaults (same effective behavior as today).
6. Allow CLI flags to override config/file defaults when explicitly set.

## Non-goals (v1)

- Implementing vdbench or smallfiles runners.
- Abstract cross-tool pattern language (`rdpct` / `seekpct` / etc.).
- Interactive config editing or remote config fetch.

## Decisions

| Topic | Choice |
|-------|--------|
| Formats | YAML and JSON by extension; sample is YAML |
| Merge | Defaults → config file → explicitly set CLI flags |
| Sample CLI | Subcommand `odf-io-stress generate-config [-o path]` |
| Serialization | Tags on config types (Approach 1) |
| Patterns | Hybrid: common fields + tool-specific `params` map |
| Suites | Encode today’s hardcoded jobs as default pattern data |
| No `--config` | Built-in defaults embedded in Go |
| PVC counts | Separate `rbd.num_pvc` and `cephfs.num_pvc`; `--num-pvc` sets both |

## CLI

```bash
# Sample config (YAML)
odf-io-stress generate-config                 # ./odf-io-stress.yaml
odf-io-stress generate-config -o my.yaml
odf-io-stress generate-config -o -            # stdout

# Run with config; flags override file
odf-io-stress run --config odf-io-stress.yaml
odf-io-stress run --config config.json --rbd-num-pvc 2 --runtime 30

# Unchanged: no config file
odf-io-stress run
```

### New / changed flags on `run`

| Flag | Behavior |
|------|----------|
| `--config` | Path to YAML (`.yaml`/`.yml`) or JSON (`.json`) |
| `--rbd-num-pvc` | RBD PVC/pod count |
| `--cephfs-num-pvc` | CephFS PVC/pod count |
| `--num-pvc` | Convenience: sets both RBD and CephFS counts |

Existing flags (`--namespace`, `--runtime`, storage classes, etc.) remain and override matching config fields when `Changed()`.

### CLI → config field map

| Flag | Config field |
|------|----------------|
| `--namespace` | `cluster.namespace` |
| `--num-pvc` | `cluster.rbd.num_pvc` **and** `cluster.cephfs.num_pvc` |
| `--rbd-num-pvc` | `cluster.rbd.num_pvc` |
| `--cephfs-num-pvc` | `cluster.cephfs.num_pvc` |
| `--rbd-storage-class` | `cluster.rbd.storage_class` |
| `--cephfs-storage-class` | `cluster.cephfs.storage_class` |
| `--pvc-size` | `cluster.pvc_size` |
| `--prefix` | `cluster.prefix` |
| `--timeout` | `cluster.wait_timeout` |
| `--no-cleanup` | `cluster.no_cleanup` |
| `--dry-run` | `cluster.dry_run` |
| `--lifecycle-interval` | `cluster.lifecycle_interval` |
| `--skip-lifecycle` | `cluster.skip_lifecycle` |
| `--skip-fio-stress` | `cluster.skip_fio_stress` |
| `--expand-factor` | `cluster.expand_factor` |
| `--snapshot-class` | `cluster.snapshot_class` |
| `--max-parallel` | `cluster.max_parallel_pods` |
| `--sustain-runtime` | `cluster.sustain_runtime` |
| `--image` | `tools.fio.image` |
| `--runtime` | `tools.fio.runtime` |
| `--bs` | `tools.fio.block_size` |
| `--offset` | `tools.fio.offset` |
| `--fio-size` | `tools.fio.size` |
| `--format` | `tools.fio.output_format` |
| `--sequential` | `tools.fio.parallel=false` |

Individual pattern lists are config-file only in v1 (no per-pattern CLI flags).

### `generate-config` flags

| Flag | Default | Behavior |
|------|---------|----------|
| `-o, --output` | `odf-io-stress.yaml` | Output path; `-` means stdout |

Refuse to overwrite an existing file unless `--force` is set.

## Config schema

```yaml
cluster:
  namespace: odf-io-stress
  rbd:
    num_pvc: 4
    storage_class: ocs-storagecluster-ceph-rbd
  cephfs:
    num_pvc: 4
    storage_class: ocs-storagecluster-cephfs
  pvc_size: 10Gi
  prefix: odf-io
  wait_timeout: 5m
  no_cleanup: false
  dry_run: false
  lifecycle_interval: 4
  skip_lifecycle: false
  skip_fio_stress: false
  expand_factor: 2
  snapshot_class: ""
  max_parallel_pods: 0
  results_dir: ""
  sustain_runtime: 180

tools:
  fio:
    image: quay.io/ocsci/nginx:fio
    runtime: 60
    size: 1G
    block_size: "512"    # default bs for patterns that use it
    offset: "512"        # default offset for unaligned patterns
    output_format: json
    parallel: true
    suites:
      common: []         # all volumes
      filesystem: []     # Filesystem volumeMode
      block: []          # Block volumeMode
      cephfs_rwx: []     # CephFS RWX shared tests
      lifecycle: []      # reduced suite for phase 2/3

  # Reserved — ignored in v1 (log warning if non-empty)
  vdbench: {}
  smallfiles: {}
```

### Pattern object

```yaml
- name: unaligned-direct          # required
  category: unaligned             # optional label for reporting
  size: 1G                        # optional; falls back to tools.fio.size
  runtime: 60                     # optional; falls back to tools.fio.runtime
  params:                         # FIO-native key/value → --key=value
    rw: randwrite
    bs: "512"
    offset: "512"
    ioengine: libaio
    direct: "1"
    iodepth: "16"
    time_based: "1"
    group_reporting: "1"
```

**Param rules:**
- Keys are FIO option names without leading `--`.
- Values are strings (or numbers coerced to strings).
- Runner always injects `--name`, `--filename`, `--output-format`.
- If `size` / `runtime` are set on the pattern (or tool defaults apply), runner adds `--size` / `--runtime` unless already present in `params`.
- `time_based` should be set in params when using runtime-based jobs (as today’s suites do).

### Suite selection (unchanged semantics)

| Suite key | When used |
|-----------|-----------|
| `common` | Every volume |
| `filesystem` | `volumeMode=Filesystem` |
| `block` | `volumeMode=Block` |
| `cephfs_rwx` | CephFS RWX phase |
| `lifecycle` | Phase 2/3 reduced verify suite |

Sample config and `NewDefault()` must encode the current hardcoded jobs from `pkg/fio/suite_*.go` and `ReducedSuite` / `CephFSRWXJobs` so default runs stay equivalent.

## Load / merge order

For `run`:

1. Start from `NewDefault()` (full nested defaults + default suites).
2. If `--config` is set, load and deep-merge/unmarshal over defaults. Omitted keys keep defaults. Unknown tool sections under `tools` other than `fio` may exist; if `vdbench`/`smallfiles` have content, log a warning and ignore.
3. Apply only CLI flags where `cmd.Flags().Changed(name)` is true.
4. Apply existing derived defaults (e.g. `sustain_runtime = runtime*3` when unset/zero, matching current behavior).
5. `Validate(cfg)`.

Format detection: by file extension only. Unsupported extension → error.

## Validation

- At least one of `cluster.rbd.num_pvc` or `cluster.cephfs.num_pvc` must be ≥ 1.
- Namespace, storage classes (for backends with num_pvc ≥ 1), pvc_size, fio image, fio runtime ≥ 1, expand_factor ≥ 1.
- If `skip_fio_stress` is false: `tools.fio.suites` must yield at least one pattern for the volumes that will be created (e.g. common non-empty, or appropriate fs/block suites).
- Every pattern must have a non-empty `name`.
- `wait_timeout` must parse as a Go duration.

## Architecture

```
cmd/odf-io-stress/main.go
  ├── generate-config → config.WriteSample
  └── run --config → config.Load + flag overrides → workload.Run

pkg/config/
  ├── types (Cluster, Backend, Tools, FIO, Pattern, Suites)
  ├── NewDefault()          # embeds current suites as data
  ├── Load / WriteSample
  └── Validate

pkg/fio/
  ├── Pattern → Job conversion (params → Args)
  └── JobsForVolume / ReducedSuite / CephFSRWXJobs read cfg.Tools.FIO.Suites

pkg/workload/
  └── setupResources uses per-backend NumPVC
```

### Config type sketch

```go
type Config struct {
    Cluster Cluster `yaml:"cluster" json:"cluster"`
    Tools   Tools   `yaml:"tools" json:"tools"`
}

type Cluster struct {
    Namespace         string        `yaml:"namespace" json:"namespace"`
    RBD               Backend       `yaml:"rbd" json:"rbd"`
    CephFS            Backend       `yaml:"cephfs" json:"cephfs"`
    PVCSize           string        `yaml:"pvc_size" json:"pvc_size"`
    // ... remaining cluster fields
}

type Backend struct {
    NumPVC       int    `yaml:"num_pvc" json:"num_pvc"`
    StorageClass string `yaml:"storage_class" json:"storage_class"`
}

type Tools struct {
    FIO        FIO            `yaml:"fio" json:"fio"`
    VDBench    map[string]any `yaml:"vdbench,omitempty" json:"vdbench,omitempty"`
    SmallFiles map[string]any `yaml:"smallfiles,omitempty" json:"smallfiles,omitempty"`
}

type FIO struct {
    Image        string `yaml:"image" json:"image"`
    Runtime      int    `yaml:"runtime" json:"runtime"`
    Size         string `yaml:"size" json:"size"`
    BlockSize    string `yaml:"block_size" json:"block_size"`
    Offset       string `yaml:"offset" json:"offset"`
    OutputFormat string `yaml:"output_format" json:"output_format"`
    Parallel     bool   `yaml:"parallel" json:"parallel"`
    Suites       Suites `yaml:"suites" json:"suites"`
}

type Suites struct {
    Common     []Pattern `yaml:"common" json:"common"`
    Filesystem []Pattern `yaml:"filesystem" json:"filesystem"`
    Block      []Pattern `yaml:"block" json:"block"`
    CephFSRWX  []Pattern `yaml:"cephfs_rwx" json:"cephfs_rwx"`
    Lifecycle  []Pattern `yaml:"lifecycle" json:"lifecycle"`
}

type Pattern struct {
    Name     string            `yaml:"name" json:"name"`
    Category string            `yaml:"category,omitempty" json:"category,omitempty"`
    Size     string            `yaml:"size,omitempty" json:"size,omitempty"`
    Runtime  *int              `yaml:"runtime,omitempty" json:"runtime,omitempty"`
    Params   map[string]string `yaml:"params" json:"params"`
}
```

Internal callers that currently use flat fields (`cfg.NumPVC`, `cfg.FIORuntime`, …) are updated to the nested fields. No long-lived dual API.

## Error handling

| Case | Behavior |
|------|----------|
| Config path missing | Error: cannot read file |
| Bad extension | Error: unsupported format |
| Parse error | Error with path and parse detail |
| Validation failure | Error before creating cluster resources |
| Existing sample file without `--force` | Error: refuse overwrite |
| Non-empty `vdbench` / `smallfiles` | Warning log; continue with FIO |

## Testing

- Load YAML and JSON fixtures; assert nested fields and suite counts.
- `WriteSample` round-trip: write → load → equal to `NewDefault()` (allowing for duration/string normalization).
- Flag override: config sets `rbd.num_pvc=4`, `--rbd-num-pvc 1` → 1; unset flag leaves file value.
- `--num-pvc 3` sets both backends to 3.
- Pattern → Job: known pattern produces expected `--rw=...` args; size/runtime defaults applied.
- `JobsForVolume` with default config returns the same job names as today for RBD FS, RBD block, and CephFS.
- Validation rejects zero PVCs on both backends and empty pattern names.

## Documentation

Update `README.md` with:

- `generate-config` usage
- `--config` and merge rules
- Per-backend PVC flags
- Abbreviated schema example and pointer to sample file

## Migration notes

- Flat `Config` fields are replaced by nested structs; update all packages in the same change.
- Hardcoded suite functions become thin converters over `cfg.Tools.FIO.Suites` (or are removed once defaults live only in `NewDefault()`).
- Behavior without a config file remains the primary path for CI/scripts that pass only CLI flags.
