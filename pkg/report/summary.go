package report

import (
	"fmt"
	"io"
	"os"
)

// PrintSummary writes the human-readable counts to stderr so stdout can stay
// a single JSON agent summary.
func PrintSummary(results []JobResult) {
	writeSummary(os.Stderr, results)
}

func writeSummary(w io.Writer, results []JobResult) {
	s := ComputeSummary(results)
	fmt.Fprintln(w, "=== RESULTS ===")
	fmt.Fprintf(w, "  Phase 1   - Total: %d  Passed: %d  Failed: %d  Skipped: %d\n",
		s.Phase1.Total, s.Phase1.Passed, s.Phase1.Failed, s.Phase1.Skipped)
	if s.Lifecycle.Total > 0 {
		fmt.Fprintf(w, "  Lifecycle - Total: %d  Passed: %d  Failed: %d  Skipped: %d",
			s.Lifecycle.Total, s.Lifecycle.Passed, s.Lifecycle.Failed, s.Lifecycle.Skipped)
		if s.Lifecycle.Slow > 0 {
			fmt.Fprintf(w, "  Slow: %d", s.Lifecycle.Slow)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "  Combined  - Total: %d  Passed: %d  Failed: %d  Skipped: %d",
		s.Total, s.Passed, s.Failed, s.Skipped)
	if s.Slow > 0 {
		fmt.Fprintf(w, "  Slow: %d", s.Slow)
	}
	fmt.Fprintln(w)
}
