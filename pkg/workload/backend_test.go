package workload

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

const (
	testODFNamespace = "openshift-storage"
	testRGWClass     = "ocs-storagecluster-ceph-rgw"
	testRGWStore     = "ocs-storagecluster"
	testRGWEndpoint  = "http://rook-ceph-rgw-ocs-storagecluster.openshift-storage.svc"
)

func enabledObjectConfig(t *testing.T, backend string) *config.Config {
	t.Helper()
	cfg := config.NewDefault()
	cfg.Tools.Object.Enabled = true
	cfg.Tools.Object.Backend = backend
	if err := config.Validate(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func rgwDiscoveryClient(t *testing.T, className string) *k8s.Client {
	t.Helper()
	store := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "ceph.rook.io/v1",
		"kind":       "CephObjectStore",
		"metadata": map[string]interface{}{
			"name":      testRGWStore,
			"namespace": testODFNamespace,
		},
	}}
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		{Group: "ceph.rook.io", Version: "v1", Resource: "cephobjectstores"}: "CephObjectStoreList",
		{Group: "ceph.rook.io", Version: "v1", Resource: "cephclusters"}:     "CephClusterList",
	}, store)
	objs := []runtime.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rook-ceph-rgw-" + testRGWStore,
				Namespace: testODFNamespace,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Port: 80}},
			},
		},
	}
	if className != "" {
		objs = append(objs, &storagev1.StorageClass{
			ObjectMeta:  metav1.ObjectMeta{Name: className},
			Provisioner: k8s.RGWBucketProvisioner,
		})
	}
	return &k8s.Client{
		Clientset: k8sfake.NewSimpleClientset(objs...),
		Dynamic:   dyn,
	}
}

func TestResolveObjectBackendRGW(t *testing.T) {
	cfg := enabledObjectConfig(t, config.BackendRGW)
	if cfg.Tools.Object.ODFNamespace != testODFNamespace {
		t.Fatalf("ODFNamespace = %q", cfg.Tools.Object.ODFNamespace)
	}
	if cfg.Tools.Object.StorageClass != "" {
		t.Fatalf("storage class = %q, want empty before discovery", cfg.Tools.Object.StorageClass)
	}
	cfg.Tools.Object.BucketClass = "noobaa-default-bucket-class"

	got, err := ResolveObjectBackend(context.Background(), rgwDiscoveryClient(t, testRGWClass), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Object.StorageClass != testRGWClass {
		t.Fatalf("storage class = %q, want %q", cfg.Tools.Object.StorageClass, testRGWClass)
	}
	if got.StorageClass != testRGWClass {
		t.Fatalf("resolved class = %q, want %q", got.StorageClass, testRGWClass)
	}
	if got.BucketClass != "" || cfg.Tools.Object.BucketClass != "" {
		t.Fatalf("bucket class = result %q config %q, want empty", got.BucketClass, cfg.Tools.Object.BucketClass)
	}
	if got.ReportStorage != "rgw" || got.Name != "rgw" {
		t.Fatalf("backend = %+v", got)
	}
	if got.FallbackEndpoint != testRGWEndpoint {
		t.Fatalf("fallback = %q, want %q", got.FallbackEndpoint, testRGWEndpoint)
	}
	if cfg.Tools.Object.Endpoint != "" {
		t.Fatalf("Object.Endpoint = %q, want empty", cfg.Tools.Object.Endpoint)
	}
}

func TestResolveObjectBackendNooBaa(t *testing.T) {
	cfg := enabledObjectConfig(t, config.BackendNooBaa)
	if cfg.Tools.Object.StorageClass != config.DefaultNooBaaStorageClass {
		t.Fatalf("storage class = %q", cfg.Tools.Object.StorageClass)
	}
	// No StorageClass objects: listing them must not be required.
	c := &k8s.Client{Clientset: k8sfake.NewSimpleClientset()}
	got, err := ResolveObjectBackend(context.Background(), c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportStorage != "noobaa" || got.Name != "noobaa" {
		t.Fatalf("backend = %+v", got)
	}
	if cfg.Tools.Object.StorageClass != config.DefaultNooBaaStorageClass || got.StorageClass != config.DefaultNooBaaStorageClass {
		t.Fatalf("storage class config %q resolved %q", cfg.Tools.Object.StorageClass, got.StorageClass)
	}
	if got.BucketClass != "" {
		t.Fatalf("bucket class = %q, want empty", got.BucketClass)
	}
	if got.FallbackEndpoint != "https://s3.openshift-storage.svc:443" {
		t.Fatalf("fallback = %q", got.FallbackEndpoint)
	}
}

func TestResolveObjectBackendMissingClass(t *testing.T) {
	cfg := enabledObjectConfig(t, config.BackendRGW)
	c := &k8s.Client{Clientset: k8sfake.NewSimpleClientset(&storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: config.DefaultNooBaaStorageClass},
		Provisioner: "noobaa.io/obc",
	})}
	_, err := ResolveObjectBackend(context.Background(), c, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "openshift-storage.ceph.rook.io/bucket") {
		t.Fatalf("error = %v", err)
	}
	if cfg.Tools.Object.StorageClass == config.DefaultNooBaaStorageClass {
		t.Fatal("missing RGW class fell through to the NooBaa storage class")
	}
}

func TestResolveObjectBackendRGWExplicitEndpoint(t *testing.T) {
	cfg := enabledObjectConfig(t, config.BackendRGW)
	cfg.Tools.Object.StorageClass = "my-rgw"
	cfg.Tools.Object.Endpoint = "https://rgw.example:443"
	c := &k8s.Client{Clientset: k8sfake.NewSimpleClientset()}
	got, err := ResolveObjectBackend(context.Background(), c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.FallbackEndpoint != "" {
		t.Fatalf("fallback = %q, want empty when endpoint is explicit", got.FallbackEndpoint)
	}
	if cfg.Tools.Object.Endpoint != "https://rgw.example:443" {
		t.Fatalf("endpoint = %q", cfg.Tools.Object.Endpoint)
	}
	if cfg.Tools.Object.StorageClass != "my-rgw" || got.StorageClass != "my-rgw" {
		t.Fatalf("class config %q resolved %q", cfg.Tools.Object.StorageClass, got.StorageClass)
	}
	if got.BucketClass != "" || got.ReportStorage != "rgw" {
		t.Fatalf("backend = %+v", got)
	}
}

func TestBindingPrefersOBCEndpoint(t *testing.T) {
	cfg := config.NewDefault()
	const fallback = "http://rook-ceph-rgw-ocs-storagecluster.openshift-storage.svc"
	const obcEndpoint = "http://s3.obc.example"
	o := &objectRun{
		cfg:     cfg,
		backend: objectBackend{Name: "rgw", FallbackEndpoint: fallback, ReportStorage: "rgw"},
	}
	got := o.binding(0, "claim", k8s.BucketBinding{Endpoint: obcEndpoint, Name: "bucket"})
	if got.Endpoint != obcEndpoint {
		t.Fatalf("endpoint = %q, want OBC %q", got.Endpoint, obcEndpoint)
	}
	got = o.binding(0, "claim", k8s.BucketBinding{Name: "bucket"})
	if got.Endpoint != fallback {
		t.Fatalf("endpoint = %q, want fallback %q", got.Endpoint, fallback)
	}
	cfg.Tools.Object.Endpoint = "https://explicit.example"
	got = o.binding(0, "claim", k8s.BucketBinding{Endpoint: obcEndpoint, Name: "bucket"})
	if got.Endpoint != "https://explicit.example" {
		t.Fatalf("endpoint = %q, want explicit", got.Endpoint)
	}
}

func TestProvisionMissingStorageClass(t *testing.T) {
	const ns = "odf-io-stress"
	const className = "ocs-storagecluster-ceph-rgw"
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		k8s.OBCGVR: "ObjectBucketClaimList",
	})
	client := &k8s.Client{
		Clientset: k8sfake.NewSimpleClientset(),
		Dynamic:   dyn,
	}
	cfg := config.NewDefault()
	cfg.Cluster.Namespace = ns
	cfg.Tools.Object.Buckets = 1
	cfg.Tools.Object.Provisioning = "obc"
	cfg.Tools.Object.StorageClass = className
	cfg.Tools.Object.BucketCreateConcurrency = 1
	cfg.Tools.Object.PhaseTimeout = config.Duration(200 * time.Millisecond)
	o := &objectRun{
		cfg:       cfg,
		client:    client,
		collector: report.NewCollector(),
		backend:   objectBackend{Name: "rgw", StorageClass: className, ReportStorage: "rgw"},
	}
	err := o.provision(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), className) || !strings.Contains(err.Error(), "rgw") {
		t.Fatalf("error = %v", err)
	}
	list, listErr := dyn.Resource(k8s.OBCGVR).Namespace(ns).List(context.Background(), metav1.ListOptions{})
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(list.Items) != 0 {
		t.Fatalf("created %d OBC(s) before the storage class check", len(list.Items))
	}
}
