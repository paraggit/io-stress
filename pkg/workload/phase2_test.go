package workload

import "testing"

func TestComputeExpandedSize(t *testing.T) {
	got, err := computeExpandedSize("10Gi", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "20Gi" {
		t.Fatalf("got %q, want 20Gi", got)
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
