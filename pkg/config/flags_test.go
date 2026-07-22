package config

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestApplyChangedFlags(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("num-pvc", 4, "")
	fs.Int("rbd-num-pvc", 4, "")
	fs.String("namespace", "odf-io-stress", "")
	_ = fs.Parse([]string{"--namespace", "ns2", "--rbd-num-pvc", "1"})

	cfg := NewDefault()
	cfg.Cluster.Namespace = "from-file"
	cfg.Cluster.RBD.NumPVC = 9

	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Cluster.Namespace != "ns2" {
		t.Errorf("namespace=%q, want ns2", cfg.Cluster.Namespace)
	}
	if cfg.Cluster.RBD.NumPVC != 1 {
		t.Errorf("rbd=%d, want 1", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 4 { // unchanged
		t.Errorf("cephfs=%d, want 4", cfg.Cluster.CephFS.NumPVC)
	}
}

func TestApplyChangedFlags_NumPVCOrder(t *testing.T) {
	// Test that num-pvc is processed before per-backend flags
	// so per-backend wins when both are set
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("num-pvc", 0, "")
	fs.Int("rbd-num-pvc", 0, "")
	fs.Int("cephfs-num-pvc", 0, "")
	_ = fs.Parse([]string{"--num-pvc", "5", "--rbd-num-pvc", "2"})

	cfg := NewDefault()
	cfg.Cluster.RBD.NumPVC = 10
	cfg.Cluster.CephFS.NumPVC = 10

	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}

	// num-pvc sets both to 5, then rbd-num-pvc overrides RBD to 2
	if cfg.Cluster.RBD.NumPVC != 2 {
		t.Errorf("rbd=%d, want 2", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 5 {
		t.Errorf("cephfs=%d, want 5", cfg.Cluster.CephFS.NumPVC)
	}
}

func TestApplyChangedFlags_NoChangedFlags(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("namespace", "default", "")
	fs.Int("rbd-num-pvc", 4, "")
	// Don't parse anything, so no flags are changed

	cfg := NewDefault()
	original := cfg.Cluster.Namespace

	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}

	// Should remain unchanged
	if cfg.Cluster.Namespace != original {
		t.Errorf("namespace changed from %q to %q", original, cfg.Cluster.Namespace)
	}
}

func TestApplyChangedFlags_Sequential(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Bool("sequential", false, "")
	_ = fs.Parse([]string{"--sequential"})

	cfg := NewDefault()
	if !cfg.Tools.FIO.Parallel {
		t.Fatal("expected default parallel=true")
	}

	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Tools.FIO.Parallel {
		t.Error("sequential flag should set parallel=false")
	}
}