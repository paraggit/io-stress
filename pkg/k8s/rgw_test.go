package k8s

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func rgwTestClient(t *testing.T, ns string, storageClasses []*storagev1.StorageClass, dynObjs []*unstructured.Unstructured, svcs ...*corev1.Service) *Client {
	t.Helper()
	scheme := runtime.NewScheme()
	kinds := map[schema.GroupVersionResource]string{
		cephObjectStoreGVR: "CephObjectStoreList",
		cephClusterGVR:     "CephClusterList",
	}
	args := make([]runtime.Object, len(dynObjs))
	for i, o := range dynObjs {
		args[i] = o
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, kinds, args...)
	csArgs := make([]runtime.Object, 0, len(storageClasses)+len(svcs))
	for _, sc := range storageClasses {
		csArgs = append(csArgs, sc)
	}
	for _, svc := range svcs {
		csArgs = append(csArgs, svc)
	}
	return &Client{
		Clientset: k8sfake.NewSimpleClientset(csArgs...),
		Dynamic:   dyn,
	}
}

func storageClass(name, provisioner string) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Provisioner: provisioner,
	}
}

func cephObjectStore(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "ceph.rook.io/v1",
		"kind":       "CephObjectStore",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
	}}
}

func cephCluster(name, namespace string, bytesAvailable *int64) *unstructured.Unstructured {
	obj := map[string]interface{}{
		"apiVersion": "ceph.rook.io/v1",
		"kind":       "CephCluster",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
	}
	if bytesAvailable != nil {
		obj["status"] = map[string]interface{}{
			"ceph": map[string]interface{}{
				"capacity": map[string]interface{}{
					"bytesAvailable": *bytesAvailable,
				},
			},
		}
	}
	return &unstructured.Unstructured{Object: obj}
}

func rgwService(store, namespace string, port int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-rgw-" + store,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: port}},
		},
	}
}

func TestDiscoverRGWStorageClass(t *testing.T) {
	const ns = "openshift-storage"
	ctx := context.Background()

	tests := []struct {
		name    string
		classes []*storagev1.StorageClass
		want    string
		errSub  string
	}{
		{
			name: "internal",
			classes: []*storagev1.StorageClass{
				storageClass("ocs-storagecluster-ceph-rgw", RGWBucketProvisioner),
			},
			want: "ocs-storagecluster-ceph-rgw",
		},
		{
			name: "external",
			classes: []*storagev1.StorageClass{
				storageClass("ocs-external-storagecluster-ceph-rgw", RGWBucketProvisioner),
			},
			want: "ocs-external-storagecluster-ceph-rgw",
		},
		{
			name:    "zero matches",
			classes: []*storagev1.StorageClass{storageClass("openshift-storage.noobaa.io", "noobaa.io/bucket")},
			errSub:  RGWBucketProvisioner,
		},
		{
			name: "two matches",
			classes: []*storagev1.StorageClass{
				storageClass("ocs-storagecluster-ceph-rgw", RGWBucketProvisioner),
				storageClass("ocs-external-storagecluster-ceph-rgw", RGWBucketProvisioner),
			},
			errSub: "--object-storage-class",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := rgwTestClient(t, ns, tt.classes, nil)
			got, err := DiscoverRGWStorageClass(ctx, c, ns)
			if tt.errSub != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Fatalf("error = %v", err)
				}
				if !strings.Contains(err.Error(), ns) {
					t.Fatalf("error should name namespace: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiscoverRGWEndpoint(t *testing.T) {
	const ns = "openshift-storage"
	const store = "ocs-storagecluster"
	ctx := context.Background()

	tests := []struct {
		name    string
		stores  []*unstructured.Unstructured
		svc     *corev1.Service
		want    string
		errSub  string
	}{
		{
			name:   "port 80",
			stores: []*unstructured.Unstructured{cephObjectStore(store, ns)},
			svc:    rgwService(store, ns, 80),
			want:   "http://rook-ceph-rgw-" + store + "." + ns + ".svc",
		},
		{
			name:   "port 443",
			stores: []*unstructured.Unstructured{cephObjectStore(store, ns)},
			svc:    rgwService(store, ns, 443),
			want:   "https://rook-ceph-rgw-" + store + "." + ns + ".svc",
		},
		{
			name:   "zero stores",
			stores: nil,
			errSub: ns,
		},
		{
			name: "two stores",
			stores: []*unstructured.Unstructured{
				cephObjectStore("alpha", ns),
				cephObjectStore("beta", ns),
			},
			errSub: "alpha",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var svcs []*corev1.Service
			if tt.svc != nil {
				svcs = append(svcs, tt.svc)
			}
			c := rgwTestClient(t, ns, nil, tt.stores, svcs...)
			got, err := DiscoverRGWEndpoint(ctx, c, ns)
			if tt.errSub != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCephFreeBytes(t *testing.T) {
	const ns = "openshift-storage"
	ctx := context.Background()

	avail := int64(42_000_000_000)
	zAvail := int64(99)
	aAvail := int64(1)

	tests := []struct {
		name     string
		clusters []*unstructured.Unstructured
		wantFree int64
		wantFound bool
		wantErr  bool
	}{
		{
			name:      "bytesAvailable present",
			clusters:  []*unstructured.Unstructured{cephCluster("ocs", ns, &avail)},
			wantFree:  avail,
			wantFound: true,
		},
		{
			name:      "field absent",
			clusters:  []*unstructured.Unstructured{cephCluster("ocs", ns, nil)},
			wantFound: false,
		},
		{
			name:      "no CephCluster",
			clusters:  nil,
			wantFound: false,
		},
		{
			name: "two clusters pick lexicographic first",
			clusters: []*unstructured.Unstructured{
				cephCluster("cluster-z", ns, &zAvail),
				cephCluster("cluster-a", ns, &aAvail),
			},
			wantFree:  aAvail,
			wantFound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := rgwTestClient(t, ns, nil, tt.clusters)
			free, found, err := CephFreeBytes(ctx, c, ns)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if free != tt.wantFree {
				t.Fatalf("free = %d, want %d", free, tt.wantFree)
			}
		})
	}
}

func TestStorageClassExists(t *testing.T) {
	ctx := context.Background()
	c := rgwTestClient(t, "ns", []*storagev1.StorageClass{
		storageClass("my-class", "example.com/provisioner"),
	}, nil)

	if err := StorageClassExists(ctx, c, "my-class"); err != nil {
		t.Fatal(err)
	}
	if err := StorageClassExists(ctx, c, "missing"); err == nil {
		t.Fatal("expected error for missing class")
	} else if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v", err)
	}
}
