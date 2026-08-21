package workload

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
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
