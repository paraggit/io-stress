package main

// workloadExamples are the run lines printed by `example workload`.
// They do not touch the cluster.
var workloadExamples = map[string]string{
	"fio": `# Default run (4 PVC/pod pairs per backend)
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
`,
	"object": `# Object workload only (no PVCs). See "Object workload" below.
./odf-io-stress run --object-only --object-profile smoke --results-dir results/object-smoke

# Same phases against Ceph RGW (OBC provisioning)
./odf-io-stress run --object-only --object-backend rgw --object-profile smoke --results-dir results/object-rgw-smoke
`,
}
