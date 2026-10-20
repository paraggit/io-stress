package config

import (
	"fmt"
	"strings"
)

const (
	recipeProvisionSeconds   = 180
	recipeWriteVerifySeconds = 180
	recipeLifecycleSeconds   = 600
)

// Recipe is one named run an agent can select with --recipe.
type Recipe struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Argv             []string `json:"argv"`
	RBDNumPVC        int      `json:"rbd-num-pvc"`
	CephFSNumPVC     int      `json:"cephfs-num-pvc"`
	Runtime          int      `json:"runtime"`
	AppType          []string `json:"app-type,omitempty"`
	SkipLifecycle    bool     `json:"skip-lifecycle"`
	WriteVerify      bool     `json:"write-verify"`
	DeletesNamespace bool     `json:"deletesNamespace"`
	Namespace        string   `json:"namespace"`
	DurationSeconds  int      `json:"durationSeconds"`
}

// RecipeNames is the stable --recipe vocabulary.
func RecipeNames() []string {
	return []string{"smoke", "write-verify", "app-vm", "app-database", "full"}
}

// ApplyRecipe sets the named run on cfg. Explicit CLI flags applied afterward win.
func ApplyRecipe(cfg *Config, name string) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	switch name {
	case "smoke":
		setRecipeBase(cfg)
		cfg.Cluster.RBD.NumPVC = 1
		cfg.Cluster.CephFS.NumPVC = 0
		cfg.Tools.FIO.Runtime = 30
		cfg.Cluster.SkipLifecycle = true
	case "write-verify":
		setRecipeBase(cfg)
		cfg.Cluster.RBD.NumPVC = 1
		cfg.Cluster.CephFS.NumPVC = 0
		cfg.Cluster.WriteVerify = true
		ApplyWriteVerify(cfg)
	case "app-vm":
		setAppRecipe(cfg, "vm")
	case "app-database":
		setAppRecipe(cfg, "database")
	case "full":
		def := NewDefault()
		setRecipeBase(cfg)
		cfg.Cluster.RBD.NumPVC = def.Cluster.RBD.NumPVC
		cfg.Cluster.CephFS.NumPVC = def.Cluster.CephFS.NumPVC
		cfg.Tools.FIO.Runtime = def.Tools.FIO.Runtime
		cfg.Cluster.SustainRuntime = def.Cluster.SustainRuntime
		cfg.Cluster.SkipLifecycle = false
	default:
		return fmt.Errorf("unknown recipe %q (valid: %s)", name, strings.Join(RecipeNames(), ", "))
	}
	return nil
}

func setRecipeBase(cfg *Config) {
	cfg.Cluster.SkipFIOStress = false
	cfg.Cluster.WriteVerify = false
	cfg.Cluster.SetupOnly = false
	cfg.Cluster.AppTypes = nil
}

func setAppRecipe(cfg *Config, app string) {
	setRecipeBase(cfg)
	cfg.Cluster.RBD.NumPVC = 1
	cfg.Cluster.CephFS.NumPVC = 0
	cfg.Cluster.AppTypes = []string{app}
	cfg.Cluster.SkipLifecycle = true
}

// Recipes describes every named run using built-in defaults plus that recipe.
func Recipes() []Recipe {
	out := make([]Recipe, 0, len(RecipeNames()))
	for _, name := range RecipeNames() {
		cfg := NewDefault()
		if err := ApplyRecipe(cfg, name); err != nil {
			continue
		}
		out = append(out, describeRecipe(cfg, name))
	}
	return out
}

func describeRecipe(cfg *Config, name string) Recipe {
	return Recipe{
		Name:             name,
		Description:      recipeDescription(name),
		Argv:             []string{"odf-io-stress", "run", "--recipe", name, "--results-dir", "results/" + name},
		RBDNumPVC:        cfg.Cluster.RBD.NumPVC,
		CephFSNumPVC:     cfg.Cluster.CephFS.NumPVC,
		Runtime:          cfg.Tools.FIO.Runtime,
		AppType:          append([]string(nil), cfg.Cluster.AppTypes...),
		SkipLifecycle:    cfg.Cluster.SkipLifecycle,
		WriteVerify:      cfg.Cluster.WriteVerify,
		DeletesNamespace: true,
		Namespace:        cfg.Cluster.Namespace,
		DurationSeconds:  EstimateDuration(cfg),
	}
}

func recipeDescription(name string) string {
	switch name {
	case "smoke":
		return "One RBD volume, 30s FIO, no lifecycle."
	case "write-verify":
		return "One RBD volume, one crc32c write-and-verify, no lifecycle."
	case "app-vm":
		return "vm profile on one RBD volume, lifecycle skipped."
	case "app-database":
		return "database profile on one RBD volume, lifecycle skipped."
	case "full":
		return "Default coverage: 4 RBD and 4 CephFS, stress, lifecycle, and verify."
	default:
		return ""
	}
}

// EstimateDuration is a rough wall-clock budget in seconds.
// Pods run together. Jobs on one pod run one after another.
// Write-verify is a size-based 1G write, not runtime multiplied by jobs.
// Lifecycle adds a fixed clone/snapshot budget plus sustain time.
func EstimateDuration(cfg *Config) int {
	if cfg == nil {
		return 0
	}
	seconds := recipeProvisionSeconds
	if cfg.Cluster.SetupOnly {
		return seconds
	}
	if cfg.Cluster.WriteVerify {
		seconds += recipeWriteVerifySeconds
	} else if !cfg.Cluster.SkipFIOStress {
		rt := cfg.Tools.FIO.Runtime
		if rt < 1 {
			rt = 1
		}
		seconds += busiestJobCount(cfg) * rt
	}
	if !cfg.Cluster.SkipLifecycle && !cfg.Cluster.WriteVerify {
		sustain := cfg.Cluster.SustainRuntime
		if sustain == 0 {
			sustain = cfg.Tools.FIO.Runtime * 3
		}
		seconds += recipeLifecycleSeconds + sustain
	}
	return seconds
}

func busiestJobCount(cfg *Config) int {
	if len(cfg.Cluster.AppTypes) > 0 {
		n := 0
		for _, at := range cfg.Cluster.AppTypes {
			n += len(AppSuitePatterns(cfg, at))
		}
		return n
	}
	common := len(cfg.Tools.FIO.Suites.Common)
	fs := common + len(cfg.Tools.FIO.Suites.Filesystem)
	block := common + len(cfg.Tools.FIO.Suites.Block)
	if block > fs {
		return block
	}
	return fs
}
