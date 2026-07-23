package vdbench

import (
	"strings"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestBuildBlockParam(t *testing.T) {
	got := BuildBlockParam(
		config.VDBenchBlock{Size: "8g"},
		config.VDBenchPattern{Name: "random_write", Rdpct: 0, Seekpct: 100, Xfersize: "4k", Skew: 0},
		"/dev/rbdblock", 60,
	)
	want := "sd=sd1,lun=/dev/rbdblock,openflags=o_direct,size=8g\n" +
		"wd=wd1,sd=sd1,rdpct=0,seekpct=100,xfersize=4k,skew=0\n" +
		"rd=rd1,wd=wd1,iorate=max,elapsed=60,interval=1\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildFilesystemParam_SequentialWrite(t *testing.T) {
	got := BuildFilesystemParam(
		config.VDBenchFilesystem{
			Depth: 2, Width: 2, Files: 4, FileSize: "1m",
			OpenFlags: "o_direct",
		},
		config.VDBenchPattern{Name: "sequential_write", Rdpct: 0, Seekpct: 0, Xfersize: "1m", Skew: 0},
		60,
	)
	if !strings.Contains(got, "fileio=sequential,fileselect=sequential,operation=write") {
		t.Fatalf("expected sequential operation=write:\n%s", got)
	}
	if strings.Contains(got, "rdpct=") {
		t.Fatalf("sequential FWD must not emit rdpct:\n%s", got)
	}
	if strings.Contains(got, "seekpct=") {
		t.Fatalf("FWD must not emit seekpct:\n%s", got)
	}
}

func TestBuildFilesystemParam_RandomSeek(t *testing.T) {
	got := BuildFilesystemParam(
		config.VDBenchFilesystem{Depth: 1, Width: 1, Files: 1, FileSize: "1m"},
		config.VDBenchPattern{Name: "random_mixed", Rdpct: 70, Seekpct: 100, Xfersize: "256k", Skew: 0},
		30,
	)
	if !strings.Contains(got, "rdpct=70") || !strings.Contains(got, "fileio=random,fileselect=random") {
		t.Fatalf("expected random+rdpct:\n%s", got)
	}
	if strings.Contains(got, "operation=") {
		t.Fatalf("random FWD should use rdpct, not operation:\n%s", got)
	}
}
