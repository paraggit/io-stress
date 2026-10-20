package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestRecipesCoverNamedRuns(t *testing.T) {
	byName := map[string]Recipe{}
	for _, r := range Recipes() {
		byName[r.Name] = r
	}
	for _, name := range RecipeNames() {
		if _, ok := byName[name]; !ok {
			t.Errorf("missing recipe %s", name)
		}
	}

	smoke := byName["smoke"]
	if smoke.RBDNumPVC != 1 || smoke.CephFSNumPVC != 0 || smoke.Runtime != 30 || !smoke.SkipLifecycle || smoke.WriteVerify {
		t.Fatalf("smoke = %+v", smoke)
	}
	if !smoke.DeletesNamespace || smoke.Namespace != "odf-io-stress" {
		t.Fatalf("smoke namespace = %+v", smoke)
	}
	wantArgv := []string{"odf-io-stress", "run", "--recipe", "smoke", "--results-dir", "results/smoke"}
	if !reflect.DeepEqual(smoke.Argv, wantArgv) {
		t.Fatalf("argv = %v", smoke.Argv)
	}
	if smoke.DurationSeconds <= 0 || smoke.DurationSeconds >= byName["full"].DurationSeconds {
		t.Fatalf("durations smoke %d full %d", smoke.DurationSeconds, byName["full"].DurationSeconds)
	}

	wv := byName["write-verify"]
	if !wv.WriteVerify || !wv.SkipLifecycle || wv.RBDNumPVC != 1 || wv.CephFSNumPVC != 0 {
		t.Fatalf("write-verify = %+v", wv)
	}

	vm := byName["app-vm"]
	if len(vm.AppType) != 1 || vm.AppType[0] != "vm" || !vm.SkipLifecycle || vm.RBDNumPVC != 1 || vm.CephFSNumPVC != 0 {
		t.Fatalf("app-vm = %+v", vm)
	}
	db := byName["app-database"]
	if len(db.AppType) != 1 || db.AppType[0] != "database" || !db.SkipLifecycle {
		t.Fatalf("app-database = %+v", db)
	}

	full := byName["full"]
	if full.RBDNumPVC != 4 || full.CephFSNumPVC != 4 || full.Runtime != 60 || full.SkipLifecycle || full.WriteVerify || len(full.AppType) != 0 {
		t.Fatalf("full = %+v", full)
	}
}

func TestApplyRecipe_FlagsOverride(t *testing.T) {
	cfg := NewDefault()
	if err := ApplyRecipe(cfg, "smoke"); err != nil {
		t.Fatal(err)
	}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("runtime", 60, "")
	if err := fs.Parse([]string{"--runtime", "15"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangedFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.FIO.Runtime != 15 || cfg.Cluster.RBD.NumPVC != 1 || !cfg.Cluster.SkipLifecycle {
		t.Fatalf("runtime=%d rbd=%d skip=%v", cfg.Tools.FIO.Runtime, cfg.Cluster.RBD.NumPVC, cfg.Cluster.SkipLifecycle)
	}
}

func TestApplyRecipe_Unknown(t *testing.T) {
	err := ApplyRecipe(NewDefault(), "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown recipe") {
		t.Fatal(err)
	}
}

func TestApplyRecipe_FullResetsEarlierValues(t *testing.T) {
	cfg := NewDefault()
	cfg.Cluster.RBD.NumPVC = 1
	cfg.Cluster.CephFS.NumPVC = 0
	cfg.Cluster.AppTypes = []string{"vm"}
	cfg.Tools.FIO.Runtime = 10
	cfg.Cluster.SkipLifecycle = true
	if err := ApplyRecipe(cfg, "full"); err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.RBD.NumPVC != 4 || cfg.Cluster.CephFS.NumPVC != 4 || len(cfg.Cluster.AppTypes) != 0 || cfg.Tools.FIO.Runtime != 60 || cfg.Cluster.SkipLifecycle {
		t.Fatalf("rbd=%d cephfs=%d apps=%v runtime=%d skip=%v", cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC, cfg.Cluster.AppTypes, cfg.Tools.FIO.Runtime, cfg.Cluster.SkipLifecycle)
	}
}
