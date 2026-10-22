package k8s

import (
	"context"
	"errors"
	"testing"
	"time"
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
	if calls != 4 {
		t.Errorf("called %d times, want 4 (3 delays → 4 attempts)", calls)
	}
}

func TestRetryContextStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := RetryContext(ctx, func() error {
		return errors.New("should not run")
	})
	if time.Since(start) > time.Second {
		t.Fatalf("cancelled retry took %s", time.Since(start))
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
