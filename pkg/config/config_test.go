package config

import (
	"testing"
	"time"
)

func TestNewDefault(t *testing.T) {
	cfg := NewDefault()
	if cfg.Cluster.RBD.NumPVC != 4 {
		t.Errorf("RBD.NumPVC = %d, want 4", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 4 {
		t.Errorf("CephFS.NumPVC = %d, want 4", cfg.Cluster.CephFS.NumPVC)
	}
	if cfg.Cluster.Namespace != "odf-io-stress" {
		t.Errorf("Namespace = %q", cfg.Cluster.Namespace)
	}
	if cfg.Cluster.RBD.StorageClass != "ocs-storagecluster-ceph-rbd" {
		t.Errorf("RBD SC = %q", cfg.Cluster.RBD.StorageClass)
	}
	if cfg.Cluster.CephFS.StorageClass != "ocs-storagecluster-cephfs" {
		t.Errorf("CephFS SC = %q", cfg.Cluster.CephFS.StorageClass)
	}
	if cfg.Tools.FIO.Runtime != 60 {
		t.Errorf("FIO.Runtime = %d, want 60", cfg.Tools.FIO.Runtime)
	}
	if cfg.Tools.FIO.Size != "1G" {
		t.Errorf("FIO.Size = %q", cfg.Tools.FIO.Size)
	}
	if time.Duration(cfg.Cluster.WaitTimeout) != 5*time.Minute {
		t.Errorf("WaitTimeout = %v", cfg.Cluster.WaitTimeout)
	}
	if !cfg.Tools.FIO.Parallel {
		t.Error("Parallel should default true")
	}
	if cfg.Cluster.SustainRuntime != 180 {
		t.Errorf("SustainRuntime = %d, want 180", cfg.Cluster.SustainRuntime)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{"valid defaults", func(c *Config) {}, false},
		{"zero both PVCs", func(c *Config) {
			c.Cluster.RBD.NumPVC = 0
			c.Cluster.CephFS.NumPVC = 0
		}, true},
		{"rbd only ok", func(c *Config) { c.Cluster.CephFS.NumPVC = 0 }, false},
		{"cephfs only ok", func(c *Config) { c.Cluster.RBD.NumPVC = 0 }, false},
		{"empty namespace", func(c *Config) { c.Cluster.Namespace = "" }, true},
		{"empty RBD SC when rbd>0", func(c *Config) { c.Cluster.RBD.StorageClass = "" }, true},
		{"empty RBD SC ok when rbd=0", func(c *Config) {
			c.Cluster.RBD.NumPVC = 0
			c.Cluster.RBD.StorageClass = ""
		}, false},
		{"empty PVC size", func(c *Config) { c.Cluster.PVCSize = "" }, true},
		{"empty FIO image", func(c *Config) { c.Tools.FIO.Image = "" }, true},
		{"zero runtime", func(c *Config) { c.Tools.FIO.Runtime = 0 }, true},
		{"zero expand factor", func(c *Config) { c.Cluster.ExpandFactor = 0 }, true},
		{"empty pattern name", func(c *Config) {
			c.Tools.FIO.Suites.Common = []Pattern{{Name: "", Params: map[string]string{"rw": "read"}}}
		}, true},
		{"empty common suite when stress not skipped", func(c *Config) {
			c.Tools.FIO.Suites.Common = []Pattern{}
		}, true},
		{"empty common suite ok when stress skipped", func(c *Config) {
			c.Cluster.SkipFIOStress = true
			c.Tools.FIO.Suites.Common = []Pattern{}
		}, false},
		{"negative RBD NumPVC", func(c *Config) {
			c.Cluster.RBD.NumPVC = -1
		}, true},
		{"negative CephFS NumPVC", func(c *Config) {
			c.Cluster.CephFS.NumPVC = -1
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewDefault()
			tt.modify(cfg)
			err := Validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultSuitesJobNames(t *testing.T) {
	s := NewDefault().Tools.FIO.Suites
	common := names(s.Common)
	for _, n := range []string{
		"unaligned-direct", "unaligned-buffered", "unaligned-randread",
		"obj-boundary-3m", "obj-boundary-5m", "mixed-bs-verify",
		"data-integrity-4k", "seq-write-verify", "high-iodepth-stress",
		"overwrite-frag-stress", "high-concurrency-randrw", "compress-pattern-stress",
	} {
		if !common[n] {
			t.Errorf("common missing %q", n)
		}
	}
	fs := names(s.Filesystem)
	for _, n := range []string{"truncate-write", "fsync-stress", "fdatasync-mixed", "append-write"} {
		if !fs[n] {
			t.Errorf("filesystem missing %q", n)
		}
	}
	block := names(s.Block)
	for _, n := range []string{"trim-write-interleave", "trim-stress", "write-zeroes", "sub-4k-rmw"} {
		if !block[n] {
			t.Errorf("block missing %q", n)
		}
	}
	rwx := names(s.CephFSRWX)
	for _, n := range []string{"rwx-concurrent-write", "rwx-read-while-write"} {
		if !rwx[n] {
			t.Errorf("cephfs_rwx missing %q", n)
		}
	}
	life := names(s.Lifecycle)
	for _, n := range []string{"data-integrity-4k", "high-iodepth-stress"} {
		if !life[n] {
			t.Errorf("lifecycle missing %q", n)
		}
	}
}

func names(patterns []Pattern) map[string]bool {
	m := map[string]bool{}
	for _, p := range patterns {
		m[p.Name] = true
	}
	return m
}

func TestNewDefault_ActiveFIO(t *testing.T) {
	cfg := NewDefault()
	if cfg.Tools.Active != "fio" {
		t.Fatalf("active=%q", cfg.Tools.Active)
	}
	if cfg.Tools.VDBench.Image != "quay.io/pakamble/vdbench:latest" {
		t.Fatalf("vdbench image=%q", cfg.Tools.VDBench.Image)
	}
	if len(cfg.Tools.VDBench.Block.Patterns) < 1 {
		t.Fatal("expected default vdbench block patterns")
	}
}

func TestValidate_VDBench(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{"vdbench ok", func(c *Config) {
			c.Tools.Active = "vdbench"
		}, false},
		{"unknown active", func(c *Config) { c.Tools.Active = "other" }, true},
		{"vdbench empty image", func(c *Config) {
			c.Tools.Active = "vdbench"
			c.Tools.VDBench.Image = ""
		}, true},
		{"vdbench no block patterns with rbd", func(c *Config) {
			c.Tools.Active = "vdbench"
			c.Tools.VDBench.Block.Patterns = nil
		}, true},
		{"vdbench bad rdpct", func(c *Config) {
			c.Tools.Active = "vdbench"
			c.Tools.VDBench.Block.Patterns[0].Rdpct = 101
		}, true},
		{"fio still ok with empty vdbench patterns", func(c *Config) {
			c.Tools.Active = "fio"
			c.Tools.VDBench.Block.Patterns = nil
			c.Tools.VDBench.Filesystem.Patterns = nil
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewDefault()
			tt.modify(cfg)
			err := Validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
