package fio

import (
	"fmt"
	"sort"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func PatternsToJobs(patterns []config.Pattern, fioCfg config.FIO) []Job {
	jobs := make([]Job, 0, len(patterns))
	for _, p := range patterns {
		params := cloneParams(p.Params)
		applyDefaults(params, p, fioCfg)
		args := paramsToArgs(params)
		jobs = append(jobs, Job{Name: p.Name, Category: p.Category, Args: args})
	}
	return jobs
}

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func applyDefaults(params map[string]string, p config.Pattern, fioCfg config.FIO) {
	if _, ok := params["size"]; !ok {
		size := fioCfg.Size
		if p.Size != "" {
			size = p.Size
		}
		if size != "" {
			params["size"] = size
		}
	}
	if _, ok := params["runtime"]; !ok {
		rt := fioCfg.Runtime
		if p.Runtime != nil {
			rt = *p.Runtime
		}
		if rt > 0 {
			params["runtime"] = fmt.Sprintf("%d", rt)
		}
	}
	if p.Category == "unaligned" || len(p.Name) >= 9 && p.Name[:9] == "unaligned" {
		if _, ok := params["bs"]; !ok && fioCfg.BlockSize != "" {
			params["bs"] = fioCfg.BlockSize
		}
		if _, ok := params["offset"]; !ok && fioCfg.Offset != "" {
			params["offset"] = fioCfg.Offset
		}
	}
}

func paramsToArgs(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys) // stable output for tests
	args := make([]string, 0, len(keys))
	for _, k := range keys {
		args = append(args, fmt.Sprintf("--%s=%s", k, params[k]))
	}
	return args
}