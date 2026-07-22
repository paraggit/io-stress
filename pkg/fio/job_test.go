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
	if len(jobs) != 2 {
		t.Errorf("ReducedSuite returned %d jobs, want 2", len(jobs))
	}
	names := map[string]bool{}
	for _, j := range jobs {
		names[j.Name] = true
	}
	if !names["data-integrity-4k"] {
		t.Error("ReducedSuite missing data-integrity-4k")
	}
	if !names["high-iodepth-stress"] {
		t.Error("ReducedSuite missing high-iodepth-stress")
	}
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
