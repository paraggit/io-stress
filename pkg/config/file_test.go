package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `
cluster:
  namespace: from-file
  rbd:
    num_pvc: 2
  cephfs:
    num_pvc: 1
tools:
  fio:
    runtime: 30
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.Namespace != "from-file" {
		t.Errorf("namespace = %q", cfg.Cluster.Namespace)
	}
	if cfg.Cluster.RBD.NumPVC != 2 || cfg.Cluster.CephFS.NumPVC != 1 {
		t.Errorf("pvc counts rbd=%d cephfs=%d", cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC)
	}
	if cfg.Tools.FIO.Runtime != 30 {
		t.Errorf("runtime = %d", cfg.Tools.FIO.Runtime)
	}
	// omitted keys keep defaults
	if cfg.Cluster.PVCSize != "10Gi" {
		t.Errorf("PVCSize default lost: %q", cfg.Cluster.PVCSize)
	}
}

func TestLoadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	content := `{"cluster":{"namespace":"json-ns"},"tools":{"fio":{"runtime":15}}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.Namespace != "json-ns" || cfg.Tools.FIO.Runtime != 15 {
		t.Fatalf("unexpected: ns=%q rt=%d", cfg.Cluster.Namespace, cfg.Tools.FIO.Runtime)
	}
}

func TestLoadUnsupportedExt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.toml")
	_ = os.WriteFile(path, []byte("x=1"), 0644)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for .toml")
	}
}

func TestWriteSampleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.yaml")
	if err := WriteSample(path, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteSample(path, false); err == nil {
		t.Fatal("expected refuse overwrite without force")
	}
	if err := WriteSample(path, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	def := NewDefault()
	if cfg.Cluster.Namespace != def.Cluster.Namespace {
		t.Errorf("namespace mismatch")
	}
	if cfg.Tools.FIO.Runtime != def.Tools.FIO.Runtime {
		t.Errorf("runtime mismatch")
	}
}

func TestWriteSampleStdout(t *testing.T) {
	// path "-" should not error; implementation may write to os.Stdout
	if err := WriteSample("-", false); err != nil {
		t.Fatal(err)
	}
}
