package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestRunCommandSelectsWorkload(t *testing.T) {
	t.Run("fio app profile", func(t *testing.T) {
		cfg := mustParseRun(t, "run", "fio", "--app-type", "vm", "--runtime", "60")
		if cfg.Tools.FIO.Runtime != 60 || len(cfg.Cluster.AppTypes) != 1 || cfg.Cluster.AppTypes[0] != "vm" {
			t.Fatalf("fio config = runtime %d app %v", cfg.Tools.FIO.Runtime, cfg.Cluster.AppTypes)
		}
		if cfg.Tools.Object.Enabled || cfg.Tools.Object.Only {
			t.Fatalf("fio run enabled object: %+v", cfg.Tools.Object)
		}
	})

	t.Run("object defaults to noobaa", func(t *testing.T) {
		cfg := mustParseRun(t, "run", "object", "--object-profile", "smoke")
		assertObjectOnly(t, cfg, config.BackendNooBaa, "smoke")
	})

	t.Run("object noobaa", func(t *testing.T) {
		cfg := mustParseRun(t, "run", "object", "noobaa", "--object-profile", "smoke")
		assertObjectOnly(t, cfg, config.BackendNooBaa, "smoke")
	})

	t.Run("object rgw", func(t *testing.T) {
		cfg := mustParseRun(t, "run", "object", "rgw", "--object-profile", "smoke")
		assertObjectOnly(t, cfg, config.BackendRGW, "smoke")
	})

	t.Run("fio with object", func(t *testing.T) {
		cfg := mustParseRun(t, "run", "fio", "--recipe", "smoke", "--object", "--object-backend", "rgw")
		if cfg.Cluster.RBD.NumPVC != 1 || cfg.Cluster.CephFS.NumPVC != 0 || cfg.Tools.FIO.Runtime != 30 {
			t.Fatalf("smoke recipe = rbd %d cephfs %d runtime %d", cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC, cfg.Tools.FIO.Runtime)
		}
		if !cfg.Tools.Object.Enabled || cfg.Tools.Object.Only || cfg.Tools.Object.Backend != config.BackendRGW {
			t.Fatalf("combined object = enabled %v only %v backend %s", cfg.Tools.Object.Enabled, cfg.Tools.Object.Only, cfg.Tools.Object.Backend)
		}
	})

	t.Run("legacy fio flags", func(t *testing.T) {
		cfg := mustParseRun(t, "run", "--app-type", "vm", "--runtime", "60")
		if cfg.Tools.FIO.Runtime != 60 || len(cfg.Cluster.AppTypes) != 1 || cfg.Cluster.AppTypes[0] != "vm" {
			t.Fatalf("legacy fio config = runtime %d app %v", cfg.Tools.FIO.Runtime, cfg.Cluster.AppTypes)
		}
		if cfg.Tools.Object.Enabled || cfg.Tools.Object.Only {
			t.Fatalf("legacy fio enabled object: %+v", cfg.Tools.Object)
		}
	})

	t.Run("legacy object-only", func(t *testing.T) {
		cfg := mustParseRun(t, "run", "--object-only", "--object-profile", "smoke")
		assertObjectOnly(t, cfg, config.BackendNooBaa, "smoke")
	})

	t.Run("fio command clears object from config", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "object.yaml")
		body := []byte("tools:\n  object:\n    enabled: true\n    only: true\n")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := mustParseRun(t, "run", "fio", "--config", path)
		if cfg.Tools.Object.Enabled || cfg.Tools.Object.Only {
			t.Fatalf("run fio kept object from config: %+v", cfg.Tools.Object)
		}
		if cfg.Cluster.RBD.NumPVC != 4 || cfg.Cluster.CephFS.NumPVC != 4 {
			t.Fatalf("pvc counts = rbd %d cephfs %d", cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC)
		}
	})
}

func TestRunCommandRejectsBadWorkloadWords(t *testing.T) {
	t.Run("backend disagrees with flag", func(t *testing.T) {
		err := parseRunErr(t, "run", "object", "noobaa", "--object-backend", "rgw")
		var plain *plainUsageError
		if !errors.As(err, &plain) || !strings.Contains(err.Error(), "noobaa") || !strings.Contains(err.Error(), "rgw") {
			t.Fatalf("error = %v, want a plain usage error naming both backends", err)
		}
	})

	t.Run("backend flag agrees", func(t *testing.T) {
		cfg := mustParseRun(t, "run", "object", "rgw", "--object-backend", "rgw", "--object-profile", "smoke")
		assertObjectOnly(t, cfg, config.BackendRGW, "smoke")
	})

	t.Run("unknown backend", func(t *testing.T) {
		err := parseRunErr(t, "run", "object", "ceph")
		var plain *plainUsageError
		if !errors.As(err, &plain) || !strings.Contains(err.Error(), "ceph") {
			t.Fatalf("error = %v, want a plain usage error naming ceph", err)
		}
		var stdout, stderr bytes.Buffer
		if code := writeExit(&stdout, &stderr, err); code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want no agent JSON", stdout.String())
		}
		if !strings.Contains(stderr.String(), "odf-io-stress run object rgw") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})

	t.Run("fio rejects object-only", func(t *testing.T) {
		err := parseRunErr(t, "run", "fio", "--object-only")
		var plain *plainUsageError
		if !errors.As(err, &plain) || !strings.Contains(err.Error(), "run object") {
			t.Fatalf("error = %v, want a plain usage error pointing at run object", err)
		}
	})
}

func TestRunHelpShowsWorkloads(t *testing.T) {
	fio := runExample(t, []string{"run", "fio", "-h"})
	for _, want := range []string{"--app-type", "--runtime", "--object", "--object-backend"} {
		if !strings.Contains(fio, want) {
			t.Errorf("run fio -h missing %s", want)
		}
	}
	if strings.Contains(fio, "--object-only") {
		t.Fatalf("run fio -h shows --object-only:\n%s", fio)
	}

	object := runExample(t, []string{"run", "object", "-h"})
	for _, want := range []string{"noobaa", "rgw", "--object-profile"} {
		if !strings.Contains(object, want) {
			t.Errorf("run object -h missing %s", want)
		}
	}
	for _, hidden := range []string{"--app-type", "--object-only", "--object-backend", "--recipe"} {
		if strings.Contains(object, hidden) {
			t.Errorf("run object -h shows %s", hidden)
		}
	}

	parent := runExample(t, []string{"run", "-h"})
	for _, want := range []string{"fio", "object", "odf-io-stress run fio", "odf-io-stress run object"} {
		if !strings.Contains(parent, want) {
			t.Errorf("run -h missing %q", want)
		}
	}
}

func mustParseRun(t *testing.T, args ...string) *config.Config {
	t.Helper()
	cfg, err := parseRun(t, args...)
	if err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return cfg
}

func parseRunErr(t *testing.T, args ...string) error {
	t.Helper()
	_, err := parseRun(t, args...)
	if err == nil {
		t.Fatalf("execute %v: want an error", args)
	}
	return err
}

func parseRun(t *testing.T, args ...string) (*config.Config, error) {
	t.Helper()
	var got *config.Config
	prev := startRun
	t.Cleanup(func() { startRun = prev })
	startRun = func(_ context.Context, cfg *config.Config, _, _ string) error {
		got = cfg
		return nil
	}
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		return nil, err
	}
	if got == nil {
		t.Fatal("command returned without starting a run")
	}
	return got, nil
}

func assertObjectOnly(t *testing.T, cfg *config.Config, backend, profile string) {
	t.Helper()
	if !cfg.Tools.Object.Enabled || !cfg.Tools.Object.Only {
		t.Fatalf("object mode = enabled %v only %v", cfg.Tools.Object.Enabled, cfg.Tools.Object.Only)
	}
	if cfg.Tools.Object.Backend != backend || cfg.Tools.Object.Profile != profile {
		t.Fatalf("object = backend %s profile %s", cfg.Tools.Object.Backend, cfg.Tools.Object.Profile)
	}
	if cfg.Cluster.RBD.NumPVC != 0 || cfg.Cluster.CephFS.NumPVC != 0 || !cfg.Cluster.SkipFIOStress || !cfg.Cluster.SkipLifecycle {
		t.Fatalf("object-only volumes = rbd %d cephfs %d skipFIO %v skipLife %v",
			cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC, cfg.Cluster.SkipFIOStress, cfg.Cluster.SkipLifecycle)
	}
}
