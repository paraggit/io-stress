package workload

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/fio"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

func runPhase2(ctx context.Context, cfg *config.Config, client *k8s.Client, readyPods []PodInfo, collector *report.Collector) error {
	log.Println("═══ PHASE 2: LIFECYCLE STORM ═══")

	rbdSnapClass := cfg.Cluster.SnapshotClass
	cephfsSnapClass := cfg.Cluster.SnapshotClass
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
		if pod.Index%cfg.Cluster.LifecycleInterval == 0 {
			lifecyclePods = append(lifecyclePods, pod)
		}
	}

	if len(lifecyclePods) == 0 {
		log.Printf("WARNING: No pods selected for lifecycle storm (interval=%d)", cfg.Cluster.LifecycleInterval)
		return nil
	}
	log.Printf("Selected %d pods for lifecycle storm", len(lifecyclePods))

	g, ctx := errgroup.WithContext(ctx)
	if !cfg.Tools.FIO.Parallel {
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
	clonePVCName := fmt.Sprintf("%s-%s-clone-pvc-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)
	clonePodName := fmt.Sprintf("%s-%s-clone-pod-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)

	cloneSize, err := sizeForCloneOrRestore(ctx, client, cfg, pod.PVCName)
	if err != nil {
		log.Printf("[%s] FAIL: CLONE: size: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: clonePodName, Job: "clone-create", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	log.Printf("[%s] CLONE: Creating clone PVC %s (size %s)", pod.Name, clonePVCName, cloneSize)
	err = k8s.CreatePVC(ctx, client, k8s.PVCSpec{
		Name:         clonePVCName,
		Namespace:    cfg.Cluster.Namespace,
		StorageClass: storageClassForPod(cfg, pod),
		Size:         cloneSize,
		VolumeMode:   pod.VolumeMode,
		AccessModes:  accessModesForPod(pod),
		Labels:       map[string]string{"app": cfg.Cluster.Prefix, "role": "clone"},
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

	if err := k8s.WaitPVCBound(ctx, client, cfg.Cluster.Namespace, clonePVCName, cfg.Cluster.WaitTimeout.Duration()); err != nil {
		log.Printf("[%s] FAIL: CLONE: PVC not Bound: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: clonePodName, Job: "clone-bound", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	err = k8s.CreatePod(ctx, client, k8s.PodSpec{
		Name:          clonePodName,
		Namespace:     cfg.Cluster.Namespace,
		Image:         cfg.Tools.FIO.Image,
		PVCName:       clonePVCName,
		VolumeMode:    pod.VolumeMode,
		Labels:        map[string]string{"app": cfg.Cluster.Prefix, "role": "clone"},
		Privileged:    pod.VolumeMode == corev1.PersistentVolumeBlock,
		ContainerName: "iotool",
	})
	if err != nil {
		log.Printf("[%s] FAIL: CLONE: pod create: %v", pod.Name, err)
		return
	}

	if err := k8s.WaitPodReady(ctx, client, cfg.Cluster.Namespace, clonePodName, cfg.Cluster.WaitTimeout.Duration()); err != nil {
		log.Printf("[%s] FAIL: CLONE: pod not Ready: %v", pod.Name, err)
		return
	}

	clonePodInfo := PodInfo{
		Index: pod.Index, Name: clonePodName, StorageType: pod.StorageType,
		VolumeMode: pod.VolumeMode, Target: pod.Target, PVCName: clonePVCName,
		ContainerName: pod.ContainerName,
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

	snapName := fmt.Sprintf("%s-%s-snap-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)
	restoredPVCName := fmt.Sprintf("%s-%s-restored-pvc-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)
	restoredPodName := fmt.Sprintf("%s-%s-restored-pod-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)

	log.Printf("[%s] SNAPSHOT: Creating VolumeSnapshot %s", pod.Name, snapName)
	if err := k8s.CreateSnapshot(ctx, client, cfg.Cluster.Namespace, snapName, pod.PVCName, snapClass); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "snapshot-create", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	if err := k8s.WaitSnapshotReady(ctx, client, cfg.Cluster.Namespace, snapName, cfg.Cluster.WaitTimeout.Duration()); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: not ready: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "snapshot-ready", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	restoreSize, err := sizeForCloneOrRestore(ctx, client, cfg, pod.PVCName)
	if err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: restored PVC size: %v", pod.Name, err)
		return
	}

	apiGroup := "snapshot.storage.k8s.io"
	err = k8s.CreatePVC(ctx, client, k8s.PVCSpec{
		Name:         restoredPVCName,
		Namespace:    cfg.Cluster.Namespace,
		StorageClass: storageClassForPod(cfg, pod),
		Size:         restoreSize,
		VolumeMode:   pod.VolumeMode,
		AccessModes:  accessModesForPod(pod),
		Labels:       map[string]string{"app": cfg.Cluster.Prefix, "role": "restored"},
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

	if err := k8s.WaitPVCBound(ctx, client, cfg.Cluster.Namespace, restoredPVCName, cfg.Cluster.WaitTimeout.Duration()); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: restored PVC not Bound: %v", pod.Name, err)
		return
	}

	err = k8s.CreatePod(ctx, client, k8s.PodSpec{
		Name:          restoredPodName,
		Namespace:     cfg.Cluster.Namespace,
		Image:         cfg.Tools.FIO.Image,
		PVCName:       restoredPVCName,
		VolumeMode:    pod.VolumeMode,
		Labels:        map[string]string{"app": cfg.Cluster.Prefix, "role": "restored"},
		Privileged:    pod.VolumeMode == corev1.PersistentVolumeBlock,
		ContainerName: "iotool",
	})
	if err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: restored pod: %v", pod.Name, err)
		return
	}

	if err := k8s.WaitPodReady(ctx, client, cfg.Cluster.Namespace, restoredPodName, cfg.Cluster.WaitTimeout.Duration()); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: restored pod not Ready: %v", pod.Name, err)
		return
	}

	restoredPodInfo := PodInfo{
		Index: pod.Index, Name: restoredPodName, StorageType: pod.StorageType,
		VolumeMode: pod.VolumeMode, Target: pod.Target, PVCName: restoredPVCName,
		ContainerName: pod.ContainerName,
	}
	for _, job := range fio.ReducedSuite(pod.Target, cfg) {
		executeFIOJob(ctx, client, restoredPodInfo, job, cfg, collector)
	}
}

func runExpandOps(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) {
	expandedSize, err := computeExpandedSize(cfg.Cluster.PVCSize, cfg.Cluster.ExpandFactor)
	if err != nil {
		log.Printf("[%s] FAIL: EXPAND: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "expand-parse", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}
	log.Printf("[%s] EXPAND: Patching %s from %s to %s", pod.Name, pod.PVCName, cfg.Cluster.PVCSize, expandedSize)

	if err := k8s.PatchPVCSize(ctx, client, cfg.Cluster.Namespace, pod.PVCName, expandedSize); err != nil {
		log.Printf("[%s] FAIL: EXPAND: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "expand-patch", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	deadline := time.After(cfg.Cluster.WaitTimeout.Duration())
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
			capStr, err := k8s.GetPVCCapacity(ctx, client, cfg.Cluster.Namespace, pod.PVCName)
			if err != nil {
				continue
			}
			actual, _ := resource.ParseQuantity(capStr)
			wanted, _ := resource.ParseQuantity(expandedSize)
			if actual.Cmp(wanted) >= 0 {
				log.Printf("[%s] EXPAND: capacity reached %s", pod.Name, expandedSize)
				halfRuntime := cfg.Tools.FIO.Runtime / 2
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
	if err := k8s.DeletePod(ctx, client, cfg.Cluster.Namespace, pod.Name); err != nil {
		log.Printf("[%s] FAIL: RESCHEDULE: delete: %v", pod.Name, err)
		return
	}
	if err := k8s.WaitPodDeleted(ctx, client, cfg.Cluster.Namespace, pod.Name, cfg.Cluster.WaitTimeout.Duration()); err != nil {
		log.Printf("[%s] FAIL: RESCHEDULE: wait delete: %v", pod.Name, err)
		return
	}

	log.Printf("[%s] RESCHEDULE: Recreating pod", pod.Name)
	err := k8s.Retry(func() error {
		return k8s.CreatePod(ctx, client, k8s.PodSpec{
			Name:          pod.Name,
			Namespace:     cfg.Cluster.Namespace,
			Image:         cfg.Tools.FIO.Image,
			PVCName:       pod.PVCName,
			VolumeMode:    pod.VolumeMode,
			Labels:        map[string]string{"app": cfg.Cluster.Prefix, "role": "reschedule"},
			Privileged:    pod.VolumeMode == corev1.PersistentVolumeBlock,
			ContainerName: pod.ContainerName,
		})
	})
	if err != nil {
		log.Printf("[%s] FAIL: RESCHEDULE: recreate: %v", pod.Name, err)
		return
	}

	if err := k8s.WaitPodReady(ctx, client, cfg.Cluster.Namespace, pod.Name, cfg.Cluster.WaitTimeout.Duration()); err != nil {
		log.Printf("[%s] FAIL: RESCHEDULE: pod not Ready: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "reschedule-ready", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	halfRuntime := cfg.Tools.FIO.Runtime / 2
	readJob := fio.Job{
		Name:     "reschedule-verify",
		Category: "lifecycle",
		Args: []string{
			"--rw=randread", "--bs=4k",
			fmt.Sprintf("--size=%s", cfg.Tools.FIO.Size),
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
			fmt.Sprintf("--size=%s", cfg.Tools.FIO.Size),
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
		return cfg.Cluster.CephFS.StorageClass
	}
	return cfg.Cluster.RBD.StorageClass
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

// sizeForCloneOrRestore picks a size >= the source PVC. Clone/restore run
// concurrently with expand, so we take the max of configured size, the
// expand target, and the source PVC's current request/capacity.
func sizeForCloneOrRestore(ctx context.Context, client *k8s.Client, cfg *config.Config, sourcePVC string) (string, error) {
	size := cfg.Cluster.PVCSize
	if expanded, err := computeExpandedSize(cfg.Cluster.PVCSize, cfg.Cluster.ExpandFactor); err == nil {
		size = maxQuantityString(size, expanded)
	}
	if req, err := k8s.GetPVCRequestedSize(ctx, client, cfg.Cluster.Namespace, sourcePVC); err == nil && req != "" {
		size = maxQuantityString(size, req)
	}
	if capStr, err := k8s.GetPVCCapacity(ctx, client, cfg.Cluster.Namespace, sourcePVC); err == nil && capStr != "" {
		size = maxQuantityString(size, capStr)
	}
	return size, nil
}

func maxQuantityString(a, b string) string {
	qa, errA := resource.ParseQuantity(a)
	qb, errB := resource.ParseQuantity(b)
	if errA != nil {
		return b
	}
	if errB != nil {
		return a
	}
	if qb.Cmp(qa) > 0 {
		return b
	}
	return a
}
