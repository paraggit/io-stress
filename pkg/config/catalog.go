package config

import "github.com/spf13/pflag"

// FlagInfo is one CLI option, taken from the live flag set.
type FlagInfo struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Default   string `json:"default"`
	Usage     string `json:"usage"`
}

// OptionsDocument is the JSON from `odf-io-stress options`.
type OptionsDocument struct {
	SchemaVersion  int        `json:"schemaVersion"`
	AppTypes       []string   `json:"appTypes"`
	Rules          []string   `json:"rules"`
	Run            []FlagInfo `json:"run"`
	GenerateConfig []FlagInfo `json:"generateConfig"`
}

// CollectFlags lists flags in stable name order, skipping cobra's help flag.
func CollectFlags(fs *pflag.FlagSet) []FlagInfo {
	if fs == nil {
		return []FlagInfo{}
	}
	out := []FlagInfo{}
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		out = append(out, FlagInfo{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Default:   f.DefValue,
			Usage:     f.Usage,
		})
	})
	return out
}

// OptionRules are the flag interactions an agent cannot learn from flag help alone.
func OptionRules() []string {
	return []string{
		"Merge order: built-in defaults, then --config, then only flags explicitly set on the command line. sustain-runtime is recomputed as runtime×3 only when it is still 0; the built-in default is 180.",
		"--rbd-num-pvc alone (without --cephfs-num-pvc and without -n/--num-pvc) sets the CephFS count to 0. --cephfs-num-pvc alone sets the RBD count to 0.",
		"-n/--num-pvc sets both RBD and CephFS counts. A per-backend flag set in the same command overrides that side.",
		"--sequential sets parallel FIO off. --sequential=false turns parallel back on.",
		"--app-type is a comma-separated list of tools.fio.app_suites profiles. Built-ins are nfs, vm, database, messaging, and ai. The selected suites run on every PVC; standard suites and CephFS RWX are skipped.",
		"Every run deletes and recreates --namespace (default odf-io-stress) at the start. --no-cleanup skips cleanup on exit and does not stop that reset.",
		"--dry-run prints YAML manifests on stdout and does not write summary.json.",
		"--results-dir empty means results/<timestamp>. Pass it so the output path is known before the run.",
		"--kubeconfig empty uses KUBECONFIG or ~/.kube/config.",
		"--max-parallel 0 means unlimited concurrent pods. --snapshot-class empty auto-selects the VolumeSnapshotClass.",
		"--format is the FIO output format: json or normal.",
		"tools.vdbench and tools.smallfiles in a config file are ignored.",
		"generate-config writes a sample config. -o - writes it to stdout. --force overwrites an existing file. It does not run a workload.",
	}
}
