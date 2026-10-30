package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// Removing or swapping a printed example is the break: fio and object
// each have their own catalog, and an unknown name is usage, not a run.
func TestExampleWorkloadPrintsCatalog(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "fio", args: []string{"example", "fio"}, want: fioExamples},
		{name: "object", args: []string{"example", "object"}, want: objectExamples},
		{name: "workload fio", args: []string{"example", "workload", "fio"}, want: fioExamples},
		{name: "workload object", args: []string{"example", "workload", "object"}, want: objectExamples},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runExample(t, tt.args)
			if got != tt.want {
				t.Fatalf("stdout:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestExampleWorkloadWithNoNamePrintsBoth(t *testing.T) {
	got := runExample(t, []string{"example", "workload"})
	want := "# fio\n" + fioExamples + "# object\n" + objectExamples
	if got != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "schemaVersion") || strings.Contains(got, "accepts 1 arg") {
		t.Fatalf("missing workload name returned an agent error:\n%s", got)
	}
}

func TestExampleWorkloadRejectsUnknownName(t *testing.T) {
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"example", "workload", "vdbench"})
	err := root.Execute()
	var plain *plainUsageError
	if !errors.As(err, &plain) || !strings.Contains(err.Error(), "vdbench") {
		t.Fatalf("error = %v, want a plain usage error naming vdbench", err)
	}
	if strings.Contains(stdout.String(), "schemaVersion") {
		t.Fatalf("stdout has agent JSON: %s", stdout.String())
	}

	var out, errOut bytes.Buffer
	if code := writeExit(&out, &errOut, err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if strings.Contains(out.String(), "schemaVersion") || out.Len() != 0 {
		t.Fatalf("stdout = %q, want no agent JSON", out.String())
	}
	if !strings.Contains(errOut.String(), "odf-io-stress example workload fio") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestExampleHelpNamesTheCommand(t *testing.T) {
	parent := runExample(t, []string{"example", "workload", "-h"})
	for _, want := range []string{
		"odf-io-stress example workload",
		"odf-io-stress example workload fio",
		"odf-io-stress example workload object",
	} {
		if !strings.Contains(parent, want) {
			t.Errorf("example workload -h missing %q\n%s", want, parent)
		}
	}
	if strings.Contains(parent, "accepts 1 arg") || strings.Contains(parent, "schemaVersion") {
		t.Fatalf("example workload -h:\n%s", parent)
	}

	fio := runExample(t, []string{"example", "fio", "-h"})
	if !strings.Contains(fio, "Usage:\n  odf-io-stress example fio\n") {
		t.Fatalf("example fio -h:\n%s", fio)
	}
	if strings.Contains(fio, "Available Commands:") {
		t.Fatalf("example fio -h shows the parent command list:\n%s", fio)
	}
}

func runExample(t *testing.T, args []string) string {
	t.Helper()
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v\nstderr: %s", args, err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr: %s", stderr.String())
	}
	return stdout.String()
}

const fioExamples = `# Default run (4 PVC/pod pairs per backend)
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

# Database profile on one RBD volume, lifecycle skipped
./odf-io-stress run --recipe app-database --results-dir results/app-database

# One crc32c write-and-verify on one RBD volume
./odf-io-stress run --recipe write-verify --results-dir results/write-verify

# Default coverage: 4 RBD and 4 CephFS, stress, lifecycle, and verify
./odf-io-stress run --recipe full --results-dir results/full

# One RBD volume and no CephFS. A lone per-backend count zeros the other side
./odf-io-stress run --rbd-num-pvc 1

# One CephFS volume and no RBD
./odf-io-stress run --cephfs-num-pvc 1

# Create RBD PVCs and pods only
./odf-io-stress run --setup-only --rbd-num-pvc 2 --cephfs-num-pvc 0

# Run FIO jobs one at a time
./odf-io-stress run --sequential --recipe smoke

# VM-like IO on the default PVCs (4 RBD and 4 CephFS)
./odf-io-stress run --app-type vm --runtime 60

# VM-like IO on 2 RBD volumes and no CephFS
./odf-io-stress run --app-type vm --rbd-num-pvc 2 --runtime 60

# Each pod runs the nfs suite, then the database suite
./odf-io-stress run --app-type nfs,database --rbd-num-pvc 2 --cephfs-num-pvc 2

# Messaging (append log) and AI (checkpoint and dataset) profiles
./odf-io-stress run --app-type messaging --runtime 120
./odf-io-stress run --app-type ai --runtime 120
`

const objectExamples = `# Short run: 2 buckets, 500 objects, full verify, no PVCs
./odf-io-stress run --object-only --object-profile smoke --results-dir results/object-smoke

# Same phases against Ceph RGW (OBC provisioning)
./odf-io-stress run --object-only --object-backend rgw --object-profile smoke --results-dir results/object-rgw-smoke

# Default shape: 10 buckets, 5000 objects each
./odf-io-stress run --object-only --object-profile standard --results-dir results/object-standard

# One million small objects across 50 buckets (sampled verify)
./odf-io-stress run --object-only --object-profile scale --results-dir results/object-scale

# Large multipart objects
./odf-io-stress run --object-only --object-profile large --results-dir results/object-large

# Large multipart objects on Ceph RGW
./odf-io-stress run --object-only --object-backend rgw --object-profile large --results-dir results/object-rgw-large

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

# Name the RGW bucket StorageClass instead of discovering it
./odf-io-stress run --object-only --object-backend rgw --object-profile smoke \
  --object-storage-class ocs-storagecluster-ceph-rgw --dry-run

# Flat keys (one prefix) and ETag checks
./odf-io-stress run --object-only --object-profile smoke --object-key-layout flat --object-verify etag

# Deep key layout
./odf-io-stress run --object-only --object-profile smoke --object-key-layout deep

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

# Start the write phase even when the capacity preflight fails
./odf-io-stress run --object-only --object-profile large --object-force

# Skip S3 TLS verification. The skip is logged
./odf-io-stress run --object-only --object-profile smoke --object-insecure-tls

# From a config file; flags still override
./odf-io-stress run --config odf-io-stress.yaml --object-only --object-profile smoke
./odf-io-stress run --config odf-io-stress.yaml --object-only \
  --object-image quay.io/my-org/odf-io-stress:dev
`
