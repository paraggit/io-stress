package fio

import (
	"fmt"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func commonJobs(cfg *config.Config) []Job {
	rt := fmt.Sprintf("%d", cfg.FIORuntime)
	return []Job{
		{
			Name:     "unaligned-direct",
			Category: "unaligned",
			Args: []string{
				"--rw=randwrite",
				"--bs=" + cfg.FIOBlockSize, "--offset=" + cfg.FIOOffset,
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=16",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "unaligned-buffered",
			Category: "unaligned",
			Args: []string{
				"--rw=randwrite",
				"--bs=" + cfg.FIOBlockSize, "--offset=" + cfg.FIOOffset,
				"--size=" + cfg.FIOSize,
				"--ioengine=psync", "--direct=0",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "unaligned-randread",
			Category: "unaligned",
			Args: []string{
				"--rw=randread",
				"--bs=" + cfg.FIOBlockSize, "--offset=" + cfg.FIOOffset,
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=16",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "obj-boundary-3m",
			Category: "boundary",
			Args: []string{
				"--rw=randwrite", "--bs=3m",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=16",
				"--time_based=1", "--runtime=" + rt,
				"--verify=crc32c", "--verify_backlog=64",
				"--verify_fatal=1", "--verify_dump=1",
				"--random_generator=lfsr", "--group_reporting=1",
			},
		},
		{
			Name:     "obj-boundary-5m",
			Category: "boundary",
			Args: []string{
				"--rw=randwrite", "--bs=5m",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=8",
				"--time_based=1", "--runtime=" + rt,
				"--verify=crc32c", "--verify_backlog=32",
				"--verify_fatal=1",
				"--random_generator=lfsr", "--group_reporting=1",
			},
		},
		{
			Name:     "mixed-bs-verify",
			Category: "integrity",
			Args: []string{
				"--rw=randrw", "--rwmixread=50",
				"--bssplit=4k/30:64k/30:512k/20:3m/20",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=16",
				"--time_based=1", "--runtime=" + rt,
				"--verify=meta", "--verify_backlog=128",
				"--verify_fatal=1", "--verify_dump=1",
				"--serialize_overlap=1", "--group_reporting=1",
			},
		},
		{
			Name:     "data-integrity-4k",
			Category: "integrity",
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
			Name:     "seq-write-verify",
			Category: "integrity",
			Args: []string{
				"--rw=write", "--bs=128k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=32",
				"--time_based=1", "--runtime=" + rt,
				"--verify=crc32c", "--verify_backlog=128",
				"--verify_fatal=1", "--verify_dump=1",
				"--group_reporting=1",
			},
		},
		{
			Name:     "high-iodepth-stress",
			Category: "stress",
			Args: []string{
				"--rw=randwrite", "--bs=4k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=128",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "overwrite-frag-stress",
			Category: "stress",
			Args: []string{
				"--rw=randwrite", "--bs=4k", "--size=64m",
				"--ioengine=libaio", "--direct=1", "--iodepth=64",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "high-concurrency-randrw",
			Category: "stress",
			Args: []string{
				"--rw=randrw", "--rwmixread=70", "--bs=4k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=128",
				"--time_based=1", "--runtime=" + rt,
				"--group_reporting=1",
			},
		},
		{
			Name:     "compress-pattern-stress",
			Category: "compression",
			Args: []string{
				"--rw=randwrite", "--bs=128k",
				"--size=" + cfg.FIOSize,
				"--ioengine=libaio", "--direct=1", "--iodepth=16",
				"--time_based=1", "--runtime=" + rt,
				"--buffer_compress_percentage=50",
				"--verify=crc32c", "--verify_backlog=64",
				"--verify_fatal=1", "--group_reporting=1",
			},
		},
	}
}
