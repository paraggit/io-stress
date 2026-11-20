package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/workload"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// startRun is the cluster run. Tests replace it so command parsing can be
// checked without talking to a cluster.
var startRun = workload.Run

type runFlagMode int

const (
	runFlagsLegacy runFlagMode = iota
	runFlagsFIO
	runFlagsObject
)

func newRunCmd() *cobra.Command {
	runCmd := &cobra.Command{
		Use:          "run",
		Short:        "Run a FIO or object workload",
		Long:         "Run a FIO or object workload. With no workload name, run still runs FIO, and --object and --object-only still select the object workload.",
		Example:      "  odf-io-stress run fio --app-type vm --runtime 60\n  odf-io-stress run object --object-profile smoke\n  odf-io-stress run object noobaa --object-profile smoke\n  odf-io-stress run object rgw --object-profile smoke\n  odf-io-stress run fio --recipe smoke --object --object-backend rgw",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeRun(cmd, "", "")
		},
	}
	addRunFlags(runCmd.Flags(), runFlagsLegacy)

	fioCmd := &cobra.Command{
		Use:           "fio",
		Short:         "Run the FIO suite on RBD and CephFS",
		Long:          "Run the FIO suite on RBD and CephFS. --object also runs the object workload in the same namespace. --object-backend selects noobaa or rgw for that combined run.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeRun(cmd, "fio", "")
		},
	}
	addRunFlags(fioCmd.Flags(), runFlagsFIO)

	objectCmd := &cobra.Command{
		Use:           "object [noobaa|rgw]",
		Short:         "Run the object workload",
		Long:          "Run only the object workload. noobaa is the default backend. rgw discovers the Ceph bucket StorageClass and rejects s3 provisioning.",
		Example:       "  odf-io-stress run object --object-profile smoke\n  odf-io-stress run object noobaa --object-profile smoke\n  odf-io-stress run object rgw --object-profile smoke",
		SilenceUsage:  true,
		SilenceErrors: true,
		ValidArgs:     []string{config.BackendNooBaa, config.BackendRGW},
		Args:          objectBackendArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			backend := ""
			if len(args) == 1 {
				backend = args[0]
			}
			return executeRun(cmd, "object", backend)
		},
	}
	addRunFlags(objectCmd.Flags(), runFlagsObject)

	runCmd.AddCommand(fioCmd, objectCmd)
	return runCmd
}

func objectBackendArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return &plainUsageError{msg: "object accepts one backend: noobaa or rgw\n\n  odf-io-stress run object noobaa\n  odf-io-stress run object rgw"}
	}
	if len(args) == 1 && args[0] != config.BackendNooBaa && args[0] != config.BackendRGW {
		return &plainUsageError{msg: fmt.Sprintf("unknown object backend %q\n\n  odf-io-stress run object noobaa\n  odf-io-stress run object rgw", args[0])}
	}
	return nil
}

func executeRun(cmd *cobra.Command, workloadName, backend string) error {
	cfg, err := prepareRun(cmd, workloadName, backend)
	if err != nil {
		return err
	}
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	recipe := ""
	if cmd.Flags().Lookup("recipe") != nil {
		recipe, err = cmd.Flags().GetString("recipe")
		if err != nil {
			return err
		}
	}
	if cfg.Cluster.DryRun {
		if err := resolveDryRunObject(cfg); err != nil {
			return err
		}
		return workload.DryRun(cfg)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return startRun(ctx, cfg, configPath, recipe)
}

func prepareRun(cmd *cobra.Command, workloadName, backend string) (*config.Config, error) {
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, err
	}
	recipe := ""
	if cmd.Flags().Lookup("recipe") != nil {
		recipe, err = cmd.Flags().GetString("recipe")
		if err != nil {
			return nil, err
		}
	}
	cfg := config.NewDefault()
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return nil, err
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
			return nil, err
		}
	}
	if err := config.ApplyChangedFlags(cmd.Flags(), cfg); err != nil {
		return nil, err
	}
	switch workloadName {
	case "fio":
		if err := applyFIOWorkload(cmd, cfg); err != nil {
			return nil, err
		}
	case "object":
		if err := applyObjectWorkload(cmd, cfg, backend); err != nil {
			return nil, err
		}
	}
	if cfg.Cluster.SustainRuntime == 0 {
		cfg.Cluster.SustainRuntime = cfg.Tools.FIO.Runtime * 3
	}
	config.ApplyObjectOnly(cfg)
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	config.ApplyWriteVerify(cfg)
	config.ApplySetupOnly(cfg)
	config.ApplyObjectOnly(cfg)
	if err := workload.EnsureObjectSeed(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyFIOWorkload keeps a fio command on the volume suite unless --object is set.
func applyFIOWorkload(cmd *cobra.Command, cfg *config.Config) error {
	if cmd.Flags().Changed("object-only") {
		only, err := cmd.Flags().GetBool("object-only")
		if err != nil {
			return err
		}
		if only {
			return &plainUsageError{msg: "run fio does not take --object-only\n\n  odf-io-stress run object"}
		}
	}
	cfg.Tools.Object.Only = false
	if !cmd.Flags().Changed("object") {
		cfg.Tools.Object.Enabled = false
	}
	return nil
}

// applyObjectWorkload makes `run object` an object-only run and applies the backend word.
func applyObjectWorkload(cmd *cobra.Command, cfg *config.Config, backend string) error {
	cfg.Tools.Object.Enabled = true
	cfg.Tools.Object.Only = true
	if backend == "" {
		return nil
	}
	if cmd.Flags().Changed("object-backend") {
		got, err := cmd.Flags().GetString("object-backend")
		if err != nil {
			return err
		}
		if got != backend {
			return &plainUsageError{msg: fmt.Sprintf("object backend %q does not match --object-backend %q\n\n  odf-io-stress run object %s\n  odf-io-stress run object %s", backend, got, backend, got)}
		}
	}
	cfg.Tools.Object.Backend = backend
	return nil
}

func addRunFlags(fs *pflag.FlagSet, mode runFlagMode) {
	def := config.NewDefault()
	obj := def.Tools.Object
	fio := mode != runFlagsObject

	fs.String("config", "", "Path to YAML/JSON config file")
	if fio {
		fs.String("recipe", "", "Named run: smoke, write-verify, app-vm, app-database, or full. Explicit flags override it")
	}
	fs.String("kubeconfig", def.Cluster.Kubeconfig, "Path to kubeconfig (default: KUBECONFIG or ~/.kube/config)")
	fs.StringP("namespace", "N", def.Cluster.Namespace, "Kubernetes namespace")
	fs.StringP("prefix", "p", def.Cluster.Prefix, "Resource name prefix")
	fs.String("results-dir", def.Cluster.ResultsDir, "Directory for report.json and summary.json (default: results/<timestamp>)")
	fs.DurationP("timeout", "t", def.Cluster.WaitTimeout.Duration(), "Wait timeout for PVC/pod readiness")
	fs.Bool("no-cleanup", def.Cluster.NoCleanup, "Skip resource cleanup on exit")
	fs.Bool("dry-run", def.Cluster.DryRun, "Emit YAML manifests without creating resources")
	fs.Int("max-parallel", def.Cluster.MaxParallelPods, "Max concurrent pods (0=unlimited)")

	if fio {
		fs.IntP("num-pvc", "n", def.Cluster.RBD.NumPVC, "Set both RBD and CephFS PVC counts")
		fs.Int("rbd-num-pvc", def.Cluster.RBD.NumPVC, "RBD PVC/pod pairs (alone: CephFS=0)")
		fs.Int("cephfs-num-pvc", def.Cluster.CephFS.NumPVC, "CephFS PVC/pod pairs (alone: RBD=0)")
		fs.String("rbd-storage-class", def.Cluster.RBD.StorageClass, "RBD StorageClass name")
		fs.String("cephfs-storage-class", def.Cluster.CephFS.StorageClass, "CephFS StorageClass name")
		fs.String("pvc-size", def.Cluster.PVCSize, "PVC size")
		fs.StringP("image", "i", def.Tools.FIO.Image, "FIO container image")
		fs.IntP("runtime", "r", def.Tools.FIO.Runtime, "FIO runtime in seconds")
		fs.StringP("bs", "b", def.Tools.FIO.BlockSize, "FIO block size in bytes")
		fs.String("offset", def.Tools.FIO.Offset, "FIO offset in bytes")
		fs.String("fio-size", def.Tools.FIO.Size, "FIO file/device size")
		fs.String("seed-size", def.Cluster.SeedSize, "Block integrity seed/verify extent (sequential, non-time-based)")
		fs.Duration("clone-timeout", def.Cluster.CloneTimeout.Duration(), "Timeout for clone/restore PVC Bound waits (CephFS; RBD uses --timeout)")
		fs.StringP("format", "f", def.Tools.FIO.OutputFormat, "FIO output format: json, normal")
		fs.Int("lifecycle-interval", def.Cluster.LifecycleInterval, "Run lifecycle ops on every Nth pod")
		fs.Bool("skip-lifecycle", def.Cluster.SkipLifecycle, "Skip lifecycle storm and verify phases")
		fs.Bool("skip-fio-stress", def.Cluster.SkipFIOStress, "Skip FIO stress phase")
		fs.Bool("write-verify", def.Cluster.WriteVerify, "Run FIO write+immediate crc32c verify only (skips stress suite and lifecycle)")
		fs.Bool("setup-only", def.Cluster.SetupOnly, "Create PVCs and pods, wait until Ready, then exit (no FIO/lifecycle; implies --no-cleanup)")
		fs.Int("expand-factor", def.Cluster.ExpandFactor, "PVC expand size multiplier")
		fs.String("snapshot-class", def.Cluster.SnapshotClass, "Override VolumeSnapshotClass")
		fs.Int("sustain-runtime", def.Cluster.SustainRuntime, "Sustain workload duration (default: runtime*3)")
		fs.Int("max-parallel-provision", def.Cluster.MaxParallelProvision, "Max in-flight PVC create, bind, pod create, clone, restore, and expand operations")
		fs.Bool("sequential", !def.Tools.FIO.Parallel, "Run FIO workloads sequentially")
		fs.String("app-type", "", "Comma-separated app workload profiles from tools.fio.app_suites (built-ins: "+strings.Join(config.ValidAppTypes(nil), ", ")+")")
		fs.Bool("object", obj.Enabled, "Run the object workload in the same namespace as FIO")
		fs.Bool("object-only", obj.Only, "Run only the object workload (no PVCs or FIO)")
	}

	fs.String("object-backend", config.BackendNooBaa, "Object backend: noobaa or rgw")
	fs.String("object-profile", obj.Profile, "Object profile: smoke, standard, scale, or large")
	fs.String("object-image", obj.Image, "Custom object worker image (overrides the profile and config file)")
	fs.Int("object-buckets", obj.Buckets, "Number of object buckets")
	fs.Int("object-count", obj.ObjectsPerBucket, "Objects per bucket")
	fs.String("object-size-mix", config.FormatSizeMix(obj.SizeMix), "Weighted object sizes, for example 4KB:60,1MB:30,16MB:10")
	fs.Int("object-workers", obj.Workers, "Worker pods per object phase")
	fs.String("object-key-layout", obj.KeyLayout, "Object key layout: hashed, flat, or deep")
	fs.String("object-verify", obj.VerifyMode, "Object verify mode: full, etag, or sample")
	fs.Int("object-versioned", obj.VersionedBuckets, "How many buckets have versioning enabled")
	fs.String("object-phases", strings.Join(obj.Phases, ","), "Comma-separated object phases")
	fs.String("object-delete-mode", obj.DeleteMode, "Object delete mode: recursive, obc-only, or both")
	fs.String("object-storage-class", obj.StorageClass, "ObjectBucketClaim StorageClass")
	fs.Duration("object-obc-delete-timeout", obj.OBCDeleteTimeout.Duration(), "Wait for ObjectBucketClaim deletion")
	fs.String("object-provisioning", obj.Provisioning, "Bucket provisioning: obc or s3")
	fs.String("object-seed", "", "Hex seed; reproduces a prior object run")
	fs.Bool("object-resume", false, "Reuse buckets from --object-seed; requires an object-only run and does not delete the namespace")
	fs.Bool("object-insecure-tls", obj.InsecureTLS, "Skip S3 TLS verification (logged)")
	fs.Bool("object-force", obj.Force, "Run the object workload even when the capacity preflight fails")

	switch mode {
	case runFlagsFIO:
		_ = fs.MarkHidden("object-only")
	case runFlagsObject:
		_ = fs.MarkHidden("object-backend")
	}
}
