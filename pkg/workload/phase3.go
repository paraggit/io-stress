package workload

import (
	"context"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

func runPhase3(ctx context.Context, cfg *config.Config, client *k8s.Client, pods []PodInfo, collector *report.Collector) error {
	return nil
}
