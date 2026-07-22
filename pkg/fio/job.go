package fio

import (
	"fmt"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

type Job struct {
	Name     string
	Category string
	Args     []string
}

func BuildArgs(j Job, target string, outputFormat string) []string {
	args := []string{
		fmt.Sprintf("--name=%s", j.Name),
		fmt.Sprintf("--filename=%s", target),
		fmt.Sprintf("--output-format=%s", outputFormat),
	}
	args = append(args, j.Args...)
	return args
}

func JobsForVolume(storageType string, volumeMode string, cfg *config.Config) []Job {
	var jobs []Job
	jobs = append(jobs, commonJobs(cfg)...)

	if volumeMode == "Filesystem" {
		jobs = append(jobs, filesystemJobs(cfg)...)
	} else {
		jobs = append(jobs, blockJobs(cfg)...)
	}

	return jobs
}

func ReducedSuite(target string, cfg *config.Config) []Job {
	rt := fmt.Sprintf("%d", cfg.FIORuntime)
	return []Job{
		{
			Name:     "data-integrity-4k",
			Category: "lifecycle",
			Args: []string{
				"--rw=randrw", "--rwmixread=50", "--bs=4k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=32",
				"--time_based=1", "--runtime=" + rt,
				"--verify=crc32c", "--verify_backlog=256",
				"--verify_fatal=1", "--verify_dump=1",
				"--random_generator=lfsr", "--group_reporting=1",
			},
		},
		{
			Name:     "high-iodepth-stress",
			Category: "lifecycle",
			Args: []string{
				"--rw=randwrite", "--bs=4k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=128",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
	}
}

func CephFSRWXJobs(cfg *config.Config) []Job {
	rt := fmt.Sprintf("%d", cfg.FIORuntime)
	return []Job{
		{
			Name:     "rwx-concurrent-write",
			Category: "cephfs",
			Args: []string{
				"--rw=randwrite", "--bs=4k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=16",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "rwx-read-while-write",
			Category: "cephfs",
			Args: []string{
				"--rw=randread", "--bs=4k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=16",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
	}
}
