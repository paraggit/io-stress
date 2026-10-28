package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteSummaryObjectOnlyOmitsFIOLines(t *testing.T) {
	var buf bytes.Buffer
	writeSummary(&buf, []JobResult{{
		Job: "write", Category: "object-write", Status: "pass",
		ObjectOutput: &ObjectResult{Bucket: "b", Phase: "write", ObjectsOK: 2, BytesMoved: 10},
	}}, true)
	got := buf.String()
	if strings.Contains(got, "Phase 1") || strings.Contains(got, "Combined") {
		t.Fatalf("object-only summary still has FIO lines:\n%s", got)
	}
	if !strings.Contains(got, "Object    - Total: 1") || !strings.Contains(got, "written: 2") {
		t.Fatalf("object lines missing:\n%s", got)
	}
}

func TestWriteSummaryFIOKeepsPhase1AndCombined(t *testing.T) {
	var buf bytes.Buffer
	writeSummary(&buf, []JobResult{{Job: "j", Category: "stress", Status: "pass"}}, false)
	got := buf.String()
	if !strings.Contains(got, "Phase 1") || !strings.Contains(got, "Combined") {
		t.Fatalf("FIO summary = %s", got)
	}
}
