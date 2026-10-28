package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

// DefaultObjectImage is the worker image used when --object-image is not set.
const DefaultObjectImage = "ghcr.io/paraggit/odf-io-stress:latest"

// ObjectPhases is the canonical phase order. A subset in config or flags is
// filtered back into this order.
var ObjectPhases = []string{"create", "write", "read", "verify", "list", "copy", "delete", "teardown"}

// Object is the NooBaa / MCG workload. Enabled is false in every built-in
// default, so a run that never mentions object flags does not exercise it.
type Object struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	Only            bool   `yaml:"only,omitempty" json:"only,omitempty"`
	Resume          bool   `yaml:"resume,omitempty" json:"resume,omitempty"`
	Force           bool   `yaml:"force,omitempty" json:"force,omitempty"`
	Profile         string `yaml:"profile" json:"profile"`
	Image           string `yaml:"image" json:"image"`
	NooBaaNamespace string `yaml:"noobaa_namespace" json:"noobaa_namespace"`
	Provisioning    string `yaml:"provisioning" json:"provisioning"`
	StorageClass    string `yaml:"storage_class" json:"storage_class"`
	BucketClass     string `yaml:"bucket_class" json:"bucket_class"`
	Endpoint        string `yaml:"endpoint" json:"endpoint"`
	InsecureTLS     bool   `yaml:"insecure_tls" json:"insecure_tls"`
	Region          string `yaml:"region" json:"region"`

	Buckets                 int          `yaml:"buckets" json:"buckets"`
	BucketCreateConcurrency int          `yaml:"bucket_create_concurrency" json:"bucket_create_concurrency"`
	VersionedBuckets        int          `yaml:"versioned_buckets" json:"versioned_buckets"`
	VersionsPerKey          int          `yaml:"versions_per_key" json:"versions_per_key"`
	ObjectsPerBucket        int          `yaml:"objects_per_bucket" json:"objects_per_bucket"`
	KeyPrefix               string       `yaml:"key_prefix" json:"key_prefix"`
	KeyLayout               string       `yaml:"key_layout" json:"key_layout"`
	SizeMix                 []SizeWeight `yaml:"size_mix" json:"size_mix"`

	Workers           int       `yaml:"workers" json:"workers"`
	WorkerConcurrency int       `yaml:"worker_concurrency" json:"worker_concurrency"`
	WorkerResources   Resources `yaml:"worker_resources" json:"worker_resources"`

	Multipart             Multipart `yaml:"multipart" json:"multipart"`
	VerifyMode            string    `yaml:"verify_mode" json:"verify_mode"`
	VerifySamplePct       float64   `yaml:"verify_sample_pct" json:"verify_sample_pct"`
	RangeReadsPct         float64   `yaml:"range_reads_pct" json:"range_reads_pct"`
	Phases                []string  `yaml:"phases" json:"phases"`
	DeleteMode            string    `yaml:"delete_mode" json:"delete_mode"`
	Seed                  string    `yaml:"seed" json:"seed"`
	OpTimeout             Duration  `yaml:"op_timeout" json:"op_timeout"`
	PhaseTimeout          Duration  `yaml:"phase_timeout" json:"phase_timeout"`
	OBCDeleteTimeout      Duration  `yaml:"obc_delete_timeout" json:"obc_delete_timeout"`
	MaxRetries            int       `yaml:"max_retries" json:"max_retries"`
	ErrorRateThresholdPct float64   `yaml:"error_rate_threshold_pct" json:"error_rate_threshold_pct"`
	CapacityPreflight     bool      `yaml:"capacity_preflight" json:"capacity_preflight"`
}

// SizeWeight is one bucket of the deterministic size mix. Weight is relative.
type SizeWeight struct {
	Size   string `yaml:"size" json:"size"`
	Weight int    `yaml:"weight" json:"weight"`
}

// Resources is the worker pod request and limit pair.
type Resources struct {
	Requests ResourceQty `yaml:"requests" json:"requests"`
	Limits   ResourceQty `yaml:"limits" json:"limits"`
}

// ResourceQty is a CPU and memory quantity string.
type ResourceQty struct {
	CPU    string `yaml:"cpu" json:"cpu"`
	Memory string `yaml:"memory" json:"memory"`
}

// Multipart controls when PutObject is split and how many uploads are aborted
// on purpose so ListMultipartUploads can prove they went away.
type Multipart struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Threshold  string `yaml:"threshold" json:"threshold"`
	PartSize   string `yaml:"part_size" json:"part_size"`
	AbortCases int    `yaml:"abort_cases" json:"abort_cases"`
}

// OrderedPhases returns selected phases in canonical order.
// An empty selection means every phase.
func OrderedPhases(selected []string) ([]string, error) {
	if len(selected) == 0 {
		out := make([]string, len(ObjectPhases))
		copy(out, ObjectPhases)
		return out, nil
	}
	want := map[string]bool{}
	for _, p := range selected {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !knownPhase(p) {
			return nil, fmt.Errorf("unknown object phase %q", p)
		}
		want[p] = true
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("object phases must not be empty")
	}
	var out []string
	for _, p := range ObjectPhases {
		if want[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

func knownPhase(p string) bool {
	for _, known := range ObjectPhases {
		if p == known {
			return true
		}
	}
	return false
}

// HasPhase reports whether the configured phase list includes name.
func (o Object) HasPhase(name string) bool {
	for _, p := range o.Phases {
		if p == name {
			return true
		}
	}
	return false
}

func validateObject(cfg *Config) error {
	o := cfg.Tools.Object
	if o.Resume {
		if !o.Only {
			return fmt.Errorf("--object-resume requires --object-only")
		}
		if strings.TrimSpace(o.Seed) == "" {
			return fmt.Errorf("--object-resume requires --object-seed")
		}
	}
	if !o.Enabled {
		return nil
	}
	if strings.TrimSpace(o.Image) == "" {
		return fmt.Errorf("object image must not be empty when the object workload is enabled")
	}
	if o.Workers < 1 {
		return fmt.Errorf("object workers must be >= 1, got %d", o.Workers)
	}
	if o.WorkerConcurrency < 1 {
		return fmt.Errorf("object worker_concurrency must be >= 1, got %d", o.WorkerConcurrency)
	}
	if o.Buckets < 1 {
		return fmt.Errorf("object buckets must be >= 1, got %d", o.Buckets)
	}
	if o.BucketCreateConcurrency < 1 {
		return fmt.Errorf("object bucket_create_concurrency must be >= 1, got %d", o.BucketCreateConcurrency)
	}
	if o.ObjectsPerBucket < 1 {
		return fmt.Errorf("object objects_per_bucket must be >= 1, got %d", o.ObjectsPerBucket)
	}
	if o.VersionedBuckets < 0 || o.VersionedBuckets > o.Buckets {
		return fmt.Errorf("object versioned_buckets must be between 0 and buckets (%d), got %d", o.Buckets, o.VersionedBuckets)
	}
	if o.VersionsPerKey < 1 {
		return fmt.Errorf("object versions_per_key must be >= 1, got %d", o.VersionsPerKey)
	}
	if o.MaxRetries < 0 {
		return fmt.Errorf("object max_retries must be >= 0, got %d", o.MaxRetries)
	}
	if o.ErrorRateThresholdPct < 0 || o.ErrorRateThresholdPct > 100 {
		return fmt.Errorf("object error_rate_threshold_pct must be between 0 and 100, got %v", o.ErrorRateThresholdPct)
	}
	if o.VerifySamplePct < 0 || o.VerifySamplePct > 100 {
		return fmt.Errorf("object verify_sample_pct must be between 0 and 100, got %v", o.VerifySamplePct)
	}
	if o.RangeReadsPct < 0 || o.RangeReadsPct > 100 {
		return fmt.Errorf("object range_reads_pct must be between 0 and 100, got %v", o.RangeReadsPct)
	}
	switch o.VerifyMode {
	case "full", "etag", "sample":
	default:
		return fmt.Errorf("object verify_mode must be full, etag, or sample, got %q", o.VerifyMode)
	}
	switch o.DeleteMode {
	case "recursive", "obc-only", "both":
	default:
		return fmt.Errorf("object delete_mode must be recursive, obc-only, or both, got %q", o.DeleteMode)
	}
	switch o.Provisioning {
	case "obc", "s3":
	default:
		return fmt.Errorf("object provisioning must be obc or s3, got %q", o.Provisioning)
	}
	switch o.KeyLayout {
	case "hashed", "flat", "deep":
	default:
		return fmt.Errorf("object key_layout must be hashed, flat, or deep, got %q", o.KeyLayout)
	}
	if o.Provisioning == "obc" && strings.TrimSpace(o.StorageClass) == "" {
		return fmt.Errorf("object storage_class must not be empty when provisioning is obc")
	}
	if strings.TrimSpace(o.NooBaaNamespace) == "" {
		return fmt.Errorf("object noobaa_namespace must not be empty")
	}
	if !validKeyPrefix(o.KeyPrefix) {
		return fmt.Errorf("object key_prefix %q must be a lowercase DNS label", o.KeyPrefix)
	}
	if len(o.SizeMix) == 0 {
		return fmt.Errorf("object size_mix must not be empty")
	}
	for _, sw := range o.SizeMix {
		if sw.Weight <= 0 {
			return fmt.Errorf("object size_mix weight must be > 0, got %d for %q", sw.Weight, sw.Size)
		}
		if _, err := ParseByteSize(sw.Size); err != nil {
			return fmt.Errorf("object size_mix size %q: %w", sw.Size, err)
		}
	}
	if _, err := OrderedPhases(o.Phases); err != nil {
		return err
	}
	if o.OpTimeout.Duration() <= 0 {
		return fmt.Errorf("object op_timeout must be > 0")
	}
	if o.PhaseTimeout.Duration() <= 0 {
		return fmt.Errorf("object phase_timeout must be > 0")
	}
	if o.OBCDeleteTimeout.Duration() <= 0 {
		return fmt.Errorf("object obc_delete_timeout must be > 0")
	}
	if strings.TrimSpace(o.Seed) != "" {
		if err := validateSeedHex(o.Seed); err != nil {
			return err
		}
	}
	if o.Region == "" {
		return fmt.Errorf("object region must not be empty")
	}
	if o.Multipart.Enabled {
		if _, err := ParseByteSize(o.Multipart.Threshold); err != nil {
			return fmt.Errorf("object multipart.threshold: %w", err)
		}
		if _, err := ParseByteSize(o.Multipart.PartSize); err != nil {
			return fmt.Errorf("object multipart.part_size: %w", err)
		}
		if o.Multipart.AbortCases < 0 {
			return fmt.Errorf("object multipart.abort_cases must be >= 0, got %d", o.Multipart.AbortCases)
		}
	}
	for _, q := range []string{o.WorkerResources.Requests.CPU, o.WorkerResources.Requests.Memory, o.WorkerResources.Limits.CPU, o.WorkerResources.Limits.Memory} {
		if q == "" {
			return fmt.Errorf("object worker_resources cpu and memory must be set")
		}
		if _, err := resource.ParseQuantity(q); err != nil {
			return fmt.Errorf("object worker_resources quantity %q: %w", q, err)
		}
	}
	return nil
}

func validKeyPrefix(s string) bool {
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func validateSeedHex(s string) error {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(s) != 64 {
		return fmt.Errorf("object seed must be 64 hex characters")
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return fmt.Errorf("object seed must be hex")
		}
	}
	return nil
}

// ParseByteSize parses 4KB, 1MB, 16MB, 5GB, and bare byte counts.
// KB/MB/GB are powers of 1024.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	upper := strings.ToUpper(s)
	mult := int64(1)
	num := upper
	switch {
	case strings.HasSuffix(upper, "KIB"), strings.HasSuffix(upper, "KB"), strings.HasSuffix(upper, "K"):
		mult = 1024
		num = trimSizeSuffix(upper)
	case strings.HasSuffix(upper, "MIB"), strings.HasSuffix(upper, "MB"), strings.HasSuffix(upper, "M"):
		mult = 1024 * 1024
		num = trimSizeSuffix(upper)
	case strings.HasSuffix(upper, "GIB"), strings.HasSuffix(upper, "GB"), strings.HasSuffix(upper, "G"):
		mult = 1024 * 1024 * 1024
		num = trimSizeSuffix(upper)
	case strings.HasSuffix(upper, "B"):
		num = strings.TrimSuffix(upper, "B")
	}
	num = strings.TrimSpace(num)
	n, err := strconv.ParseFloat(num, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return int64(n * float64(mult)), nil
}

func trimSizeSuffix(upper string) string {
	for _, suf := range []string{"KIB", "MIB", "GIB", "KB", "MB", "GB", "K", "M", "G"} {
		if strings.HasSuffix(upper, suf) {
			return strings.TrimSuffix(upper, suf)
		}
	}
	return upper
}

// FormatSizeMix renders a mix as 4KB:60,1MB:30.
func FormatSizeMix(mix []SizeWeight) string {
	parts := make([]string, 0, len(mix))
	for _, sw := range mix {
		parts = append(parts, fmt.Sprintf("%s:%d", sw.Size, sw.Weight))
	}
	return strings.Join(parts, ",")
}

// ParseSizeMix parses a flag value such as 4KB:60,1MB:30,16MB:10.
func ParseSizeMix(s string) ([]SizeWeight, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty size mix")
	}
	var out []SizeWeight
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		size, weight, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("size mix entry %q: want size:weight", part)
		}
		w, err := strconv.Atoi(strings.TrimSpace(weight))
		if err != nil {
			return nil, fmt.Errorf("size mix weight %q: %w", weight, err)
		}
		out = append(out, SizeWeight{Size: strings.TrimSpace(size), Weight: w})
	}
	return out, nil
}

// MeanObjectSize is the weighted mean of the size mix, in bytes.
func MeanObjectSize(mix []SizeWeight) (int64, error) {
	var weight int64
	var sum int64
	for _, sw := range mix {
		n, err := ParseByteSize(sw.Size)
		if err != nil {
			return 0, err
		}
		if sw.Weight <= 0 {
			continue
		}
		weight += int64(sw.Weight)
		sum += n * int64(sw.Weight)
	}
	if weight == 0 {
		return 0, fmt.Errorf("size mix has no positive weight")
	}
	return sum / weight, nil
}

// ProjectedBytes estimates bytes the write phase will store.
// When any bucket is versioned, every bucket is multiplied by versions_per_key
// so the preflight errs on the high side.
func (o Object) ProjectedBytes() (int64, error) {
	mean, err := MeanObjectSize(o.SizeMix)
	if err != nil {
		return 0, err
	}
	mult := int64(1)
	if o.VersionedBuckets > 0 {
		mult = int64(o.VersionsPerKey)
		if mult < 1 {
			mult = 1
		}
	}
	return int64(o.Buckets) * int64(o.ObjectsPerBucket) * mean * mult, nil
}

// Default durations used by profiles. Exposed for tests.
const (
	defaultOpTimeout    = Duration(60 * time.Second)
	defaultPhaseTimeout = Duration(6 * time.Hour)
	defaultOBCTimeout   = Duration(10 * time.Minute)
)
