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
