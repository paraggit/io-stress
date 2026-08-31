package fio

import (
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestJobsForVolume_RBDFilesystem(t *testing.T) {
	cfg := config.NewDefault()
	jobs := JobsForVolume("rbd", "Filesystem", cfg)

	names := map[string]bool{}
	for _, j := range jobs {
		names[j.Name] = true
	}

	expected := []string{
		"unaligned-direct", "unaligned-buffered", "unaligned-randread",
		"obj-boundary-3m", "obj-boundary-5m",
		"mixed-bs-verify", "data-integrity-4k", "seq-write-verify",
		"high-iodepth-stress", "overwrite-frag-stress", "high-concurrency-randrw",
		"compress-pattern-stress",
		"truncate-write", "fsync-stress", "fdatasync-mixed", "append-write",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing job %q for RBD Filesystem", name)
		}
	}

	unwanted := []string{"trim-write-interleave", "trim-stress", "write-zeroes", "sub-4k-rmw"}
	for _, name := range unwanted {
		if names[name] {
			t.Errorf("unexpected block job %q for RBD Filesystem", name)
		}
	}
}

func TestJobsForVolume_RBDBlock(t *testing.T) {
	cfg := config.NewDefault()
	jobs := JobsForVolume("rbd", "Block", cfg)

	names := map[string]bool{}
	for _, j := range jobs {
		names[j.Name] = true
	}

	expected := []string{
		"unaligned-direct", "unaligned-buffered", "unaligned-randread",
		"trim-write-interleave", "trim-stress", "write-zeroes", "sub-4k-rmw",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing job %q for RBD Block", name)
		}
	}

	unwanted := []string{"truncate-write", "fsync-stress", "fdatasync-mixed", "append-write"}
	for _, name := range unwanted {
		if names[name] {
			t.Errorf("unexpected FS job %q for RBD Block", name)
		}
	}
}

func TestJobsForVolume_CephFSFilesystem(t *testing.T) {
	cfg := config.NewDefault()
	jobs := JobsForVolume("cephfs", "Filesystem", cfg)

	names := map[string]bool{}
	for _, j := range jobs {
		names[j.Name] = true
	}

	if !names["truncate-write"] {
		t.Error("CephFS Filesystem should include truncate-write")
	}
	if names["trim-write-interleave"] {
		t.Error("CephFS should not include block jobs")
	}
}

func TestReducedSuite(t *testing.T) {
	cfg := config.NewDefault()
	jobs := ReducedSuite("/mnt/data/fio.dat", cfg)
	if len(jobs) != 1 {
		t.Errorf("ReducedSuite returned %d jobs, want 1", len(jobs))
	}
	names := map[string]bool{}
	for _, j := range jobs {
		names[j.Name] = true
	}
	if !names["data-integrity-4k"] {
		t.Error("ReducedSuite missing data-integrity-4k")
	}
	if names["high-iodepth-stress"] {
		t.Error("ReducedSuite must not include high-iodepth-stress (clobbers integrity seed)")
	}
}

func TestIntegritySeedAndVerify_SameCoverageNoTimeBased(t *testing.T) {
	cfg := config.NewDefault()
	cfg.Tools.FIO.Size = "2G"

	seed := IntegritySeedJob(cfg)
	verify := IntegrityVerifyJob(cfg)

	if seed.Name != "integrity-seed" {
		t.Errorf("seed name = %q, want integrity-seed", seed.Name)
	}
	if verify.Name != "phase3-verify" {
		t.Errorf("verify name = %q, want phase3-verify", verify.Name)
	}

	seedArgs := joinArgs(seed.Args)
	verifyArgs := joinArgs(verify.Args)

	for _, jobName := range []string{"seed", "verify"} {
		args := seedArgs
		if jobName == "verify" {
			args = verifyArgs
		}
		for _, forbidden := range []string{"--time_based", "--runtime="} {
			if containsArgPrefix(args, forbidden) {
				t.Errorf("%s job must not include %s; got %v", jobName, forbidden, args)
			}
		}
	}

	wantSize := "--size=2G"
	wantBS := "--bs=" + IntegritySeedBS
	if !containsArg(seed.Args, wantSize) || !containsArg(verify.Args, wantSize) {
		t.Errorf("seed/verify size mismatch: seed=%v verify=%v", seed.Args, verify.Args)
	}
	if !containsArg(seed.Args, wantBS) || !containsArg(verify.Args, wantBS) {
		t.Errorf("seed/verify bs mismatch: seed=%v verify=%v", seed.Args, verify.Args)
	}
	if IntegritySize(cfg) != "2G" {
		t.Errorf("IntegritySize = %q, want 2G", IntegritySize(cfg))
	}
	if !containsArg(seed.Args, "--rw=write") {
		t.Errorf("seed must be sequential write, got %v", seed.Args)
	}
	if !containsArg(verify.Args, "--rw=read") {
		t.Errorf("verify must be sequential read, got %v", verify.Args)
	}
	if !containsArg(verify.Args, "--verify_only=1") {
		t.Errorf("verify must be verify_only, got %v", verify.Args)
	}
	if !containsArg(seed.Args, "--verify=crc32c") || !containsArg(verify.Args, "--verify=crc32c") {
		t.Error("seed and verify must use crc32c")
	}
}

func TestBuildArgs_FilenameOverride(t *testing.T) {
	j := Job{
		Name:     "expand-verify",
		Filename: "/mnt/data/expand-verify.dat",
		Args:     []string{"--rw=write"},
	}
	args := BuildArgs(j, "/mnt/data/fio.dat", "json")
	if !containsArg(args, "--filename=/mnt/data/expand-verify.dat") {
		t.Errorf("expected Filename override, got %v", args)
	}
	if containsArg(args, "--filename=/mnt/data/fio.dat") {
		t.Errorf("pod target should not win over Job.Filename, got %v", args)
	}
}

func joinArgs(args []string) []string { return args }

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsArgPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if len(a) >= len(prefix) && a[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func TestCephFSRWXJobs(t *testing.T) {
	cfg := config.NewDefault()
	jobs := CephFSRWXJobs(cfg)
	if len(jobs) != 2 {
		t.Errorf("CephFSRWXJobs returned %d jobs, want 2", len(jobs))
	}
}

func TestBuildArgs(t *testing.T) {
	j := Job{
		Name: "test-job",
		Args: []string{"--rw=randwrite", "--bs=4k"},
	}
	args := BuildArgs(j, "/mnt/data/fio.dat", "json")
	found := map[string]bool{}
	for _, a := range args {
		found[a] = true
	}
	if !found["--name=test-job"] {
		t.Error("missing --name arg")
	}
	if !found["--filename=/mnt/data/fio.dat"] {
		t.Error("missing --filename arg")
	}
	if !found["--output-format=json"] {
		t.Error("missing --output-format arg")
	}
	if !found["--rw=randwrite"] {
		t.Error("missing --rw arg")
	}
}
