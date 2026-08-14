package workload

import (
	"encoding/json"
	"testing"
)

func TestFioOutputRaw_JSON(t *testing.T) {
	in := []byte(`{"jobs":[{"jobname":"x"}]}`)
	raw := fioOutputRaw(in)
	if !json.Valid(raw) {
		t.Fatalf("expected valid JSON, got %q", raw)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["jobs"]; !ok {
		t.Fatalf("expected jobs key, got %#v", obj)
	}
}

func TestFioOutputRaw_NormalText(t *testing.T) {
	in := []byte("fio-3.1\nStarting 1 process\n")
	raw := fioOutputRaw(in)
	if !json.Valid(raw) {
		t.Fatalf("wrapped output must be valid JSON, got %q", raw)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s != string(in) {
		t.Fatalf("got %q, want %q", s, in)
	}

	// Embedding into a JobResult must keep the whole report marshalable.
	type wrap struct {
		Out json.RawMessage `json:"fioOutput"`
	}
	b, err := json.Marshal(wrap{Out: raw})
	if err != nil {
		t.Fatalf("marshal report fragment: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("report fragment invalid: %s", b)
	}
}
