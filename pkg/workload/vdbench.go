package workload

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
	"github.com/red-hat-storage/odf-io-stress/pkg/vdbench"
)

func activeImage(cfg *config.Config) string {
	if cfg.Tools.Active == "vdbench" {
		return cfg.Tools.VDBench.Image
	}
	return cfg.Tools.FIO.Image
}

func skipLifecycleForTool(cfg *config.Config) bool {
	return cfg.Tools.Active == "vdbench" || cfg.Cluster.SkipLifecycle
}

func vdbenchPatternsForPod(cfg *config.Config, pod PodInfo) []config.VDBenchPattern {
	if pod.VolumeMode == corev1.PersistentVolumeBlock {
		// RBD Block pods → vdbench.block.patterns
		return cfg.Tools.VDBench.Block.Patterns
	}
	// RBD FS + CephFS → vdbench.filesystem.patterns
	return cfg.Tools.VDBench.Filesystem.Patterns
}

func runVdbenchOnPod(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) error {
	patterns := vdbenchPatternsForPod(cfg, pod)
	for _, p := range patterns {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		result := executeVDBenchPattern(ctx, client, pod, p, cfg, collector)
		if err := report.WriteJobFile(cfg.Cluster.ResultsDir, result); err != nil {
			log.Printf("warning: failed to write job file: %v", err)
		}
	}
	return nil
}

func executeVDBenchPattern(ctx context.Context, client *k8s.Client, pod PodInfo, pattern config.VDBenchPattern, cfg *config.Config, collector *report.Collector) report.JobResult {
	log.Printf("[%s] Running VDBench pattern %s", pod.Name, pattern.Name)
	start := time.Now()

	// Build VDBench parameter file content
	var param string
	if pod.VolumeMode == corev1.PersistentVolumeBlock {
		param = vdbench.BuildBlockParam(cfg.Tools.VDBench.Block, pattern, pod.Target, cfg.Tools.VDBench.Runtime)
	} else {
		param = vdbench.BuildFilesystemParam(cfg.Tools.VDBench.Filesystem, pattern, cfg.Tools.VDBench.Runtime)
	}

	// Write parameter file to pod
	paramPath := fmt.Sprintf("/tmp/vdbench-%s.vdbench", pattern.Name)
	containerName := pod.ContainerName
	if containerName == "" {
		containerName = "iotool"
	}

	if err := k8s.WriteFileInPod(ctx, client, cfg.Cluster.Namespace, pod.Name, containerName, paramPath, []byte(param)); err != nil {
		result := report.JobResult{
			Pod:        pod.Name,
			Job:        pattern.Name,
			Category:   "vdbench",
			Storage:    pod.StorageType,
			VolumeMode: pod.VolumeModeStr(),
			Status:     "fail",
			ExitCode:   -1,
			Duration:   time.Since(start),
			Error:      fmt.Sprintf("failed to write parameter file: %v", err),
		}
		log.Printf("[%s] FAIL %s (param write error, %v)", pod.Name, pattern.Name, result.Duration)
		collector.Add(result)
		return result
	}

	// Execute VDBench command
	outputDir := filepath.Join(cfg.Tools.VDBench.OutputDir, pod.Name, pattern.Name)
	cmd := []string{"vdbench", "-f", paramPath, "-o", outputDir}

	stdout, stderr, exitCode, err := k8s.ExecInPod(ctx, client, cfg.Cluster.Namespace, pod.Name, containerName, cmd)
	duration := time.Since(start)

	result := report.JobResult{
		Pod:        pod.Name,
		Job:        pattern.Name,
		Category:   "vdbench",
		Storage:    pod.StorageType,
		VolumeMode: pod.VolumeModeStr(),
		ExitCode:   exitCode,
		Duration:   duration,
	}

	if err != nil {
		result.Status = "fail"
		result.Error = fmt.Sprintf("%v; stderr: %s", err, string(stderr))
		log.Printf("[%s] FAIL %s (rc=%d, %v)", pod.Name, pattern.Name, exitCode, duration)
	} else if exitCode != 0 {
		result.Status = "fail"
		result.Error = string(stderr)
		log.Printf("[%s] FAIL %s (rc=%d, %v)", pod.Name, pattern.Name, exitCode, duration)
	} else {
		result.Status = "pass"
		// VDBench doesn't output JSON like FIO, so we marshal raw stdout as JSON string
		// This ensures FIOOutput (json.RawMessage) can be properly marshaled for reports
		result.FIOOutput, _ = json.Marshal(string(stdout))
		log.Printf("[%s] PASS %s (%v)", pod.Name, pattern.Name, duration)
	}

	collector.Add(result)
	return result
}
