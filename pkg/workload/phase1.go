package workload

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/fio"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
	"golang.org/x/sync/errgroup"
)

func runPhase1(ctx context.Context, cfg *config.Config, client *k8s.Client, pods []PodInfo, collector *report.Collector) error {
	log.Println("=== PHASE 1: FIO STRESS ===")

	g, ctx := errgroup.WithContext(ctx)
	if cfg.MaxParallelPods > 0 {
		g.SetLimit(cfg.MaxParallelPods)
	}

	for _, pod := range pods {
		pod := pod
		g.Go(func() error {
			return runFIOOnPod(ctx, cfg, client, pod, collector)
		})
	}

	if err := g.Wait(); err != nil {
		log.Printf("Phase 1 completed with errors: %v", err)
	}

	if hasCephFS(pods) {
		if err := runCephFSRWXTests(ctx, cfg, client, pods, collector); err != nil {
			log.Printf("CephFS RWX tests completed with errors: %v", err)
		}
	}

	log.Println("=== PHASE 1 COMPLETE ===")
	return nil
}

func hasCephFS(pods []PodInfo) bool {
	for _, p := range pods {
		if p.StorageType == "cephfs" {
			return true
		}
	}
	return false
}

func runFIOOnPod(ctx context.Context, cfg *config.Config, client *k8s.Client, pod PodInfo, collector *report.Collector) error {
	jobs := fio.JobsForVolume(pod.StorageType, pod.VolumeModeStr(), cfg)
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		result := executeFIOJob(ctx, client, pod, job, cfg, collector)
		if err := report.WriteJobFile(cfg.ResultsDir, result); err != nil {
			log.Printf("warning: failed to write job file: %v", err)
		}
	}
	return nil
}

func executeFIOJob(ctx context.Context, client *k8s.Client, pod PodInfo, job fio.Job, cfg *config.Config, collector *report.Collector) report.JobResult {
	log.Printf("[%s] Running %s", pod.Name, job.Name)
	start := time.Now()

	args := fio.BuildArgs(job, pod.Target, cfg.OutputFormat)
	cmd := append([]string{"fio"}, args...)

	stdout, stderr, exitCode, err := k8s.ExecInPod(ctx, client, cfg.Namespace, pod.Name, "fio", cmd)
	duration := time.Since(start)

	result := report.JobResult{
		Pod:        pod.Name,
		Job:        job.Name,
		Category:   job.Category,
		Storage:    pod.StorageType,
		VolumeMode: pod.VolumeModeStr(),
		ExitCode:   exitCode,
		Duration:   duration,
	}

	if err != nil {
		result.Status = "fail"
		result.Error = fmt.Sprintf("%v; stderr: %s", err, string(stderr))
		log.Printf("[%s] FAIL %s (rc=%d, %v)", pod.Name, job.Name, exitCode, duration)
	} else if exitCode != 0 {
		result.Status = "fail"
		result.Error = string(stderr)
		log.Printf("[%s] FAIL %s (rc=%d, %v)", pod.Name, job.Name, exitCode, duration)
	} else {
		result.Status = "pass"
		result.FIOOutput = json.RawMessage(stdout)
		log.Printf("[%s] PASS %s (%v)", pod.Name, job.Name, duration)
	}

	collector.Add(result)
	return result
}

func runCephFSRWXTests(ctx context.Context, cfg *config.Config, client *k8s.Client, pods []PodInfo, collector *report.Collector) error {
	log.Println("Running CephFS RWX multi-pod tests")

	var cephfsPods []PodInfo
	for _, p := range pods {
		if p.StorageType == "cephfs" {
			cephfsPods = append(cephfsPods, p)
		}
	}

	rwxJobs := fio.CephFSRWXJobs(cfg)
	g, ctx := errgroup.WithContext(ctx)

	for _, pod := range cephfsPods {
		pod := pod
		g.Go(func() error {
			secondPodName := pod.Name + "-rwx"
			secondPod := k8s.PodSpec{
				Name:       secondPodName,
				Namespace:  cfg.Namespace,
				Image:      cfg.FIOImage,
				PVCName:    pod.PVCName,
				VolumeMode: pod.VolumeMode,
				Labels:     map[string]string{"app": cfg.Prefix, "role": "rwx"},
			}
			if err := k8s.CreatePod(ctx, client, secondPod); err != nil {
				return fmt.Errorf("create RWX pod %s: %w", secondPodName, err)
			}
			defer func() {
				k8s.DeletePod(context.Background(), client, cfg.Namespace, secondPodName)
			}()

			if err := k8s.WaitPodReady(ctx, client, cfg.Namespace, secondPodName, cfg.WaitTimeout); err != nil {
				return fmt.Errorf("wait RWX pod %s: %w", secondPodName, err)
			}

			writeJob := rwxJobs[0]
			readJob := rwxJobs[1]

			innerG, innerCtx := errgroup.WithContext(ctx)
			innerG.Go(func() error {
				executeFIOJob(innerCtx, client, pod, writeJob, cfg, collector)
				return nil
			})

			rwxPodInfo := PodInfo{
				Name:        secondPodName,
				StorageType: "cephfs",
				VolumeMode:  pod.VolumeMode,
				Target:      pod.Target,
				PVCName:     pod.PVCName,
			}
			innerG.Go(func() error {
				executeFIOJob(innerCtx, client, rwxPodInfo, readJob, cfg, collector)
				return nil
			})

			return innerG.Wait()
		})
	}

	return g.Wait()
}
