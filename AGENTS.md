# Agent guide

Run the tool, then read `summary.json`. Open `report.json` only for jobs listed in `failures`.

## Run

Smoke run (one RBD volume, no lifecycle, known output directory):

```bash
odf-io-stress run --rbd-num-pvc 1 --cephfs-num-pvc 0 --runtime 30 --skip-lifecycle --results-dir results/smoke
```

Every `run` deletes and recreates the namespace (`odf-io-stress` unless `--namespace` is set).

`--dry-run` prints YAML manifests on stdout. It does not print the JSON summary.

Pass `--results-dir` so the output path is known before the run starts. Without it, the directory is `results/<timestamp>/`.

## Read the result

Stdout is one JSON object. The same bytes are `summary.json` in the results directory. Logs and the human `=== RESULTS ===` block go to stderr.

| Exit | Meaning | What to read |
|------|---------|----------------|
| 0 | Pass | `summary.json` |
| 1 | Run finished, one or more jobs failed | `summary.json` `failures`, then those jobs in `report.json` |
| 2 | Bad config or usage | `error` in the JSON. No report. |
| 3 | Cluster step failed before the run finished cleanly | `error` in the JSON. `summary.json` is written when the results directory exists. |

`failures[]` fields: `pod`, `job`, `storage`, `volumeMode`, `exitCode`, `durationSeconds`, `error`, `classification`.

`classification`:

- `harness` — likely a verify false positive: `bad magic header`, `bad header length`, or `crc32c` verify failed on a Block volume
- `storage` — the job failed for another reason
- `unknown` — the failure text was empty

`durationSeconds` is seconds. In `report.json`, `duration` is a Go `time.Duration` in nanoseconds.

Per-job files in the same directory are named `<pod>-<job>.json`. Full FIO output is on those records and in `report.json`, not in `summary.json`.
