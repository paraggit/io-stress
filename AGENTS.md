# Agent guide

Before choosing how to run the tool, use the option list below. `odf-io-stress options` prints the same options as JSON, including each flag's default and help text, plus the rules in this file. If this guide and the command disagree, trust `odf-io-stress options`.

After a run, `summary.json` field `options` is the effective set that was used. Its keys are the long flag names.

## Commands

- `odf-io-stress run` runs the suite. Stdout is one JSON summary. Logs go to stderr.
- `odf-io-stress options` prints every option. It does not touch the cluster.
- `odf-io-stress generate-config` writes a sample config. It does not run a workload.
- `odf-io-stress --help` and `odf-io-stress run --help` print human help.

## Option rules

- Merge order: built-in defaults, then --config, then --recipe, then only flags explicitly set on the command line. sustain-runtime is recomputed as runtime×3 only when it is still 0; the built-in default is 180.
- --recipe applies a named run (smoke, write-verify, app-vm, app-database, full) after --config and before explicit flags, so flags you set override the recipe. odf-io-stress options lists each recipe's argv, PVC counts, namespace deletion, and approximate durationSeconds.
- --rbd-num-pvc alone (without --cephfs-num-pvc and without -n/--num-pvc) sets the CephFS count to 0. --cephfs-num-pvc alone sets the RBD count to 0.
- -n/--num-pvc sets both RBD and CephFS counts. A per-backend flag set in the same command overrides that side.
- --sequential sets parallel FIO off. --sequential=false turns parallel back on.
- --app-type is a comma-separated list of tools.fio.app_suites profiles. Built-ins are nfs, vm, database, messaging, and ai. The selected suites run on every PVC; standard suites and CephFS RWX are skipped.
- --write-verify runs one sequential write that crc32c-verifies each block and skips lifecycle. --setup-only creates PVCs and pods, then exits without FIO. If both are set, setup-only wins and also sets --no-cleanup.
- Every run deletes and recreates --namespace (default odf-io-stress) at the start. --no-cleanup skips cleanup on exit and does not stop that reset.
- --dry-run prints YAML manifests on stdout and does not write summary.json.
- --results-dir empty means results/<timestamp>. Pass it so the output path is known before the run.
- --kubeconfig empty uses KUBECONFIG or ~/.kube/config.
- --max-parallel 0 means unlimited concurrent pods. --snapshot-class empty auto-selects the VolumeSnapshotClass.
- --format is the FIO output format: json or normal.
- tools.vdbench and tools.smallfiles in a config file are ignored.
- generate-config writes a sample config. -o - writes it to stdout. --force overwrites an existing file. It does not run a workload.
- The object workload is opt-in via --object. --object-only sets RBD and CephFS PVC counts to 0, skips FIO stress and lifecycle, and is the only case where zero PVCs is valid. --object-resume requires --object-only and --object-seed and does not delete the namespace. --object-profile (smoke, standard, scale, large) applies before other explicit object flags. delete_mode both empties even-ordinal buckets and deletes odd-ordinal OBCs while they are still full; a single bucket uses obc-only. S3 calls run only in worker pods. A capability probe answer of NotImplemented, MethodNotAllowed, or HTTP 501 skips that capability instead of failing the run.

## run options

| Flag | Default | Purpose |
|------|---------|---------|
| `--config` | empty | YAML (`.yaml`/`.yml`) or JSON (`.json`) config file |
| `--recipe` | empty | Named run: `smoke`, `write-verify`, `app-vm`, `app-database`, `full`. Flags set on the command override it |
| `--kubeconfig` | empty | Kubeconfig path; empty uses `KUBECONFIG` or `~/.kube/config` |
| `-n, --num-pvc` | `4` | Set both RBD and CephFS PVC counts |
| `--rbd-num-pvc` | `4` | RBD PVC/pod pairs. Alone, CephFS becomes 0 |
| `--cephfs-num-pvc` | `4` | CephFS PVC/pod pairs. Alone, RBD becomes 0 |
| `-N, --namespace` | `odf-io-stress` | Namespace created and deleted for the run |
| `--rbd-storage-class` | `ocs-storagecluster-ceph-rbd` | RBD StorageClass |
| `--cephfs-storage-class` | `ocs-storagecluster-cephfs` | CephFS StorageClass |
| `--pvc-size` | `10Gi` | PVC size |
| `-i, --image` | `quay.io/ocsci/nginx:fio` | FIO container image |
| `-r, --runtime` | `60` | FIO runtime in seconds |
| `-b, --bs` | `512` | FIO block size |
| `--offset` | `512` | FIO offset |
| `--fio-size` | `1G` | FIO file or device size |
| `--seed-size` | `512m` | Block integrity seed/verify extent |
| `-p, --prefix` | `odf-io` | Resource name prefix |
| `--results-dir` | `results/<timestamp>` | Directory for `report.json` and `summary.json` |
| `-t, --timeout` | `5m` | Wait for PVC/pod readiness. Also the RBD clone/restore wait |
| `--clone-timeout` | `20m` | CephFS clone/restore Bound wait |
| `-f, --format` | `json` | FIO output format: `json` or `normal` |
| `--no-cleanup` | `false` | Skip resource cleanup on exit |
| `--dry-run` | `false` | Print YAML manifests and do not create resources |
| `--lifecycle-interval` | `4` | Run lifecycle ops on every Nth pod |
| `--skip-lifecycle` | `false` | Skip lifecycle storm and verify phases |
| `--skip-fio-stress` | `false` | Skip the FIO stress phase |
| `--write-verify` | `false` | One sequential write that crc32c-verifies each block; skips lifecycle |
| `--setup-only` | `false` | Create PVCs and pods, then exit without FIO. Implies `--no-cleanup`. Wins over `--write-verify` |
| `--expand-factor` | `2` | PVC expand size multiplier |
| `--snapshot-class` | empty | VolumeSnapshotClass override; empty auto-selects |
| `--sustain-runtime` | `180` | Sustain workload seconds. Recomputed as runtime×3 only when still 0 |
| `--max-parallel` | `0` | Max concurrent pods; 0 is unlimited |
| `--max-parallel-provision` | `4` | Max in-flight PVC create, bind, pod create, clone, restore, and expand operations |
| `--sequential` | `false` | Run FIO jobs one at a time |
| `--app-type` | empty | Comma-separated profiles: `nfs`, `vm`, `database`, `messaging`, `ai` |
| `--object` | `false` | Enable the NooBaa object workload |
| `--object-only` | `false` | Run only the object workload (no PVCs or FIO) |
| `--object-profile` | `standard` | Object profile: `smoke`, `standard`, `scale`, or `large` |
| `--object-image` | `quay.io/paraggit/odf-io-stress:latest` | Custom object worker image. Overrides the profile and the config file |
| `--object-buckets` | `10` | Number of object buckets |
| `--object-count` | `5000` | Objects per bucket |
| `--object-size-mix` | `4KB:60,1MB:30,16MB:10` | Weighted object sizes |
| `--object-workers` | `4` | Worker pods per object phase |
| `--object-key-layout` | `hashed` | Object key layout: `hashed`, `flat`, or `deep` |
| `--object-verify` | `full` | Object verify mode: `full`, `etag`, or `sample` |
| `--object-versioned` | `2` | How many buckets have versioning enabled |
| `--object-phases` | `create,write,read,verify,list,copy,delete,teardown` | Comma-separated object phases |
| `--object-delete-mode` | `both` | Object delete mode: `recursive`, `obc-only`, or `both` |
| `--object-storage-class` | `openshift-storage.noobaa.io` | ObjectBucketClaim StorageClass |
| `--object-provisioning` | `obc` | Bucket provisioning: `obc` or `s3` |
| `--object-seed` | empty | Hex seed; reproduces a prior object run |
| `--object-resume` | `false` | Reuse buckets from `--object-seed`; requires `--object-only` and does not delete the namespace |
| `--object-insecure-tls` | `false` | Skip S3 TLS verification (logged) |
| `--object-force` | `false` | Run the object workload even when the capacity preflight fails |

## Recipes

`odf-io-stress options` prints `recipes[]` with `argv`, PVC counts, `deletesNamespace`, and `durationSeconds`. Every recipe deletes and recreates namespace `odf-io-stress` at the start. `durationSeconds` is a rough wall-clock budget: pods run together, jobs on one pod run one after another.

| Recipe | Run |
|--------|-----|
| `smoke` | 1 RBD volume, 30s FIO, no lifecycle |
| `write-verify` | 1 RBD volume, one crc32c write-and-verify, no lifecycle |
| `app-vm` | `vm` profile on 1 RBD volume, lifecycle skipped |
| `app-database` | `database` profile on 1 RBD volume, lifecycle skipped |
| `full` | 4 RBD and 4 CephFS, stress, lifecycle, and verify |

```bash
odf-io-stress run --recipe app-vm --results-dir results/app-vm
odf-io-stress run --recipe smoke --runtime 15 --results-dir results/smoke
```

## generate-config options

| Flag | Default | Purpose |
|------|---------|---------|
| `-o, --output` | `odf-io-stress.yaml` | Output path. `-` writes to stdout |
| `--force` | `false` | Overwrite an existing file |

## Read the result

Stdout of `run` is one JSON object. The same bytes are `summary.json` in the results directory. Logs and the human `=== RESULTS ===` block go to stderr.

`options` on that object is the effective flag set for the run. `num-pvc` is present only when the RBD and CephFS counts are equal. `rbd-num-pvc` and `cephfs-num-pvc` are always the counts that were used.

| Exit | Meaning | What to read |
|------|---------|----------------|
| 0 | Pass | `summary.json` |
| 1 | Run finished, one or more jobs failed | `summary.json` `failures`, then those jobs in `report.json` |
| 2 | Bad config or usage | `error` in the JSON. No report. |
| 3 | Cluster step failed before the run finished cleanly | `error` in the JSON. `summary.json` is written when the results directory exists. |

An object run uses the same numbers for two more outcomes. Exit 2 is also a cleanup failure after the object workload itself passed (stuck OBC finalizers or a namespace left behind); `summary.json` is written in that case. Exit 3 is also an infrastructure failure (`infra-fail`: the worker was OOMKilled, evicted, or exited without a final result). A skipped capability does not fail the run. A hidden `s3-worker` subcommand runs inside the worker pod; it is not part of `odf-io-stress options`.

`failures[]` fields: `pod`, `job`, `storage`, `volumeMode`, `exitCode`, `durationSeconds`, `error`, `classification`.

`classification`:

- `harness` — likely a verify false positive: `bad magic header`, `bad header length`, or `crc32c` verify failed on a Block volume
- `storage` — the job failed for another reason
- `unknown` — the failure text was empty
- `infra` — object worker was OOMKilled, evicted, or exited without a final result
- `cleanup` — object cleanup left an OBC, ObjectBucket, or namespace behind

`durationSeconds` is seconds. In `report.json`, `duration` is a Go `time.Duration` in nanoseconds.

Per-job files in the same directory are named `<pod>-<job>.json`. Full FIO output is on those records and in `report.json`, not in `summary.json`.
