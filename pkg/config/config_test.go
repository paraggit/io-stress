package config

import (
	"testing"
	"time"
)

func TestNewDefault(t *testing.T) {
	cfg := NewDefault()
	if cfg.NumPVC != 4 {
		t.Errorf("NumPVC = %d, want 4", cfg.NumPVC)
	}
	if cfg.Namespace != "odf-io-stress" {
		t.Errorf("Namespace = %q, want %q", cfg.Namespace, "odf-io-stress")
	}
	if cfg.RBDStorageClass != "ocs-storagecluster-ceph-rbd" {
		t.Errorf("RBDStorageClass = %q, want %q", cfg.RBDStorageClass, "ocs-storagecluster-ceph-rbd")
	}
	if cfg.CephFSStorageClass != "ocs-storagecluster-cephfs" {
		t.Errorf("CephFSStorageClass = %q, want %q", cfg.CephFSStorageClass, "ocs-storagecluster-cephfs")
	}
	if cfg.PVCSize != "10Gi" {
		t.Errorf("PVCSize = %q, want %q", cfg.PVCSize, "10Gi")
	}
	if cfg.FIOImage != "quay.io/ocsci/nginx:fio" {
		t.Errorf("FIOImage = %q, want %q", cfg.FIOImage, "quay.io/ocsci/nginx:fio")
	}
	if cfg.FIORuntime != 60 {
		t.Errorf("FIORuntime = %d, want 60", cfg.FIORuntime)
	}
	if cfg.FIOBlockSize != "512" {
		t.Errorf("FIOBlockSize = %q, want %q", cfg.FIOBlockSize, "512")
	}
	if cfg.FIOOffset != "512" {
		t.Errorf("FIOOffset = %q, want %q", cfg.FIOOffset, "512")
	}
	if cfg.FIOSize != "1G" {
		t.Errorf("FIOSize = %q, want %q", cfg.FIOSize, "1G")
	}
	if cfg.Prefix != "odf-io" {
		t.Errorf("Prefix = %q, want %q", cfg.Prefix, "odf-io")
	}
	if cfg.WaitTimeout != 5*time.Minute {
		t.Errorf("WaitTimeout = %v, want 5m", cfg.WaitTimeout)
	}
	if !cfg.Parallel {
		t.Error("Parallel should default to true")
	}
	if cfg.LifecycleInterval != 4 {
		t.Errorf("LifecycleInterval = %d, want 4", cfg.LifecycleInterval)
	}
	if cfg.ExpandFactor != 2 {
		t.Errorf("ExpandFactor = %d, want 2", cfg.ExpandFactor)
	}
	if cfg.SustainRuntime != 180 {
		t.Errorf("SustainRuntime = %d, want 180 (FIORuntime*3)", cfg.SustainRuntime)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{"valid defaults", func(c *Config) {}, false},
		{"zero PVCs", func(c *Config) { c.NumPVC = 0 }, true},
		{"negative PVCs", func(c *Config) { c.NumPVC = -1 }, true},
		{"empty namespace", func(c *Config) { c.Namespace = "" }, true},
		{"empty RBD storage class", func(c *Config) { c.RBDStorageClass = "" }, true},
		{"empty CephFS storage class", func(c *Config) { c.CephFSStorageClass = "" }, true},
		{"empty PVC size", func(c *Config) { c.PVCSize = "" }, true},
		{"empty FIO image", func(c *Config) { c.FIOImage = "" }, true},
		{"zero runtime", func(c *Config) { c.FIORuntime = 0 }, true},
		{"zero expand factor", func(c *Config) { c.ExpandFactor = 0 }, true},
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
