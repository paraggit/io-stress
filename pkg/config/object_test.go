package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func TestObjectProfileExpansion(t *testing.T) {
	smoke, err := objectProfile("smoke")
	if err != nil {
		t.Fatal(err)
	}
	if smoke.Buckets != 2 || smoke.ObjectsPerBucket != 500 || smoke.Workers != 2 || smoke.VerifyMode != "full" {
		t.Fatalf("smoke = buckets %d objects %d workers %d verify %s", smoke.Buckets, smoke.ObjectsPerBucket, smoke.Workers, smoke.VerifyMode)
	}
	scale, err := objectProfile("scale")
	if err != nil {
		t.Fatal(err)
	}
	if scale.Buckets != 50 || scale.ObjectsPerBucket != 20000 || scale.Workers != 16 || scale.VerifyMode != "sample" || scale.VerifySamplePct != 1 {
		t.Fatalf("scale = %+v", scale)
	}
	if _, err := objectProfile("nope"); err == nil {
		t.Fatal("expected unknown profile error")
	}
}

func TestLoadObjectProfileExplicitFieldsWin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `
tools:
  object:
    profile: smoke
    buckets: 3
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Object.Profile != "smoke" {
		t.Fatalf("profile = %q", cfg.Tools.Object.Profile)
	}
	if cfg.Tools.Object.Buckets != 3 {
		t.Fatalf("buckets = %d, want 3 (explicit field beats profile)", cfg.Tools.Object.Buckets)
	}
	if cfg.Tools.Object.ObjectsPerBucket != 500 || cfg.Tools.Object.Workers != 2 {
		t.Fatalf("smoke fields not applied: objects %d workers %d", cfg.Tools.Object.ObjectsPerBucket, cfg.Tools.Object.Workers)
	}
	if cfg.Tools.Object.Enabled {
		t.Fatal("profile expansion must not enable the workload")
	}
}

func TestApplyChangedFlagsObjectProfileThenFlag(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("object-profile", "standard", "")
	fs.Int("object-buckets", 10, "")
	fs.String("object-image", DefaultObjectImage, "")
	fs.Bool("object", false, "")
	if err := fs.Parse([]string{"--object-profile", "smoke", "--object-buckets", "9", "--object", "--object-image", "quay.io/example/odf-io-stress:dev"}); err != nil {
		t.Fatal(err)
	}
	cfg := NewDefault()
	cfg.Tools.Object.Buckets = 10
	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Object.Buckets != 9 {
		t.Fatalf("buckets = %d, want 9", cfg.Tools.Object.Buckets)
	}
	if cfg.Tools.Object.ObjectsPerBucket != 500 {
		t.Fatalf("objects = %d, want smoke 500", cfg.Tools.Object.ObjectsPerBucket)
	}
	if !cfg.Tools.Object.Enabled {
		t.Fatal("expected --object to enable")
	}
	if cfg.Tools.Object.Image != "quay.io/example/odf-io-stress:dev" {
		t.Fatalf("image = %q, want the --object-image value", cfg.Tools.Object.Image)
	}
}

func TestApplyObjectOnlyZeroesPVCs(t *testing.T) {
	cfg := NewDefault()
	cfg.Tools.Object.Only = true
	cfg.Cluster.WriteVerify = true
	ApplyWriteVerify(cfg)
	ApplyObjectOnly(cfg)
	if !cfg.Tools.Object.Enabled || !cfg.Cluster.SkipFIOStress || !cfg.Cluster.SkipLifecycle {
		t.Fatalf("object-only did not skip FIO: %+v", cfg.Cluster)
	}
	if cfg.Cluster.RBD.NumPVC != 0 || cfg.Cluster.CephFS.NumPVC != 0 {
		t.Fatalf("pvcs rbd=%d cephfs=%d, want 0", cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC)
	}
}

func TestValidateObject(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{"disabled zero pvcs still rejected", func(c *Config) {
			c.Cluster.RBD.NumPVC = 0
			c.Cluster.CephFS.NumPVC = 0
		}, true},
		{"enabled zero pvcs ok", func(c *Config) {
			c.Tools.Object.Enabled = true
			c.Cluster.RBD.NumPVC = 0
			c.Cluster.CephFS.NumPVC = 0
		}, false},
		{"enabled empty image", func(c *Config) {
			c.Tools.Object.Enabled = true
			c.Tools.Object.Image = ""
		}, true},
		{"sample pct out of range", func(c *Config) {
			c.Tools.Object.Enabled = true
			c.Tools.Object.VerifySamplePct = 101
		}, true},
		{"negative sample pct", func(c *Config) {
			c.Tools.Object.Enabled = true
			c.Tools.Object.VerifySamplePct = -1
		}, true},
		{"empty size mix", func(c *Config) {
			c.Tools.Object.Enabled = true
			c.Tools.Object.SizeMix = nil
		}, true},
		{"workers below 1", func(c *Config) {
			c.Tools.Object.Enabled = true
			c.Tools.Object.Workers = 0
		}, true},
		{"resume without seed", func(c *Config) {
			c.Tools.Object.Only = true
			c.Tools.Object.Resume = true
			c.Tools.Object.Seed = ""
			ApplyObjectOnly(c)
		}, true},
		{"resume without object-only", func(c *Config) {
			c.Tools.Object.Resume = true
			c.Tools.Object.Seed = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		}, true},
		{"resume ok", func(c *Config) {
			c.Tools.Object.Only = true
			c.Tools.Object.Resume = true
			c.Tools.Object.Seed = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
			ApplyObjectOnly(c)
		}, false},
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

func TestOrderedPhasesCanonical(t *testing.T) {
	got, err := OrderedPhases([]string{"verify", "write"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "write" || got[1] != "verify" {
		t.Fatalf("phases = %v", got)
	}
	if _, err := OrderedPhases([]string{"nope"}); err == nil {
		t.Fatal("expected unknown phase error")
	}
}

func TestGenerateConfigIncludesObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.yaml")
	if err := WriteSample(path, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Object.Enabled {
		t.Fatal("sample must keep the object workload disabled")
	}
	if cfg.Tools.Object.Profile != "standard" || cfg.Tools.Object.Buckets != 10 || cfg.Tools.Object.ObjectsPerBucket != 5000 {
		t.Fatalf("sample object = profile %s buckets %d objects %d", cfg.Tools.Object.Profile, cfg.Tools.Object.Buckets, cfg.Tools.Object.ObjectsPerBucket)
	}
	if cfg.Tools.Object.Image == "" {
		t.Fatal("sample object image is empty")
	}
}
