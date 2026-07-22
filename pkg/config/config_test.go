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
