package workload

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

// ExitError is a finished run whose process exit code is Code.
// Emitted means the agent JSON summary was already written to stdout.
type ExitError struct {
	Code    int
	Err     error
	Emitted bool
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return "run failed"
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

type finalizeOpts struct {
	writeReport bool
	printHuman  bool
	start       time.Time
}

// finalize writes report.json when requested, then writes the agent summary
// to w and to summary.json. A nil error means exit 0.
func finalize(w io.Writer, dir string, results []report.JobResult, runErr error, opts finalizeOpts) error {
	if results == nil {
		results = []report.JobResult{}
	}
	counts := report.ComputeSummary(results)

	code := report.ExitPass
	switch {
	case runErr != nil && counts.Failed > 0:
		code = report.ExitJobsFailed
	case runErr != nil:
		code = report.ExitCluster
	case counts.Failed > 0:
		code = report.ExitJobsFailed
		runErr = fmt.Errorf("%d job(s) failed", counts.Failed)
	}

	reportWritten := false
	if opts.writeReport && dir != "" {
		end := time.Now()
		start := opts.start
		if start.IsZero() {
			start = end
		}
		rpt := &report.RunReport{
			StartTime: start,
			EndTime:   end,
			Results:   results,
			Summary:   counts,
		}
		if err := report.WriteJSON(dir, rpt); err != nil {
			log.Printf("warning: failed to write report: %v", err)
		} else {
			reportWritten = true
		}
	}

	agent := report.BuildAgentSummary(code, dir, results, runErr, reportWritten)
	if opts.printHuman {
		report.PrintSummary(results)
	}
	if dir != "" {
		log.Printf("Results in %s", dir)
	}
	if err := report.EmitAgentSummary(w, agent); err != nil {
		log.Printf("warning: agent summary: %v", err)
	}

	if code == report.ExitPass {
		return nil
	}
	return &ExitError{Code: code, Err: runErr, Emitted: true}
}

// EmitUsage prints the agent summary for a config or CLI error and returns exit code 2.
func EmitUsage(w io.Writer, err error) int {
	s := report.BuildAgentSummary(report.ExitUsage, "", nil, err, false)
	if emitErr := report.EmitAgentSummary(w, s); emitErr != nil {
		log.Printf("warning: agent summary: %v", emitErr)
	}
	return report.ExitUsage
}
