package worker

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

// SpecVersion is the handshake between the control plane and the worker image.
// Bump it when Spec's meaning changes. A mismatch refuses the run before any S3 call.
const SpecVersion = 1

// SpecPath is where the worker pod mounts the job spec.
const SpecPath = "/etc/objstress/spec.json"

// CAPath is the injected OpenShift service CA inside the worker pod.
const CAPath = "/etc/ssl/service-ca/service-ca.crt"

// Spec is one worker generation: one role over one shard of one or more buckets.
type Spec struct {
	SpecVersion           int                 `json:"specVersion"`
	Phase                 string              `json:"phase"`
	Role                  string              `json:"role"`
	Worker                int                 `json:"worker"`
	Workers               int                 `json:"workers"`
	Seed                  string              `json:"seed"`
	Buckets               []BucketSpec        `json:"buckets"`
	ObjectsPerBucket      int                 `json:"objectsPerBucket"`
	KeyPrefix             string              `json:"keyPrefix"`
	KeyLayout             string              `json:"keyLayout"`
	SizeMix               []config.SizeWeight `json:"sizeMix"`
	Concurrency           int                 `json:"concurrency"`
	VerifyMode            string              `json:"verifyMode"`
	VerifySamplePct       float64             `json:"verifySamplePct"`
	RangeReadsPct         float64             `json:"rangeReadsPct"`
	MultipartEnabled      bool                `json:"multipartEnabled"`
	MultipartThreshold    int64               `json:"multipartThreshold"`
	MultipartPartSize     int64               `json:"multipartPartSize"`
	AbortCases            int                 `json:"abortCases"`
	OpTimeout             config.Duration     `json:"opTimeout"`
	MaxRetries            int                 `json:"maxRetries"`
	InsecureTLS           bool                `json:"insecureTLS"`
	CAFile                string              `json:"caFile"`
	VersionsPerKey        int                 `json:"versionsPerKey"`
	ErrorRateThresholdPct float64             `json:"errorRateThresholdPct"`
	Region                string              `json:"region"`
	Provisioning          string              `json:"provisioning"`
	Resume                bool                `json:"resume"`
}

// BucketSpec is one bucket the worker may touch. Credentials are read from CredDir.
type BucketSpec struct {
	Name         string            `json:"name"`
	Ordinal      int               `json:"ordinal"`
	Versioned    bool              `json:"versioned"`
	DeletePath   string            `json:"deletePath"`
	Endpoint     string            `json:"endpoint"`
	Region       string            `json:"region"`
	CredDir      string            `json:"credDir"`
	Capabilities map[string]string `json:"capabilities,omitempty"`
	ProbeDetail  map[string]string `json:"probeDetail,omitempty"`
}

// LoadSpec reads a worker spec and refuses a specVersion the binary does not speak.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec %s: %w", path, err)
	}
	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse spec %s: %w", path, err)
	}
	if spec.SpecVersion != SpecVersion {
		return nil, fmt.Errorf("specVersion mismatch: worker speaks %d, spec is %d (the worker image does not match this control plane; rebuild and push the image)", SpecVersion, spec.SpecVersion)
	}
	if spec.Role == "" {
		spec.Role = spec.Phase
	}
	return &spec, nil
}
