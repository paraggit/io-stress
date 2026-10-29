package config

import (
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// ApplyChangedFlags overlays only flags that were explicitly set onto cfg.
func ApplyChangedFlags(fs *pflag.FlagSet, cfg *Config) error {
	var err error
	get := func(name string, apply func()) {
		if err != nil || !fs.Changed(name) {
			return
		}
		apply()
	}

	get("namespace", func() {
		cfg.Cluster.Namespace, err = fs.GetString("namespace")
	})
	get("kubeconfig", func() {
		cfg.Cluster.Kubeconfig, err = fs.GetString("kubeconfig")
	})
	get("num-pvc", func() {
		var n int
		n, err = fs.GetInt("num-pvc")
		if err == nil {
			cfg.Cluster.RBD.NumPVC = n
			cfg.Cluster.CephFS.NumPVC = n
		}
	})
	get("rbd-num-pvc", func() {
		cfg.Cluster.RBD.NumPVC, err = fs.GetInt("rbd-num-pvc")
	})
	get("cephfs-num-pvc", func() {
		cfg.Cluster.CephFS.NumPVC, err = fs.GetInt("cephfs-num-pvc")
	})
	get("rbd-storage-class", func() {
		cfg.Cluster.RBD.StorageClass, err = fs.GetString("rbd-storage-class")
	})
	get("cephfs-storage-class", func() {
		cfg.Cluster.CephFS.StorageClass, err = fs.GetString("cephfs-storage-class")
	})
	get("pvc-size", func() {
		cfg.Cluster.PVCSize, err = fs.GetString("pvc-size")
	})
	get("image", func() {
		cfg.Tools.FIO.Image, err = fs.GetString("image")
	})
	get("runtime", func() {
		cfg.Tools.FIO.Runtime, err = fs.GetInt("runtime")
	})
	get("bs", func() {
		cfg.Tools.FIO.BlockSize, err = fs.GetString("bs")
	})
	get("offset", func() {
		cfg.Tools.FIO.Offset, err = fs.GetString("offset")
	})
	get("fio-size", func() {
		cfg.Tools.FIO.Size, err = fs.GetString("fio-size")
	})
	get("seed-size", func() {
		cfg.Cluster.SeedSize, err = fs.GetString("seed-size")
	})
	get("prefix", func() {
		cfg.Cluster.Prefix, err = fs.GetString("prefix")
	})
	get("results-dir", func() {
		cfg.Cluster.ResultsDir, err = fs.GetString("results-dir")
	})
	get("timeout", func() {
		var d time.Duration
		d, err = fs.GetDuration("timeout")
		if err == nil {
			cfg.Cluster.WaitTimeout = Duration(d)
		}
	})
	get("clone-timeout", func() {
		var d time.Duration
		d, err = fs.GetDuration("clone-timeout")
		if err == nil {
			cfg.Cluster.CloneTimeout = Duration(d)
		}
	})
	get("format", func() {
		cfg.Tools.FIO.OutputFormat, err = fs.GetString("format")
	})
	get("no-cleanup", func() {
		cfg.Cluster.NoCleanup, err = fs.GetBool("no-cleanup")
	})
	get("dry-run", func() {
		cfg.Cluster.DryRun, err = fs.GetBool("dry-run")
	})
	get("lifecycle-interval", func() {
		cfg.Cluster.LifecycleInterval, err = fs.GetInt("lifecycle-interval")
	})
	get("skip-lifecycle", func() {
		cfg.Cluster.SkipLifecycle, err = fs.GetBool("skip-lifecycle")
	})
	get("skip-fio-stress", func() {
		cfg.Cluster.SkipFIOStress, err = fs.GetBool("skip-fio-stress")
	})
	get("write-verify", func() {
		cfg.Cluster.WriteVerify, err = fs.GetBool("write-verify")
	})
	get("setup-only", func() {
		cfg.Cluster.SetupOnly, err = fs.GetBool("setup-only")
	})
	get("expand-factor", func() {
		cfg.Cluster.ExpandFactor, err = fs.GetInt("expand-factor")
	})
	get("snapshot-class", func() {
		cfg.Cluster.SnapshotClass, err = fs.GetString("snapshot-class")
	})
	get("sustain-runtime", func() {
		cfg.Cluster.SustainRuntime, err = fs.GetInt("sustain-runtime")
	})
	get("max-parallel", func() {
		cfg.Cluster.MaxParallelPods, err = fs.GetInt("max-parallel")
	})
	get("max-parallel-provision", func() {
		cfg.Cluster.MaxParallelProvision, err = fs.GetInt("max-parallel-provision")
	})
	get("sequential", func() {
		var seq bool
		seq, err = fs.GetBool("sequential")
		if err == nil {
			cfg.Tools.FIO.Parallel = !seq
		}
	})
	get("app-type", func() {
		var raw string
		raw, err = fs.GetString("app-type")
		if err == nil && raw != "" {
			parts := strings.Split(raw, ",")
			cfg.Cluster.AppTypes = cfg.Cluster.AppTypes[:0]
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					cfg.Cluster.AppTypes = append(cfg.Cluster.AppTypes, p)
				}
			}
		}
	})

	// Profile first so later explicit object flags override it.
	if err == nil && fs.Changed("object-profile") {
		var name string
		name, err = fs.GetString("object-profile")
		if err == nil {
			err = ApplyObjectProfile(&cfg.Tools.Object, name)
		}
	}
	get("object", func() {
		cfg.Tools.Object.Enabled, err = fs.GetBool("object")
	})
	get("object-only", func() {
		cfg.Tools.Object.Only, err = fs.GetBool("object-only")
	})
	get("object-backend", func() {
		cfg.Tools.Object.Backend, err = fs.GetString("object-backend")
	})
	get("object-image", func() {
		cfg.Tools.Object.Image, err = fs.GetString("object-image")
	})
	get("object-buckets", func() {
		cfg.Tools.Object.Buckets, err = fs.GetInt("object-buckets")
	})
	get("object-count", func() {
		cfg.Tools.Object.ObjectsPerBucket, err = fs.GetInt("object-count")
	})
	get("object-size-mix", func() {
		var raw string
		raw, err = fs.GetString("object-size-mix")
		if err == nil {
			cfg.Tools.Object.SizeMix, err = ParseSizeMix(raw)
		}
	})
	get("object-workers", func() {
		cfg.Tools.Object.Workers, err = fs.GetInt("object-workers")
	})
	get("object-key-layout", func() {
		cfg.Tools.Object.KeyLayout, err = fs.GetString("object-key-layout")
	})
	get("object-verify", func() {
		cfg.Tools.Object.VerifyMode, err = fs.GetString("object-verify")
	})
	get("object-versioned", func() {
		cfg.Tools.Object.VersionedBuckets, err = fs.GetInt("object-versioned")
	})
	get("object-phases", func() {
		var raw string
		raw, err = fs.GetString("object-phases")
		if err != nil {
			return
		}
		var phases []string
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				phases = append(phases, p)
			}
		}
		cfg.Tools.Object.Phases, err = OrderedPhases(phases)
	})
	get("object-delete-mode", func() {
		cfg.Tools.Object.DeleteMode, err = fs.GetString("object-delete-mode")
	})
	get("object-storage-class", func() {
		cfg.Tools.Object.StorageClass, err = fs.GetString("object-storage-class")
	})
	get("object-obc-delete-timeout", func() {
		var d time.Duration
		d, err = fs.GetDuration("object-obc-delete-timeout")
		if err == nil {
			cfg.Tools.Object.OBCDeleteTimeout = Duration(d)
		}
	})
	get("object-provisioning", func() {
		cfg.Tools.Object.Provisioning, err = fs.GetString("object-provisioning")
	})
	get("object-seed", func() {
		cfg.Tools.Object.Seed, err = fs.GetString("object-seed")
	})
	get("object-resume", func() {
		cfg.Tools.Object.Resume, err = fs.GetBool("object-resume")
	})
	get("object-insecure-tls", func() {
		cfg.Tools.Object.InsecureTLS, err = fs.GetBool("object-insecure-tls")
	})
	get("object-force", func() {
		cfg.Tools.Object.Force, err = fs.GetBool("object-force")
	})

	// Backend-only CLI: mentioning one side without the other (and without --num-pvc)
	// means "only that backend" — zero the unspecified side.
	if err == nil && !fs.Changed("num-pvc") {
		if fs.Changed("rbd-num-pvc") && !fs.Changed("cephfs-num-pvc") {
			cfg.Cluster.CephFS.NumPVC = 0
		}
		if fs.Changed("cephfs-num-pvc") && !fs.Changed("rbd-num-pvc") {
			cfg.Cluster.RBD.NumPVC = 0
		}
	}

	return err
}
