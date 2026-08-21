package config

import (
	"fmt"
	"sort"
	"strings"
)

// AppProfile describes one application IO profile: volume shape + FIO patterns.
// Built-ins ship in defaultAppSuites(); users can add/override via tools.fio.app_suites
// in the config file without recompiling.
type AppProfile struct {
	// VolumeMode is "Filesystem" (default) or "Block".
	VolumeMode string    `yaml:"volume_mode,omitempty" json:"volume_mode,omitempty"`
	Patterns   []Pattern `yaml:"patterns" json:"patterns"`
}

func (p AppProfile) ResolvedVolumeMode() string {
	if strings.EqualFold(p.VolumeMode, "Block") {
		return "Block"
	}
	return "Filesystem"
}

func defaultAppSuites() map[string]AppProfile {
	return map[string]AppProfile{
		"nfs": {
			VolumeMode: "Filesystem",
			Patterns:   nfsSuite(),
		},
		"vm": {
			VolumeMode: "Block",
			Patterns:   vmSuite(),
		},
		"database": {
			VolumeMode: "Filesystem",
			Patterns:   databaseSuite(),
		},
		// ODF-relevant FIO shapes that were missing as first-class profiles.
		"messaging": {
			VolumeMode: "Filesystem",
			Patterns:   messagingSuite(),
		},
		"ai": {
			VolumeMode: "Filesystem",
			Patterns:   aiSuite(),
		},
	}
}

// MergeAppSuites fills missing built-in profiles after config load so a YAML that
// only adds a custom suite does not wipe nfs/vm/database/messaging/ai defaults.
func MergeAppSuites(cfg *Config) {
	defaults := defaultAppSuites()
	if cfg.Tools.FIO.AppSuites == nil {
		cfg.Tools.FIO.AppSuites = defaults
		return
	}
	for name, profile := range defaults {
		if _, ok := cfg.Tools.FIO.AppSuites[name]; !ok {
			cfg.Tools.FIO.AppSuites[name] = profile
		}
	}
}

// LookupAppProfile returns the profile for appType from cfg (after defaults merge).
func LookupAppProfile(cfg *Config, appType string) (AppProfile, bool) {
	if cfg.Tools.FIO.AppSuites == nil {
		MergeAppSuites(cfg)
	}
	p, ok := cfg.Tools.FIO.AppSuites[appType]
	return p, ok
}

// AppSuitePatterns returns FIO patterns for an app type from config.
func AppSuitePatterns(cfg *Config, appType string) []Pattern {
	p, ok := LookupAppProfile(cfg, appType)
	if !ok {
		return nil
	}
	return p.Patterns
}

// ValidAppTypes returns sorted built-in + currently configured app suite names.
func ValidAppTypes(cfg *Config) []string {
	if cfg == nil || cfg.Tools.FIO.AppSuites == nil {
		cfg = &Config{Tools: Tools{FIO: FIO{AppSuites: defaultAppSuites()}}}
	}
	out := make([]string, 0, len(cfg.Tools.FIO.AppSuites))
	for name := range cfg.Tools.FIO.AppSuites {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func formatValidAppTypes(cfg *Config) string {
	return strings.Join(ValidAppTypes(cfg), ", ")
}

func validateAppTypes(cfg *Config) error {
	for _, at := range cfg.Cluster.AppTypes {
		at = strings.TrimSpace(at)
		if at == "" {
			return fmt.Errorf("app_types contains an empty entry")
		}
		profile, ok := LookupAppProfile(cfg, at)
		if !ok {
			return fmt.Errorf("unknown app-type %q (valid: %s); add tools.fio.app_suites.%s to define a custom profile",
				at, formatValidAppTypes(cfg), at)
		}
		if len(profile.Patterns) == 0 {
			return fmt.Errorf("app-type %q has no patterns in tools.fio.app_suites", at)
		}
		for _, p := range profile.Patterns {
			if p.Name == "" {
				return fmt.Errorf("app-type %q has a pattern with empty name", at)
			}
		}
		mode := profile.ResolvedVolumeMode()
		if mode != "Filesystem" && mode != "Block" {
			return fmt.Errorf("app-type %q has invalid volume_mode %q (use Filesystem or Block)", at, profile.VolumeMode)
		}
	}
	return nil
}

func nfsSuite() []Pattern {
	return []Pattern{
		{
			Name:     "nfs-small-file-metadata",
			Category: "nfs",
			Size:     "64m",
			Params: map[string]string{
				"rw": "randrw", "rwmixread": "80", "bs": "4k", "ioengine": "psync",
				"direct": "0", "fsync": "1", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "nfs-large-file-transfer",
			Category: "nfs",
			Params: map[string]string{
				"rw": "rw", "rwmixread": "60", "bs": "1m", "ioengine": "psync",
				"direct": "0", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "nfs-mixed-file-ops",
			Category: "nfs",
			Params: map[string]string{
				"rw": "randrw", "rwmixread": "70", "bssplit": "4k/40:64k/35:1m/25",
				"ioengine": "psync", "direct": "0", "fdatasync": "1",
				"time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "nfs-sequential-append",
			Category: "nfs",
			Params: map[string]string{
				"rw": "write", "bs": "64k", "ioengine": "psync", "direct": "0",
				"fallocate": "none", "fsync": "4", "time_based": "1", "group_reporting": "1",
			},
		},
	}
}

func vmSuite() []Pattern {
	return []Pattern{
		{
			Name:     "cnv-virtio-random-io",
			Category: "vm",
			Params: map[string]string{
				"rw": "randrw", "rwmixread": "60", "bs": "4k", "ioengine": "libaio",
				"direct": "1", "iodepth": "64", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "cnv-guest-boot-storm",
			Category: "vm",
			Params: map[string]string{
				"rw": "read", "bs": "128k", "ioengine": "libaio", "direct": "1",
				"iodepth": "32", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "cnv-live-migration-precopy",
			Category: "vm",
			Params: map[string]string{
				"rw": "read", "bs": "256k", "ioengine": "libaio", "direct": "1",
				"iodepth": "16", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "cnv-snapshot-commit",
			Category: "vm",
			Params: map[string]string{
				"rw": "write", "bs": "64k", "ioengine": "libaio", "direct": "1",
				"iodepth": "32", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "cnv-high-queue-depth",
			Category: "vm",
			Params: map[string]string{
				"rw": "randwrite", "bs": "4k", "ioengine": "libaio", "direct": "1",
				"iodepth": "128", "time_based": "1", "group_reporting": "1",
			},
		},
	}
}

func databaseSuite() []Pattern {
	return []Pattern{
		{
			Name:     "db-wal-write",
			Category: "database",
			Size:     "256m",
			Params: map[string]string{
				"rw": "write", "bs": "8k", "ioengine": "psync", "direct": "1",
				"fsync": "1", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "db-random-page-read",
			Category: "database",
			Params: map[string]string{
				"rw": "randread", "bs": "8k", "ioengine": "libaio", "direct": "1",
				"iodepth": "32", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "db-oltp-mixed",
			Category: "database",
			Params: map[string]string{
				"rw": "randrw", "rwmixread": "70", "bs": "8k", "ioengine": "libaio",
				"direct": "1", "iodepth": "64", "fsync": "1", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "db-tablespace-seq-write",
			Category: "database",
			Params: map[string]string{
				"rw": "write", "bs": "128k", "ioengine": "libaio", "direct": "1",
				"iodepth": "16", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "db-checkpoint-flush",
			Category: "database",
			Params: map[string]string{
				"rw": "randwrite", "bs": "16k", "ioengine": "psync", "direct": "1",
				"fsync": "1", "time_based": "1", "group_reporting": "1",
			},
		},
	}
}

func messagingSuite() []Pattern {
	return []Pattern{
		{
			Name:     "msg-log-append",
			Category: "messaging",
			Params: map[string]string{
				"rw": "write", "bs": "64k", "ioengine": "psync", "direct": "1",
				"fsync": "1", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "msg-consumer-random-read",
			Category: "messaging",
			Params: map[string]string{
				"rw": "randread", "bs": "64k", "ioengine": "libaio", "direct": "1",
				"iodepth": "32", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "msg-burst-mixed",
			Category: "messaging",
			Params: map[string]string{
				"rw": "randrw", "rwmixread": "40", "bs": "32k", "ioengine": "libaio",
				"direct": "1", "iodepth": "64", "time_based": "1", "group_reporting": "1",
			},
		},
	}
}

func aiSuite() []Pattern {
	return []Pattern{
		{
			Name:     "ai-checkpoint-seq-write",
			Category: "ai",
			Params: map[string]string{
				"rw": "write", "bs": "1m", "ioengine": "libaio", "direct": "1",
				"iodepth": "16", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "ai-dataset-seq-read",
			Category: "ai",
			Params: map[string]string{
				"rw": "read", "bs": "1m", "ioengine": "libaio", "direct": "1",
				"iodepth": "32", "time_based": "1", "group_reporting": "1",
			},
		},
		{
			Name:     "ai-shuffle-random-read",
			Category: "ai",
			Params: map[string]string{
				"rw": "randread", "bs": "256k", "ioengine": "libaio", "direct": "1",
				"iodepth": "64", "time_based": "1", "group_reporting": "1",
			},
		},
	}
}
