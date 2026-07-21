package report

import "fmt"

func PrintSummary(results []JobResult) {
	s := ComputeSummary(results)
	fmt.Println("=== RESULTS ===")
	fmt.Printf("  Phase 1   - Total: %d  Passed: %d  Failed: %d  Skipped: %d\n",
		s.Phase1.Total, s.Phase1.Passed, s.Phase1.Failed, s.Phase1.Skipped)
	if s.Lifecycle.Total > 0 {
		fmt.Printf("  Lifecycle - Total: %d  Passed: %d  Failed: %d  Skipped: %d\n",
			s.Lifecycle.Total, s.Lifecycle.Passed, s.Lifecycle.Failed, s.Lifecycle.Skipped)
	}
	fmt.Printf("  Combined  - Total: %d  Passed: %d  Failed: %d  Skipped: %d\n",
		s.Total, s.Passed, s.Failed, s.Skipped)
}
