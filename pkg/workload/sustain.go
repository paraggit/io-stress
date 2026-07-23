package workload

import (
	"context"
	"fmt"
	"log"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
)

func startSustainWorkload(ctx context.Context, client *k8s.Client, cfg *config.Config, pod PodInfo) {
	log.Printf("[%s] Starting sustain workload", pod.Name)
	cmd := []string{
		"fio",
		"--name=sustain",
		fmt.Sprintf("--filename=%s", pod.Target),
		"--rw=randrw", "--rwmixread=70", "--bs=4k",
		fmt.Sprintf("--size=%s", cfg.Tools.FIO.Size),
		"--ioengine=libaio", "--direct=1", "--iodepth=8",
		"--time_based=1",
		fmt.Sprintf("--runtime=%d", cfg.Cluster.SustainRuntime),
		"--group_reporting=1",
	}
	containerName := pod.ContainerName
	if containerName == "" {
		containerName = "iotool"
	}
	_, _, _, err := k8s.ExecInPod(ctx, client, cfg.Cluster.Namespace, pod.Name, containerName, cmd)
	if err != nil && ctx.Err() == nil {
		log.Printf("[%s] Sustain workload error: %v", pod.Name, err)
	}
	log.Printf("[%s] Sustain workload stopped", pod.Name)
}
