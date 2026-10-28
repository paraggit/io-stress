package workload

import (
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

// UsageError is a bad flag combination or a preflight refusal. Exit code 2.
type UsageError struct{ Err error }

func (e *UsageError) Error() string {
	if e == nil || e.Err == nil {
		return "invalid object run"
	}
	return e.Err.Error()
}
func (e *UsageError) Unwrap() error { return e.Err }

// CleanupError is a passing workload that left resources behind. Exit code 2.
type CleanupError struct{ Err error }

func (e *CleanupError) Error() string {
	if e == nil || e.Err == nil {
		return "cleanup failed"
	}
	return e.Err.Error()
}
func (e *CleanupError) Unwrap() error { return e.Err }

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
	hasOptions  bool
	options     report.UsedOptions
}

// finalize writes report.json when requested, then writes the agent summary
// to w and to summary.json. A nil error means exit 0.
func finalize(w io.Writer, dir string, results []report.JobResult, runErr error, opts finalizeOpts) error {
	if results == nil {
		results = []report.JobResult{}
	}
	counts := report.ComputeSummary(results)
	if opts.options.ObjectOnly {
		counts.OmitVolumePhases()
	}

	code := report.ExitPass
	var usage *UsageError
	var cleanup *CleanupError
	switch {
	case counts.Failed > 0:
		code = report.ExitJobsFailed
		if runErr == nil {
			runErr = fmt.Errorf("%d job(s) failed", counts.Failed)
		}
	case counts.Infra > 0:
		code = report.ExitInfra
		if runErr == nil {
			runErr = fmt.Errorf("%d infrastructure failure(s)", counts.Infra)
		}
	case errors.As(runErr, &usage):
		code = report.ExitUsage
	case counts.Cleanup > 0 || errors.As(runErr, &cleanup):
		code = report.ExitCleanup
		if runErr == nil {
			runErr = fmt.Errorf("%d cleanup failure(s)", counts.Cleanup)
		}
	case runErr != nil:
		code = report.ExitCluster
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
	if opts.options.ObjectOnly {
		agent.Summary.OmitVolumePhases()
	}
	if opts.hasOptions {
		recorded := opts.options
		agent.Options = &recorded
	}
	if opts.printHuman {
		report.PrintSummary(results, opts.options.ObjectOnly)
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
