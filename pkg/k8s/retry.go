package k8s

import (
	"fmt"
	"time"
)

func Retry(fn func() error) error {
	delays := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	var lastErr error
	for i, delay := range delays {
		if err := fn(); err != nil {
			lastErr = err
			if i < len(delays)-1 {
				time.Sleep(delay)
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("failed after %d attempts: %w", len(delays), lastErr)
}
