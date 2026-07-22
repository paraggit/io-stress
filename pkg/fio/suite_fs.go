package fio

import (
	"fmt"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func filesystemJobs(cfg *config.Config) []Job {
	rt := fmt.Sprintf("%d", cfg.FIORuntime)
	return []Job{
		{
			Name:     "truncate-write",
			Category: "filesystem",
			Args: []string{
				"--rw=write", "--bs=4k",
				"--size=" + cfg.FIOSize,
				"--ioengine=psync", "--direct=0",
				"--fallocate=truncate",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "fsync-stress",
			Category: "filesystem",
			Args: []string{
				"--rw=randwrite", "--bs=4k",
				"--size=" + cfg.FIOSize,
				"--ioengine=psync", "--direct=0", "--fsync=1",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "fdatasync-mixed",
			Category: "filesystem",
			Args: []string{
				"--rw=randrw", "--rwmixread=70", "--bs=8k",
				"--size=" + cfg.FIOSize,
				"--ioengine=psync", "--direct=0", "--fdatasync=1",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "append-write",
			Category: "filesystem",
			Args: []string{
				"--rw=write", "--bs=64k",
				"--size=" + cfg.FIOSize,
				"--ioengine=psync", "--direct=0",
				"--fallocate=none",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
	}
}
