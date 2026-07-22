package k8s

import (
	"errors"
	"testing"
)

func TestRetry_SuccessFirst(t *testing.T) {
	calls := 0
	err := Retry(func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("called %d times, want 1", calls)
	}
}

func TestRetry_SuccessThird(t *testing.T) {
	calls := 0
	err := Retry(func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("called %d times, want 3", calls)
	}
}

func TestRetry_AllFail(t *testing.T) {
	calls := 0
	err := Retry(func() error {
		calls++
		return errors.New("persistent")
	})
	if err == nil {
		t.Error("expected error, got nil")
	}
	if calls != 3 {
		t.Errorf("called %d times, want 3", calls)
	}
}
