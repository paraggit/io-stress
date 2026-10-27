package config

import (
	"fmt"
	"time"
)

// standardObject is the built-in object block. Enabled stays false so a FIO
// run that never sets the object workload is unchanged. generate-config emits
// this block with the standard profile expanded.
func standardObject() Object {
	return Object{
		Enabled:                 false,
		Profile:                 "standard",
		Image:                   DefaultObjectImage,
		NooBaaNamespace:         "openshift-storage",
		Provisioning:            "obc",
		StorageClass:            "openshift-storage.noobaa.io",
		Region:                  "us-east-1",
		Buckets:                 10,
		BucketCreateConcurrency: 10,
		VersionedBuckets:        2,
		VersionsPerKey:          3,
		ObjectsPerBucket:        5000,
		KeyPrefix:               "objstress",
		KeyLayout:               "hashed",
		SizeMix: []SizeWeight{
			{Size: "4KB", Weight: 60},
			{Size: "1MB", Weight: 30},
			{Size: "16MB", Weight: 10},
		},
		Workers:           4,
		WorkerConcurrency: 32,
		WorkerResources: Resources{
			Requests: ResourceQty{CPU: "200m", Memory: "256Mi"},
			Limits:   ResourceQty{CPU: "2", Memory: "2Gi"},
		},
		Multipart: Multipart{
			Enabled:    true,
			Threshold:  "16MB",
			PartSize:   "8MB",
			AbortCases: 2,
		},
		VerifyMode:            "full",
		VerifySamplePct:       1,
		RangeReadsPct:         5,
		Phases:                append([]string(nil), ObjectPhases...),
		DeleteMode:            "both",
		OpTimeout:             defaultOpTimeout,
		PhaseTimeout:          defaultPhaseTimeout,
		OBCDeleteTimeout:      defaultOBCTimeout,
		MaxRetries:            5,
		ErrorRateThresholdPct: 0,
		CapacityPreflight:     true,
	}
}

func objectProfile(name string) (Object, error) {
	switch name {
	case "standard":
		return standardObject(), nil
	case "smoke":
		o := standardObject()
		o.Profile = "smoke"
		o.Buckets = 2
		o.BucketCreateConcurrency = 2
		o.VersionedBuckets = 1
		o.ObjectsPerBucket = 500
		o.SizeMix = []SizeWeight{
			{Size: "4KB", Weight: 80},
			{Size: "1MB", Weight: 20},
		}
		o.Workers = 2
		o.WorkerConcurrency = 8
		o.VerifyMode = "full"
		o.Multipart.AbortCases = 1
		return o, nil
	case "scale":
		o := standardObject()
		o.Profile = "scale"
		o.Buckets = 50
		o.BucketCreateConcurrency = 10
		o.VersionedBuckets = 2
		o.ObjectsPerBucket = 20000
		o.SizeMix = []SizeWeight{
			{Size: "4KB", Weight: 70},
			{Size: "16KB", Weight: 20},
			{Size: "64KB", Weight: 10},
		}
		o.Workers = 16
		o.WorkerConcurrency = 64
		o.VerifyMode = "sample"
		o.VerifySamplePct = 1
		return o, nil
	case "large":
		o := standardObject()
		o.Profile = "large"
		o.Buckets = 4
		o.BucketCreateConcurrency = 4
		o.VersionedBuckets = 1
		o.ObjectsPerBucket = 50
		o.SizeMix = []SizeWeight{
			{Size: "100MB", Weight: 50},
			{Size: "1GB", Weight: 40},
			{Size: "5GB", Weight: 10},
		}
		o.Workers = 4
		o.WorkerConcurrency = 4
		o.VerifyMode = "full"
		o.OpTimeout = Duration(15 * time.Minute) // 5GB parts need more than 60s
		return o, nil
	case "custom":
		return Object{}, fmt.Errorf("custom profile is not expanded")
	default:
		return Object{}, fmt.Errorf("unknown object profile %q (want smoke, standard, scale, large, or custom)", name)
	}
}

// ApplyObjectProfile replaces workload shape fields with the named profile.
// Run-mode fields (enabled, only, resume, force, seed) are kept so a profile
// flag cannot turn the workload on or change the seed. Explicit flags applied
// after this call still win. custom leaves the current fields in place.
func ApplyObjectProfile(obj *Object, name string) error {
	if obj == nil {
		return fmt.Errorf("nil object config")
	}
	if name == "custom" {
		obj.Profile = "custom"
		return nil
	}
	next, err := objectProfile(name)
	if err != nil {
		return err
	}
	enabled, only, resume, force, seed := obj.Enabled, obj.Only, obj.Resume, obj.Force, obj.Seed
	*obj = next
	obj.Enabled = enabled
	obj.Only = only
	obj.Resume = resume
	obj.Force = force
	obj.Seed = seed
	obj.Profile = name
	return nil
}
