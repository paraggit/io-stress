package config

import (
	"fmt"
	"time"
)

type Config struct {
	Cluster Cluster `yaml:"cluster" json:"cluster"`
	Tools   Tools   `yaml:"tools" json:"tools"`
}

type Cluster struct {
	Namespace            string   `yaml:"namespace" json:"namespace"`
	Kubeconfig           string   `yaml:"kubeconfig" json:"kubeconfig"`
	RBD                  Backend  `yaml:"rbd" json:"rbd"`
	CephFS               Backend  `yaml:"cephfs" json:"cephfs"`
	PVCSize              string   `yaml:"pvc_size" json:"pvc_size"`
	Prefix               string   `yaml:"prefix" json:"prefix"`
	WaitTimeout          Duration `yaml:"wait_timeout" json:"wait_timeout"`
	CloneTimeout         Duration `yaml:"clone_timeout" json:"clone_timeout"`
	NoCleanup            bool     `yaml:"no_cleanup" json:"no_cleanup"`
	DryRun               bool     `yaml:"dry_run" json:"dry_run"`
	LifecycleInterval    int      `yaml:"lifecycle_interval" json:"lifecycle_interval"`
	SkipLifecycle        bool     `yaml:"skip_lifecycle" json:"skip_lifecycle"`
	SkipFIOStress        bool     `yaml:"skip_fio_stress" json:"skip_fio_stress"`
	WriteVerify          bool     `yaml:"write_verify" json:"write_verify"`
	SetupOnly            bool     `yaml:"setup_only" json:"setup_only"`
	ExpandFactor         int      `yaml:"expand_factor" json:"expand_factor"`
	SnapshotClass        string   `yaml:"snapshot_class" json:"snapshot_class"`
	MaxParallelPods      int      `yaml:"max_parallel_pods" json:"max_parallel_pods"`
	MaxParallelProvision int      `yaml:"max_parallel_provision" json:"max_parallel_provision"`
	ResultsDir           string   `yaml:"results_dir" json:"results_dir"`
	SustainRuntime       int      `yaml:"sustain_runtime" json:"sustain_runtime"`
	SeedSize             string   `yaml:"seed_size" json:"seed_size"`
	AppTypes             []string `yaml:"app_types,omitempty" json:"app_types,omitempty"`
}

type Backend struct {
	NumPVC       int    `yaml:"num_pvc" json:"num_pvc"`
	StorageClass string `yaml:"storage_class" json:"storage_class"`
}

type Tools struct {
	FIO        FIO            `yaml:"fio" json:"fio"`
	VDBench    map[string]any `yaml:"vdbench,omitempty" json:"vdbench,omitempty"`
	SmallFiles map[string]any `yaml:"smallfiles,omitempty" json:"smallfiles,omitempty"`
}

type FIO struct {
	Image        string                `yaml:"image" json:"image"`
	Runtime      int                   `yaml:"runtime" json:"runtime"`
	Size         string                `yaml:"size" json:"size"`
	BlockSize    string                `yaml:"block_size" json:"block_size"`
	Offset       string                `yaml:"offset" json:"offset"`
	OutputFormat string                `yaml:"output_format" json:"output_format"`
	Parallel     bool                  `yaml:"parallel" json:"parallel"`
	Suites       Suites                `yaml:"suites" json:"suites"`
	AppSuites    map[string]AppProfile `yaml:"app_suites,omitempty" json:"app_suites,omitempty"`
}

type Suites struct {
	Common     []Pattern `yaml:"common" json:"common"`
	Filesystem []Pattern `yaml:"filesystem" json:"filesystem"`
	Block      []Pattern `yaml:"block" json:"block"`
	CephFSRWX  []Pattern `yaml:"cephfs_rwx" json:"cephfs_rwx"`
	Lifecycle  []Pattern `yaml:"lifecycle" json:"lifecycle"`
}

type Pattern struct {
	Name     string            `yaml:"name" json:"name"`
	Category string            `yaml:"category,omitempty" json:"category,omitempty"`
	Size     string            `yaml:"size,omitempty" json:"size,omitempty"`
	Runtime  *int              `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Params   map[string]string `yaml:"params" json:"params"`
}

func NewDefault() *Config {
	return &Config{
		Cluster: Cluster{
			Namespace: "odf-io-stress",
			RBD: Backend{
				NumPVC:       4,
				StorageClass: "ocs-storagecluster-ceph-rbd",
			},
			CephFS: Backend{
				NumPVC:       4,
				StorageClass: "ocs-storagecluster-cephfs",
			},
			PVCSize:              "10Gi",
			Prefix:               "odf-io",
			WaitTimeout:          Duration(5 * time.Minute),
			CloneTimeout:         Duration(20 * time.Minute),
			LifecycleInterval:    4,
			ExpandFactor:         2,
			SustainRuntime:       180,
			MaxParallelProvision: 4,
			SeedSize:             "512m",
		},
		Tools: Tools{
			FIO: FIO{
				Image:        "quay.io/ocsci/nginx:fio",
				Runtime:      60,
				Size:         "1G",
				BlockSize:    "512",
				Offset:       "512",
				OutputFormat: "json",
				Parallel:     true,
				Suites:       defaultSuites(),
				AppSuites:    defaultAppSuites(),
			},
		},
	}
}

func Validate(cfg *Config) error {
	MergeAppSuites(cfg)

	hasStandard := cfg.Cluster.RBD.NumPVC > 0 || cfg.Cluster.CephFS.NumPVC > 0
	hasAppTypes := len(cfg.Cluster.AppTypes) > 0
	if !hasStandard {
		return fmt.Errorf("at least one of rbd.num_pvc or cephfs.num_pvc must be >= 1")
	}
	if cfg.Cluster.Namespace == "" {
		return fmt.Errorf("namespace must not be empty")
	}
	if cfg.Cluster.RBD.NumPVC >= 1 && cfg.Cluster.RBD.StorageClass == "" {
		return fmt.Errorf("rbd storage class must not be empty when rbd.num_pvc >= 1")
	}
	if cfg.Cluster.CephFS.NumPVC >= 1 && cfg.Cluster.CephFS.StorageClass == "" {
		return fmt.Errorf("cephfs storage class must not be empty when cephfs.num_pvc >= 1")
	}
	if cfg.Cluster.PVCSize == "" {
		return fmt.Errorf("pvc-size must not be empty")
	}
	if cfg.Tools.FIO.Image == "" {
		return fmt.Errorf("image must not be empty")
	}
	if cfg.Tools.FIO.Runtime < 1 {
		return fmt.Errorf("runtime must be >= 1, got %d", cfg.Tools.FIO.Runtime)
	}
	if cfg.Cluster.ExpandFactor < 1 {
		return fmt.Errorf("expand-factor must be >= 1, got %d", cfg.Cluster.ExpandFactor)
	}
	if cfg.Cluster.CloneTimeout.Duration() <= 0 {
		return fmt.Errorf("clone-timeout must be > 0, got %v", cfg.Cluster.CloneTimeout.Duration())
	}
	if cfg.Cluster.MaxParallelProvision < 0 {
		return fmt.Errorf("max-parallel-provision must be >= 0, got %d", cfg.Cluster.MaxParallelProvision)
	}
	if cfg.Cluster.SeedSize == "" {
		return fmt.Errorf("seed-size must not be empty")
	}
	// Standard suites are used only when app_types is empty.
	if !cfg.Cluster.SkipFIOStress && !cfg.Cluster.WriteVerify && !cfg.Cluster.SetupOnly && !hasAppTypes && hasStandard {
		if len(cfg.Tools.FIO.Suites.Common) == 0 {
			return fmt.Errorf("when skip_fio_stress is false and volumes will be created, at least one common FIO pattern must be defined")
		}
	}

	// Validate negative NumPVC
	if cfg.Cluster.RBD.NumPVC < 0 {
		return fmt.Errorf("rbd.num_pvc must not be negative, got %d", cfg.Cluster.RBD.NumPVC)
	}
	if cfg.Cluster.CephFS.NumPVC < 0 {
		return fmt.Errorf("cephfs.num_pvc must not be negative, got %d", cfg.Cluster.CephFS.NumPVC)
	}

	for _, p := range allPatterns(cfg.Tools.FIO.Suites) {
		if p.Name == "" {
			return fmt.Errorf("pattern name must not be empty")
		}
	}

	if err := validateAppTypes(cfg); err != nil {
		return err
	}
	return nil
}

// ProvisionTimeout is the Bound wait for clone/restore PVCs. CephFS clone is a
// full subvolume copy and needs a longer budget than the shared WaitTimeout.
func (c Cluster) ProvisionTimeout(storageType string) time.Duration {
	cloneTO := c.CloneTimeout.Duration()
	if cloneTO <= 0 {
		cloneTO = 20 * time.Minute
	}
	if storageType == "cephfs" {
		return cloneTO
	}
	waitTO := c.WaitTimeout.Duration()
	if waitTO <= 0 {
		waitTO = 5 * time.Minute
	}
	return waitTO
}

// ProvisionLimit is the cap on in-flight clone/restore/expand operations.
func (c Cluster) ProvisionLimit() int64 {
	if c.MaxParallelProvision > 0 {
		return int64(c.MaxParallelProvision)
	}
	return 4
}

// ApplyWriteVerify, when WriteVerify is set, skips lifecycle/phase3 so FIO
// write+immediate-verify is the only integrity check. Phase 1 still runs.
func ApplyWriteVerify(cfg *Config) {
	if cfg == nil || !cfg.Cluster.WriteVerify {
		return
	}
	cfg.Cluster.SkipLifecycle = true
	cfg.Cluster.SkipFIOStress = false
}

// ApplySetupOnly, when SetupOnly is set, provisions PVCs/pods and skips
// workloads plus cleanup so the namespace is left ready for later use.
func ApplySetupOnly(cfg *Config) {
	if cfg == nil || !cfg.Cluster.SetupOnly {
		return
	}
	cfg.Cluster.SkipFIOStress = true
	cfg.Cluster.SkipLifecycle = true
	cfg.Cluster.NoCleanup = true
}

func allPatterns(s Suites) []Pattern {
	var out []Pattern
	out = append(out, s.Common...)
	out = append(out, s.Filesystem...)
	out = append(out, s.Block...)
	out = append(out, s.CephFSRWX...)
	out = append(out, s.Lifecycle...)
	return out
}
