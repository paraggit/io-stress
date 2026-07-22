package workload

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"golang.org/x/sync/errgroup"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/fio"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

func runPhase2(ctx context.Context, cfg *config.Config, client *k8s.Client, readyPods []PodInfo, collector *report.Collector) error {
	log.Println("═══ PHASE 2: LIFECYCLE STORM ═══")

	rbdSnapClass := cfg.SnapshotClass
	cephfsSnapClass := cfg.SnapshotClass
	if rbdSnapClass == "" {
		rbdSnapClass, _ = k8s.DetectSnapshotClass(ctx, client, "rbd.csi.ceph.com")
	}
	if cephfsSnapClass == "" {
		cephfsSnapClass, _ = k8s.DetectSnapshotClass(ctx, client, "cephfs.csi.ceph.com")
	}

	if rbdSnapClass == "" {
		log.Println("WARNING: No RBD VolumeSnapshotClass found — RBD snapshot operations will be skipped")
	}
	if cephfsSnapClass == "" {
		log.Println("WARNING: No CephFS VolumeSnapshotClass found — CephFS snapshot operations will be skipped")
	}

	var lifecyclePods []PodInfo
	for _, pod := range readyPods {
		if pod.Index%cfg.LifecycleInterval == 0 {
			lifecyclePods = append(lifecyclePods, pod)
		}
	}

	if len(lifecyclePods) == 0 {
		log.Printf("WARNING: No pods selected for lifecycle storm (interval=%d)", cfg.LifecycleInterval)
		return nil
	}
	log.Printf("Selected %d pods for lifecycle storm", len(lifecyclePods))

	g, ctx := errgroup.WithContext(ctx)
	if !cfg.Parallel {
		g.SetLimit(1)
	}

	for _, pod := range lifecyclePods {
		pod := pod
		snapClass := rbdSnapClass
		if pod.StorageType == "cephfs" {
			snapClass = cephfsSnapClass
		}
		g.Go(func() error {
			runLifecycleOnPod(ctx, cfg, client, pod, snapClass, collector)
			return nil
		})
	}

	g.Wait()
	log.Println("═══ PHASE 2 COMPLETE ═══")
	return nil
}

func runLifecycleOnPod(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, snapClass string, collector *report.Collector) {
	log.Printf("[%s] Starting lifecycle storm", pod.Name)

	sustainCtx, sustainCancel := context.WithCancel(ctx)
	go startSustainWorkload(sustainCtx, client, cfg, pod)

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error { runCloneOps(gCtx, cfg, client, pod, collector); return nil })
	g.Go(func() error { runSnapshotOps(gCtx, cfg, client, pod, snapClass, collector); return nil })
	g.Go(func() error { runExpandOps(gCtx, cfg, client, pod, collector); return nil })
	g.Wait()

	sustainCancel()

	runRescheduleOps(ctx, cfg, client, pod, collector)

	log.Printf("[%s] Lifecycle storm complete", pod.Name)
}

func runCloneOps(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) {
	clonePVCName := fmt.Sprintf("%s-%s-clone-pvc-%d", cfg.Prefix, pod.StorageType, pod.Index)
	clonePodName := fmt.Sprintf("%s-%s-clone-pod-%d", cfg.Prefix, pod.StorageType, pod.Index)

	log.Printf("[%s] CLONE: Creating clone PVC %s", pod.Name, clonePVCName)
	err := k8s.CreatePVC(ctx, client, k8s.PVCSpec{
		Name:         clonePVCName,
		Namespace:    cfg.Namespace,
		StorageClass: storageClassForPod(cfg, pod),
		Size:         cfg.PVCSize,
		VolumeMode:   pod.VolumeMode,
		AccessModes:  accessModesForPod(pod),
		Labels:       map[string]string{"app": cfg.Prefix, "role": "clone"},
		DataSource: &corev1.TypedLocalObjectReference{
			Kind: "PersistentVolumeClaim",
			Name: pod.PVCName,
		},
	})
	if err != nil {
		log.Printf("[%s] FAIL: CLONE: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: clonePodName, Job: "clone-create", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	if err := k8s.WaitPVCBound(ctx, client, cfg.Namespace, clonePVCName, cfg.WaitTimeout); err != nil {
		log.Printf("[%s] FAIL: CLONE: PVC not Bound: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: clonePodName, Job: "clone-bound", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	err = k8s.CreatePod(ctx, client, k8s.PodSpec{
		Name:       clonePodName,
		Namespace:  cfg.Namespace,
		Image:      cfg.FIOImage,
		PVCName:    clonePVCName,
		VolumeMode: pod.VolumeMode,
		Labels:     map[string]string{"app": cfg.Prefix, "role": "clone"},
		Privileged: pod.VolumeMode == corev1.PersistentVolumeBlock,
	})
	if err != nil {
		log.Printf("[%s] FAIL: CLONE: pod create: %v", pod.Name, err)
		return
	}

	if err := k8s.WaitPodReady(ctx, client, cfg.Namespace, clonePodName, cfg.WaitTimeout); err != nil {
		log.Printf("[%s] FAIL: CLONE: pod not Ready: %v", pod.Name, err)
		return
	}

	clonePodInfo := PodInfo{
		Index: pod.Index, Name: clonePodName, StorageType: pod.StorageType,
		VolumeMode: pod.VolumeMode, Target: pod.Target, PVCName: clonePVCName,
	}
	for _, job := range fio.ReducedSuite(pod.Target, cfg) {
		executeFIOJob(ctx, client, clonePodInfo, job, cfg, collector)
	}
}

func runSnapshotOps(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, snapClass string, collector *report.Collector) {
	if snapClass == "" {
		log.Printf("[%s] SKIP: SNAPSHOT: No VolumeSnapshotClass", pod.Name)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "snapshot-skip", Category: "lifecycle", Status: "skip", Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	snapName := fmt.Sprintf("%s-%s-snap-%d", cfg.Prefix, pod.StorageType, pod.Index)
	restoredPVCName := fmt.Sprintf("%s-%s-restored-pvc-%d", cfg.Prefix, pod.StorageType, pod.Index)
	restoredPodName := fmt.Sprintf("%s-%s-restored-pod-%d", cfg.Prefix, pod.StorageType, pod.Index)

	log.Printf("[%s] SNAPSHOT: Creating VolumeSnapshot %s", pod.Name, snapName)
	if err := k8s.CreateSnapshot(ctx, client, cfg.Namespace, snapName, pod.PVCName, snapClass); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "snapshot-create", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	if err := k8s.WaitSnapshotReady(ctx, client, cfg.Namespace, snapName, cfg.WaitTimeout); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: not ready: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "snapshot-ready", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	apiGroup := "snapshot.storage.k8s.io"
	err := k8s.CreatePVC(ctx, client, k8s.PVCSpec{
		Name:         restoredPVCName,
		Namespace:    cfg.Namespace,
		StorageClass: storageClassForPod(cfg, pod),
		Size:         cfg.PVCSize,
		VolumeMode:   pod.VolumeMode,
		AccessModes:  accessModesForPod(pod),
		Labels:       map[string]string{"app": cfg.Prefix, "role": "restored"},
		DataSource: &corev1.TypedLocalObjectReference{
			APIGroup: &apiGroup,
			Kind:     "VolumeSnapshot",
			Name:     snapName,
		},
	})
	if err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: restored PVC: %v", pod.Name, err)
		return
	}

	if err := k8s.WaitPVCBound(ctx, client, cfg.Namespace, restoredPVCName, cfg.WaitTimeout); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: restored PVC not Bound: %v", pod.Name, err)
		return
	}

	err = k8s.CreatePod(ctx, client, k8s.PodSpec{
		Name:       restoredPodName,
		Namespace:  cfg.Namespace,
		Image:      cfg.FIOImage,
		PVCName:    restoredPVCName,
		VolumeMode: pod.VolumeMode,
		Labels:     map[string]string{"app": cfg.Prefix, "role": "restored"},
		Privileged: pod.VolumeMode == corev1.PersistentVolumeBlock,
	})
	if err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: restored pod: %v", pod.Name, err)
		return
	}

	if err := k8s.WaitPodReady(ctx, client, cfg.Namespace, restoredPodName, cfg.WaitTimeout); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: restored pod not Ready: %v", pod.Name, err)
		return
	}

	restoredPodInfo := PodInfo{
		Index: pod.Index, Name: restoredPodName, StorageType: pod.StorageType,
		VolumeMode: pod.VolumeMode, Target: pod.Target, PVCName: restoredPVCName,
	}
	for _, job := range fio.ReducedSuite(pod.Target, cfg) {
		executeFIOJob(ctx, client, restoredPodInfo, job, cfg, collector)
	}
}

func runExpandOps(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) {
	expandedSize, err := computeExpandedSize(cfg.PVCSize, cfg.ExpandFactor)
	if err != nil {
		log.Printf("[%s] FAIL: EXPAND: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "expand-parse", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}
	log.Printf("[%s] EXPAND: Patching %s from %s to %s", pod.Name, pod.PVCName, cfg.PVCSize, expandedSize)

	if err := k8s.PatchPVCSize(ctx, client, cfg.Namespace, pod.PVCName, expandedSize); err != nil {
		log.Printf("[%s] FAIL: EXPAND: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "expand-patch", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	deadline := time.After(cfg.WaitTimeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			log.Printf("[%s] FAIL: EXPAND: capacity did not reach %s", pod.Name, expandedSize)
			collector.Add(report.JobResult{Pod: pod.Name, Job: "expand-wait", Category: "lifecycle", Status: "fail", Error: "timeout", Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
			return
		case <-ticker.C:
			capStr, err := k8s.GetPVCCapacity(ctx, client, cfg.Namespace, pod.PVCName)
			if err != nil {
				continue
			}
			actual, _ := resource.ParseQuantity(capStr)
			wanted, _ := resource.ParseQuantity(expandedSize)
			if actual.Cmp(wanted) >= 0 {
				log.Printf("[%s] EXPAND: capacity reached %s", pod.Name, expandedSize)
				halfRuntime := cfg.FIORuntime / 2
				expandJob := fio.Job{
					Name:     "expand-verify",
					Category: "lifecycle",
					Args: []string{
						"--rw=randwrite", "--bs=4k",
						fmt.Sprintf("--size=%s", expandedSize),
						"--ioengine=libaio", "--direct=1", "--iodepth=16",
						"--time_based=1", fmt.Sprintf("--runtime=%d", halfRuntime),
						"--verify=crc32c", "--verify_backlog=128",
						"--verify_fatal=1", "--group_reporting=1",
					},
				}
				executeFIOJob(ctx, client, pod, expandJob, cfg, collector)
				return
			}
		}
	}
}

func runRescheduleOps(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) {
	log.Printf("[%s] RESCHEDULE: Deleting pod", pod.Name)
	if err := k8s.DeletePod(ctx, client, cfg.Namespace, pod.Name); err != nil {
		log.Printf("[%s] FAIL: RESCHEDULE: delete: %v", pod.Name, err)
		return
	}

	log.Printf("[%s] RESCHEDULE: Recreating pod", pod.Name)
	err := k8s.Retry(func() error {
		return k8s.CreatePod(ctx, client, k8s.PodSpec{
			Name:       pod.Name,
			Namespace:  cfg.Namespace,
			Image:      cfg.FIOImage,
			PVCName:    pod.PVCName,
			VolumeMode: pod.VolumeMode,
			Labels:     map[string]string{"app": cfg.Prefix, "role": "reschedule"},
			Privileged: pod.VolumeMode == corev1.PersistentVolumeBlock,
		})
	})
	if err != nil {
		log.Printf("[%s] FAIL: RESCHEDULE: recreate: %v", pod.Name, err)
		return
	}

	if err := k8s.WaitPodReady(ctx, client, cfg.Namespace, pod.Name, cfg.WaitTimeout); err != nil {
		log.Printf("[%s] FAIL: RESCHEDULE: pod not Ready: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "reschedule-ready", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	halfRuntime := cfg.FIORuntime / 2
	readJob := fio.Job{
		Name:     "reschedule-verify",
		Category: "lifecycle",
		Args: []string{
			"--rw=randread", "--bs=4k",
			fmt.Sprintf("--size=%s", cfg.FIOSize),
			"--ioengine=libaio", "--direct=1", "--iodepth=16",
			"--time_based=1", fmt.Sprintf("--runtime=%d", halfRuntime),
			"--group_reporting=1",
		},
	}
	executeFIOJob(ctx, client, pod, readJob, cfg, collector)

	writeVerifyJob := fio.Job{
		Name:     "reschedule-write-verify",
		Category: "lifecycle",
		Args: []string{
			"--rw=randrw", "--rwmixread=50", "--bs=4k",
			fmt.Sprintf("--size=%s", cfg.FIOSize),
			"--ioengine=libaio", "--direct=1", "--iodepth=16",
			"--time_based=1", fmt.Sprintf("--runtime=%d", halfRuntime),
			"--verify=crc32c", "--verify_backlog=128",
			"--verify_fatal=1", "--group_reporting=1",
		},
	}
	executeFIOJob(ctx, client, pod, writeVerifyJob, cfg, collector)
}

func storageClassForPod(cfg *config.Config, pod PodInfo) string {
	if pod.StorageType == "cephfs" {
		return cfg.CephFSStorageClass
	}
	return cfg.RBDStorageClass
}

func accessModesForPod(pod PodInfo) []corev1.PersistentVolumeAccessMode {
	if pod.StorageType == "cephfs" {
		return []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
	}
	return []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
}

func computeExpandedSize(original string, factor int) (string, error) {
	numStr := strings.TrimRight(original, "GiMiTiKi")
	unit := original[len(numStr):]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return "", fmt.Errorf("parse PVC size %q: %w", original, err)
	}
	return fmt.Sprintf("%d%s", num*factor, unit), nil
}
