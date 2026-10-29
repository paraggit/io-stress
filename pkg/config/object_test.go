package config

import (
	"os"
	"path/filepath"
	"strings"
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

func registerObjectApplyFlags(fs *pflag.FlagSet, cfg *Config) {
	fs.String("object-profile", cfg.Tools.Object.Profile, "")
	fs.String("object-backend", BackendNooBaa, "")
	fs.String("object-storage-class", cfg.Tools.Object.StorageClass, "")
	fs.Bool("object", false, "")
}

func TestObjectBackendFlagOverridesProfile(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg := NewDefault()
	registerObjectApplyFlags(fs, cfg)
	if err := fs.Parse([]string{"--object-profile", "scale", "--object-backend", "rgw"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Object.Backend != BackendRGW {
		t.Fatalf("backend = %q, want %q", cfg.Tools.Object.Backend, BackendRGW)
	}
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Object.StorageClass != "" {
		t.Fatalf("storage class = %q, want empty after Validate", cfg.Tools.Object.StorageClass)
	}
}

func TestObjectStorageClassFlagWinsOnRGW(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg := NewDefault()
	registerObjectApplyFlags(fs, cfg)
	if err := fs.Parse([]string{"--object-backend", "rgw", "--object-storage-class", "my-class"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Object.StorageClass != "my-class" {
		t.Fatalf("storage class = %q, want my-class", cfg.Tools.Object.StorageClass)
	}
}

func TestPublishedStorageClassDefault(t *testing.T) {
	def := NewDefault()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("object-backend", BackendNooBaa, "Object backend: noobaa or rgw")
	fs.String("object-storage-class", def.Tools.Object.StorageClass, "ObjectBucketClaim StorageClass")
	backend := fs.Lookup("object-backend")
	if backend == nil || backend.DefValue != BackendNooBaa {
		t.Fatalf("object-backend default = %q, want %q", backend.DefValue, BackendNooBaa)
	}
	sc := fs.Lookup("object-storage-class")
	if sc == nil || sc.DefValue != DefaultNooBaaStorageClass {
		t.Fatalf("object-storage-class default = %q, want %q", sc.DefValue, DefaultNooBaaStorageClass)
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

func TestNormalizeObjectBackend(t *testing.T) {
	if NewDefault().Tools.Object.StorageClass != DefaultNooBaaStorageClass {
		t.Fatalf("NewDefault storage class = %q, want %q", NewDefault().Tools.Object.StorageClass, DefaultNooBaaStorageClass)
	}

	loadYAML := func(t *testing.T, content string) *Config {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "cfg.yaml")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	tests := []struct {
		name       string
		setup      func(t *testing.T) *Config
		wantErr    bool
		errSubstrs []string
		check      func(t *testing.T, cfg *Config)
	}{
		{
			name: "unset backend",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object = standardObject()
				return cfg
			},
			check: func(t *testing.T, cfg *Config) {
				o := cfg.Tools.Object
				if o.Backend != BackendNooBaa {
					t.Fatalf("backend = %q, want %q", o.Backend, BackendNooBaa)
				}
				if o.StorageClass != DefaultNooBaaStorageClass {
					t.Fatalf("storage class = %q, want %q", o.StorageClass, DefaultNooBaaStorageClass)
				}
			},
		},
		{
			name: "profile keeps backend",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object = standardObject()
				cfg.Tools.Object.Backend = BackendRGW
				if err := ApplyObjectProfile(&cfg.Tools.Object, "scale"); err != nil {
					t.Fatal(err)
				}
				return cfg
			},
			check: func(t *testing.T, cfg *Config) {
				o := cfg.Tools.Object
				if o.Backend != BackendRGW {
					t.Fatalf("backend = %q, want %q", o.Backend, BackendRGW)
				}
				if o.StorageClass != "" {
					t.Fatalf("storage class = %q, want empty", o.StorageClass)
				}
				if o.Buckets != 50 || o.ObjectsPerBucket != 20000 {
					t.Fatalf("scale profile = buckets %d objects %d", o.Buckets, o.ObjectsPerBucket)
				}
			},
		},
		{
			name: "explicit class wins",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object = standardObject()
				if err := ApplyObjectProfile(&cfg.Tools.Object, "scale"); err != nil {
					t.Fatal(err)
				}
				cfg.Tools.Object.Backend = BackendRGW
				cfg.Tools.Object.StorageClass = "my-class"
				return cfg
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Tools.Object.StorageClass != "my-class" {
					t.Fatalf("storage class = %q, want my-class", cfg.Tools.Object.StorageClass)
				}
			},
		},
		{
			name: "config class wins",
			setup: func(t *testing.T) *Config {
				return loadYAML(t, `
tools:
  object:
    backend: rgw
    storage_class: my-class
    profile: standard
`)
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Tools.Object.StorageClass != "my-class" {
					t.Fatalf("storage class = %q, want my-class", cfg.Tools.Object.StorageClass)
				}
			},
		},
		{
			name: "config discovers",
			setup: func(t *testing.T) *Config {
				return loadYAML(t, `
tools:
  object:
    backend: rgw
    profile: standard
`)
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Tools.Object.StorageClass != "" {
					t.Fatalf("storage class = %q, want empty for discovery", cfg.Tools.Object.StorageClass)
				}
			},
		},
		{
			name: "noobaa unchanged",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object = standardObject()
				if err := ApplyObjectProfile(&cfg.Tools.Object, "smoke"); err != nil {
					t.Fatal(err)
				}
				return cfg
			},
			check: func(t *testing.T, cfg *Config) {
				o := cfg.Tools.Object
				if o.Backend != BackendNooBaa {
					t.Fatalf("backend = %q, want %q", o.Backend, BackendNooBaa)
				}
				if o.StorageClass != DefaultNooBaaStorageClass {
					t.Fatalf("storage class = %q, want %q", o.StorageClass, DefaultNooBaaStorageClass)
				}
			},
		},
		{
			name: "rgw plus s3",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object.Enabled = true
				cfg.Tools.Object.Backend = BackendRGW
				cfg.Tools.Object.Provisioning = "s3"
				return cfg
			},
			wantErr:    true,
			errSubstrs: []string{"OBC", "rgw"},
		},
		{
			name: "alias namespace",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object.NooBaaNamespace = "openshift-storage"
				cfg.Tools.Object.ODFNamespace = ""
				return cfg
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Tools.Object.ODFNamespace != "openshift-storage" {
					t.Fatalf("ODFNamespace = %q, want openshift-storage", cfg.Tools.Object.ODFNamespace)
				}
			},
		},
		{
			name: "both namespaces differ",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object.Enabled = true
				cfg.Tools.Object.NooBaaNamespace = "openshift-storage"
				cfg.Tools.Object.ODFNamespace = "other-ns"
				return cfg
			},
			wantErr: true,
		},
		{
			name: "empty namespace",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object.Enabled = true
				cfg.Tools.Object.NooBaaNamespace = ""
				cfg.Tools.Object.ODFNamespace = ""
				return cfg
			},
			wantErr:    true,
			errSubstrs: []string{"odf_namespace"},
		},
		{
			name: "rgw empty class validates",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object.Enabled = true
				cfg.Tools.Object.Backend = BackendRGW
				cfg.Tools.Object.Provisioning = "obc"
				return cfg
			},
			wantErr: false,
		},
		{
			name: "noobaa empty class",
			setup: func(t *testing.T) *Config {
				cfg := NewDefault()
				cfg.Tools.Object.Enabled = true
				cfg.Tools.Object.Backend = BackendNooBaa
				cfg.Tools.Object.StorageClass = ""
				cfg.Tools.Object.Provisioning = "obc"
				return cfg
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setup(t)
			NormalizeObjectBackend(&cfg.Tools.Object)
			if tt.check != nil {
				tt.check(t, cfg)
			}
			err := Validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			for _, sub := range tt.errSubstrs {
				if err == nil || !strings.Contains(err.Error(), sub) {
					t.Fatalf("Validate() error = %v, want substring %q", err, sub)
				}
			}
		})
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
