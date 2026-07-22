package workload

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestDryRun(t *testing.T) {
	cfg := config.NewDefault()
	cfg.NumPVC = 2

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := DryRun(cfg)
	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	stdout.ReadFrom(r)
	output := stdout.String()

	if !strings.Contains(output, "kind: PersistentVolumeClaim") {
		t.Error("dry-run output should contain PVC YAML")
	}
	if !strings.Contains(output, "kind: Pod") {
		t.Error("dry-run output should contain Pod YAML")
	}
	if !strings.Contains(output, "rbd") {
		t.Error("dry-run output should contain RBD resources")
	}
	if !strings.Contains(output, "cephfs") {
		t.Error("dry-run output should contain CephFS resources")
	}
}
