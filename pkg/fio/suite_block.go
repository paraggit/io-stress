package fio

import (
	"fmt"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func blockJobs(cfg *config.Config) []Job {
	rt := fmt.Sprintf("%d", cfg.FIORuntime)
	return []Job{
		{
			Name:     "trim-write-interleave",
			Category: "block",
			Args: []string{
				"--rw=randtrimwrite", "--bs=64k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=32",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "trim-stress",
			Category: "block",
			Args: []string{
				"--rw=randtrim", "--bs=64k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=32",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "write-zeroes",
			Category: "block",
			Args: []string{
				"--rw=randwrite", "--bs=64k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=16",
				"--zero_buffers=1",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "sub-4k-rmw",
			Category: "block",
			Args: []string{
				"--rw=randwrite", "--bs=512",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=32",
				"--time_based=1", "--runtime=" + rt,
				"--verify=crc32c", "--verify_backlog=256",
				"--verify_fatal=1",
				"--random_generator=lfsr", "--group_reporting=1",
			},
		},
	}
}
