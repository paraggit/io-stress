package fio

import (
	"fmt"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

type Job struct {
	Name     string
	Category string
	Args     []string
	// Filename overrides the per-pod target when set (e.g. expand-verify on a
	// distinct file so it does not collide with the integrity seed).
	Filename string
}

// Integrity seed/verify share these so phase3 never reads a region that was not
// deterministically written. Do not add time_based/runtime to either job.
const (
	IntegritySeedBS       = "256k"
	IntegritySeedIODepth  = "32"
	IntegritySeedRandSeed = "1"
)

func IntegritySize(cfg *config.Config) string {
	if cfg != nil && cfg.Tools.FIO.Size != "" {
		return cfg.Tools.FIO.Size
	}
	return "1G"
}

func IntegritySeedJob(cfg *config.Config) Job {
	size := IntegritySize(cfg)
	return Job{
		Name:     "integrity-seed",
		Category: "lifecycle",
		Args: []string{
			"--rw=write",
			fmt.Sprintf("--bs=%s", IntegritySeedBS),
			fmt.Sprintf("--size=%s", size),
			"--ioengine=libaio",
			"--direct=1",
			fmt.Sprintf("--iodepth=%s", IntegritySeedIODepth),
			"--verify=crc32c",
			"--do_verify=0",
			"--serialize_overlap=1",
			fmt.Sprintf("--randseed=%s", IntegritySeedRandSeed),
			"--group_reporting=1",
		},
	}
}

func IntegrityVerifyJob(cfg *config.Config) Job {
	size := IntegritySize(cfg)
	return Job{
		Name:     "phase3-verify",
		Category: "lifecycle",
		Args: []string{
			"--rw=read",
			fmt.Sprintf("--bs=%s", IntegritySeedBS),
			fmt.Sprintf("--size=%s", size),
			"--ioengine=libaio",
			"--direct=1",
			fmt.Sprintf("--iodepth=%s", IntegritySeedIODepth),
			"--verify=crc32c",
			"--verify_only=1",
			"--serialize_overlap=1",
			fmt.Sprintf("--randseed=%s", IntegritySeedRandSeed),
			"--group_reporting=1",
		},
	}
}

func BuildArgs(j Job, target string, outputFormat string) []string {
	filename := target
	if j.Filename != "" {
		filename = j.Filename
	}
	args := []string{
		fmt.Sprintf("--name=%s", j.Name),
		fmt.Sprintf("--filename=%s", filename),
		fmt.Sprintf("--output-format=%s", outputFormat),
	}
	args = append(args, j.Args...)
	return args
}

func JobsForVolume(storageType string, volumeMode string, cfg *config.Config) []Job {
	s := cfg.Tools.FIO.Suites
	fioCfg := cfg.Tools.FIO
	jobs := PatternsToJobs(s.Common, fioCfg)
	if volumeMode == "Filesystem" {
		jobs = append(jobs, PatternsToJobs(s.Filesystem, fioCfg)...)
	} else {
		jobs = append(jobs, PatternsToJobs(s.Block, fioCfg)...)
	}
	return jobs
}

func ReducedSuite(target string, cfg *config.Config) []Job {
	_ = target
	return PatternsToJobs(cfg.Tools.FIO.Suites.Lifecycle, cfg.Tools.FIO)
}

func CephFSRWXJobs(cfg *config.Config) []Job {
	return PatternsToJobs(cfg.Tools.FIO.Suites.CephFSRWX, cfg.Tools.FIO)
}

func AppTypeJobs(appType string, cfg *config.Config) []Job {
	patterns := config.AppSuitePatterns(cfg, appType)
	return PatternsToJobs(patterns, cfg.Tools.FIO)
}
