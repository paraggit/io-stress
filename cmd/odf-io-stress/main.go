package main

import (
	"fmt"
	"os"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/spf13/cobra"
)

func main() {
	cfg := config.NewDefault()

	rootCmd := &cobra.Command{
		Use:   "odf-io-stress",
		Short: "ODF IO stress testing tool for RBD and CephFS",
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the FIO stress test suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg.SustainRuntime == 0 {
				cfg.SustainRuntime = cfg.FIORuntime * 3
			}
			if err := config.Validate(cfg); err != nil {
				return err
			}
			fmt.Printf("Config: %+v\n", cfg)
			return nil
		},
	}

	f := runCmd.Flags()
	f.IntVarP(&cfg.NumPVC, "num-pvc", "n", cfg.NumPVC, "Number of PVC/pod pairs per backend")
	f.StringVarP(&cfg.Namespace, "namespace", "N", cfg.Namespace, "Kubernetes namespace")
	f.StringVar(&cfg.RBDStorageClass, "rbd-storage-class", cfg.RBDStorageClass, "RBD StorageClass name")
	f.StringVar(&cfg.CephFSStorageClass, "cephfs-storage-class", cfg.CephFSStorageClass, "CephFS StorageClass name")
	f.StringVar(&cfg.PVCSize, "pvc-size", cfg.PVCSize, "PVC size")
	f.StringVarP(&cfg.FIOImage, "image", "i", cfg.FIOImage, "FIO container image")
	f.IntVarP(&cfg.FIORuntime, "runtime", "r", cfg.FIORuntime, "FIO runtime in seconds")
	f.StringVarP(&cfg.FIOBlockSize, "bs", "b", cfg.FIOBlockSize, "FIO block size in bytes")
	f.StringVar(&cfg.FIOOffset, "offset", cfg.FIOOffset, "FIO offset in bytes")
	f.StringVar(&cfg.FIOSize, "fio-size", cfg.FIOSize, "FIO file/device size")
	f.StringVarP(&cfg.Prefix, "prefix", "p", cfg.Prefix, "Resource name prefix")
	f.DurationVarP(&cfg.WaitTimeout, "timeout", "t", cfg.WaitTimeout, "Wait timeout for PVC/pod readiness")
	f.StringVarP(&cfg.OutputFormat, "format", "f", cfg.OutputFormat, "FIO output format: json, normal")
	f.BoolVar(&cfg.NoCleanup, "no-cleanup", cfg.NoCleanup, "Skip resource cleanup on exit")
	f.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "Emit YAML manifests without creating resources")
	f.IntVar(&cfg.LifecycleInterval, "lifecycle-interval", cfg.LifecycleInterval, "Run lifecycle ops on every Nth pod")
	f.BoolVar(&cfg.SkipLifecycle, "skip-lifecycle", cfg.SkipLifecycle, "Skip lifecycle storm and verify phases")
	f.BoolVar(&cfg.SkipFIOStress, "skip-fio-stress", cfg.SkipFIOStress, "Skip FIO stress phase")
	f.IntVar(&cfg.ExpandFactor, "expand-factor", cfg.ExpandFactor, "PVC expand size multiplier")
	f.StringVar(&cfg.SnapshotClass, "snapshot-class", cfg.SnapshotClass, "Override VolumeSnapshotClass")
	f.IntVar(&cfg.SustainRuntime, "sustain-runtime", 0, "Sustain workload duration (default: runtime*3)")
	f.IntVar(&cfg.MaxParallelPods, "max-parallel", cfg.MaxParallelPods, "Max concurrent pods (0=unlimited)")

	sequential := f.Bool("sequential", false, "Run FIO workloads sequentially")
	runCmd.PreRun = func(cmd *cobra.Command, args []string) {
		if *sequential {
			cfg.Parallel = false
		}
	}

	rootCmd.AddCommand(runCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
