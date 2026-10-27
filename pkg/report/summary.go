package report

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
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
	if s.Object.Total > 0 {
		writeObjectSummary(w, results, s)
	}
	fmt.Fprintf(w, "  Combined  - Total: %d  Passed: %d  Failed: %d  Skipped: %d",
		s.Total, s.Passed, s.Failed, s.Skipped)
	if s.Slow > 0 {
		fmt.Fprintf(w, "  Slow: %d", s.Slow)
	}
	fmt.Fprintln(w)
}

func writeObjectSummary(w io.Writer, results []JobResult, s Summary) {
	fmt.Fprintf(w, "  Object    - Total: %d  Passed: %d  Failed: %d  Skipped: %d",
		s.Object.Total, s.Object.Passed, s.Object.Failed, s.Object.Skipped)
	if s.Object.Infra > 0 {
		fmt.Fprintf(w, "  Infra: %d", s.Object.Infra)
	}
	if s.Object.Cleanup > 0 {
		fmt.Fprintf(w, "  Cleanup: %d", s.Object.Cleanup)
	}
	fmt.Fprintln(w)

	buckets := map[string]struct{}{}
	var written, verified, failed, bytes, mismatches uint64
	skipped := map[string]struct{}{}
	errs := map[string]uint64{}
	var binds []float64
	for _, r := range results {
		o := r.ObjectOutput
		if o == nil {
			continue
		}
		if o.Bucket != "" {
			buckets[o.Bucket] = struct{}{}
		}
		switch o.Phase {
		case "write":
			written += o.ObjectsOK
		case "verify":
			verified += o.ObjectsOK
		}
		failed += o.ObjectsFail
		bytes += o.BytesMoved
		mismatches += uint64(len(o.Mismatches)) + o.MismatchMore
		for cap, state := range o.Capabilities {
			if state == "unsupported" || strings.HasPrefix(state, "unsupported") {
				reason := cap
				if o.ProbeDetail != nil && o.ProbeDetail[cap] != "" {
					reason = cap + " (" + o.ProbeDetail[cap] + ")"
				}
				skipped[reason] = struct{}{}
			}
		}
		for code, n := range o.S3Errors {
			errs[code] += n
		}
		if st, exists := o.Ops["bind"]; exists && st.Count > 0 {
			binds = append(binds, st.MaxMs)
		}
	}
	fmt.Fprintf(w, "  Object IO - buckets: %d  written: %d  verified: %d  failed: %d  bytes: %d  mismatches: %d\n",
		len(buckets), written, verified, failed, bytes, mismatches)
	if len(skipped) > 0 {
		names := make([]string, 0, len(skipped))
		for name := range skipped {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(w, "  Object skip - %s\n", strings.Join(names, ", "))
	}
	if len(errs) > 0 {
		fmt.Fprintf(w, "  Object S3 errors - %s\n", topErrors(errs, 5))
	}
	if len(binds) > 0 {
		sort.Float64s(binds)
		fmt.Fprintf(w, "  Object bind - n: %d  p50: %.0fms  p99: %.0fms  max: %.0fms\n",
			len(binds), percentile(binds, 0.50), percentile(binds, 0.99), binds[len(binds)-1])
	}
}

func topErrors(errs map[string]uint64, n int) string {
	type pair struct {
		code string
		n    uint64
	}
	all := make([]pair, 0, len(errs))
	for code, count := range errs {
		all = append(all, pair{code, count})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n == all[j].n {
			return all[i].code < all[j].code
		}
		return all[i].n > all[j].n
	})
	if len(all) > n {
		all = all[:n]
	}
	parts := make([]string, len(all))
	for i, p := range all {
		parts[i] = fmt.Sprintf("%s=%d", p.code, p.n)
	}
	return strings.Join(parts, " ")
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q*float64(len(sorted)-1) + 0.5)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
