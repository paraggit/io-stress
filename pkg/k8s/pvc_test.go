package k8s

import (
	"errors"
	"testing"
	"time"
)

func TestExtendProvisionDeadline_ExtendsOnProgress(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(5 * time.Minute)
	maxEnd := now.Add(60 * time.Minute)
	stall := 5 * time.Minute

	later := now.Add(4 * time.Minute)
	got := ExtendProvisionDeadline(later, deadline, maxEnd, stall)
	want := later.Add(stall)
	if !got.Equal(want) {
		t.Fatalf("extended deadline = %v, want %v", got, want)
	}
}

func TestExtendProvisionDeadline_CapsAtMaxEnd(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(50 * time.Minute)
	maxEnd := now.Add(60 * time.Minute)
	later := now.Add(58 * time.Minute)

	got := ExtendProvisionDeadline(later, deadline, maxEnd, 20*time.Minute)
	if !got.Equal(maxEnd) {
		t.Fatalf("got %v, want maxEnd %v", got, maxEnd)
	}
}

func TestIsProvisionTimeout(t *testing.T) {
	err := &ProvisionTimeoutError{Name: "clone-pvc", Duration: 5 * time.Minute, State: "phase=Pending"}
	if !IsProvisionTimeout(err) {
		t.Fatal("expected IsProvisionTimeout true")
	}
	if IsProvisionTimeout(errors.New("create PVC failed")) {
		t.Fatal("plain error must not be classified as provision timeout")
	}
	if got := err.Error(); got == "" {
		t.Fatal("Error() should include name and state")
	}
}
