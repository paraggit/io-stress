package report

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSummaryJSONObjectOnlyOmitsVolumePhases(t *testing.T) {
	s := Summary{Total: 1, Passed: 1, Object: PhaseSummary{Total: 1, Passed: 1}}
	s.OmitVolumePhases()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"phase1"`)) || bytes.Contains(b, []byte(`"lifecycle"`)) {
		t.Fatalf("volume phases present: %s", b)
	}
	if !bytes.Contains(b, []byte(`"object"`)) {
		t.Fatalf("object phase missing: %s", b)
	}
}

func TestSummaryJSONFIOKeepsEmptyLifecycle(t *testing.T) {
	s := ComputeSummary([]JobResult{{Category: "stress", Status: "pass"}})
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"phase1"`)) || !bytes.Contains(b, []byte(`"lifecycle"`)) {
		t.Fatalf("FIO summary = %s", b)
	}
}

func TestUsedOptionsObjectOnlyJSONOmitsFIO(t *testing.T) {
	o := UsedOptions{
		Kubeconfig:             "",
		RBDNumPVC:              0,
		CephFSNumPVC:           0,
		Namespace:              "odf-io-stress",
		Image:                  "quay.io/ocsci/nginx:fio",
		Runtime:                60,
		BlockSize:              "512",
		PVCSize:                "10Gi",
		Prefix:                 "odf-io",
		Timeout:                "5m0s",
		CloneTimeout:           "20m0s",
		Format:                 "json",
		SkipFIOStress:          true,
		SkipLifecycle:          true,
		Object:                 true,
		ObjectOnly:             true,
		ObjectProfile:          "smoke",
		ObjectImage:            "ghcr.io/paraggit/odf-io-stress:latest",
		ObjectBuckets:          2,
		ObjectBackend:          "rgw",
		ObjectOBCDeleteTimeout: "10m0s",
	}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"rbd-num-pvc", "cephfs-num-pvc", "rbd-storage-class", "cephfs-storage-class",
		"pvc-size", "image", "runtime", "bs", "offset", "fio-size", "seed-size",
		"clone-timeout", "format", "lifecycle-interval", "skip-lifecycle", "skip-fio-stress",
		"write-verify", "setup-only", "expand-factor", "snapshot-class", "sustain-runtime",
		"sequential",
	} {
		if bytes.Contains(b, []byte(`"`+bad+`"`)) {
			t.Errorf("object-only options contain %s: %s", bad, b)
		}
	}
	for _, good := range []string{
		"kubeconfig", "namespace", "prefix", "timeout", "no-cleanup", "dry-run",
		"max-parallel", "max-parallel-provision", "object-only", "object-image", "object-profile",
		"object-backend", "object-obc-delete-timeout",
	} {
		if !bytes.Contains(b, []byte(`"`+good+`"`)) {
			t.Errorf("object-only options missing %s: %s", good, b)
		}
	}
}

func TestUsedOptionsFIOJSONKeepsZeroPVCCount(t *testing.T) {
	b, err := json.Marshal(UsedOptions{CephFSNumPVC: 0, RBDNumPVC: 1, Namespace: "odf-io-stress"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"cephfs-num-pvc":0`)) && !bytes.Contains(b, []byte(`"cephfs-num-pvc": 0`)) {
		t.Fatalf("FIO options = %s", b)
	}
}
