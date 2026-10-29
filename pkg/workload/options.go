package workload

import (
	"strings"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

// usedRunOptions is the effective option set for a run, after defaults, config, and flags.
func usedRunOptions(cfg *config.Config, configPath, recipe string) report.UsedOptions {
	if cfg == nil {
		return report.UsedOptions{}
	}
	opts := report.UsedOptions{
		Config:               configPath,
		Recipe:               recipe,
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
		WriteVerify:          cfg.Cluster.WriteVerify,
		SetupOnly:            cfg.Cluster.SetupOnly,
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
	if cfg.Tools.Object.Enabled {
		o := cfg.Tools.Object
		opts.Object = true
		opts.ObjectOnly = o.Only
		backend := o.Backend
		if strings.TrimSpace(backend) == "" {
			backend = config.BackendNooBaa
		}
		opts.ObjectBackend = backend
		opts.ObjectOBCDeleteTimeout = o.OBCDeleteTimeout.Duration().String()
		opts.ObjectProfile = o.Profile
		opts.ObjectImage = o.Image
		opts.ObjectBuckets = o.Buckets
		opts.ObjectCount = o.ObjectsPerBucket
		opts.ObjectSizeMix = config.FormatSizeMix(o.SizeMix)
		opts.ObjectWorkers = o.Workers
		opts.ObjectKeyLayout = o.KeyLayout
		opts.ObjectVerify = o.VerifyMode
		opts.ObjectVersioned = o.VersionedBuckets
		opts.ObjectPhases = strings.Join(o.Phases, ",")
		opts.ObjectDeleteMode = o.DeleteMode
		opts.ObjectStorageClass = o.StorageClass
		opts.ObjectProvisioning = o.Provisioning
		opts.ObjectSeed = o.Seed
		opts.ObjectResume = o.Resume
		opts.ObjectInsecureTLS = o.InsecureTLS
		opts.ObjectForce = o.Force
	}
	return opts
}
