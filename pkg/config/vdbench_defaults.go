package config

func defaultVDBench() VDBench {
	return VDBench{
		Image:     "quay.io/pakamble/vdbench:latest",
		Runtime:   60,
		OutputDir: "/tmp/vdbench-out",
		Block: VDBenchBlock{
			// Must fit under default cluster.pvc_size (10Gi).
			Size: "8g",
			Patterns: []VDBenchPattern{
				{Name: "random_write", Rdpct: 0, Seekpct: 100, Xfersize: "4k", Skew: 0},
				{Name: "random_read", Rdpct: 100, Seekpct: 100, Xfersize: "8k", Skew: 0},
				{Name: "mixed_workload", Rdpct: 50, Seekpct: 100, Xfersize: "64k", Skew: 0},
			},
		},
		Filesystem: VDBenchFilesystem{
			Size:                "10m",
			Depth:               4,
			Width:               5,
			Files:               10,
			FileSize:            "1m",
			OpenFlags:           "o_direct",
			GroupAllFWDsInOneRD: true,
			Patterns: []VDBenchPattern{
				{Name: "sequential_write", Rdpct: 0, Seekpct: 0, Xfersize: "1m", Skew: 0},
				{Name: "random_mixed", Rdpct: 70, Seekpct: 100, Xfersize: "256k", Skew: 0},
				{Name: "small_file_ops", Rdpct: 50, Seekpct: 100, Xfersize: "4k", Skew: 0},
			},
		},
	}
}
