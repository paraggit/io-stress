package workload

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/fio"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

const expandVerifyMargin int64 = 256 * 1024 * 1024 // 256Mi safety margin on CephFS quota

type lifecycleRuntime struct {
	provision     *semaphore.Weighted
	sustainCancel context.CancelFunc
	sustainDone   <-chan struct{}
}

func (r *lifecycleRuntime) acquireProvision(ctx context.Context) error {
	if r == nil || r.provision == nil {
		return nil
	}
	return r.provision.Acquire(ctx, 1)
}

func (r *lifecycleRuntime) releaseProvision() {
	if r == nil || r.provision == nil {
		return
	}
	r.provision.Release(1)
}

func (r *lifecycleRuntime) quiesceSustain() {
	if r == nil {
		return
	}
	if r.sustainCancel != nil {
		r.sustainCancel()
	}
	if r.sustainDone != nil {
		<-r.sustainDone
	}
}

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

	provision := semaphore.NewWeighted(cfg.Cluster.ProvisionLimit())

	for _, pod := range lifecyclePods {
		pod := pod
		snapClass := rbdSnapClass
		if pod.StorageType == "cephfs" {
			snapClass = cephfsSnapClass
		}
		g.Go(func() error {
			runLifecycleOnPod(ctx, cfg, client, pod, snapClass, collector, provision)
			return nil
		})
	}

	g.Wait()
	log.Println("═══ PHASE 2 COMPLETE ═══")
	return nil
}

func runLifecycleOnPod(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, snapClass string, collector *report.Collector, provision *semaphore.Weighted) {
	log.Printf("[%s] Starting lifecycle storm", pod.Name)

	if err := seedIntegrity(ctx, cfg, client, pod, collector); err != nil {
		log.Printf("[%s] FAIL: integrity seed: %v — skipping clone/snapshot/expand", pod.Name, err)
		return
	}

	sustainCtx, sustainCancel := context.WithCancel(ctx)
	sustainDone := make(chan struct{})
	go func() {
		defer close(sustainDone)
		startSustainWorkload(sustainCtx, client, cfg, pod)
	}()

	rt := &lifecycleRuntime{
		provision:     provision,
		sustainCancel: sustainCancel,
		sustainDone:   sustainDone,
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error { runCloneOps(gCtx, cfg, client, pod, collector, rt); return nil })
	g.Go(func() error { runSnapshotOps(gCtx, cfg, client, pod, snapClass, collector, rt); return nil })
	g.Go(func() error { runExpandOps(gCtx, cfg, client, pod, collector, rt); return nil })
	g.Wait()

	rt.quiesceSustain()

	runRescheduleOps(ctx, cfg, client, pod, collector)

	log.Printf("[%s] Lifecycle storm complete", pod.Name)
}

func seedIntegrity(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) error {
	result := executeFIOJob(ctx, client, pod, fio.IntegritySeedJob(cfg), cfg, collector)
	if result.Status != "pass" {
		if result.Error != "" {
			return fmt.Errorf("%s", result.Error)
		}
		return fmt.Errorf("integrity-seed status %s", result.Status)
	}
	return nil
}

func recordBoundWait(resultPod string, pod PodInfo, job string, err error, collector *report.Collector) {
	status := "fail"
	if k8s.IsProvisionTimeout(err) {
		status = "slow"
		log.Printf("[%s] SLOW: %s (retryable under load): %v", pod.Name, job, err)
	} else {
		log.Printf("[%s] FAIL: %s: %v", pod.Name, job, err)
	}
	collector.Add(report.JobResult{
		Pod:        resultPod,
		Job:        job,
		Category:   "lifecycle",
		Status:     status,
		Error:      err.Error(),
		Storage:    pod.StorageType,
		VolumeMode: pod.VolumeModeStr(),
	})
}

func runLifecycleStress(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) {
	// Stress the cloned/restored volume on a distinct file so it cannot
	// overwrite the integrity-seeded region that phase3 will verify.
	if pod.VolumeMode != corev1.PersistentVolumeFilesystem {
		return
	}
	stress := pod
	stress.Target = "/mnt/data/lifecycle-stress.dat"
	for _, job := range fio.ReducedSuite(stress.Target, cfg) {
		executeFIOJob(ctx, client, stress, job, cfg, collector)
	}
}

func runCloneOps(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector, rt *lifecycleRuntime) {
	clonePVCName := fmt.Sprintf("%s-%s-clone-pvc-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)
	clonePodName := fmt.Sprintf("%s-%s-clone-pod-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)

	cloneSize, err := sizeForCloneOrRestore(ctx, client, cfg, pod.PVCName)
	if err != nil {
		log.Printf("[%s] FAIL: CLONE: size: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: clonePodName, Job: "clone-create", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	if err := rt.acquireProvision(ctx); err != nil {
		log.Printf("[%s] FAIL: CLONE: provision slot: %v", pod.Name, err)
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
		rt.releaseProvision()
		log.Printf("[%s] FAIL: CLONE: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: clonePodName, Job: "clone-create", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}

	if err := k8s.WaitPVCBoundProgress(ctx, client, cfg.Cluster.Namespace, clonePVCName, cfg.Cluster.ProvisionTimeout(pod.StorageType)); err != nil {
		rt.releaseProvision()
		recordBoundWait(clonePodName, pod, "clone-bound", err, collector)
		return
	}
	rt.releaseProvision()

	err = k8s.CreatePod(ctx, client, k8s.PodSpec{
		Name:       clonePodName,
		Namespace:  cfg.Cluster.Namespace,
		Image:      cfg.Tools.FIO.Image,
		PVCName:    clonePVCName,
		VolumeMode: pod.VolumeMode,
		Labels:     map[string]string{"app": cfg.Cluster.Prefix, "role": "clone"},
		Privileged: pod.VolumeMode == corev1.PersistentVolumeBlock,
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
	}
	runLifecycleStress(ctx, cfg, client, clonePodInfo, collector)
}

func runSnapshotOps(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, snapClass string, collector *report.Collector, rt *lifecycleRuntime) {
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

	if err := k8s.WaitSnapshotReady(ctx, client, cfg.Cluster.Namespace, snapName, cfg.Cluster.ProvisionTimeout(pod.StorageType)); err != nil {
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
	if err := rt.acquireProvision(ctx); err != nil {
		log.Printf("[%s] FAIL: SNAPSHOT: provision slot: %v", pod.Name, err)
		return
	}
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
		rt.releaseProvision()
		log.Printf("[%s] FAIL: SNAPSHOT: restored PVC: %v", pod.Name, err)
		return
	}

	if err := k8s.WaitPVCBoundProgress(ctx, client, cfg.Cluster.Namespace, restoredPVCName, cfg.Cluster.ProvisionTimeout(pod.StorageType)); err != nil {
		rt.releaseProvision()
		recordBoundWait(restoredPodName, pod, "restore-bound", err, collector)
		return
	}
	rt.releaseProvision()

	err = k8s.CreatePod(ctx, client, k8s.PodSpec{
		Name:       restoredPodName,
		Namespace:  cfg.Cluster.Namespace,
		Image:      cfg.Tools.FIO.Image,
		PVCName:    restoredPVCName,
		VolumeMode: pod.VolumeMode,
		Labels:     map[string]string{"app": cfg.Cluster.Prefix, "role": "restored"},
		Privileged: pod.VolumeMode == corev1.PersistentVolumeBlock,
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
	}
	runLifecycleStress(ctx, cfg, client, restoredPodInfo, collector)
}

func runExpandOps(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector, rt *lifecycleRuntime) {
	expandedSize, err := computeExpandedSize(cfg.Cluster.PVCSize, cfg.Cluster.ExpandFactor)
	if err != nil {
		log.Printf("[%s] FAIL: EXPAND: %v", pod.Name, err)
		collector.Add(report.JobResult{Pod: pod.Name, Job: "expand-parse", Category: "lifecycle", Status: "fail", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
		return
	}
	log.Printf("[%s] EXPAND: Patching %s from %s to %s", pod.Name, pod.PVCName, cfg.Cluster.PVCSize, expandedSize)

	if err := rt.acquireProvision(ctx); err != nil {
		log.Printf("[%s] FAIL: EXPAND: provision slot: %v", pod.Name, err)
		return
	}

	if err := k8s.PatchPVCSize(ctx, client, cfg.Cluster.Namespace, pod.PVCName, expandedSize); err != nil {
		rt.releaseProvision()
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
			rt.releaseProvision()
			return
		case <-deadline:
			rt.releaseProvision()
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
				rt.releaseProvision()
				log.Printf("[%s] EXPAND: capacity reached %s", pod.Name, expandedSize)
				runExpandVerify(ctx, cfg, client, pod, expandedSize, collector, rt)
				return
			}
		}
	}
}

func runExpandVerify(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, expandedSize string, collector *report.Collector, rt *lifecycleRuntime) {
	rt.quiesceSustain()

	writeSize := expandedSize
	expandJob := fio.Job{
		Name:     "expand-verify",
		Category: "lifecycle",
		Args: []string{
			"--rw=randwrite", "--bs=4k",
			fmt.Sprintf("--size=%s", writeSize),
			"--ioengine=libaio", "--direct=1", "--iodepth=16",
			"--time_based=1", fmt.Sprintf("--runtime=%d", cfg.Tools.FIO.Runtime/2),
			"--verify=crc32c", "--verify_backlog=128",
			"--verify_fatal=1", "--group_reporting=1",
		},
	}

	if pod.StorageType == "cephfs" {
		avail := queryDFAvail(ctx, client, cfg.Cluster.Namespace, pod.Name, "/mnt/data")
		sized, err := computeExpandVerifyWriteSize(cfg.Cluster.PVCSize, expandedSize, avail, pod.StorageType)
		if err != nil {
			log.Printf("[%s] SKIP: EXPAND: no headroom for expand-verify: %v", pod.Name, err)
			collector.Add(report.JobResult{Pod: pod.Name, Job: "expand-verify", Category: "lifecycle", Status: "skip", Error: err.Error(), Storage: pod.StorageType, VolumeMode: pod.VolumeModeStr()})
			return
		}
		writeSize = sized
		expandJob.Filename = "/mnt/data/expand-verify.dat"
		expandJob.Args = []string{
			"--rw=write", "--bs=256k",
			fmt.Sprintf("--size=%s", writeSize),
			"--ioengine=libaio", "--direct=1", "--iodepth=16",
			"--verify=crc32c", "--do_verify=0",
			"--group_reporting=1",
		}
		log.Printf("[%s] EXPAND: CephFS verify write size %s (avail=%d)", pod.Name, writeSize, avail)
	}

	executeFIOJob(ctx, client, pod, expandJob, cfg, collector)
}

func queryDFAvail(ctx context.Context, client *k8s.Client, namespace, podName, mount string) int64 {
	stdout, _, _, err := k8s.ExecInPod(ctx, client, namespace, podName, "fio", []string{"df", "-B1", "-P", mount})
	if err != nil {
		return 0
	}
	n, err := parseDFAvail(string(stdout))
	if err != nil {
		return 0
	}
	return n
}

func parseDFAvail(stdout string) (int64, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("df: unexpected output")
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("df: bad line %q", lines[len(lines)-1])
	}
	return strconv.ParseInt(fields[3], 10, 64)
}

func computeExpandVerifyWriteSize(original, expanded string, availBytes int64, storageType string) (string, error) {
	if storageType != "cephfs" {
		return expanded, nil
	}
	orig, err := resource.ParseQuantity(original)
	if err != nil {
		return "", fmt.Errorf("parse original size %q: %w", original, err)
	}
	exp, err := resource.ParseQuantity(expanded)
	if err != nil {
		return "", fmt.Errorf("parse expanded size %q: %w", expanded, err)
	}
	headroom := exp.Value() - orig.Value() - expandVerifyMargin
	if headroom < 0 {
		headroom = 0
	}
	if availBytes > 0 {
		capped := availBytes * 80 / 100
		if capped < headroom {
			headroom = capped
		}
	}
	if headroom <= 0 {
		return "", fmt.Errorf("no headroom for expand-verify write")
	}
	return resource.NewQuantity(headroom, resource.BinarySI).String(), nil
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
			Name:       pod.Name,
			Namespace:  cfg.Cluster.Namespace,
			Image:      cfg.Tools.FIO.Image,
			PVCName:    pod.PVCName,
			VolumeMode: pod.VolumeMode,
			Labels:     map[string]string{"app": cfg.Cluster.Prefix, "role": "reschedule"},
			Privileged: pod.VolumeMode == corev1.PersistentVolumeBlock,
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
	if factor < 1 {
		return "", fmt.Errorf("expand factor must be >= 1, got %d", factor)
	}
	q, err := resource.ParseQuantity(original)
	if err != nil {
		return "", fmt.Errorf("parse PVC size %q: %w", original, err)
	}
	bytes := q.Value()
	if factor > 1 && bytes > (1<<63-1)/int64(factor) {
		return "", fmt.Errorf("overflow expanding %q by %d", original, factor)
	}
	scaled := resource.NewQuantity(bytes*int64(factor), q.Format)
	return scaled.String(), nil
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
