package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/worker"
	"github.com/red-hat-storage/odf-io-stress/pkg/workload"
	"github.com/spf13/cobra"
)

// version is set via -ldflags at release build time (e.g. -X main.version=v1.0.0).
var version = "dev"

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "odf-io-stress",
		Short:   "ODF IO stress testing tool for RBD and CephFS",
		Version: version,
	}
	// Don't dump the flags list on runtime failures (e.g. "N job(s) failed").
	rootCmd.SilenceUsage = true

	var (
		configPath string
		recipe     string
		genOut     string
		genForce   bool
	)

	runCmd := &cobra.Command{
		Use:          "run",
		Short:        "Run the FIO stress test suite",
		SilenceUsage: true,
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
			if recipe != "" {
				if err := config.ApplyRecipe(cfg, recipe); err != nil {
					return err
				}
			}
			if err := config.ApplyChangedFlags(cmd.Flags(), cfg); err != nil {
				return err
			}
			if cfg.Cluster.SustainRuntime == 0 {
				cfg.Cluster.SustainRuntime = cfg.Tools.FIO.Runtime * 3
			}
			config.ApplyObjectOnly(cfg)
			if err := config.Validate(cfg); err != nil {
				return err
			}
			config.ApplyWriteVerify(cfg)
			config.ApplySetupOnly(cfg)
			config.ApplyObjectOnly(cfg)
			if err := workload.EnsureObjectSeed(cfg); err != nil {
				return err
			}
			if cfg.Cluster.DryRun {
				return workload.DryRun(cfg)
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return workload.Run(ctx, cfg, configPath, recipe)
		},
	}

	def := config.NewDefault()
	f := runCmd.Flags()
	f.StringVar(&configPath, "config", "", "Path to YAML/JSON config file")
	f.StringVar(&recipe, "recipe", "", "Named run: smoke, write-verify, app-vm, app-database, or full. Explicit flags override it")
	f.String("kubeconfig", def.Cluster.Kubeconfig, "Path to kubeconfig (default: KUBECONFIG or ~/.kube/config)")
	f.IntP("num-pvc", "n", def.Cluster.RBD.NumPVC, "Set both RBD and CephFS PVC counts")
	f.Int("rbd-num-pvc", def.Cluster.RBD.NumPVC, "RBD PVC/pod pairs (alone: CephFS=0)")
	f.Int("cephfs-num-pvc", def.Cluster.CephFS.NumPVC, "CephFS PVC/pod pairs (alone: RBD=0)")
	f.StringP("namespace", "N", def.Cluster.Namespace, "Kubernetes namespace")
	f.String("rbd-storage-class", def.Cluster.RBD.StorageClass, "RBD StorageClass name")
	f.String("cephfs-storage-class", def.Cluster.CephFS.StorageClass, "CephFS StorageClass name")
	f.String("pvc-size", def.Cluster.PVCSize, "PVC size")
	f.StringP("image", "i", def.Tools.FIO.Image, "FIO container image")
	f.IntP("runtime", "r", def.Tools.FIO.Runtime, "FIO runtime in seconds")
	f.StringP("bs", "b", def.Tools.FIO.BlockSize, "FIO block size in bytes")
	f.String("offset", def.Tools.FIO.Offset, "FIO offset in bytes")
	f.String("fio-size", def.Tools.FIO.Size, "FIO file/device size")
	f.String("seed-size", def.Cluster.SeedSize, "Block integrity seed/verify extent (sequential, non-time-based)")
	f.StringP("prefix", "p", def.Cluster.Prefix, "Resource name prefix")
	f.String("results-dir", def.Cluster.ResultsDir, "Directory for report.json and summary.json (default: results/<timestamp>)")
	f.DurationP("timeout", "t", def.Cluster.WaitTimeout.Duration(), "Wait timeout for PVC/pod readiness")
	f.Duration("clone-timeout", def.Cluster.CloneTimeout.Duration(), "Timeout for clone/restore PVC Bound waits (CephFS; RBD uses --timeout)")
	f.StringP("format", "f", def.Tools.FIO.OutputFormat, "FIO output format: json, normal")
	f.Bool("no-cleanup", def.Cluster.NoCleanup, "Skip resource cleanup on exit")
	f.Bool("dry-run", def.Cluster.DryRun, "Emit YAML manifests without creating resources")
	f.Int("lifecycle-interval", def.Cluster.LifecycleInterval, "Run lifecycle ops on every Nth pod")
	f.Bool("skip-lifecycle", def.Cluster.SkipLifecycle, "Skip lifecycle storm and verify phases")
	f.Bool("skip-fio-stress", def.Cluster.SkipFIOStress, "Skip FIO stress phase")
	f.Bool("write-verify", def.Cluster.WriteVerify, "Run FIO write+immediate crc32c verify only (skips stress suite and lifecycle)")
	f.Bool("setup-only", def.Cluster.SetupOnly, "Create PVCs and pods, wait until Ready, then exit (no FIO/lifecycle; implies --no-cleanup)")
	f.Int("expand-factor", def.Cluster.ExpandFactor, "PVC expand size multiplier")
	f.String("snapshot-class", def.Cluster.SnapshotClass, "Override VolumeSnapshotClass")
	f.Int("sustain-runtime", def.Cluster.SustainRuntime, "Sustain workload duration (default: runtime*3)")
	f.Int("max-parallel", def.Cluster.MaxParallelPods, "Max concurrent pods (0=unlimited)")
	f.Int("max-parallel-provision", def.Cluster.MaxParallelProvision, "Max in-flight PVC create, bind, pod create, clone, restore, and expand operations")
	f.Bool("sequential", !def.Tools.FIO.Parallel, "Run FIO workloads sequentially")
	f.String("app-type", "", "Comma-separated app workload profiles from tools.fio.app_suites (built-ins: "+strings.Join(config.ValidAppTypes(nil), ", ")+")")
	obj := def.Tools.Object
	f.Bool("object", obj.Enabled, "Enable the NooBaa object workload")
	f.Bool("object-only", obj.Only, "Run only the object workload (no PVCs or FIO)")
	f.String("object-profile", obj.Profile, "Object profile: smoke, standard, scale, or large")
	f.String("object-image", obj.Image, "Custom object worker image (overrides the profile and config file)")
	f.Int("object-buckets", obj.Buckets, "Number of object buckets")
	f.Int("object-count", obj.ObjectsPerBucket, "Objects per bucket")
	f.String("object-size-mix", config.FormatSizeMix(obj.SizeMix), "Weighted object sizes, for example 4KB:60,1MB:30,16MB:10")
	f.Int("object-workers", obj.Workers, "Worker pods per object phase")
	f.String("object-key-layout", obj.KeyLayout, "Object key layout: hashed, flat, or deep")
	f.String("object-verify", obj.VerifyMode, "Object verify mode: full, etag, or sample")
	f.Int("object-versioned", obj.VersionedBuckets, "How many buckets have versioning enabled")
	f.String("object-phases", strings.Join(obj.Phases, ","), "Comma-separated object phases")
	f.String("object-delete-mode", obj.DeleteMode, "Object delete mode: recursive, obc-only, or both")
	f.String("object-storage-class", obj.StorageClass, "ObjectBucketClaim StorageClass")
	f.String("object-provisioning", obj.Provisioning, "Bucket provisioning: obc or s3")
	f.String("object-seed", "", "Hex seed; reproduces a prior object run")
	f.Bool("object-resume", false, "Reuse buckets from --object-seed; requires --object-only and does not delete the namespace")
	f.Bool("object-insecure-tls", obj.InsecureTLS, "Skip S3 TLS verification (logged)")
	f.Bool("object-force", obj.Force, "Run the object workload even when the capacity preflight fails")

	genCmd := &cobra.Command{
		Use:          "generate-config",
		Short:        "Write a sample YAML config file",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.WriteSample(genOut, genForce)
		},
	}
	genCmd.Flags().StringVarP(&genOut, "output", "o", "odf-io-stress.yaml", "Output path (`-` for stdout)")
	genCmd.Flags().BoolVar(&genForce, "force", false, "Overwrite existing file")

	optCmd := &cobra.Command{
		Use:          "options",
		Short:        "Print every run and generate-config option as JSON",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			doc := config.OptionsDocument{
				SchemaVersion:  1,
				AppTypes:       config.ValidAppTypes(nil),
				Rules:          config.OptionRules(),
				Recipes:        config.Recipes(),
				Run:            config.CollectFlags(runCmd.Flags()),
				GenerateConfig: config.CollectFlags(genCmd.Flags()),
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			return enc.Encode(doc)
		},
	}

	var specPath string
	workerCmd := &cobra.Command{
		Use:          "s3-worker",
		Short:        "Run one object-workload phase inside the cluster",
		Hidden:       true,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := worker.LoadSpec(specPath)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return worker.Run(ctx, spec, cmd.OutOrStdout())
		},
	}
	workerCmd.Flags().StringVar(&specPath, "spec", worker.SpecPath, "Path to the job spec JSON")

	rootCmd.AddCommand(runCmd, genCmd, optCmd, workerCmd)
	return rootCmd
}

// exitCode maps a run error to a process status an agent can branch on:
// 1 jobs failed, 2 config or usage, 3 cluster failure before a clean finish.
func exitCode(err error) int {
	var exit *workload.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return workload.EmitUsage(os.Stdout, err)
}
