package k8s

import (
	"fmt"
	"time"
)

func Retry(fn func() error) error {
	// Delays are slept after a failed attempt before the next try.
	// With N delays there are N+1 attempts so the final delay is used.
	delays := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	attempts := len(delays) + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := fn(); err != nil {
			lastErr = err
			if i < len(delays) {
				time.Sleep(delays[i])
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("failed after %d attempts: %w", attempts, lastErr)
}
