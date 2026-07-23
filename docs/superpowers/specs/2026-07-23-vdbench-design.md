# Vdbench Tool Support

**Date:** 2026-07-23  
**Status:** Approved for implementation planning

## Problem

`odf-io-stress` only executes FIO. The config already reserves `tools.vdbench`, but it is ignored. Users need Vdbench block and filesystem workloads on RBD/CephFS using a selectable IO engine.

## Goals

1. Selectable engine via `tools.active: fio|vdbench` and CLI `--tool`.
2. Typed `tools.vdbench` config matching the sample schema (`block` / `filesystem` + patterns).
3. Default image `quay.io/pakamble/vdbench:latest`.
4. Phase 1 runs Vdbench patterns by generating parameter files and exec’ing `vdbench` in-pod.
5. When `active=vdbench`, auto-skip Phase 2/3 lifecycle.
6. Keep FIO path unchanged when `active=fio` (default).

## Non-goals (v1)

- Vdbench-driven lifecycle / expand / clone / snapshot / verify.
- Running FIO and Vdbench in the same run.
- Smallfiles tool.
- Full parse of every Vdbench summary metric (exit code + raw output capture is enough for v1).

## Decisions

| Topic | Choice |
|-------|--------|
| Engine selection | `tools.active` + `--tool` override |
| Scope | Phase 1 only |
| Lifecycle when vdbench | Auto-skip |
| Mapping | By volume mode: `block` → RBD Block; `filesystem` → RBD FS + CephFS |
| Execution | Generate param file in pod, run `vdbench -f … -o …` |
| Approach | Typed config + param-file generator (Approach 1) |

## Config schema

```yaml
tools:
  active: vdbench   # fio | vdbench (default: fio)

  fio: { ... }      # existing

  vdbench:
    image: quay.io/pakamble/vdbench:latest
    runtime: 60                 # seconds → RD elapsed=
    output_dir: /tmp/vdbench-out

    block:
      size: "15g"               # SD size=
      patterns:
        - name: random_write
          rdpct: 0
          seekpct: 100
          xfersize: "4k"
          skew: 0

    filesystem:
      size: "10m"               # retained for parity; FSD uses file_size
      depth: 4
      width: 5
      files: 10
      file_size: "1m"           # FSD size=
      openflags: "o_direct"     # empty = buffered
      group_all_fwds_in_one_rd: true
      patterns:
        - name: sequential_write
          rdpct: 0
          seekpct: 0
          xfersize: "1m"
          skew: 0
```

### Types (sketch)

```go
type Tools struct {
    Active     string     `yaml:"active" json:"active"` // "fio" | "vdbench"
    FIO        FIO        `yaml:"fio" json:"fio"`
    VDBench    VDBench    `yaml:"vdbench" json:"vdbench"`
    SmallFiles map[string]any `yaml:"smallfiles,omitempty" json:"smallfiles,omitempty"`
}

type VDBench struct {
    Image      string           `yaml:"image" json:"image"`
    Runtime    int              `yaml:"runtime" json:"runtime"`
    OutputDir  string           `yaml:"output_dir" json:"output_dir"`
    Block      VDBenchBlock     `yaml:"block" json:"block"`
    Filesystem VDBenchFilesystem `yaml:"filesystem" json:"filesystem"`
}

type VDBenchPattern struct {
    Name     string `yaml:"name" json:"name"`
    Rdpct    int    `yaml:"rdpct" json:"rdpct"`
    Seekpct  int    `yaml:"seekpct" json:"seekpct"`
    Xfersize string `yaml:"xfersize" json:"xfersize"`
    Skew     int    `yaml:"skew" json:"skew"`
}

type VDBenchBlock struct {
    Size     string           `yaml:"size" json:"size"`
    Patterns []VDBenchPattern `yaml:"patterns" json:"patterns"`
}

type VDBenchFilesystem struct {
    Size                 string           `yaml:"size" json:"size"`
    Depth                int              `yaml:"depth" json:"depth"`
    Width                int              `yaml:"width" json:"width"`
    Files                int              `yaml:"files" json:"files"`
    FileSize             string           `yaml:"file_size" json:"file_size"`
    OpenFlags            string           `yaml:"openflags" json:"openflags"`
    GroupAllFWDsInOneRD  bool             `yaml:"group_all_fwds_in_one_rd" json:"group_all_fwds_in_one_rd"`
    Patterns             []VDBenchPattern `yaml:"patterns" json:"patterns"`
}
```

### Defaults

- `active: fio`
- Vdbench image: `quay.io/pakamble/vdbench:latest`
- Vdbench runtime: same default spirit as FIO (`60`)
- `output_dir: /tmp/vdbench-out`
- Sample `generate-config` includes the user’s example block/filesystem patterns when documenting vdbench (FIO defaults remain for `active: fio`)

### CLI

| Flag | Maps to |
|------|---------|
| `--tool fio\|vdbench` | `tools.active` |
| `--vdbench-image` | `tools.vdbench.image` |

Existing FIO flags unchanged; ignored for execution when `active=vdbench` (still may appear in merged config).

## Runtime behavior

### Pod setup

When `active=vdbench`:

- Pod image = `tools.vdbench.image`
- Container name = `iotool` for both FIO and Vdbench (parameterize `k8s.PodSpec` + all `ExecInPod` call sites; replaces hardcoded `"fio"`)
- Mount/device paths unchanged (`/mnt/data`, `/dev/rbdblock`)
- Entrypoint remains `sleep infinity`; workloads run via exec

### Pattern selection

| Pod | Patterns |
|-----|----------|
| RBD Block | `vdbench.block.patterns` |
| RBD Filesystem | `vdbench.filesystem.patterns` |
| CephFS | `vdbench.filesystem.patterns` |

### Per-pattern execution

1. Build param file text (`pkg/vdbench.BuildBlockParam` / `BuildFilesystemParam`).
2. Write file into pod at `/tmp/vdbench-<pattern>.vdbench` via exec (`sh -c 'cat > …'` with stdin, or equivalent).
3. Run:  
   `vdbench -f /tmp/vdbench-<pattern>.vdbench -o <output_dir>/<pod>/<pattern>`
4. Record `report.JobResult` with tool=`vdbench`, exit code, duration; store stdout/summary snippet as job artifact under `results/<run>/`.
5. Fail the job if exit code ≠ 0.

### Block param file template

```
sd=sd1,lun=<target>,openflags=o_direct,size=<block.size>
wd=wd1,sd=sd1,rdpct=<rdpct>,seekpct=<seekpct>,xfersize=<xfersize>,skew=<skew>
rd=rd1,wd=wd1,iorate=max,elapsed=<runtime>,interval=1
```

- `<target>` = `/dev/rbdblock` for block pods.
- `openflags=o_direct` for block SD by default.
- Block RD requires `iorate=` (use `max` for uncontrolled stress).

### Filesystem param file template

```
fsd=fsd1,anchor=/mnt/data,depth=<depth>,width=<width>,files=<files>,size=<file_size>[,openflags=<openflags>]
fwd=fwd1,fsd=fsd1,rdpct=<rdpct>,xfersize=<xfersize>,skew=<skew>,fileio=<mode>,fileselect=<mode>
rd=rd1,fwd=fwd1,fwdrate=max,format=yes,elapsed=<runtime>,interval=1
```

- Omit `openflags=` when empty (buffered).
- Config `seekpct` maps to FWD `fileio`/`fileselect`: `0` → `sequential`, else `random` (`seekpct` is WD-only and rejected on FWD).
- Filesystem RD uses `fwdrate=` (not `iorate=`); `format=yes` creates the file tree before the run.
- `group_all_fwds_in_one_rd` is kept in config for compatibility but **not emitted** — unsupported on vdbench50407 (default image).
- Block `size` must be ≤ PVC capacity (default `8g` for default `pvc_size: 10Gi`).

### Lifecycle

When `tools.active == vdbench` (after flag overlay):

- Log: `Phase 2/3 skipped (tools.active=vdbench)`
- Do not run Phase 2 or Phase 3 regardless of `skip_lifecycle` (vdbench mode implies skip).  
  If user sets `active=fio`, existing `skip_lifecycle` behavior remains.

### CephFS RWX

v1: **skip** CephFS RWX multi-pod sub-phase for vdbench (FIO-only feature). Log a short skip message.

## Validation

When `active=vdbench`:

- `vdbench.image` non-empty
- `vdbench.runtime >= 1`
- At least one pattern exists for volumes that will be created:
  - If any Block RBD PVC → `block.patterns` non-empty
  - If any Filesystem PVC (RBD FS or CephFS) → `filesystem.patterns` non-empty
- Every pattern: non-empty `name`, non-empty `xfersize`
- `rdpct` in `0..100`

When `active=fio`: existing FIO validation unchanged; vdbench section may be empty/absent.

Reject unknown `active` values.

## Architecture

```
cmd/odf-io-stress
  --tool / --vdbench-image → config

pkg/config
  Tools.Active, typed VDBench, Validate, WriteSample

pkg/vdbench
  BuildBlockParam / BuildFilesystemParam
  (optional) ParseSummary stub

pkg/k8s
  PodSpec.ContainerName (default "iotool")
  WriteFileInPod(ctx, ns, pod, container, path, data)
  ExecInPod unchanged

pkg/workload
  setupResources: image from active tool
  Phase1: if fio → existing; if vdbench → runVdbenchOnPod
  auto-skip phase2/3 when active=vdbench
```

## Error handling

| Case | Behavior |
|------|----------|
| Unknown `active` | Validate error |
| Missing patterns for required volume mode | Validate error |
| Param write / vdbench exec failure | Job fail; continue other patterns/pods (same as FIO) |
| Non-zero vdbench exit | Job fail |

## Testing

- Unit: param file generation for block and filesystem (golden strings).
- Unit: Validate active=vdbench with empty patterns → error; happy path → ok.
- Unit: flag `--tool vdbench` overlays `active`.
- Workload: dry-run or table test that active=vdbench selects vdbench image and skips lifecycle (no cluster required if tested via small exported helpers).

## Documentation

- README: `--tool`, vdbench schema summary, image default, lifecycle skip note.
- `generate-config` sample includes `tools.active` and a populated `vdbench` section (plus existing FIO).

## Migration

- Default remains `active: fio` — no behavior change for existing users.
- Remove warn-and-ignore for non-empty `tools.vdbench` in `main.go`.
- Replace `VDBench map[string]any` with typed struct (breaking for anyone who put arbitrary YAML under `vdbench`; none supported before).
