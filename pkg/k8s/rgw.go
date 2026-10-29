package k8s

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const RGWBucketProvisioner = "openshift-storage.ceph.rook.io/bucket"

var (
	cephObjectStoreGVR = schema.GroupVersionResource{Group: "ceph.rook.io", Version: "v1", Resource: "cephobjectstores"}
	cephClusterGVR     = schema.GroupVersionResource{Group: "ceph.rook.io", Version: "v1", Resource: "cephclusters"}
)

// DiscoverRGWStorageClass finds the cluster StorageClass whose provisioner is
// RGWBucketProvisioner. namespace is included in error text only.
func DiscoverRGWStorageClass(ctx context.Context, c *Client, namespace string) (string, error) {
	if c == nil || c.Clientset == nil {
		return "", fmt.Errorf("kubernetes client not configured")
	}
	list, err := c.Clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list storageclasses: %w", err)
	}
	var matches []string
	for i := range list.Items {
		if list.Items[i].Provisioner == RGWBucketProvisioner {
			matches = append(matches, list.Items[i].Name)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no StorageClass with provisioner %q in namespace %q", RGWBucketProvisioner, namespace)
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", fmt.Errorf("multiple StorageClasses with provisioner %q: %s; pass --object-storage-class",
			RGWBucketProvisioner, strings.Join(matches, ", "))
	}
}

// DiscoverRGWEndpoint reads the sole CephObjectStore in namespace and builds
// an in-cluster URL from the rook-ceph-rgw-<store> Service.
func DiscoverRGWEndpoint(ctx context.Context, c *Client, namespace string) (string, error) {
	dc, err := dynamicClient(c)
	if err != nil {
		return "", err
	}
	list, err := dc.Resource(cephObjectStoreGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list cephobjectstores in namespace %q: %w", namespace, err)
	}
	switch len(list.Items) {
	case 0:
		return "", fmt.Errorf("no CephObjectStore in namespace %q", namespace)
	case 1:
	default:
		names := make([]string, len(list.Items))
		for i := range list.Items {
			names[i] = list.Items[i].GetName()
		}
		sort.Strings(names)
		return "", fmt.Errorf("multiple CephObjectStores in namespace %q: %s", namespace, strings.Join(names, ", "))
	}
	store := list.Items[0].GetName()
	svcName := "rook-ceph-rgw-" + store
	if c == nil || c.Clientset == nil {
		return "", fmt.Errorf("kubernetes client not configured")
	}
	svc, err := c.Clientset.CoreV1().Services(namespace).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("service %q not found in namespace %q", svcName, namespace)
		}
		return "", fmt.Errorf("get service %q: %w", svcName, err)
	}
	if len(svc.Spec.Ports) == 0 {
		return "", fmt.Errorf("service %q has no ports", svcName)
	}
	port := svc.Spec.Ports[0].Port
	host := svcName + "." + namespace + ".svc"
	return rgwEndpointURL(host, port), nil
}

func rgwEndpointURL(host string, port int32) string {
	portStr := strconv.Itoa(int(port))
	scheme := "https"
	if port == 80 {
		scheme = "http"
	}
	if portStr == "" || portStr == "443" || portStr == "80" {
		return scheme + "://" + host
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, portStr)
}

// CephFreeBytes reads raw bytesAvailable from the lexicographically first
// CephCluster in namespace. found is false when no cluster or no capacity field.
func CephFreeBytes(ctx context.Context, c *Client, namespace string) (free int64, found bool, err error) {
	dc, err := dynamicClient(c)
	if err != nil {
		return 0, false, err
	}
	list, err := dc.Resource(cephClusterGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, false, fmt.Errorf("list cephclusters: %w", err)
	}
	if len(list.Items) == 0 {
		return 0, false, nil
	}
	names := make([]string, len(list.Items))
	byName := make(map[string]*unstructured.Unstructured, len(list.Items))
	for i := range list.Items {
		name := list.Items[i].GetName()
		names[i] = name
		byName[name] = &list.Items[i]
	}
	sort.Strings(names)
	cluster := byName[names[0]]
	n, ok, err := unstructured.NestedInt64(cluster.Object, "status", "ceph", "capacity", "bytesAvailable")
	if err != nil {
		return 0, false, fmt.Errorf("read bytesAvailable on CephCluster %q: %w", names[0], err)
	}
	if !ok {
		return 0, false, nil
	}
	return n, true, nil
}

// StorageClassExists returns nil when the cluster StorageClass is present.
func StorageClassExists(ctx context.Context, c *Client, name string) error {
	if c == nil || c.Clientset == nil {
		return fmt.Errorf("kubernetes client not configured")
	}
	_, err := c.Clientset.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("StorageClass %q not found", name)
	}
	return err
}
