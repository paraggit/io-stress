package fio

import (
	"strings"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestPatternsToJobs_InjectsSizeRuntimeAndParams(t *testing.T) {
	fioCfg := config.NewDefault().Tools.FIO
	jobs := PatternsToJobs([]config.Pattern{{
		Name:     "unaligned-direct",
		Category: "unaligned",
		Params: map[string]string{
			"rw": "randwrite", "ioengine": "libaio", "direct": "1",
			"iodepth": "16", "time_based": "1", "group_reporting": "1",
		},
	}}, fioCfg)
	if len(jobs) != 1 {
		t.Fatalf("len=%d", len(jobs))
	}
	args := strings.Join(jobs[0].Args, " ")
	for _, want := range []string{"--rw=randwrite", "--size=1G", "--runtime=60", "--bs=512", "--offset=512"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q; got %v", want, jobs[0].Args)
		}
	}
}

func TestPatternsToJobs_PatternSizeOverride(t *testing.T) {
	fioCfg := config.NewDefault().Tools.FIO
	jobs := PatternsToJobs([]config.Pattern{{
		Name: "overwrite-frag-stress", Category: "stress", Size: "64m",
		Params: map[string]string{"rw": "randwrite", "bs": "4k", "ioengine": "libaio", "direct": "1", "iodepth": "64", "time_based": "1", "group_reporting": "1"},
	}}, fioCfg)
	if !strings.Contains(strings.Join(jobs[0].Args, " "), "--size=64m") {
		t.Errorf("got %v", jobs[0].Args)
	}
}
