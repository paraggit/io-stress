package config

import (
	"fmt"
	"time"
)

type Config struct {
	NumPVC             int
	Namespace          string
	RBDStorageClass    string
	CephFSStorageClass string
	PVCSize            string
	FIOImage           string
	FIORuntime         int
	FIOBlockSize       string
	FIOOffset          string
	FIOSize            string
	Prefix             string
	WaitTimeout        time.Duration
	OutputFormat       string
	Parallel           bool
	NoCleanup          bool
	DryRun             bool
	LifecycleInterval  int
	SkipLifecycle      bool
	SkipFIOStress      bool
	ExpandFactor       int
	SnapshotClass      string
	SustainRuntime     int
	MaxParallelPods    int
	ResultsDir         string
}

func NewDefault() *Config {
	return &Config{
		NumPVC:             4,
		Namespace:          "odf-io-stress",
		RBDStorageClass:    "ocs-storagecluster-ceph-rbd",
		CephFSStorageClass: "ocs-storagecluster-cephfs",
		PVCSize:            "10Gi",
		FIOImage:           "quay.io/ocsci/nginx:fio",
		FIORuntime:         60,
		FIOBlockSize:       "512",
		FIOOffset:          "512",
		FIOSize:            "1G",
		Prefix:             "odf-io",
		WaitTimeout:        5 * time.Minute,
		OutputFormat:       "json",
		Parallel:           true,
		LifecycleInterval:  4,
		ExpandFactor:       2,
		SustainRuntime:     180,
	}
}

func Validate(cfg *Config) error {
	if cfg.NumPVC < 1 {
		return fmt.Errorf("num-pvc must be >= 1, got %d", cfg.NumPVC)
	}
	if cfg.Namespace == "" {
		return fmt.Errorf("namespace must not be empty")
	}
	if cfg.RBDStorageClass == "" {
		return fmt.Errorf("rbd-storage-class must not be empty")
	}
	if cfg.CephFSStorageClass == "" {
		return fmt.Errorf("cephfs-storage-class must not be empty")
	}
	if cfg.PVCSize == "" {
		return fmt.Errorf("pvc-size must not be empty")
	}
	if cfg.FIOImage == "" {
		return fmt.Errorf("image must not be empty")
	}
	if cfg.FIORuntime < 1 {
		return fmt.Errorf("runtime must be >= 1, got %d", cfg.FIORuntime)
	}
	if cfg.ExpandFactor < 1 {
		return fmt.Errorf("expand-factor must be >= 1, got %d", cfg.ExpandFactor)
	}
	return nil
}
