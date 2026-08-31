# odf-io-stress

ODF IO stress testing tool for RBD and CephFS. It creates PVC/pod pairs on an OpenShift Data Foundation cluster, runs FIO workloads, exercises volume lifecycle operations (expand, clone, snapshot/restore), and verifies data integrity.

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

# Run with application workload profiles
./odf-io-stress run --app-type nfs
./odf-io-stress run --app-type vm,database
./odf-io-stress run --app-type nfs,vm,database --runtime 120
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
  seed_size: 512m   # Block integrity seed/verify extent (see Data integrity verify)
  app_types: [nfs, vm, database]  # optional, comma-separated in CLI
  # ... lifecycle, cleanup, sustain, etc.

tools:
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

  # Reserved for future IO engines — ignored in v1 (warning if non-empty)
  vdbench: {}
  smallfiles: {}
```

Each suite entry is a pattern with `name`, optional `category`/`size`/`runtime`, and FIO-native `params` (e.g. `rw`, `bs`, `ioengine`). Run `generate-config` for a full sample including default suites.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | _(none)_ | Path to YAML (`.yaml`/`.yml`) or JSON (`.json`) config file |
| `--kubeconfig` | _(default loading)_ | Path to kubeconfig (else `KUBECONFIG` or `~/.kube/config`) |
| `-n, --num-pvc` | `4` | Set both RBD and CephFS PVC/pod counts |
| `--rbd-num-pvc` | `4` | RBD PVC/pod pairs. If set alone (without `--cephfs-num-pvc` / `-n`), CephFS is set to `0` |
| `--cephfs-num-pvc` | `4` | CephFS PVC/pod pairs. If set alone (without `--rbd-num-pvc` / `-n`), RBD is set to `0` |
| `-N, --namespace` | `odf-io-stress` | Kubernetes namespace |
| `--rbd-storage-class` | `ocs-storagecluster-ceph-rbd` | RBD StorageClass |
| `--cephfs-storage-class` | `ocs-storagecluster-cephfs` | CephFS StorageClass |
| `--pvc-size` | `10Gi` | PVC size |
| `-i, --image` | `quay.io/ocsci/nginx:fio` | FIO container image |
| `-r, --runtime` | `60` | FIO runtime (seconds) |
| `-b, --bs` | `512` | FIO block size |
| `--offset` | `512` | FIO offset |
| `--fio-size` | `1G` | FIO file/device size |
| `--seed-size` | `512m` | Block integrity seed/verify extent (sequential, not time-based) |
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
| `--app-type` | _(none)_ | Comma-separated app profiles from `tools.fio.app_suites` (built-ins: `nfs`, `vm`, `database`, `messaging`, `ai`) |

`generate-config` flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-o, --output` | `odf-io-stress.yaml` | Output path (`-` for stdout) |
| `--force` | `false` | Overwrite existing output file |

## App-type workload profiles

`--app-type` / `cluster.app_types` selects which FIO **profile suite(s)** to run. When set, those suites run on **every** RBD/CephFS PVC/pod created by `num_pvc` (standard suites and CephFS RWX are skipped). No extra dedicated app PVC is created.

```bash
# VM-like IO on all default PVCs (4 RBD + 4 CephFS)
./odf-io-stress run --app-type vm --runtime 60

# Same, but only 2 RBD volumes (no CephFS)
./odf-io-stress run --app-type vm --rbd-num-pvc 2 --runtime 60

# Multiple profiles: each pod runs nfs then database suites
./odf-io-stress run --app-type nfs,database --rbd-num-pvc 2 --cephfs-num-pvc 2
```

| App type | Simulates | Suggested volume shape | IO patterns |
|----------|-----------|------------------------|-------------|
| `nfs` | NFS file server | Filesystem | Small-file metadata (4k fsync), large transfers, mixed sizes, sequential append |
| `vm` | OpenShift Virtualization (CNV) | Block-oriented | virtio random IO, guest boot storm, live-migration pre-copy, snapshot commit, high QD |
| `database` | PostgreSQL/MySQL-like | Filesystem | WAL fsync, random page reads, OLTP mixed, tablespace seq write, checkpoint flush |
| `messaging` | Kafka/AMQ-like log IO | Filesystem | Append log writes, consumer random reads, burst mixed |
| `ai` | Training checkpoint / dataset | Filesystem | Large seq checkpoint write, dataset seq read, shuffle random read |

`volume_mode` on a profile is metadata for authors; runtime always applies the selected suite(s) to whatever PVCs you provisioned.

### Onboarding a new FIO app profile (no code change)

Add under `tools.fio.app_suites` in your config (or extend the sample from `generate-config`):

```yaml
cluster:
  rbd:
    num_pvc: 2
  cephfs:
    num_pvc: 0
  app_types: [backup]
tools:
  fio:
    app_suites:
      backup:
        volume_mode: Filesystem   # documentation hint; suite runs on all PVCs
        patterns:
          - name: backup-stream-seq-write
            category: backup
            params:
              rw: write
              bs: 1m
              ioengine: libaio
              direct: "1"
              iodepth: "16"
              time_based: "1"
              group_reporting: "1"
```

Then:

```bash
./odf-io-stress run --config my.yaml
```

Built-in profiles remain available unless you override the same key. Pattern `params` are FIO-native flags (same model as `tools.fio.suites`).

### Still missing for real ODF coverage (need more than FIO profiles)

| Gap | Why FIO-only is insufficient |
|-----|------------------------------|
| Object / RGW / NooBaa | Needs S3 client workloads |
| Real NFS-Ganesha / CSI NFS | Needs NFS mount + metadata tools (e.g. smallfiles) |
| Real CNV VMs | Needs KubeVirt guests, not only block FIO |
| Real databases / brokers | Needs pgbench/sysbench/kafka tools |
| Encrypted StorageClass / KMS | Mostly SC + cluster setup; FIO can run on encrypted SC once provisioned |
| Multi-attach beyond CephFS RWX | Needs CSI multi-attach orchestration |

Those belong as future `tools.*` runners, not as `app_suites` entries.

## Test phases

1. **FIO stress** — Unaligned IO, object-boundary writes, mixed block sizes, integrity checks, and backend-specific jobs (RBD block / CephFS filesystem, including RWX where applicable).
2. **Lifecycle storm** — PVC expand, clone, and snapshot/restore on a subset of pods (controlled by `--lifecycle-interval`). Before clone/snapshot, the source is sequentially seeded for later verify.
3. **Data integrity verify** — FIO `verify_only` against clone and restored volumes, covering **exactly** the seeded extent.

### Block vs Filesystem data-integrity

On **Filesystem** volumes the seed and verify target a dedicated file (`/mnt/data/fio.dat`) sized to `tools.fio.size` / `--fio-size`. Fio creates that file, writes it sequentially, and verify cannot see leftover phase-1 IO.

On **Block** volumes (raw RBD, `/dev/rbdblock`) there is no private file: the clone inherits the entire device, including phase-1 4 KiB verify headers, non-verify writes, and unwritten zeros. The harness therefore:

- Sequentially seeds a **bounded region** from offset 0 (`cluster.seed_size` / `--seed-size`, default `512m`) with a single block size (`256k`), `--rw=write`, `--verify=crc32c`, **no** `--time_based` / `--runtime`.
- Runs `phase3-verify` as `--rw=read --verify_only=1` with the **same** `--bs` and `--size`.

**Invariant:** the integrity verify never reads a region or block size the seed did not deterministically write. Violating that produces false-positive `bad magic header` / `bad header length` / `crc32c verify failed` errors that are **not** storage-product faults.

### Troubleshooting false-positive verify errors

| Symptom | Likely harness cause |
|---------|----------------------|
| `verify: bad magic header 0, wanted acca` at offset 0 | Seed was random/`time_based` (or truncated) so the start of the device was never written |
| `verify: bad header length 4096, wanted 262144` | Verify `--size`/`--bs` larger than the sequential seed; leftover phase-1 4 KiB headers |
| `crc32c: verify failed` on Block only | Verify walked past the seeded extent into mixed phase-1 data |

If `integrity-seed` reports pass but `phase3-verify` fails on Block, check that seed `write.io_bytes >= seed_size` and that both jobs share `bs`/`size`.

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
pkg/k8s/             # Kubernetes helpers (PVC, pod, snapshot, exec)
pkg/workload/        # Orchestration (phases, dry-run, sustain)
pkg/report/          # Result collection and summary
```
