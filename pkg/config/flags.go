package config

import (
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
	get("prefix", func() {
		cfg.Cluster.Prefix, err = fs.GetString("prefix")
	})
	get("timeout", func() {
		var d time.Duration
		d, err = fs.GetDuration("timeout")
		if err == nil {
			cfg.Cluster.WaitTimeout = Duration(d)
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
	get("sequential", func() {
		var seq bool
		seq, err = fs.GetBool("sequential")
		if err == nil && seq {
			cfg.Tools.FIO.Parallel = false
		}
	})
	return err
}
