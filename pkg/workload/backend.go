package workload

import (
	"context"
	"fmt"
	"strings"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/objstore"
)

// objectBackend is the object target for one run, resolved after Validate.
// FallbackEndpoint is not copied into Object.Endpoint; binding prefers an
// explicit endpoint, then the OBC endpoint, then this fallback.
type objectBackend struct {
	Name             string
	StorageClass     string
	BucketClass      string
	FallbackEndpoint string
	ReportStorage    string
}

// ResolveObjectBackend requires the object workload to be enabled.
// NooBaa uses the configured class and objstore.DefaultEndpoint(ODFNamespace).
// RGW discovers an empty storage class and, when Object.Endpoint is empty, the
// CephObjectStore URL. Discovery errors are returned unchanged.
func ResolveObjectBackend(ctx context.Context, c *k8s.Client, cfg *config.Config) (objectBackend, error) {
	if cfg == nil || !cfg.Tools.Object.Enabled {
		return objectBackend{}, fmt.Errorf("object workload is not enabled")
	}
	switch cfg.Tools.Object.Backend {
	case config.BackendRGW:
		return resolveRGW(ctx, c, cfg)
	case config.BackendNooBaa, "":
		return resolveNooBaa(cfg), nil
	default:
		return objectBackend{}, fmt.Errorf("object backend must be noobaa or rgw, got %q", cfg.Tools.Object.Backend)
	}
}

func resolveNooBaa(cfg *config.Config) objectBackend {
	obj := cfg.Tools.Object
	return objectBackend{
		Name:             config.BackendNooBaa,
		StorageClass:     obj.StorageClass,
		BucketClass:      obj.BucketClass,
		FallbackEndpoint: objstore.DefaultEndpoint(obj.ODFNamespace),
		ReportStorage:    config.BackendNooBaa,
	}
}

func resolveRGW(ctx context.Context, c *k8s.Client, cfg *config.Config) (objectBackend, error) {
	obj := &cfg.Tools.Object
	class := strings.TrimSpace(obj.StorageClass)
	if class == "" {
		discovered, err := k8s.DiscoverRGWStorageClass(ctx, c, obj.ODFNamespace)
		if err != nil {
			return objectBackend{}, err
		}
		class = discovered
		obj.StorageClass = discovered
	}
	var fallback string
	if strings.TrimSpace(obj.Endpoint) == "" {
		// DiscoverRGWEndpoint returns a full URL. Keep it on the backend
		// only; binding prefers a non-empty OBC endpoint over this fallback.
		ep, err := k8s.DiscoverRGWEndpoint(ctx, c, obj.ODFNamespace)
		if err != nil {
			return objectBackend{}, err
		}
		fallback = ep
	}
	// RGW has no OBC bucket class. Clearing it lets dry-run omit additionalConfig.
	obj.BucketClass = ""
	return objectBackend{
		Name:             config.BackendRGW,
		StorageClass:     class,
		BucketClass:      "",
		FallbackEndpoint: fallback,
		ReportStorage:    config.BackendRGW,
	}, nil
}
