package workload

import (
	"context"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/fio"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
)

func startSustainWorkload(ctx context.Context, client *k8s.Client, cfg *config.Config, pod PodInfo) {
	log.Printf("[%s] Starting sustain workload", pod.Name)
	filename := pod.Target
	if pod.VolumeMode == corev1.PersistentVolumeFilesystem {
		// Keep sustain off the integrity-seeded file so clone/snapshot copy a stable region.
		filename = "/mnt/data/sustain.dat"
	}
	cmd := []string{
		"fio",
		"--name=sustain",
		fmt.Sprintf("--filename=%s", filename),
		"--rw=randrw", "--rwmixread=70", "--bs=4k",
		fmt.Sprintf("--size=%s", cfg.Tools.FIO.Size),
		"--ioengine=libaio", "--direct=1", "--iodepth=8",
		"--time_based=1",
		fmt.Sprintf("--runtime=%d", cfg.Cluster.SustainRuntime),
		"--group_reporting=1",
	}
	if pod.VolumeMode == corev1.PersistentVolumeBlock {
		// Write past the seeded extent so phase3-verify still sees crc32c headers.
		cmd = append(cmd, fmt.Sprintf("--offset=%s", fio.IntegritySize(cfg)))
	}
	_, _, _, err := k8s.ExecInPod(ctx, client, cfg.Cluster.Namespace, pod.Name, "fio", cmd)
	if err != nil && ctx.Err() == nil {
		log.Printf("[%s] Sustain workload error: %v", pod.Name, err)
	}
	log.Printf("[%s] Sustain workload stopped", pod.Name)
}
