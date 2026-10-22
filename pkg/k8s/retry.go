package k8s

import (
	"context"
	"fmt"
	"time"
)

func Retry(fn func() error) error {
	return RetryContext(context.Background(), fn)
}

// RetryContext retries fn until it succeeds, attempts are exhausted, or ctx
// is cancelled. Cancellation skips the remaining backoff.
func RetryContext(ctx context.Context, fn func() error) error {
	// Delays are slept after a failed attempt before the next try.
	// With N delays there are N+1 attempts so the final delay is used.
	delays := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	attempts := len(delays) + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}
		if err := fn(); err != nil {
			lastErr = err
			if i >= len(delays) {
				break
			}
			timer := time.NewTimer(delays[i])
			select {
			case <-ctx.Done():
				timer.Stop()
				return lastErr
			case <-timer.C:
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("failed after %d attempts: %w", attempts, lastErr)
}
