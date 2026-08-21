package fio

import (
	"testing"
)

func TestParseResult_ReadWrite(t *testing.T) {
	data := []byte(`{
		"jobs": [{
			"jobname": "rw",
			"error": 0,
			"read": {"iops": 10.5, "bw_bytes": 1000, "lat_ns": {"mean": 1.2, "max": 3.4}},
			"write": {"iops": 20.5, "bw_bytes": 2000, "lat_ns": {"mean": 2.2, "max": 4.4}}
		}]
	}`)
	r, err := ParseResult(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Jobs) != 1 {
		t.Fatalf("jobs=%d", len(r.Jobs))
	}
	j := r.Jobs[0]
	if j.Read.IOPS != 10.5 || j.Write.IOPS != 20.5 {
		t.Fatalf("read/write iops = %v/%v", j.Read.IOPS, j.Write.IOPS)
	}
}

func TestParseResult_Trim(t *testing.T) {
	data := []byte(`{
		"jobs": [{
			"jobname": "trim-stress",
			"error": 0,
			"read": {"iops": 0, "bw_bytes": 0, "lat_ns": {"mean": 0, "max": 0}},
			"write": {"iops": 0, "bw_bytes": 0, "lat_ns": {"mean": 0, "max": 0}},
			"trim": {"iops": 123.4, "bw_bytes": 5678, "lat_ns": {"mean": 9.1, "max": 99.0}}
		}]
	}`)
	r, err := ParseResult(data)
	if err != nil {
		t.Fatal(err)
	}
	j := r.Jobs[0]
	if j.Trim.IOPS != 123.4 {
		t.Fatalf("trim.iops=%v, want 123.4", j.Trim.IOPS)
	}
	if j.Trim.BWBytes != 5678 {
		t.Fatalf("trim.bw_bytes=%d, want 5678", j.Trim.BWBytes)
	}
	if j.Trim.LatNS.Mean != 9.1 || j.Trim.LatNS.Max != 99.0 {
		t.Fatalf("trim.lat_ns=%+v", j.Trim.LatNS)
	}
}
