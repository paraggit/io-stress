package workload

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
	"golang.org/x/sync/errgroup"
)

func Run(ctx context.Context, cfg *config.Config) error {
	if cfg.Cluster.ResultsDir == "" {
		cfg.Cluster.ResultsDir = filepath.Join(".", "results", time.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(cfg.Cluster.ResultsDir, 0755); err != nil {
		return fmt.Errorf("create results dir: %w", err)
	}

	client, err := k8s.NewClient(cfg.Cluster.Kubeconfig)
	if err != nil {
		return fmt.Errorf("create k8s client: %w", err)
	}

	if !cfg.Cluster.NoCleanup {
		defer cleanup(context.Background(), client, cfg)
	}

	log.Printf("Ensuring namespace %s", cfg.Cluster.Namespace)
	if err := k8s.EnsureNamespace(ctx, client, cfg.Cluster.Namespace); err != nil {
		return err
	}

	allPods, err := setupResources(ctx, cfg, client)
	if err != nil {
		return err
	}

	readyPods, err := waitForPods(ctx, cfg, client, allPods)
	if err != nil {
		return err
	}

	collector := report.NewCollector()
	startTime := time.Now()

	if !cfg.Cluster.SkipFIOStress {
		if err := runPhase1(ctx, cfg, client, readyPods, collector); err != nil {
			return err
		}
	} else {
		log.Println("Phase 1 skipped (--skip-fio-stress)")
	}

	if !cfg.Cluster.SkipLifecycle {
		if err := runPhase2(ctx, cfg, client, readyPods, collector); err != nil {
			log.Printf("Phase 2 completed with errors: %v", err)
		}
		if err := runPhase3(ctx, cfg, client, readyPods, collector); err != nil {
			log.Printf("Phase 3 completed with errors: %v", err)
		}
	}

	results := collector.Results()
	summary := report.ComputeSummary(results)
	rpt := &report.RunReport{
		StartTime: startTime,
		EndTime:   time.Now(),
		Results:   results,
		Summary:   summary,
	}
	if err := report.WriteJSON(cfg.Cluster.ResultsDir, rpt); err != nil {
		log.Printf("warning: failed to write report: %v", err)
	}

	report.PrintSummary(results)
	log.Printf("Results in %s", cfg.Cluster.ResultsDir)

	if summary.Failed > 0 {
		return fmt.Errorf("%d job(s) failed", summary.Failed)
	}
	return nil
}

func setupResources(ctx context.Context, cfg *config.Config, client *k8s.Client) ([]PodInfo, error) {
	var allPods []PodInfo

	totalRBD := cfg.Cluster.RBD.NumPVC
	totalCephFS := cfg.Cluster.CephFS.NumPVC
	log.Printf("Creating %d PVCs (%d RBD + %d CephFS)", totalRBD+totalCephFS, totalRBD, totalCephFS)

	g, gCtx := errgroup.WithContext(ctx)

	for i := 1; i <= totalRBD; i++ {
		i := i
		var volumeMode corev1.PersistentVolumeMode
		if i%2 == 1 {
			volumeMode = corev1.PersistentVolumeFilesystem
		} else {
			volumeMode = corev1.PersistentVolumeBlock
		}

		pvcName := fmt.Sprintf("%s-rbd-pvc-%d", cfg.Cluster.Prefix, i)
		podName := fmt.Sprintf("%s-rbd-pod-%d", cfg.Cluster.Prefix, i)
		target := "/mnt/data/fio.dat"
		if volumeMode == corev1.PersistentVolumeBlock {
			target = "/dev/rbdblock"
		}

		allPods = append(allPods, PodInfo{
			Index:       i,
			Name:        podName,
			StorageType: "rbd",
			VolumeMode:  volumeMode,
			Target:      target,
			PVCName:     pvcName,
		})

		g.Go(func() error {
			return k8s.Retry(func() error {
				return k8s.CreatePVC(gCtx, client, k8s.PVCSpec{
					Name:         pvcName,
					Namespace:    cfg.Cluster.Namespace,
					StorageClass: cfg.Cluster.RBD.StorageClass,
					Size:         cfg.Cluster.PVCSize,
					VolumeMode:   volumeMode,
					AccessModes:  []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Labels:       map[string]string{"app": cfg.Cluster.Prefix, "index": strconv.Itoa(i), "backend": "rbd"},
				})
			})
		})
	}

	for i := 1; i <= totalCephFS; i++ {
		i := i
		pvcName := fmt.Sprintf("%s-cephfs-pvc-%d", cfg.Cluster.Prefix, i)
		podName := fmt.Sprintf("%s-cephfs-pod-%d", cfg.Cluster.Prefix, i)

		allPods = append(allPods, PodInfo{
			Index:       i,
			Name:        podName,
			StorageType: "cephfs",
			VolumeMode:  corev1.PersistentVolumeFilesystem,
			Target:      "/mnt/data/fio.dat",
			PVCName:     pvcName,
		})

		g.Go(func() error {
			return k8s.Retry(func() error {
				return k8s.CreatePVC(gCtx, client, k8s.PVCSpec{
					Name:         pvcName,
					Namespace:    cfg.Cluster.Namespace,
					StorageClass: cfg.Cluster.CephFS.StorageClass,
					Size:         cfg.Cluster.PVCSize,
					VolumeMode:   corev1.PersistentVolumeFilesystem,
					AccessModes:  []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
					Labels:       map[string]string{"app": cfg.Cluster.Prefix, "index": strconv.Itoa(i), "backend": "cephfs"},
				})
			})
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("create PVCs: %w", err)
	}

	log.Printf("Waiting for PVCs to be Bound")
	gBound, boundCtx := errgroup.WithContext(ctx)
	for _, pod := range allPods {
		pod := pod
		gBound.Go(func() error {
			return k8s.WaitPVCBound(boundCtx, client, cfg.Cluster.Namespace, pod.PVCName, cfg.Cluster.WaitTimeout.Duration())
		})
	}
	if err := gBound.Wait(); err != nil {
		return nil, fmt.Errorf("wait PVCs bound: %w", err)
	}
	log.Printf("PVCs Bound: %d/%d", len(allPods), len(allPods))

	log.Printf("Creating %d pods", len(allPods))
	gPod, podCtx := errgroup.WithContext(ctx)
	for _, pod := range allPods {
		pod := pod
		gPod.Go(func() error {
			return k8s.Retry(func() error {
				return k8s.CreatePod(podCtx, client, k8s.PodSpec{
					Name:       pod.Name,
					Namespace:  cfg.Cluster.Namespace,
					Image:      cfg.Tools.FIO.Image,
					PVCName:    pod.PVCName,
					VolumeMode: pod.VolumeMode,
					Labels:     map[string]string{"app": cfg.Cluster.Prefix, "index": strconv.Itoa(pod.Index), "backend": pod.StorageType},
					Privileged: pod.VolumeMode == corev1.PersistentVolumeBlock,
				})
			})
		})
	}
	if err := gPod.Wait(); err != nil {
		return nil, fmt.Errorf("create pods: %w", err)
	}

	return allPods, nil
}

func waitForPods(ctx context.Context, cfg *config.Config, client *k8s.Client, allPods []PodInfo) ([]PodInfo, error) {
	log.Printf("Waiting for pods to be Ready")
	var readyPods []PodInfo
	for _, pod := range allPods {
		if err := k8s.WaitPodReady(ctx, client, cfg.Cluster.Namespace, pod.Name, cfg.Cluster.WaitTimeout.Duration()); err != nil {
			log.Printf("WARNING: pod %s did not become Ready - skipping", pod.Name)
			continue
		}
		readyPods = append(readyPods, pod)
	}
	if len(readyPods) == 0 {
		return nil, fmt.Errorf("no pods became Ready")
	}
	log.Printf("Pods Ready: %d/%d", len(readyPods), len(allPods))
	return readyPods, nil
}

func cleanup(ctx context.Context, client *k8s.Client, cfg *config.Config) {
	log.Printf("Cleaning up resources in namespace %s", cfg.Cluster.Namespace)

	// Clean up RBD resources
	for i := 1; i <= cfg.Cluster.RBD.NumPVC; i++ {
		for _, name := range []string{
			fmt.Sprintf("%s-rbd-pod-%d", cfg.Cluster.Prefix, i),
			fmt.Sprintf("%s-rbd-clone-pod-%d", cfg.Cluster.Prefix, i),
			fmt.Sprintf("%s-rbd-restored-pod-%d", cfg.Cluster.Prefix, i),
		} {
			k8s.DeletePod(ctx, client, cfg.Cluster.Namespace, name)
		}
	}

	// Clean up CephFS resources
	for i := 1; i <= cfg.Cluster.CephFS.NumPVC; i++ {
		for _, name := range []string{
			fmt.Sprintf("%s-cephfs-pod-%d", cfg.Cluster.Prefix, i),
			fmt.Sprintf("%s-cephfs-pod-%d-rwx", cfg.Cluster.Prefix, i),
			fmt.Sprintf("%s-cephfs-clone-pod-%d", cfg.Cluster.Prefix, i),
			fmt.Sprintf("%s-cephfs-restored-pod-%d", cfg.Cluster.Prefix, i),
		} {
			k8s.DeletePod(ctx, client, cfg.Cluster.Namespace, name)
		}
	}

	// Clean up RBD PVCs
	for i := 1; i <= cfg.Cluster.RBD.NumPVC; i++ {
		for _, suffix := range []string{"pvc", "clone-pvc", "restored-pvc"} {
			name := fmt.Sprintf("%s-rbd-%s-%d", cfg.Cluster.Prefix, suffix, i)
			k8s.DeletePVC(ctx, client, cfg.Cluster.Namespace, name)
		}
	}

	// Clean up CephFS PVCs
	for i := 1; i <= cfg.Cluster.CephFS.NumPVC; i++ {
		for _, suffix := range []string{"pvc", "clone-pvc", "restored-pvc"} {
			name := fmt.Sprintf("%s-cephfs-%s-%d", cfg.Cluster.Prefix, suffix, i)
			k8s.DeletePVC(ctx, client, cfg.Cluster.Namespace, name)
		}
	}

	// Clean up RBD snapshots
	for i := 1; i <= cfg.Cluster.RBD.NumPVC; i++ {
		snapName := fmt.Sprintf("%s-rbd-snap-%d", cfg.Cluster.Prefix, i)
		k8s.DeleteSnapshot(ctx, client, cfg.Cluster.Namespace, snapName)
	}

	// Clean up CephFS snapshots
	for i := 1; i <= cfg.Cluster.CephFS.NumPVC; i++ {
		snapName := fmt.Sprintf("%s-cephfs-snap-%d", cfg.Cluster.Prefix, i)
		k8s.DeleteSnapshot(ctx, client, cfg.Cluster.Namespace, snapName)
	}

	k8s.DeleteNamespace(ctx, client, cfg.Cluster.Namespace)
	log.Printf("Cleanup complete")
}
