package k8s

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewClient_ExplicitPathMissing(t *testing.T) {
	_, err := NewClient(filepath.Join(t.TempDir(), "does-not-exist.kubeconfig"))
	if err == nil {
		t.Fatal("expected error for missing kubeconfig path")
	}
}

func TestNewClient_ExplicitPathInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.kubeconfig")
	if err := os.WriteFile(path, []byte("not: valid: kubeconfig: [[["), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewClient(path)
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig")
	}
}
