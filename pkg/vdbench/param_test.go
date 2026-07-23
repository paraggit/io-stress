package vdbench

import (
	"strings"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestBuildBlockParam(t *testing.T) {
	got := BuildBlockParam(
		config.VDBenchBlock{Size: "15g"},
		config.VDBenchPattern{Name: "random_write", Rdpct: 0, Seekpct: 100, Xfersize: "4k", Skew: 0},
		"/dev/rbdblock", 60,
	)
	want := "sd=sd1,lun=/dev/rbdblock,openflags=o_direct,size=15g\n" +
		"wd=wd1,sd=sd1,rdpct=0,seekpct=100,xfersize=4k,skew=0\n" +
		"rd=rd1,wd=wd1,iorate=max,elapsed=60,interval=1\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildFilesystemParam(t *testing.T) {
	got := BuildFilesystemParam(
		config.VDBenchFilesystem{
			Depth: 4, Width: 5, Files: 10, FileSize: "1m",
			OpenFlags: "o_direct", GroupAllFWDsInOneRD: true,
		},
		config.VDBenchPattern{Name: "sequential_write", Rdpct: 0, Seekpct: 0, Xfersize: "1m", Skew: 0},
		60,
	)
	wantFSD := "fsd=fsd1,anchor=/mnt/data,depth=4,width=5,files=10,size=1m,openflags=o_direct\n"
	wantFWD := "fwd=fwd1,fsd=fsd1,rdpct=0,xfersize=1m,skew=0,fileio=sequential,fileselect=sequential\n"
	wantRD := "rd=rd1,fwd=fwd1,fwdrate=max,format=yes,elapsed=60,interval=1\n"
	if !strings.Contains(got, wantFSD) {
		t.Fatalf("fsd line missing:\n%s", got)
	}
	if !strings.Contains(got, wantFWD) {
		t.Fatalf("fwd line missing:\n%s", got)
	}
	if !strings.Contains(got, wantRD) {
		t.Fatalf("rd line missing:\n%s", got)
	}
	if strings.Contains(got, "seekpct=") {
		t.Fatalf("FWD must not emit seekpct: %s", got)
	}
	if strings.Contains(got, "group_all_fwds_in_one_rd") {
		t.Fatalf("must not emit unsupported group_all_fwds_in_one_rd: %s", got)
	}
}

func TestBuildFilesystemParam_RandomSeek(t *testing.T) {
	got := BuildFilesystemParam(
		config.VDBenchFilesystem{Depth: 1, Width: 1, Files: 1, FileSize: "1m"},
		config.VDBenchPattern{Name: "random_mixed", Rdpct: 70, Seekpct: 100, Xfersize: "256k", Skew: 0},
		30,
	)
	if !strings.Contains(got, "fileio=random,fileselect=random") {
		t.Fatalf("expected random fileio/fileselect:\n%s", got)
	}
}
