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
	fs.String("kubeconfig", "", "")
	_ = fs.Parse([]string{"--namespace", "ns2", "--rbd-num-pvc", "1", "--kubeconfig", "/tmp/kc"})

	cfg := NewDefault()
	cfg.Cluster.Namespace = "from-file"
	cfg.Cluster.RBD.NumPVC = 9
	cfg.Cluster.Kubeconfig = "from-file"

	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Cluster.Namespace != "ns2" {
		t.Errorf("namespace=%q, want ns2", cfg.Cluster.Namespace)
	}
	if cfg.Cluster.RBD.NumPVC != 1 {
		t.Errorf("rbd=%d, want 1", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 0 {
		t.Errorf("cephfs=%d, want 0 (--rbd-num-pvc alone disables CephFS)", cfg.Cluster.CephFS.NumPVC)
	}
	if cfg.Cluster.Kubeconfig != "/tmp/kc" {
		t.Errorf("kubeconfig=%q, want /tmp/kc", cfg.Cluster.Kubeconfig)
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

	// num-pvc sets both to 5, then rbd-num-pvc overrides RBD to 2; CephFS stays 5
	// because --num-pvc was also set (exclusive-backend zeroing does not apply).
	if cfg.Cluster.RBD.NumPVC != 2 {
		t.Errorf("rbd=%d, want 2", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 5 {
		t.Errorf("cephfs=%d, want 5", cfg.Cluster.CephFS.NumPVC)
	}
}

func TestApplyChangedFlags_RBDOnlyZerosCephFS(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("num-pvc", 4, "")
	fs.Int("rbd-num-pvc", 4, "")
	fs.Int("cephfs-num-pvc", 4, "")
	_ = fs.Parse([]string{"--rbd-num-pvc", "2"})

	cfg := NewDefault()
	cfg.Cluster.CephFS.NumPVC = 6 // config had CephFS

	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.RBD.NumPVC != 2 {
		t.Errorf("rbd=%d, want 2", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 0 {
		t.Errorf("cephfs=%d, want 0", cfg.Cluster.CephFS.NumPVC)
	}
}

func TestApplyChangedFlags_CephFSOnlyZerosRBD(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("num-pvc", 4, "")
	fs.Int("rbd-num-pvc", 4, "")
	fs.Int("cephfs-num-pvc", 4, "")
	_ = fs.Parse([]string{"--cephfs-num-pvc", "3"})

	cfg := NewDefault()
	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.RBD.NumPVC != 0 {
		t.Errorf("rbd=%d, want 0", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC != 3 {
		t.Errorf("cephfs=%d, want 3", cfg.Cluster.CephFS.NumPVC)
	}
}

func TestApplyChangedFlags_BothBackendsExplicit(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("num-pvc", 4, "")
	fs.Int("rbd-num-pvc", 4, "")
	fs.Int("cephfs-num-pvc", 4, "")
	_ = fs.Parse([]string{"--rbd-num-pvc", "2", "--cephfs-num-pvc", "1"})

	cfg := NewDefault()
	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.RBD.NumPVC != 2 || cfg.Cluster.CephFS.NumPVC != 1 {
		t.Fatalf("rbd=%d cephfs=%d, want 2 and 1", cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC)
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

func TestApplyChangedFlags_SequentialFalseOverridesConfig(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Bool("sequential", true, "")
	_ = fs.Parse([]string{"--sequential=false"})

	cfg := NewDefault()
	cfg.Tools.FIO.Parallel = false // as if config disabled parallel

	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Tools.FIO.Parallel {
		t.Error("--sequential=false should set parallel=true")
	}
}
