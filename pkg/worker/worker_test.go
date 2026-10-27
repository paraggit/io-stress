package worker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestLoadSpecMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(path, []byte(`{"specVersion":99}`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSpec(path)
	if err == nil || !strings.Contains(err.Error(), "specVersion mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestAssertOBCOnlySkipsWithoutS3(t *testing.T) {
	var buf bytes.Buffer
	err := Run(context.Background(), &Spec{
		SpecVersion:      SpecVersion,
		Phase:            "teardown",
		Role:             "assert",
		Worker:           0,
		Workers:          1,
		Seed:             strings.Repeat("ab", 32),
		ObjectsPerBucket: 1,
		KeyPrefix:        "objstress",
		KeyLayout:        "hashed",
		SizeMix:          []config.SizeWeight{{Size: "4KB", Weight: 1}},
		Buckets: []BucketSpec{{
			Name:       "b",
			DeletePath: "obc-only",
			Endpoint:   "https://example.invalid",
			Region:     "us-east-1",
		}},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseResults(buf.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SkipReason == "" || got[0].ObjectsFail != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseResultsIgnoresProgress(t *testing.T) {
	logs := "some log line\n" +
		`{"type":"progress","phase":"write","worker":0,"objectsOk":256}` + "\n" +
		`{"type":"result","result":{"bucket":"b","phase":"write","objectsOk":1,"ops":{},"s3Errors":{}}}` + "\n"
	got, err := ParseResults(logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Bucket != "b" || got[0].ObjectsOK != 1 {
		t.Fatalf("got %+v", got)
	}
}
