package workload

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

func TestComputeExpandedSize(t *testing.T) {
	got, err := computeExpandedSize("10Gi", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := resource.MustParse("20Gi")
	gotQ := resource.MustParse(got)
	if gotQ.Cmp(want) != 0 {
		t.Fatalf("got %q (%s), want 20Gi", got, gotQ.String())
	}
}

func TestComputeExpandedSize_PiEi(t *testing.T) {
	tests := []struct {
		in     string
		factor int
		want   string
	}{
		{"1Pi", 2, "2Pi"},
		{"2Ei", 2, "4Ei"},
		{"100Mi", 3, "300Mi"},
		{"1k", 2, "2k"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := computeExpandedSize(tt.in, tt.factor)
			if err != nil {
				t.Fatal(err)
			}
			wantQ := resource.MustParse(tt.want)
			gotQ := resource.MustParse(got)
			if gotQ.Cmp(wantQ) != 0 {
				t.Fatalf("got %q (%s), want %s", got, gotQ.String(), tt.want)
			}
		})
	}
}

func TestComputeExpandedSize_Invalid(t *testing.T) {
	_, err := computeExpandedSize("not-a-size", 2)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parse PVC size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestComputeExpandVerifyWriteSize_RBDKeepsFullExpanded(t *testing.T) {
	got, err := computeExpandVerifyWriteSize("10Gi", "20Gi", 0, "rbd")
	if err != nil {
		t.Fatal(err)
	}
	want := resource.MustParse("20Gi")
	gotQ := resource.MustParse(got)
	if gotQ.Cmp(want) != 0 {
		t.Fatalf("RBD write size = %q, want 20Gi", got)
	}
}

func TestComputeExpandVerifyWriteSize_CephFSHeadroom(t *testing.T) {
	got, err := computeExpandVerifyWriteSize("10Gi", "20Gi", 0, "cephfs")
	if err != nil {
		t.Fatal(err)
	}
	orig := resource.MustParse("10Gi")
	exp := resource.MustParse("20Gi")
	wantBytes := exp.Value() - orig.Value() - expandVerifyMargin
	gotQ := resource.MustParse(got)
	if gotQ.Value() != wantBytes {
		t.Fatalf("CephFS write size = %s (%d), want headroom %d", got, gotQ.Value(), wantBytes)
	}
}

func TestComputeExpandVerifyWriteSize_CephFSCapsAtFreeSpace(t *testing.T) {
	avail := int64(1 << 30) // 1Gi free
	got, err := computeExpandVerifyWriteSize("10Gi", "20Gi", avail, "cephfs")
	if err != nil {
		t.Fatal(err)
	}
	want := avail * 80 / 100
	gotQ := resource.MustParse(got)
	if gotQ.Value() != want {
		t.Fatalf("got %s (%d), want 80%% of 1Gi (%d)", got, gotQ.Value(), want)
	}
}

func TestParseDFAvail(t *testing.T) {
	out := `Filesystem     1B-blocks        Used    Available Capacity Mounted on
/dev/rbd0      21474836480  2147483648  19327352832      10% /mnt/data
`
	got, err := parseDFAvail(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != 19327352832 {
		t.Fatalf("avail = %d, want 19327352832", got)
	}
}

func TestRecordBoundWait_ProvisionTimeoutIsSlow(t *testing.T) {
	c := report.NewCollector()
	err := &k8s.ProvisionTimeoutError{Name: "clone-pvc", Duration: 5 * time.Minute, State: "phase=Pending"}
	pod := PodInfo{Name: "src-pod", StorageType: "cephfs", VolumeMode: corev1.PersistentVolumeFilesystem}
	recordBoundWait("clone-pod", pod, "clone-bound", err, c)
	results := c.Results()
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].Status != "slow" {
		t.Errorf("status = %q, want slow", results[0].Status)
	}
	if results[0].Job != "clone-bound" || results[0].Pod != "clone-pod" {
		t.Errorf("job/pod = %s/%s", results[0].Job, results[0].Pod)
	}
	if !strings.Contains(results[0].Error, "phase=Pending") {
		t.Errorf("error should capture CSI state, got %q", results[0].Error)
	}
}

func TestMaxQuantityString(t *testing.T) {
	if got := maxQuantityString("10Gi", "20Gi"); got != "20Gi" {
		t.Fatalf("got %q, want 20Gi", got)
	}
	if got := maxQuantityString("20Gi", "10Gi"); got != "20Gi" {
		t.Fatalf("got %q, want 20Gi", got)
	}
	// binary form from API vs Gi suffix
	if got := maxQuantityString("10Gi", "21474836480"); got != "21474836480" {
		t.Fatalf("got %q, want 21474836480", got)
	}
}
