# odf-io-stress

ODF IO stress testing tool for RBD and CephFS. It creates PVC/pod pairs on an OpenShift Data Foundation cluster, runs FIO or Vdbench workloads, exercises volume lifecycle operations (expand, clone, snapshot/restore), and verifies data integrity.

## Prerequisites

- Go 1.26+
- Access to an OpenShift/Kubernetes cluster with ODF (RBD and CephFS StorageClasses)
- Cluster access via kubeconfig (`--kubeconfig`, `cluster.kubeconfig`, `KUBECONFIG`, or `~/.kube/config`)

## Build

```bash
make build
# or
go build -o odf-io-stress ./cmd/odf-io-stress
```

## Release

Every merge (push) to `main` triggers GitHub Actions to test, cross-compile binaries, and publish a GitHub Release.

Version format: `v0.0.<run_number>-<short_sha>` (for example `v0.0.12-a1b2c3d`).

Artifacts (linux/darwin × amd64/arm64) and SHA256 checksums are attached to the release. Locally you can build the same set with:

```bash
make release-binaries
```

## Usage

```bash
./odf-io-stress run [flags]
```

Run without `--config` uses built-in defaults (same effective behavior as before). Pass `--config` to load a YAML or JSON file; explicitly set CLI flags override matching config fields.

### Merge order

1. Built-in defaults (`NewDefault()`)
2. Config file (when `--config` is set)
3. Explicitly set CLI flags only (`Changed()`)
4. Derived defaults (e.g. `sustain_runtime = runtime*3` when unset)
5. Validation

### Examples

```bash
# Default run (4 PVC/pod pairs per backend)
./odf-io-stress run

# Smaller / faster smoke run
./odf-io-stress run -n 2 --runtime 30

# Per-backend PVC counts
./odf-io-stress run --rbd-num-pvc 2 --cephfs-num-pvc 6

# Run from config; flags override file values
./odf-io-stress run --config odf-io-stress.yaml
./odf-io-stress run --config config.json --rbd-num-pvc 2 --runtime 30

# Target a specific cluster
./odf-io-stress run --kubeconfig ~/.kube/my-odf.kubeconfig

# Preview manifests without creating resources
./odf-io-stress run --dry-run

# FIO stress only (skip lifecycle and verify)
./odf-io-stress run --skip-lifecycle

# Keep resources after the run
./odf-io-stress run --no-cleanup

# Vdbench stress (lifecycle phases auto-skipped)
./odf-io-stress run --config odf-io-stress.yaml --tool vdbench
```

### Generate config

Write a sample YAML config with current defaults:

```bash
./odf-io-stress generate-config                    # writes odf-io-stress.yaml
./odf-io-stress generate-config -o my.yaml
./odf-io-stress generate-config -o -               # stdout
./odf-io-stress generate-config -o my.yaml --force # overwrite existing file
```

Format is detected by file extension (`.yaml`, `.yml`, or `.json`).

### Config schema (abbreviated)

```yaml
cluster:
  namespace: odf-io-stress
  kubeconfig: ""   # empty → KUBECONFIG env or ~/.kube/config
  rbd:
    num_pvc: 4
    storage_class: ocs-storagecluster-ceph-rbd
  cephfs:
    num_pvc: 4
    storage_class: ocs-storagecluster-cephfs
  pvc_size: 10Gi
  prefix: odf-io
  wait_timeout: 5m
  # ... lifecycle, cleanup, sustain, etc.

tools:
  active: fio          # fio | vdbench (default: fio); overridden by --tool

  fio:
    image: quay.io/ocsci/nginx:fio
    runtime: 60
    size: 1G
    block_size: "512"
    offset: "512"
    output_format: json
    parallel: true
    suites:
      common: []       # all volumes
      filesystem: []   # Filesystem volumeMode
      block: []        # Block volumeMode
      cephfs_rwx: []   # CephFS RWX shared tests
      lifecycle: []    # reduced suite for phase 2/3

  vdbench:
    image: quay.io/pakamble/vdbench:latest
    runtime: 60
    output_dir: /tmp/vdbench-out
    block:
      size: "15g"
      patterns:
        - name: random_write
          rdpct: 0
          seekpct: 100
          xfersize: "4k"
          skew: 0
    filesystem:
      size: "10m"
      depth: 4
      width: 5
      files: 10
      file_size: "1m"
      openflags: "o_direct"   # empty = buffered
      group_all_fwds_in_one_rd: true
      patterns:
        - name: sequential_write
          rdpct: 0
          seekpct: 0
          xfersize: "1m"
          skew: 0

  smallfiles: {}       # reserved; not supported yet
```

FIO suite entries are patterns with `name`, optional `category`/`size`/`runtime`, and FIO-native `params` (e.g. `rw`, `bs`, `ioengine`). Vdbench patterns use `rdpct`, `seekpct`, `xfersize`, and `skew`; block patterns apply to RBD block volumes, filesystem patterns to RBD filesystem and CephFS volumes. Run `generate-config` for a full sample including default FIO suites and Vdbench patterns.

When `tools.active` is `vdbench`, Phase 2 (lifecycle storm) and Phase 3 (data integrity verify) are skipped automatically regardless of `skip_lifecycle`.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | _(none)_ | Path to YAML (`.yaml`/`.yml`) or JSON (`.json`) config file |
| `--kubeconfig` | _(default loading)_ | Path to kubeconfig (else `KUBECONFIG` or `~/.kube/config`) |
| `-n, --num-pvc` | `4` | Set both RBD and CephFS PVC/pod counts |
| `--rbd-num-pvc` | `4` | RBD PVC/pod pairs |
| `--cephfs-num-pvc` | `4` | CephFS PVC/pod pairs |
| `-N, --namespace` | `odf-io-stress` | Kubernetes namespace |
| `--rbd-storage-class` | `ocs-storagecluster-ceph-rbd` | RBD StorageClass |
| `--cephfs-storage-class` | `ocs-storagecluster-cephfs` | CephFS StorageClass |
| `--pvc-size` | `10Gi` | PVC size |
| `-i, --image` | `quay.io/ocsci/nginx:fio` | FIO container image |
| `--tool` | `fio` | IO engine: `fio` or `vdbench` (`tools.active`) |
| `--vdbench-image` | `quay.io/pakamble/vdbench:latest` | Vdbench container image |
| `-r, --runtime` | `60` | FIO runtime (seconds) |
| `-b, --bs` | `512` | FIO block size |
| `--offset` | `512` | FIO offset |
| `--fio-size` | `1G` | FIO file/device size |
| `-p, --prefix` | `odf-io` | Resource name prefix |
| `-t, --timeout` | `5m` | Wait timeout for PVC/pod readiness |
| `-f, --format` | `json` | FIO output format (`json`, `normal`) |
| `--sequential` | `false` | Run FIO jobs sequentially (`tools.fio.parallel=false`) |
| `--max-parallel` | `0` | Max concurrent pods (`0` = unlimited) |
| `--no-cleanup` | `false` | Skip resource cleanup on exit |
| `--dry-run` | `false` | Emit YAML manifests only |
| `--lifecycle-interval` | `4` | Run lifecycle ops on every Nth pod |
| `--skip-lifecycle` | `false` | Skip lifecycle storm and verify phases |
| `--skip-fio-stress` | `false` | Skip FIO stress phase |
| `--expand-factor` | `2` | PVC expand size multiplier |
| `--snapshot-class` | _(auto)_ | Override VolumeSnapshotClass |
| `--sustain-runtime` | `runtime*3` | Sustain workload duration (seconds) |

`generate-config` flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-o, --output` | `odf-io-stress.yaml` | Output path (`-` for stdout) |
| `--force` | `false` | Overwrite existing output file |

## Test phases

1. **IO stress** — FIO or Vdbench workloads on created volumes (engine selected by `tools.active` / `--tool`). FIO runs unaligned IO, object-boundary writes, mixed block sizes, integrity checks, and backend-specific jobs (RBD block / CephFS filesystem, including RWX where applicable). Vdbench runs block/filesystem patterns from config; CephFS RWX multi-pod tests are skipped.
2. **Lifecycle storm** — PVC expand, clone, and snapshot/restore on a subset of pods (controlled by `--lifecycle-interval`). Skipped automatically when `tools.active` is `vdbench`.
3. **Data integrity verify** — FIO verify against clone and restored volumes. Skipped automatically when `tools.active` is `vdbench`.

## Results

Per-run output is written under `results/<timestamp>/`:

- Individual FIO job JSON files
- Aggregated `report.json` with summary

The `results/` directory is gitignored.

## Project layout

```
cmd/odf-io-stress/   # CLI entrypoint
pkg/config/          # Config types, load/merge, flags, defaults
pkg/fio/             # FIO job definitions (pattern → job)
pkg/vdbench/         # Vdbench param file generation
pkg/k8s/             # Kubernetes helpers (PVC, pod, snapshot, exec)
pkg/workload/        # Orchestration (phases, dry-run, sustain)
pkg/report/          # Result collection and summary
```
