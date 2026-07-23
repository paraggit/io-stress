package workload

import (
	"context"
	"fmt"
	"log"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/fio"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

func runPhase3(ctx context.Context, cfg *config.Config, client *k8s.Client, readyPods []PodInfo, collector *report.Collector) error {
	log.Println("═══ PHASE 3: DATA INTEGRITY VERIFY ═══")

	var verifyPods []PodInfo
	for _, pod := range readyPods {
		if pod.Index%cfg.Cluster.LifecycleInterval == 0 {
			verifyPods = append(verifyPods, pod)
		}
	}

	if len(verifyPods) == 0 {
		log.Println("No pods to verify")
		return nil
	}

	g, ctx := errgroup.WithContext(ctx)

	for _, pod := range verifyPods {
		pod := pod
		g.Go(func() error {
			verifyCloneAndRestored(ctx, cfg, client, pod, collector)
			return nil
		})
	}

	g.Wait()
	log.Println("═══ PHASE 3 COMPLETE ═══")
	return nil
}

func verifyCloneAndRestored(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) {
	clonePodName := fmt.Sprintf("%s-%s-clone-pod-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)
	restoredPodName := fmt.Sprintf("%s-%s-restored-pod-%d", cfg.Cluster.Prefix, pod.StorageType, pod.Index)

	halfRuntime := cfg.Tools.FIO.Runtime / 2
	verifyJob := fio.Job{
		Name:     "phase3-verify",
		Category: "lifecycle",
		Args: []string{
			"--rw=randread", "--bs=4k",
			fmt.Sprintf("--size=%s", cfg.Tools.FIO.Size),
			"--ioengine=libaio", "--direct=1", "--iodepth=16",
			"--time_based=1", fmt.Sprintf("--runtime=%d", halfRuntime),
			"--verify=crc32c", "--verify_only=1",
			"--group_reporting=1",
		},
	}

	for _, targetPod := range []string{clonePodName, restoredPodName} {
		if err := k8s.WaitPodReady(ctx, client, cfg.Cluster.Namespace, targetPod, 10*time.Second); err != nil {
			log.Printf("SKIP: VERIFY: %s does not exist", targetPod)
			collector.Add(report.JobResult{
				Pod:        targetPod,
				Job:        "phase3-verify",
				Category:   "lifecycle",
				Status:     "skip",
				Storage:    pod.StorageType,
				VolumeMode: pod.VolumeModeStr(),
			})
			continue
		}

		podInfo := PodInfo{
			Index: pod.Index, Name: targetPod, StorageType: pod.StorageType,
			VolumeMode: pod.VolumeMode, Target: pod.Target,
			ContainerName: pod.ContainerName,
		}
		executeFIOJob(ctx, client, podInfo, verifyJob, cfg, collector)
	}
}
