# odf-io-stress

ODF IO stress testing tool for RBD, CephFS, and (opt-in) NooBaa / Multicloud Object Gateway. It creates PVC/pod pairs on an OpenShift Data Foundation cluster, runs FIO workloads, exercises volume lifecycle operations (expand, clone, snapshot/restore), and verifies data integrity. `--object` adds an S3 object workload that runs inside the cluster.

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

The object workload runs from a container image. Build and push it before a cluster run:

```bash
make image          # IMAGE defaults to quay.io/paraggit/odf-io-stress:$(VERSION)
make image-push
```

How to tag, push, and pass your own image is in [docs/object-image.md](docs/object-image.md).

## Release

Every merge (push) to `main` triggers GitHub Actions to test, cross-compile binaries, publish a GitHub Release, and push the worker image to GitHub Container Registry.

Version format: `v0.0.<run_number>-<short_sha>` (for example `v0.0.12-a1b2c3d`).

The image tags are `ghcr.io/paraggit/odf-io-stress:<version>` and `ghcr.io/paraggit/odf-io-stress:latest`. A run uses `ghcr.io/paraggit/odf-io-stress:latest` unless `--object-image` is set. The same build is also pushed to `quay.io/paraggit/odf-io-stress` when the `QUAY_USERNAME` and `QUAY_PASSWORD` secrets are set. Details are in [docs/object-image.md](docs/object-image.md).

Artifacts (linux/darwin × amd64/arm64) and SHA256 checksums are attached to the release. Locally you can build the same set with:

```bash
make release-binaries
```

## Usage

```bash
./odf-io-stress run [flags]
./odf-io-stress options
./odf-io-stress example workload fio
./odf-io-stress example workload object
```

`options` prints every `run` and `generate-config` flag as JSON, including defaults and the flag interactions an agent needs. `AGENTS.md` lists the same options. `example workload` prints the run examples for `fio` or `object` and does not touch the cluster. Run without `--config` uses built-in defaults. Pass `--config` to load a YAML or JSON file; explicitly set CLI flags override matching config fields.

### Merge order

1. Built-in defaults (`NewDefault()`)
2. Config file (when `--config` is set)
3. `--recipe`, when set (`smoke`, `write-verify`, `app-vm`, `app-database`, `full`)
4. Explicitly set CLI flags only (`Changed()`), which override the recipe
5. Derived defaults (e.g. `sustain_runtime = runtime*3` when unset)
6. Validation

### Examples

```bash
# Default run (4 PVC/pod pairs per backend)
./odf-io-stress run

# Smaller / faster smoke run
./odf-io-stress run --recipe smoke --results-dir results/smoke

# App workload on one RBD volume
./odf-io-stress run --recipe app-vm --results-dir results/app-vm

# Recipe plus an override
./odf-io-stress run --recipe smoke --runtime 15

# Per-backend PVC counts
./odf-io-stress run --rbd-num-pvc 2 --cephfs-num-pvc 6

# Run from config; flags override file values
./odf-io-stress run --config odf-io-stress.yaml
./odf-io-stress run --config config.json --rbd-num-pvc 2 --runtime 30

# Target a specific cluster
./odf-io-stress run --kubeconfig ~/.kube/my-odf.kubeconfig

# Preview manifests without creating resources
./odf-io-stress run --dry-run

# Create PVCs and pods only (no FIO; resources kept)
./odf-io-stress run --setup-only --num-pvc 4

# FIO stress only (skip lifecycle clone/snapshot and phase-3 verify_only)
./odf-io-stress run --skip-lifecycle

# Data verification: write then crc32c-verify immediately (no stress suite, no lifecycle)
./odf-io-stress run --write-verify
./odf-io-stress run --write-verify --fio-size 2G
./odf-io-stress run --write-verify --rbd-num-pvc 2 --cephfs-num-pvc 0
./odf-io-stress run --write-verify --num-pvc 4 --no-cleanup

# Lifecycle integrity verify (default): seed then verify_only on clone/restore
./odf-io-stress run --seed-size 256m

# Keep resources after the run
./odf-io-stress run --no-cleanup

# Run with application workload profiles
./odf-io-stress run --app-type nfs
./odf-io-stress run --app-type vm,database
./odf-io-stress run --app-type nfs,vm,database --runtime 120

# Object workload only (no PVCs). See "Object workload" below.
./odf-io-stress run --object-only --object-profile smoke --results-dir results/object-smoke

# Same phases against Ceph RGW (OBC provisioning)
./odf-io-stress run --object-only --object-backend rgw --object-profile smoke --results-dir results/object-rgw-smoke
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
  results_dir: ""    # empty → results/<timestamp> ; CLI: --results-dir
  wait_timeout: 5m
  clone_timeout: 20m           # CephFS clone/restore Bound wait (RBD uses wait_timeout)
  max_parallel_provision: 4    # in-flight PVC create/bind/pod create and clone/restore/expand
  seed_size: 512m              # Block integrity seed/verify extent (see Data integrity verify)
  write_verify: false          # true → single write+immediate crc32c job; skips stress suite and lifecycle
  setup_only: false            # true → create PVCs/pods and exit (implies no_cleanup)
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

  object:
    enabled: false                 # true, or pass --object / --object-only
    profile: standard              # smoke | standard | scale | large
    image: ghcr.io/paraggit/odf-io-stress:latest
    buckets: 10
    objects_per_bucket: 5000
    key_layout: hashed             # hashed | flat | deep
    verify_mode: full              # full | etag | sample
    phases: [create, write, read, verify, list, copy, delete, teardown]
    delete_mode: both              # recursive | obc-only | both
    seed: ""                       # empty → random; printed and stored in summary.json
```

Each suite entry is a pattern with `name`, optional `category`/`size`/`runtime`, and FIO-native `params` (e.g. `rw`, `bs`, `ioengine`). Run `generate-config` for a full sample including default suites.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | _(none)_ | Path to YAML (`.yaml`/`.yml`) or JSON (`.json`) config file |
| `--recipe` | _(none)_ | Named run: `smoke`, `write-verify`, `app-vm`, `app-database`, `full`. Other flags override it. `odf-io-stress options` lists each recipe |
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
| `--results-dir` | `results/<timestamp>` | Directory for `report.json` and `summary.json` |
| `-t, --timeout` | `5m` | Wait timeout for PVC/pod readiness |
| `--clone-timeout` | `20m` | Bound wait for clone/restore PVCs on CephFS (RBD uses `--timeout`) |
| `-f, --format` | `json` | FIO output format (`json`, `normal`) |
| `--sequential` | `false` | Run FIO jobs sequentially (`tools.fio.parallel=false`) |
| `--max-parallel` | `0` | Max concurrent pods (`0` = unlimited) |
| `--max-parallel-provision` | `4` | Max in-flight PVC create, bind, pod create, clone, restore, and expand operations |
| `--no-cleanup` | `false` | Skip resource cleanup on exit |
| `--dry-run` | `false` | Emit YAML manifests only |
| `--lifecycle-interval` | `4` | Run lifecycle ops on every Nth pod |
| `--skip-lifecycle` | `false` | Skip lifecycle storm and verify phases |
| `--skip-fio-stress` | `false` | Skip FIO stress phase |
| `--write-verify` | `false` | Write then crc32c-verify in the same FIO job (`--do_verify=1 --verify_backlog=1`); skips the stress suite and lifecycle |
| `--setup-only` | `false` | Create PVCs and pods, wait until Ready, then exit (no FIO/lifecycle; implies `--no-cleanup`) |
| `--expand-factor` | `2` | PVC expand size multiplier |
| `--snapshot-class` | _(auto)_ | Override VolumeSnapshotClass |
| `--sustain-runtime` | `runtime*3` | Sustain workload duration (seconds) |
| `--app-type` | _(none)_ | Comma-separated app profiles from `tools.fio.app_suites` (built-ins: `nfs`, `vm`, `database`, `messaging`, `ai`) |
| `--object` | `false` | Enable the object workload alongside FIO |
| `--object-only` | `false` | Run only the object workload. Sets RBD and CephFS PVC counts to 0 and skips FIO |
| `--object-backend` | `noobaa` | Object backend: `noobaa` or `rgw` |
| `--object-profile` | `standard` | `smoke`, `standard`, `scale`, or `large`. Applied before the other object flags |
| `--object-image` | `ghcr.io/paraggit/odf-io-stress:latest` | Custom worker image. Overrides the profile and the config file. The image must contain this binary |
| `--object-buckets` | `10` | Number of buckets |
| `--object-count` | `5000` | Objects per bucket |
| `--object-size-mix` | `4KB:60,1MB:30,16MB:10` | Weighted sizes, for example `4KB:80,1MB:20` |
| `--object-workers` | `4` | Worker pods per phase |
| `--object-key-layout` | `hashed` | `hashed`, `flat`, or `deep` |
| `--object-verify` | `full` | `full` (every byte), `etag`, or `sample` |
| `--object-versioned` | `2` | How many buckets have versioning enabled |
| `--object-phases` | all eight | Comma-separated subset. Order is always create, write, read, verify, list, copy, delete, teardown |
| `--object-delete-mode` | `both` | `recursive`, `obc-only`, or `both` |
| `--object-storage-class` | `openshift-storage.noobaa.io` | ObjectBucketClaim StorageClass |
| `--object-obc-delete-timeout` | `10m` | Wait for ObjectBucketClaim deletion |
| `--object-provisioning` | `obc` | `obc` or `s3` (direct `CreateBucket` with the `noobaa-admin` secret; rejected with `--object-backend=rgw`) |
| `--object-seed` | random | 64 hex characters. Reproduces bucket names and object bytes |
| `--object-resume` | `false` | Reuse buckets from `--object-seed`. Requires `--object-only` and does not delete the namespace |
| `--object-insecure-tls` | `false` | Skip S3 TLS verification. Logged on the control plane and in the worker |
| `--object-force` | `false` | Start the write phase even when the capacity preflight fails |

`generate-config` flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-o, --output` | `odf-io-stress.yaml` | Output path (`-` for stdout) |
| `--force` | `false` | Overwrite existing output file |

## Object workload

`--object` runs an S3 object workload in the same namespace as FIO. `--object-only` runs it alone: PVC counts become 0 and FIO is skipped. Without either flag, a run is unchanged. `--object-backend` selects NooBaa / MCG (`noobaa`, the default) or Ceph RGW (`rgw`). The same phase engine, worker image, and profiles apply to both.

With `--object-backend=rgw`, the run reuses the object workload through ObjectBucketClaims: it discovers the StorageClass whose provisioner is `openshift-storage.ceph.rook.io/bucket` unless you set `--object-storage-class`, and validation rejects `--object-provisioning=s3` (OBC is the supported path for RGW). Scale and large profiles are unchanged.

The control plane uses only the Kubernetes API. Worker pods run this binary's `s3-worker` command and make every S3 call, including bucket creation when `--object-provisioning s3`. Build an image with `make image` and pass it on the command line with `--object-image`. The default is `ghcr.io/paraggit/odf-io-stress:latest`. [docs/object-image.md](docs/object-image.md) is the build and push walkthrough.

Bucket names come from the seed: `<key-prefix>-<first 8 hex chars>-<ordinal>`, for example `objstress-4f2a9c1d-007`. The seed is random when unset, printed on stderr, and stored in `summary.json` as `options.object-seed`. Object bytes are derived from that seed, so a later verify does not need a saved object list. Repeat the same profile, bucket count, object count, size mix, and key layout when you resume.

### Examples

```bash
# Short run: 2 buckets, 500 objects, full verify, no PVCs
./odf-io-stress run --object-only --object-profile smoke --results-dir results/object-smoke

# Default shape: 10 buckets, 5000 objects each
./odf-io-stress run --object-only --object-profile standard --results-dir results/object-standard

# One million small objects across 50 buckets (sampled verify)
./odf-io-stress run --object-only --object-profile scale --results-dir results/object-scale

# Large multipart objects
./odf-io-stress run --object-only --object-profile large --results-dir results/object-large

# Shrink a profile. Explicit flags override the profile
./odf-io-stress run --object-only --object-profile smoke --object-buckets 1 --object-count 20

# Use your own worker image instead of ghcr.io/paraggit/odf-io-stress:latest
./odf-io-stress run --object-only --object-profile smoke \
  --object-image quay.io/my-org/odf-io-stress:dev \
  --results-dir results/object-smoke

# Object workload and a FIO smoke recipe in the same namespace
./odf-io-stress run --object --object-profile smoke --recipe smoke --results-dir results/both

# Print ObjectBucketClaim and worker-pod YAML. Creates nothing
./odf-io-stress run --object-only --object-profile smoke --dry-run

# Flat keys (one prefix) and ETag checks
./odf-io-stress run --object-only --object-profile smoke --object-key-layout flat --object-verify etag

# Keep the buckets, then verify them later. Copy object-seed from the first summary
./odf-io-stress run --object-only --object-profile smoke --no-cleanup --results-dir results/object-keep
./odf-io-stress run --object-only --object-resume --object-seed <seed> \
  --object-profile smoke --object-phases verify --results-dir results/object-verify

# Write, then stop before delete, using a seed you choose
./odf-io-stress run --object-only --object-profile smoke \
  --object-seed 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  --object-phases create,write,verify --no-cleanup

# Empty every bucket before deleting its claim
./odf-io-stress run --object-only --object-profile smoke --object-delete-mode recursive

# Delete claims while the buckets are still full (finalizer case)
./odf-io-stress run --object-only --object-profile smoke --object-delete-mode obc-only

# Create buckets with the noobaa-admin secret instead of ObjectBucketClaims
./odf-io-stress run --object-only --object-profile smoke --object-provisioning s3

# From a config file; flags still override
./odf-io-stress run --config odf-io-stress.yaml --object-only --object-profile smoke
```

A phase list that omits `create` has to be a resume (`--object-resume --object-only --object-seed`). Otherwise the namespace reset would delete the buckets the later phases need. `--object-resume` also skips deleting the namespace at the end. `--no-cleanup` on the first run is what leaves the buckets in place.

`--setup-only` still wins when both are set: the command provisions (nothing, if `--object-only`) and exits before the object phases.

### Profiles

| Profile | Buckets | Objects each | Sizes | Workers | Verify |
|---------|---------|--------------|-------|---------|--------|
| `smoke` | 2 | 500 | 4KB, 1MB | 2 | full |
| `standard` | 10 | 5,000 | 4KB, 1MB, 16MB | 4 | full |
| `scale` | 50 | 20,000 | 4KB–64KB | 16 | sample, 1% |
| `large` | 4 | 50 | 100MB–5GB | 4 | full, multipart |

### Phases

Phases always run in this order. `--object-phases` selects a subset.

| Phase | What it does |
|-------|----------------|
| `create` | Create ObjectBucketClaims (or S3 buckets), probe capabilities, enable versioning |
| `write` | PUT every object. Multipart above 16MB. A 1% sample is read back immediately |
| `read` | GET and HEAD, with some range reads |
| `verify` | `full`, `etag`, or `sample`. A mismatch names the key, both SHA-256 values, and the first bad byte |
| `list` | Paginated listing, prefix/delimiter, and version listing on versioned buckets |
| `copy` | Server-side copy of a sample under `copies/` |
| `delete` | Empty buckets and/or leave them full, depending on `--object-delete-mode` |
| `teardown` | Confirm recursive buckets are empty, then delete the claims |

`delete_mode: both` (the default) empties even-numbered buckets and deletes odd-numbered claims while they are still full. A single bucket uses `obc-only`, so the finalizer path is the one that runs.

Versioning, multipart, version listing, copy, and batch delete are probed first. `NotImplemented`, `MethodNotAllowed`, `AccessDenied`, HTTP 403, HTTP 405, and HTTP 501 skip that operation. A supported operation that then fails fails the run.

### Config

`generate-config` writes `tools.object` with the standard profile and `enabled: false`.

```yaml
tools:
  object:
    enabled: true
    profile: smoke
    image: ghcr.io/paraggit/odf-io-stress:latest
    provisioning: obc          # obc | s3
    storage_class: openshift-storage.noobaa.io
    noobaa_namespace: openshift-storage
    buckets: 2
    objects_per_bucket: 500
    versioned_buckets: 1
    versions_per_key: 3
    key_prefix: objstress
    key_layout: hashed         # hashed | flat | deep
    verify_mode: full          # full | etag | sample
    verify_sample_pct: 1
    delete_mode: both          # recursive | obc-only | both
    insecure_tls: false
    capacity_preflight: true
    seed: ""
```

```bash
./odf-io-stress generate-config -o odf-io-stress.yaml
./odf-io-stress run --config odf-io-stress.yaml --object-only
# A flag still wins over tools.object.image in the file
./odf-io-stress run --config odf-io-stress.yaml --object-only \
  --object-image quay.io/my-org/odf-io-stress:dev
```

Before `write`, the tool runs a capacity preflight. On NooBaa it compares projected bytes with free capacity on the NooBaa CR. On RGW it compares projected logical bytes with raw `bytesAvailable` from the Ceph cluster (optimistic under replication). If the projection is larger, the run stops. `--object-force` continues. If free capacity cannot be read, the run continues and logs a warning.

Transport errors, including a short HTTP read failure, are retried in the worker (`max_retries` defaults to 5, about six seconds of backoff across the attempts). A disruption longer than that retry budget still fails the phase on the first exhausted operation. The summary records operation counts, not a time-series latency chart.

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
| Real NFS-Ganesha / CSI NFS | Needs NFS mount + metadata tools (e.g. smallfiles) |
| Real CNV VMs | Needs KubeVirt guests, not only block FIO |
| Real databases / brokers | Needs pgbench/sysbench/kafka tools |
| Encrypted StorageClass / KMS | Mostly SC + cluster setup; FIO can run on encrypted SC once provisioned |
| Multi-attach beyond CephFS RWX | Needs CSI multi-attach orchestration |

Those belong as future `tools.*` runners, not as `app_suites` entries.

## Test phases

1. **FIO stress** — Unaligned IO, object-boundary writes, mixed block sizes, integrity checks, and backend-specific jobs (RBD block / CephFS filesystem, including RWX where applicable). With `--write-verify`, this phase is a single sequential write that verifies crc32c immediately after each IO (see below).
2. **Lifecycle storm** — PVC expand, clone, and snapshot/restore on a subset of pods (controlled by `--lifecycle-interval`). Before clone/snapshot, the source is sequentially seeded for later verify.
3. **Data integrity verify** — FIO `verify_only` against clone and restored volumes, covering **exactly** the seeded extent.

`--setup-only` stops after PVC/pod provisioning (none of the phases above run). `--write-verify` runs only a write+immediate-verify job in phase 1 and skips lifecycle/phase 3.

### Data verification options

There are two integrity paths. `--write-verify` writes and checks crc32c in the same FIO job; the default run seeds in phase 2 and uses fio `--verify_only` on clone/restore in phase 3.

| What you want | Flag / config | What FIO does | Lifecycle / phase 3 |
|---------------|---------------|---------------|---------------------|
| Write data and verify it immediately | `--write-verify` / `cluster.write_verify: true` | Sequential `--rw=write` of `--fio-size`, `--verify=crc32c`, `--do_verify=1`, `--verify_backlog=1` (same job) | Skipped |
| Check clone/restore after lifecycle | default run (no `--write-verify`) | Phase 2 `integrity-seed` (`--do_verify=0`), then phase 3 `--rw=read --verify_only=1` on the seeded extent | Runs |
| Stress only, no integrity check | `--skip-lifecycle` | Full FIO stress suite | Skipped |

### Setup-only mode (`--setup-only`)

`--setup-only` / `cluster.setup_only` creates the namespace, PVCs, and FIO pods, waits until they are Bound/Ready, then exits. FIO, lifecycle, and phase-3 verify are not run. Cleanup is skipped (`--no-cleanup`) so the resources stay in the cluster (for example a manual `fio` exec). Each new `run` still resets the namespace.

```bash
./odf-io-stress run --setup-only --num-pvc 4
./odf-io-stress run --setup-only --rbd-num-pvc 2 --cephfs-num-pvc 0
```

`--dry-run --setup-only` still only prints manifests.

### Write-verify mode (`--write-verify`)

Use this when you only care that the storage can **write and read back crc32c correctly**, without the stress suite or clone/snapshot.

`--write-verify` / `cluster.write_verify` replaces phase 1 with one FIO job (`write-verify`): sequential `--rw=write` of `--fio-size`, `--verify=crc32c`, `--do_verify=1`, `--verify_backlog=1`. FIO writes a block and verifies it before moving on. The job is not `--time_based`, so the full size is written and verified. Size follows `--fio-size` / `tools.fio.size` (default `1G`).

```bash
# All default PVCs (4 RBD + 4 CephFS), 1G write+verify each
./odf-io-stress run --write-verify

# Larger region
./odf-io-stress run --write-verify --fio-size 2G

# RBD only (Filesystem + Block pods)
./odf-io-stress run --write-verify --rbd-num-pvc 2 --cephfs-num-pvc 0

# Leave pods up after verify
./odf-io-stress run --write-verify --num-pvc 4 --no-cleanup

# From config
./odf-io-stress run --config odf-io-stress.yaml
```

```yaml
cluster:
  write_verify: true
  rbd:
    num_pvc: 2
  cephfs:
    num_pvc: 2
tools:
  fio:
    size: 2G   # region written and verified
```

This is **not** phase-3 `verify_only` (read-only check of a previously seeded clone/restore). If both `--write-verify` and `--setup-only` are set, setup-only wins (provision and exit).

### Lifecycle verify (default: seed + `verify_only`)

A normal `run` (without `--write-verify`) still verifies data, but later: phase 2 sequentially seeds a known extent, then phase 3 runs `--verify_only=1` against clone and restored volumes.

```bash
# Full suite including clone/restore integrity
./odf-io-stress run

# Smaller Block seed/verify extent (default 512m)
./odf-io-stress run --seed-size 256m

# Skip that path entirely
./odf-io-stress run --skip-lifecycle
```

```yaml
cluster:
  write_verify: false
  skip_lifecycle: false
  seed_size: 512m   # Block seed/verify extent; Filesystem uses tools.fio.size
```

### Block vs Filesystem data-integrity

On **Filesystem** volumes the seed and verify target a dedicated file (`/mnt/data/fio.dat`) sized to `tools.fio.size` / `--fio-size`. Fio creates that file, writes it sequentially, and verify cannot see leftover phase-1 IO.

On **Block** volumes (raw RBD, `/dev/rbdblock`) there is no private file: the clone inherits the entire device, including phase-1 4 KiB verify headers, non-verify writes, and unwritten zeros. The harness therefore:

- Sequentially seeds a **bounded region** from offset 0 (`cluster.seed_size` / `--seed-size`, default `512m`) with a single block size (`256k`), `--rw=write`, `--verify=crc32c`, **no** `--time_based` / `--runtime`.
- Runs `phase3-verify` as `--rw=read --verify_only=1` with the **same** `--bs` and `--size`.
- Confines the concurrent **`expand-verify`** write to the region the volume *grew into* (`--offset=max(pvc_size, seed_size)`, `--size=expanded−offset`). On Block the expand runs on the raw device *while a clone is being COW-copied*, so an unconstrained 4k randwrite over `[0, seed_size)` would clobber the seeded blocks the clone captures and make `phase3-verify` fail on the clone. Filesystem volumes keep writing a dedicated headroom-sized file (`/mnt/data/expand-verify.dat`) instead.

**Invariant:** the integrity verify never reads a region or block size the seed did not deterministically write, and no concurrent writer (sustain, expand-verify) touches the seeded `[0, seed_size)` extent on Block. Violating that produces false-positive `bad magic header` / `bad header length` / `crc32c verify failed` errors that are **not** storage-product faults.

### Troubleshooting false-positive verify errors

| Symptom | Likely harness cause |
|---------|----------------------|
| `verify: bad magic header 0, wanted acca` at offset 0 | Seed was random/`time_based` (or truncated) so the start of the device was never written |
| `verify: bad header length 4096, wanted 262144` | Verify `--size`/`--bs` larger than the sequential seed; leftover phase-1 4 KiB headers |
| `crc32c: verify failed` on Block only | Verify walked past the seeded extent into mixed phase-1 data |

If `integrity-seed` reports pass but `phase3-verify` fails on Block, check that seed `write.io_bytes >= seed_size` and that both jobs share `bs`/`size`.

## Results

Per-run output is written under `results/<timestamp>/`, or under `--results-dir` when that flag is set:

- Individual FIO job JSON files
- Aggregated `report.json` with every job, including full FIO output
- `summary.json`, the small document to read first

Stdout is that same `summary.json` object. Logs and the human `=== RESULTS ===` block go to stderr. `--dry-run` still prints YAML manifests on stdout and does not write a summary.

Exit status: `0` pass, `1` the run finished and at least one job failed, `2` bad config or usage, `3` the cluster step failed before jobs finished. A non-zero exit sets the JSON `error` field (`N job(s) failed` on exit 1). `failures[].classification` is `harness` (known verify false positive), `storage`, or `unknown`. `failures[].durationSeconds` is seconds; `report.json` `duration` remains a Go duration in nanoseconds.

On an object run, exit 2 is also a cleanup failure after the workload passed (stuck OBC finalizers; the report is still written) and exit 3 is also an infrastructure failure (`infra-fail`, classification `infra`). Those two classifications are `cleanup` and `infra`. A capability the cluster does not implement is `skip`, not a failure. `--object-only` leaves `phase1`, `lifecycle`, and the FIO and volume flags out of `summary.json`. The human results block for that run is the object lines only.

The `results/` directory is gitignored.

## Project layout

```
cmd/odf-io-stress/   # CLI entrypoint (run, options, example, generate-config, hidden s3-worker)
pkg/config/          # Config types, load/merge, flags, defaults, object profiles
pkg/fio/             # FIO job definitions (pattern → job)
pkg/objstore/        # Deterministic keys/payloads and the S3 client used by s3-worker
pkg/worker/          # In-cluster object phases
pkg/k8s/             # Kubernetes helpers (PVC, pod, snapshot, ObjectBucketClaim)
pkg/workload/        # Orchestration (phases, dry-run, object run)
pkg/report/          # Result collection and summary
build/Dockerfile     # Worker image
docs/object-image.md # How to build and pass a custom worker image
```
