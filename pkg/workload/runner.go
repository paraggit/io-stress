package workload

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
	"golang.org/x/sync/errgroup"
)

func Run(ctx context.Context, cfg *config.Config, configPath, recipe string) error {
	config.ApplyWriteVerify(cfg)
	config.ApplySetupOnly(cfg)
	config.ApplyObjectOnly(cfg)
	if err := EnsureObjectSeed(cfg); err != nil {
		return finalize(os.Stdout, "", nil, err, finalizeOpts{})
	}

	if cfg.Cluster.ResultsDir == "" {
		cfg.Cluster.ResultsDir = filepath.Join(".", "results", time.Now().Format("20060102-150405"))
	}
	base := finalizeOpts{hasOptions: true, options: usedRunOptions(cfg, configPath, recipe)}
	if err := os.MkdirAll(cfg.Cluster.ResultsDir, 0755); err != nil {
		return finalize(os.Stdout, "", nil, fmt.Errorf("create results dir: %w", err), base)
	}

	client, err := k8s.NewClient(cfg.Cluster.Kubeconfig)
	if err != nil {
		return finalize(os.Stdout, cfg.Cluster.ResultsDir, nil, fmt.Errorf("create k8s client: %w", err), base)
	}

	results, runErr, opts := execute(ctx, cfg, client, base)
	if cfg.Tools.Object.Enabled {
		if !cfg.Cluster.NoCleanup {
			if cerr := cleanup(client, cfg); cerr != nil {
				log.Printf("Cleanup: %v", cerr)
				runErr = joinCleanup(runErr, cerr)
			}
		}
		return finalize(os.Stdout, cfg.Cluster.ResultsDir, results, runErr, opts)
	}

	err = finalize(os.Stdout, cfg.Cluster.ResultsDir, results, runErr, opts)
	if !cfg.Cluster.NoCleanup {
		_ = cleanup(client, cfg)
	}
	return err
}

// execute runs the workload and returns results without writing the report.
// FIO-only failures before any job keep writeReport false, matching the
// previous exit path. Object runs write the report once work has started.
func execute(ctx context.Context, cfg *config.Config, client *k8s.Client, base finalizeOpts) ([]report.JobResult, error, finalizeOpts) {
	opts := base
	if resetNamespace(cfg) {
		log.Printf("Resetting namespace %s (clearing leftovers)", cfg.Cluster.Namespace)
		if err := k8s.ResetNamespace(ctx, client, cfg.Cluster.Namespace, cfg.Cluster.WaitTimeout.Duration()); err != nil {
			return nil, fmt.Errorf("reset namespace: %w", err), opts
		}
	} else {
		log.Printf("object resume: not deleting namespace %s", cfg.Cluster.Namespace)
		if err := k8s.EnsureNamespace(ctx, client, cfg.Cluster.Namespace); err != nil {
			return nil, err, opts
		}
	}

	var readyPods []PodInfo
	if pods := plannedPods(cfg); len(pods) > 0 {
		allPods, err := setupResources(ctx, cfg, client)
		if err != nil {
			return nil, err, opts
		}
		readyPods, err = waitForPods(ctx, cfg, client, allPods)
		if err != nil {
			return nil, err, opts
		}
	}

	if cfg.Cluster.SetupOnly {
		log.Printf("Setup-only: %d PVC/pod(s) ready in namespace %s (no FIO/lifecycle; resources kept)", len(readyPods), cfg.Cluster.Namespace)
		return nil, nil, opts
	}

	if cfg.Cluster.WriteVerify {
		log.Println("Write-verify mode: skipping lifecycle (crc32c verify runs with each write)")
	}

	collector := report.NewCollector()
	startTime := time.Now()
	opts.writeReport = true
	opts.printHuman = true
	opts.start = startTime

	if !cfg.Cluster.SkipFIOStress {
		if len(readyPods) == 0 {
			return collector.Results(), fmt.Errorf("no pods became Ready"), opts
		}
		if err := runPhase1(ctx, cfg, client, readyPods, collector); err != nil {
			return collector.Results(), err, opts
		}
	} else if notice := phase1SkipNotice(cfg); notice != "" {
		log.Println(notice)
	}

	if !cfg.Cluster.SkipLifecycle {
		if err := runPhase2(ctx, cfg, client, readyPods, collector); err != nil {
			log.Printf("Phase 2 completed with errors: %v", err)
		}
		if err := runPhase3(ctx, cfg, client, readyPods, collector); err != nil {
			log.Printf("Phase 3 completed with errors: %v", err)
		}
	}

	if cfg.Tools.Object.Enabled {
		if err := runObject(ctx, cfg, client, collector); err != nil {
			return collector.Results(), err, opts
		}
	}
	return collector.Results(), nil, opts
}

// phase1SkipNotice is the log line for a FIO run that skipped stress.
// An object-only run never ran FIO, so it stays quiet.
func phase1SkipNotice(cfg *config.Config) string {
	if cfg != nil && cfg.Cluster.SkipFIOStress && !cfg.Tools.Object.Only {
		return "Phase 1 skipped (--skip-fio-stress)"
	}
	return ""
}

// resetNamespace reports whether this run deletes and recreates the namespace
// before doing any work. An object resume reuses the claims that live there.
func resetNamespace(cfg *config.Config) bool {
	return cfg == nil || !cfg.Tools.Object.Resume
}

func joinCleanup(runErr, cerr error) error {
	if runErr == nil {
		return &CleanupError{Err: cerr}
	}
	return fmt.Errorf("%w; cleanup: %v", runErr, cerr)
}

func setupResources(ctx context.Context, cfg *config.Config, client *k8s.Client) ([]PodInfo, error) {
	allPods := plannedPods(cfg)
	if err := ensurePodCapacity(ctx, client, podsNeeded(cfg)); err != nil {
		return nil, err
	}
	log.Printf("Creating %d PVCs (%d RBD + %d CephFS), %d at a time",
		len(allPods), cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC, cfg.Cluster.ProvisionLimit())
	if len(cfg.Cluster.AppTypes) > 0 {
		log.Printf("App-type mode enabled: %v (same suite(s) on every PVC/pod)", cfg.Cluster.AppTypes)
	}

	// Hold each volume until it is Bound and its pod exists before starting
	// another past the provision limit. Creating every PVC first opens one
	// watch per claim and gives the Ceph provisioner the whole set at once.
	g, gCtx := newProvisionGroup(ctx, cfg)
	for _, pod := range allPods {
		pod := pod
		g.Go(func() error {
			if err := k8s.RetryContext(gCtx, func() error {
				return k8s.CreatePVC(gCtx, client, pvcSpec(cfg, pod))
			}); err != nil {
				return fmt.Errorf("create PVC %s: %w", pod.PVCName, err)
			}
			if err := k8s.WaitPVCBound(gCtx, client, cfg.Cluster.Namespace, pod.PVCName, cfg.Cluster.WaitTimeout.Duration()); err != nil {
				return fmt.Errorf("wait PVC %s bound: %w", pod.PVCName, err)
			}
			if err := k8s.RetryContext(gCtx, func() error {
				return k8s.CreatePod(gCtx, client, podSpec(cfg, pod))
			}); err != nil {
				return fmt.Errorf("create pod %s: %w", pod.Name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("provision volumes: %w", err)
	}
	log.Printf("PVCs Bound and pods created: %d/%d", len(allPods), len(allPods))
	return allPods, nil
}

func plannedPods(cfg *config.Config) []PodInfo {
	allPods := make([]PodInfo, 0, cfg.Cluster.RBD.NumPVC+cfg.Cluster.CephFS.NumPVC)
	for i := 1; i <= cfg.Cluster.RBD.NumPVC; i++ {
		volumeMode := corev1.PersistentVolumeFilesystem
		target := "/mnt/data/fio.dat"
		if i%2 == 0 {
			volumeMode = corev1.PersistentVolumeBlock
			target = "/dev/rbdblock"
		}
		allPods = append(allPods, PodInfo{
			Index:       i,
			Name:        fmt.Sprintf("%s-rbd-pod-%d", cfg.Cluster.Prefix, i),
			StorageType: "rbd",
			VolumeMode:  volumeMode,
			Target:      target,
			PVCName:     fmt.Sprintf("%s-rbd-pvc-%d", cfg.Cluster.Prefix, i),
		})
	}
	for i := 1; i <= cfg.Cluster.CephFS.NumPVC; i++ {
		allPods = append(allPods, PodInfo{
			Index:       i,
			Name:        fmt.Sprintf("%s-cephfs-pod-%d", cfg.Cluster.Prefix, i),
			StorageType: "cephfs",
			VolumeMode:  corev1.PersistentVolumeFilesystem,
			Target:      "/mnt/data/fio.dat",
			PVCName:     fmt.Sprintf("%s-cephfs-pvc-%d", cfg.Cluster.Prefix, i),
		})
	}
	return allPods
}

func pvcSpec(cfg *config.Config, pod PodInfo) k8s.PVCSpec {
	spec := k8s.PVCSpec{
		Name:        pod.PVCName,
		Namespace:   cfg.Cluster.Namespace,
		Size:        cfg.Cluster.PVCSize,
		VolumeMode:  pod.VolumeMode,
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Labels:      map[string]string{"app": cfg.Cluster.Prefix, "index": strconv.Itoa(pod.Index), "backend": pod.StorageType},
	}
	if pod.StorageType == "cephfs" {
		spec.StorageClass = cfg.Cluster.CephFS.StorageClass
		spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
	} else {
		spec.StorageClass = cfg.Cluster.RBD.StorageClass
	}
	return spec
}

func podSpec(cfg *config.Config, pod PodInfo) k8s.PodSpec {
	labels := map[string]string{"app": cfg.Cluster.Prefix, "index": strconv.Itoa(pod.Index), "backend": pod.StorageType}
	if len(cfg.Cluster.AppTypes) > 0 {
		labels["app-types"] = strings.Join(cfg.Cluster.AppTypes, ",")
	}
	return k8s.PodSpec{
		Name:       pod.Name,
		Namespace:  cfg.Cluster.Namespace,
		Image:      cfg.Tools.FIO.Image,
		PVCName:    pod.PVCName,
		VolumeMode: pod.VolumeMode,
		Labels:     labels,
		Privileged: pod.VolumeMode == corev1.PersistentVolumeBlock,
	}
}

// newProvisionGroup caps in-flight PVC create, bind, pod create, and pod ready waits.
func newProvisionGroup(ctx context.Context, cfg *config.Config) (*errgroup.Group, context.Context) {
	g, gctx := errgroup.WithContext(ctx)
	if n := int(cfg.Cluster.ProvisionLimit()); n > 0 {
		g.SetLimit(n)
	}
	return g, gctx
}

func ensurePodCapacity(ctx context.Context, client *k8s.Client, need int) error {
	nodes, free, err := k8s.SchedulablePodSlots(ctx, client)
	if err != nil {
		return fmt.Errorf("check pod capacity: %w", err)
	}
	log.Printf("Pod slots: %d free on %d schedulable nodes, requesting %d", free, nodes, need)
	if free < need {
		return fmt.Errorf("not enough pod slots: requested %d, %d free on %d schedulable nodes (workers are at max pods; masters are unschedulable)", need, free, nodes)
	}
	return nil
}

// podsNeeded is the peak pod count for the run. CephFS RWX adds one companion
// pod per CephFS volume when the standard suite runs.
func podsNeeded(cfg *config.Config) int {
	n := cfg.Cluster.RBD.NumPVC + cfg.Cluster.CephFS.NumPVC
	if cfg.Cluster.CephFS.NumPVC > 0 && !cfg.Cluster.SkipFIOStress && !cfg.Cluster.WriteVerify && len(cfg.Cluster.AppTypes) == 0 {
		n += cfg.Cluster.CephFS.NumPVC
	}
	if cfg.Tools.Object.Enabled {
		peak := cfg.Tools.Object.Workers
		if peak < 1 {
			peak = 1
		}
		if cfg.Tools.Object.HasPhase("list") && cfg.Tools.Object.Buckets > peak {
			peak = cfg.Tools.Object.Buckets
		}
		n += peak
	}
	return n
}

func waitForPods(ctx context.Context, cfg *config.Config, client *k8s.Client, allPods []PodInfo) ([]PodInfo, error) {
	if len(allPods) == 0 {
		return nil, nil
	}
	log.Printf("Waiting for pods to be Ready")
	g, gCtx := newProvisionGroup(ctx, cfg)
	ready := make([]PodInfo, len(allPods))
	for i, pod := range allPods {
		i, pod := i, pod
		g.Go(func() error {
			if err := k8s.WaitPodReady(gCtx, client, cfg.Cluster.Namespace, pod.Name, cfg.Cluster.WaitTimeout.Duration()); err != nil {
				var unsched *k8s.UnschedulableError
				if errors.As(err, &unsched) {
					return err
				}
				if gCtx.Err() != nil {
					return nil
				}
				log.Printf("WARNING: pod %s did not become Ready - skipping: %v", pod.Name, err)
				return nil
			}
			ready[i] = pod
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var readyPods []PodInfo
	for _, pod := range ready {
		if pod.Name != "" {
			readyPods = append(readyPods, pod)
		}
	}
	if len(readyPods) == 0 {
		return nil, fmt.Errorf("no pods became Ready")
	}
	log.Printf("Pods Ready: %d/%d", len(readyPods), len(allPods))
	return readyPods, nil
}

func cleanup(client *k8s.Client, cfg *config.Config) error {
	if cfg.Tools.Object.Resume {
		log.Printf("object resume: not deleting namespace %s", cfg.Cluster.Namespace)
		return nil
	}
	log.Printf("Cleaning up resources in namespace %s", cfg.Cluster.Namespace)

	// Fresh context per cleanup so a prior timeout cannot poison every API call.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Cluster.WaitTimeout.Duration())
	defer cancel()

	// Namespace delete cascades pods/PVCs/snapshots; much more reliable than per-object deletes.
	log.Printf("Cleanup: deleting namespace %s", cfg.Cluster.Namespace)
	if err := k8s.DeleteNamespace(ctx, client, cfg.Cluster.Namespace); err != nil {
		log.Printf("Cleanup: delete namespace: %v", err)
		return err
	}
	if err := k8s.WaitNamespaceDeleted(ctx, client, cfg.Cluster.Namespace, cfg.Cluster.WaitTimeout.Duration()); err != nil {
		log.Printf("Cleanup: wait namespace deleted: %v (may still be terminating)", err)
		return err
	}
	log.Printf("Cleanup complete")
	return nil
}
