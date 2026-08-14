package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAppSuites(t *testing.T) {
	cfg := NewDefault()
	want := []string{"ai", "database", "messaging", "nfs", "vm"}
	got := ValidAppTypes(cfg)
	if len(got) != len(want) {
		t.Fatalf("ValidAppTypes=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ValidAppTypes=%v, want %v", got, want)
		}
	}
	vm, ok := LookupAppProfile(cfg, "vm")
	if !ok || vm.ResolvedVolumeMode() != "Block" {
		t.Fatalf("vm profile volume_mode=%q ok=%v", vm.VolumeMode, ok)
	}
	nfs, ok := LookupAppProfile(cfg, "nfs")
	if !ok || nfs.ResolvedVolumeMode() != "Filesystem" {
		t.Fatalf("nfs profile volume_mode=%q ok=%v", nfs.VolumeMode, ok)
	}
}

func TestMergeAppSuitesKeepsBuiltinsWhenCustomAdded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `
cluster:
  namespace: test
  rbd:
    num_pvc: 1
  cephfs:
    num_pvc: 0
  app_types: [custom]
tools:
  fio:
    app_suites:
      custom:
        volume_mode: Filesystem
        patterns:
          - name: custom-job
            category: custom
            params:
              rw: read
              bs: 4k
              time_based: "1"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := LookupAppProfile(cfg, "nfs"); !ok {
		t.Fatal("expected built-in nfs to remain after loading custom suite")
	}
	custom, ok := LookupAppProfile(cfg, "custom")
	if !ok || len(custom.Patterns) != 1 || custom.Patterns[0].Name != "custom-job" {
		t.Fatalf("custom profile missing or wrong: %+v", custom)
	}
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestAppSuitePatternsFromConfig(t *testing.T) {
	cfg := NewDefault()
	patterns := AppSuitePatterns(cfg, "database")
	if len(patterns) < 1 {
		t.Fatal("expected database patterns")
	}
	if AppSuitePatterns(cfg, "missing") != nil {
		t.Fatal("expected nil for unknown type")
	}
}
