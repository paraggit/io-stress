package fio

import (
	"fmt"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

type Job struct {
	Name     string
	Category string
	Args     []string
	// Filename overrides the per-pod target when set (e.g. expand-verify on a
	// distinct file so it does not collide with the integrity seed).
	Filename string
}

// Integrity seed/verify share these so phase3 never reads a region that was not
// deterministically written. Do not add time_based/runtime to either job.
const (
	IntegritySeedBS       = "256k"
	IntegritySeedIODepth  = "32"
	IntegritySeedRandSeed = "1"
	DefaultSeedSize       = "512m" // Block-only extent; must be a multiple of IntegritySeedBS
)

func isBlockMode(volumeMode string) bool {
	return volumeMode == "Block"
}

// IntegrityExtent is the exact byte range seed writes and phase3 verifies.
// Filesystem keeps the dedicated-file size (tools.fio.size). Block uses the
// bounded SeedSize so the raw device is not verified past what we overwrote.
func IntegrityExtent(cfg *config.Config, volumeMode string) string {
	if isBlockMode(volumeMode) {
		return blockSeedSize(cfg)
	}
	if cfg != nil && cfg.Tools.FIO.Size != "" {
		return cfg.Tools.FIO.Size
	}
	return "1G"
}

func blockSeedSize(cfg *config.Config) string {
	if cfg != nil && cfg.Cluster.SeedSize != "" {
		return cfg.Cluster.SeedSize
	}
	return DefaultSeedSize
}

// IntegritySize is the Block seed extent, used as the sustain --offset so
// background IO stays past the verified region.
func IntegritySize(cfg *config.Config) string {
	return blockSeedSize(cfg)
}

func IntegritySeedJob(cfg *config.Config, volumeMode string) Job {
	size := IntegrityExtent(cfg, volumeMode)
	args := []string{
		"--rw=write",
		fmt.Sprintf("--bs=%s", IntegritySeedBS),
		fmt.Sprintf("--size=%s", size),
		"--ioengine=libaio",
		"--direct=1",
		fmt.Sprintf("--iodepth=%s", IntegritySeedIODepth),
		"--verify=crc32c",
		"--do_verify=0",
		"--serialize_overlap=1",
		fmt.Sprintf("--randseed=%s", IntegritySeedRandSeed),
		"--group_reporting=1",
	}
	if isBlockMode(volumeMode) {
		args = append(args, "--offset=0")
	}
	return Job{
		Name:     "integrity-seed",
		Category: "lifecycle",
		Args:     args,
	}
}

func IntegrityVerifyJob(cfg *config.Config, volumeMode string) Job {
	size := IntegrityExtent(cfg, volumeMode)
	args := []string{
		"--rw=read",
		fmt.Sprintf("--bs=%s", IntegritySeedBS),
		fmt.Sprintf("--size=%s", size),
		"--ioengine=libaio",
		"--direct=1",
		fmt.Sprintf("--iodepth=%s", IntegritySeedIODepth),
		"--verify=crc32c",
		"--verify_only=1",
		"--serialize_overlap=1",
		fmt.Sprintf("--randseed=%s", IntegritySeedRandSeed),
		"--group_reporting=1",
	}
	if isBlockMode(volumeMode) {
		args = append(args, "--offset=0")
	}
	return Job{
		Name:     "phase3-verify",
		Category: "lifecycle",
		Args:     args,
	}
}

// ExpandVerifyBlockJob builds the Block/RBD expand-verify job. A raw block
// device is shared with the integrity seed ([0,seedSize)) and all phase-1 IO,
// and a clone is COW-copied concurrently with this write. So the verify write
// must be confined to the region the volume *grew into* (past both the seed and
// the original request) — otherwise a 4k randwrite here clobbers the seeded
// [0,seedSize) blocks that the concurrent clone captures, and phase3-verify then
// reports false "bad header length"/"crc32c verify failed" on the clone.
// Returns an error if the expansion added no usable room past the seed.
func ExpandVerifyBlockJob(cfg *config.Config, originalSize, expandedSize string, runtime int) (Job, error) {
	seed := blockSeedSize(cfg)
	seedBytes, err := SizeBytes(seed)
	if err != nil {
		return Job{}, fmt.Errorf("seed size %q: %w", seed, err)
	}
	origBytes, err := SizeBytes(originalSize)
	if err != nil {
		return Job{}, fmt.Errorf("original size %q: %w", originalSize, err)
	}
	expBytes, err := SizeBytes(expandedSize)
	if err != nil {
		return Job{}, fmt.Errorf("expanded size %q: %w", expandedSize, err)
	}
	// Start past whichever is larger: the seeded extent or the original request.
	offset := seedBytes
	if origBytes > offset {
		offset = origBytes
	}
	writeBytes := expBytes - offset
	if writeBytes <= 0 {
		return Job{}, fmt.Errorf("expand-verify: no room past seed (expanded=%d offset=%d)", expBytes, offset)
	}
	if runtime < 1 {
		runtime = 1
	}
	return Job{
		Name:     "expand-verify",
		Category: "lifecycle",
		Args: []string{
			"--rw=randwrite", "--bs=4k",
			fmt.Sprintf("--offset=%d", offset),
			fmt.Sprintf("--size=%d", writeBytes),
			"--ioengine=libaio", "--direct=1", "--iodepth=16",
			"--time_based=1", fmt.Sprintf("--runtime=%d", runtime),
			"--verify=crc32c", "--verify_backlog=128",
			"--verify_fatal=1", "--group_reporting=1",
		},
	}, nil
}

func BuildArgs(j Job, target string, outputFormat string) []string {
	filename := target
	if j.Filename != "" {
		filename = j.Filename
	}
	args := []string{
		fmt.Sprintf("--name=%s", j.Name),
		fmt.Sprintf("--filename=%s", filename),
		fmt.Sprintf("--output-format=%s", outputFormat),
	}
	args = append(args, j.Args...)
	return args
}

func JobsForVolume(storageType string, volumeMode string, cfg *config.Config) []Job {
	s := cfg.Tools.FIO.Suites
	fioCfg := cfg.Tools.FIO
	jobs := PatternsToJobs(s.Common, fioCfg)
	if volumeMode == "Filesystem" {
		jobs = append(jobs, PatternsToJobs(s.Filesystem, fioCfg)...)
	} else {
		jobs = append(jobs, PatternsToJobs(s.Block, fioCfg)...)
	}
	return jobs
}

// WriteVerifyJob writes a region then verifies it in the same FIO job
// (--do_verify=1, --verify_backlog=1). Not verify_only: data is written first.
func WriteVerifyJob(cfg *config.Config) Job {
	size := "1G"
	if cfg != nil && cfg.Tools.FIO.Size != "" {
		size = cfg.Tools.FIO.Size
	}
	return Job{
		Name:     "write-verify",
		Category: "integrity",
		Args: []string{
			"--rw=write",
			fmt.Sprintf("--bs=%s", IntegritySeedBS),
			fmt.Sprintf("--size=%s", size),
			"--ioengine=libaio",
			"--direct=1",
			fmt.Sprintf("--iodepth=%s", IntegritySeedIODepth),
			"--verify=crc32c",
			"--do_verify=1",
			"--verify_backlog=1",
			"--verify_fatal=1",
			"--verify_dump=1",
			"--serialize_overlap=1",
			fmt.Sprintf("--randseed=%s", IntegritySeedRandSeed),
			"--group_reporting=1",
		},
	}
}

// Phase1Jobs is the FIO suite for phase 1. WriteVerify replaces the full
// stress suite with a single write+immediate-verify job.
func Phase1Jobs(storageType, volumeMode string, cfg *config.Config) []Job {
	if cfg != nil && cfg.Cluster.WriteVerify {
		return []Job{WriteVerifyJob(cfg)}
	}
	return JobsForVolume(storageType, volumeMode, cfg)
}

func ReducedSuite(target string, cfg *config.Config) []Job {
	_ = target
	return PatternsToJobs(cfg.Tools.FIO.Suites.Lifecycle, cfg.Tools.FIO)
}

func CephFSRWXJobs(cfg *config.Config) []Job {
	return PatternsToJobs(cfg.Tools.FIO.Suites.CephFSRWX, cfg.Tools.FIO)
}

func AppTypeJobs(appType string, cfg *config.Config) []Job {
	patterns := config.AppSuitePatterns(cfg, appType)
	return PatternsToJobs(patterns, cfg.Tools.FIO)
}
