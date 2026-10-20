package workload

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

func TestUsedRunOptions(t *testing.T) {
	cfg := config.NewDefault()
	cfg.Cluster.RBD.NumPVC = 1
	cfg.Cluster.CephFS.NumPVC = 0
	cfg.Cluster.ResultsDir = "results/smoke"
	cfg.Tools.FIO.Runtime = 30
	cfg.Cluster.SkipLifecycle = true
	cfg.Cluster.WriteVerify = true
	cfg.Cluster.AppTypes = []string{"vm"}
	cfg.Tools.FIO.Parallel = false

	got := usedRunOptions(cfg, "my.yaml")
	if got.Config != "my.yaml" || got.RBDNumPVC != 1 || got.CephFSNumPVC != 0 || got.NumPVC != 0 {
		t.Fatalf("counts/config = %+v", got)
	}
	if got.Runtime != 30 || got.ResultsDir != "results/smoke" || !got.SkipLifecycle || !got.WriteVerify || got.SetupOnly || !got.Sequential {
		t.Fatalf("run knobs = %+v", got)
	}
	if len(got.AppType) != 1 || got.AppType[0] != "vm" {
		t.Fatalf("app-type = %v", got.AppType)
	}
	if got.Timeout != "5m0s" || got.CloneTimeout != "20m0s" {
		t.Fatalf("timeouts = %s %s", got.Timeout, got.CloneTimeout)
	}

	cfg.Cluster.RBD.NumPVC = 2
	cfg.Cluster.CephFS.NumPVC = 2
	if usedRunOptions(cfg, "").NumPVC != 2 {
		t.Fatal("num-pvc should be set when both backends match")
	}
}

func TestFinalize_RecordsOptions(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer
	err := finalize(&stdout, dir, []report.JobResult{
		{Pod: "p", Job: "j", Status: "pass"},
	}, nil, finalizeOpts{
		writeReport: true,
		hasOptions:  true,
		options: report.UsedOptions{
			Namespace:     "odf-io-stress",
			Runtime:       30,
			RBDNumPVC:     1,
			SkipLifecycle: true,
			ResultsDir:    dir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var summary report.AgentSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Options == nil || summary.Options.Namespace != "odf-io-stress" || summary.Options.Runtime != 30 || !summary.Options.SkipLifecycle {
		t.Fatalf("options = %+v", summary.Options)
	}
}
