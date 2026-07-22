# odf-io-stress

ODF IO stress testing tool for RBD and CephFS. It creates PVC/pod pairs on an OpenShift Data Foundation cluster, runs FIO workloads, exercises volume lifecycle operations (expand, clone, snapshot/restore), and verifies data integrity.

## Prerequisites

- Go 1.26+
- Access to an OpenShift/Kubernetes cluster with ODF (RBD and CephFS StorageClasses)
- `kubeconfig` configured (`KUBECONFIG` or `~/.kube/config`)

## Build

```bash
go build -o odf-io-stress ./cmd/odf-io-stress
```

## Usage

```bash
./odf-io-stress run [flags]
```

### Examples

```bash
# Default run (4 PVC/pod pairs per backend)
./odf-io-stress run

# Smaller / faster smoke run
./odf-io-stress run -n 2 --runtime 30

# Preview manifests without creating resources
./odf-io-stress run --dry-run

# FIO stress only (skip lifecycle and verify)
./odf-io-stress run --skip-lifecycle

# Keep resources after the run
./odf-io-stress run --no-cleanup
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --num-pvc` | `4` | Number of PVC/pod pairs per backend |
| `-N, --namespace` | `odf-io-stress` | Kubernetes namespace |
| `--rbd-storage-class` | `ocs-storagecluster-ceph-rbd` | RBD StorageClass |
| `--cephfs-storage-class` | `ocs-storagecluster-cephfs` | CephFS StorageClass |
| `--pvc-size` | `10Gi` | PVC size |
| `-i, --image` | `quay.io/ocsci/nginx:fio` | FIO container image |
| `-r, --runtime` | `60` | FIO runtime (seconds) |
| `-b, --bs` | `512` | FIO block size |
| `--offset` | `512` | FIO offset |
| `--fio-size` | `1G` | FIO file/device size |
| `-p, --prefix` | `odf-io` | Resource name prefix |
| `-t, --timeout` | `5m` | Wait timeout for PVC/pod readiness |
| `-f, --format` | `json` | FIO output format (`json`, `normal`) |
| `--sequential` | `false` | Run FIO jobs sequentially |
| `--max-parallel` | `0` | Max concurrent pods (`0` = unlimited) |
| `--no-cleanup` | `false` | Skip resource cleanup on exit |
| `--dry-run` | `false` | Emit YAML manifests only |
| `--lifecycle-interval` | `4` | Run lifecycle ops on every Nth pod |
| `--skip-lifecycle` | `false` | Skip lifecycle storm and verify phases |
| `--skip-fio-stress` | `false` | Skip FIO stress phase |
| `--expand-factor` | `2` | PVC expand size multiplier |
| `--snapshot-class` | _(auto)_ | Override VolumeSnapshotClass |
| `--sustain-runtime` | `runtime*3` | Sustain workload duration (seconds) |

## Test phases

1. **FIO stress** — Unaligned IO, object-boundary writes, mixed block sizes, integrity checks, and backend-specific jobs (RBD block / CephFS filesystem, including RWX where applicable).
2. **Lifecycle storm** — PVC expand, clone, and snapshot/restore on a subset of pods (controlled by `--lifecycle-interval`).
3. **Data integrity verify** — FIO verify against clone and restored volumes.

## Results

Per-run output is written under `results/<timestamp>/`:

- Individual FIO job JSON files
- Aggregated `report.json` with summary

The `results/` directory is gitignored.

## Project layout

```
cmd/odf-io-stress/   # CLI entrypoint
pkg/config/          # Flags and defaults
pkg/fio/             # FIO job definitions
pkg/k8s/             # Kubernetes helpers (PVC, pod, snapshot, exec)
pkg/workload/        # Orchestration (phases, dry-run, sustain)
pkg/report/          # Result collection and summary
```
