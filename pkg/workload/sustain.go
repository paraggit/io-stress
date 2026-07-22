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
		fmt.Sprintf("--size=%s", cfg.FIOSize),
		"--ioengine=libaio", "--direct=1", "--iodepth=8",
		"--time_based=1",
		fmt.Sprintf("--runtime=%d", cfg.SustainRuntime),
		"--group_reporting=1",
	}
	_, _, _, err := k8s.ExecInPod(ctx, client, cfg.Namespace, pod.Name, "fio", cmd)
	if err != nil && ctx.Err() == nil {
		log.Printf("[%s] Sustain workload error: %v", pod.Name, err)
	}
	log.Printf("[%s] Sustain workload stopped", pod.Name)
}
