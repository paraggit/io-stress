package workload

import (
	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

// usedRunOptions is the effective option set for a run, after defaults, config, and flags.
func usedRunOptions(cfg *config.Config, configPath string) report.UsedOptions {
	if cfg == nil {
		return report.UsedOptions{}
	}
	opts := report.UsedOptions{
		Config:               configPath,
		Kubeconfig:           cfg.Cluster.Kubeconfig,
		RBDNumPVC:            cfg.Cluster.RBD.NumPVC,
		CephFSNumPVC:         cfg.Cluster.CephFS.NumPVC,
		Namespace:            cfg.Cluster.Namespace,
		RBDStorageClass:      cfg.Cluster.RBD.StorageClass,
		CephFSStorageClass:   cfg.Cluster.CephFS.StorageClass,
		PVCSize:              cfg.Cluster.PVCSize,
		Image:                cfg.Tools.FIO.Image,
		Runtime:              cfg.Tools.FIO.Runtime,
		BlockSize:            cfg.Tools.FIO.BlockSize,
		Offset:               cfg.Tools.FIO.Offset,
		FIOSize:              cfg.Tools.FIO.Size,
		SeedSize:             cfg.Cluster.SeedSize,
		Prefix:               cfg.Cluster.Prefix,
		ResultsDir:           cfg.Cluster.ResultsDir,
		Timeout:              cfg.Cluster.WaitTimeout.Duration().String(),
		CloneTimeout:         cfg.Cluster.CloneTimeout.Duration().String(),
		Format:               cfg.Tools.FIO.OutputFormat,
		NoCleanup:            cfg.Cluster.NoCleanup,
		DryRun:               cfg.Cluster.DryRun,
		LifecycleInterval:    cfg.Cluster.LifecycleInterval,
		SkipLifecycle:        cfg.Cluster.SkipLifecycle,
		SkipFIOStress:        cfg.Cluster.SkipFIOStress,
		ExpandFactor:         cfg.Cluster.ExpandFactor,
		SnapshotClass:        cfg.Cluster.SnapshotClass,
		SustainRuntime:       cfg.Cluster.SustainRuntime,
		MaxParallel:          cfg.Cluster.MaxParallelPods,
		MaxParallelProvision: cfg.Cluster.MaxParallelProvision,
		Sequential:           !cfg.Tools.FIO.Parallel,
		AppType:              append([]string(nil), cfg.Cluster.AppTypes...),
	}
	if cfg.Cluster.RBD.NumPVC == cfg.Cluster.CephFS.NumPVC {
		opts.NumPVC = cfg.Cluster.RBD.NumPVC
	}
	return opts
}
