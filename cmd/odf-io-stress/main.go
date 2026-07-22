package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/workload"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{Use: "odf-io-stress", Short: "ODF IO stress testing tool for RBD and CephFS"}

	var (
		configPath string
		genOut     string
		genForce   bool
	)

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the FIO stress test suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.NewDefault()
			if configPath != "" {
				loaded, err := config.Load(configPath)
				if err != nil {
					return err
				}
				cfg = loaded
				if len(cfg.Tools.VDBench) > 0 {
					log.Printf("WARNING: tools.vdbench is set but not supported yet; ignoring")
				}
				if len(cfg.Tools.SmallFiles) > 0 {
					log.Printf("WARNING: tools.smallfiles is set but not supported yet; ignoring")
				}
			}
			if err := config.ApplyChangedFlags(cmd.Flags(), cfg); err != nil {
				return err
			}
			if cfg.Cluster.SustainRuntime == 0 {
				cfg.Cluster.SustainRuntime = cfg.Tools.FIO.Runtime * 3
			}
			if err := config.Validate(cfg); err != nil {
				return err
			}
			if cfg.Cluster.DryRun {
				return workload.DryRun(cfg)
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return workload.Run(ctx, cfg)
		},
	}

	def := config.NewDefault()
	f := runCmd.Flags()
	f.StringVar(&configPath, "config", "", "Path to YAML/JSON config file")
	f.Int("num-pvc", def.Cluster.RBD.NumPVC, "Set both RBD and CephFS PVC counts")
	f.Int("rbd-num-pvc", def.Cluster.RBD.NumPVC, "Number of RBD PVC/pod pairs")
	f.Int("cephfs-num-pvc", def.Cluster.CephFS.NumPVC, "Number of CephFS PVC/pod pairs")
	f.StringP("namespace", "N", def.Cluster.Namespace, "Kubernetes namespace")
	f.String("rbd-storage-class", def.Cluster.RBD.StorageClass, "RBD StorageClass name")
	f.String("cephfs-storage-class", def.Cluster.CephFS.StorageClass, "CephFS StorageClass name")
	f.String("pvc-size", def.Cluster.PVCSize, "PVC size")
	f.StringP("image", "i", def.Tools.FIO.Image, "FIO container image")
	f.IntP("runtime", "r", def.Tools.FIO.Runtime, "FIO runtime in seconds")
	f.StringP("bs", "b", def.Tools.FIO.BlockSize, "FIO block size in bytes")
	f.String("offset", def.Tools.FIO.Offset, "FIO offset in bytes")
	f.String("fio-size", def.Tools.FIO.Size, "FIO file/device size")
	f.StringP("prefix", "p", def.Cluster.Prefix, "Resource name prefix")
	f.DurationP("timeout", "t", def.Cluster.WaitTimeout.Duration(), "Wait timeout for PVC/pod readiness")
	f.StringP("format", "f", def.Tools.FIO.OutputFormat, "FIO output format: json, normal")
	f.Bool("no-cleanup", def.Cluster.NoCleanup, "Skip resource cleanup on exit")
	f.Bool("dry-run", def.Cluster.DryRun, "Emit YAML manifests without creating resources")
	f.Int("lifecycle-interval", def.Cluster.LifecycleInterval, "Run lifecycle ops on every Nth pod")
	f.Bool("skip-lifecycle", def.Cluster.SkipLifecycle, "Skip lifecycle storm and verify phases")
	f.Bool("skip-fio-stress", def.Cluster.SkipFIOStress, "Skip FIO stress phase")
	f.Int("expand-factor", def.Cluster.ExpandFactor, "PVC expand size multiplier")
	f.String("snapshot-class", def.Cluster.SnapshotClass, "Override VolumeSnapshotClass")
	f.Int("sustain-runtime", def.Cluster.SustainRuntime, "Sustain workload duration (default: runtime*3)")
	f.Int("max-parallel", def.Cluster.MaxParallelPods, "Max concurrent pods (0=unlimited)")
	f.Bool("sequential", !def.Tools.FIO.Parallel, "Run FIO workloads sequentially")

	genCmd := &cobra.Command{
		Use:   "generate-config",
		Short: "Write a sample YAML config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.WriteSample(genOut, genForce)
		},
	}
	genCmd.Flags().StringVarP(&genOut, "output", "o", "odf-io-stress.yaml", "Output path (`-` for stdout)")
	genCmd.Flags().BoolVar(&genForce, "force", false, "Overwrite existing file")

	rootCmd.AddCommand(runCmd, genCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}