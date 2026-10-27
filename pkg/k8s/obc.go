package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// OBCGVR is the ObjectBucketClaim resource. ObjectBucket is cluster-scoped.
var (
	OBCGVR = schema.GroupVersionResource{Group: "objectbucket.io", Version: "v1alpha1", Resource: "objectbucketclaims"}
	OBGVR  = schema.GroupVersionResource{Group: "objectbucket.io", Version: "v1alpha1", Resource: "objectbuckets"}
)

// OBCSpec is an ObjectBucketClaim with a deterministic bucket name.
type OBCSpec struct {
	Name         string
	Namespace    string
	StorageClass string
	BucketName   string
	BucketClass  string
	Labels       map[string]string
}

// BucketBinding is what a Bound claim hands the worker: endpoint and creds.
type BucketBinding struct {
	Name             string
	OBCName          string
	ObjectBucketName string
	Phase            string
	Endpoint         string
	Region           string
	SecretName       string
	ConfigMapName    string
	StatusJSON       string
}

// BuildOBC renders a claim. spec.bucketName is set; generateBucketName is not.
func BuildOBC(spec OBCSpec) *unstructured.Unstructured {
	body := map[string]interface{}{
		"bucketName":       spec.BucketName,
		"storageClassName": spec.StorageClass,
	}
	if spec.BucketClass != "" {
		body["additionalConfig"] = map[string]interface{}{"bucketclass": spec.BucketClass}
	}
	labels := map[string]interface{}{}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	obj := map[string]interface{}{
		"apiVersion": "objectbucket.io/v1alpha1",
		"kind":       "ObjectBucketClaim",
		"metadata": map[string]interface{}{
			"name":      spec.Name,
			"namespace": spec.Namespace,
		},
		"spec": body,
	}
	if len(labels) > 0 {
		obj["metadata"].(map[string]interface{})["labels"] = labels
	}
	return &unstructured.Unstructured{Object: obj}
}

// CreateOBC creates the claim. An existing claim with the same bucket name is
// reused. A different bucket name is a collision, not a reason to pick a new one.
func CreateOBC(ctx context.Context, c *Client, spec OBCSpec) error {
	dc, err := dynamicClient(c)
	if err != nil {
		return err
	}
	_, err = dc.Resource(OBCGVR).Namespace(spec.Namespace).Create(ctx, BuildOBC(spec), metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := dc.Resource(OBCGVR).Namespace(spec.Namespace).Get(ctx, spec.Name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("OBC %s already exists and could not be read: %w", spec.Name, getErr)
		}
		got, _, _ := unstructured.NestedString(existing.Object, "spec", "bucketName")
		if got != spec.BucketName {
			return fmt.Errorf("OBC %s already exists with bucketName %q, want %q; pick a different seed or clean up", spec.Name, got, spec.BucketName)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create OBC %s: %w", spec.Name, err)
	}
	return nil
}

// GetOBC reads a claim. A missing claim returns a NotFound error.
func GetOBC(ctx context.Context, c *Client, namespace, name string) (*unstructured.Unstructured, error) {
	dc, err := dynamicClient(c)
	if err != nil {
		return nil, err
	}
	u, err := dc.Resource(OBCGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return u, nil
}

// WaitOBCBound waits until status.phase is Bound, then reads the claim's
// ConfigMap and Secret. On timeout the error includes status and events.
func WaitOBCBound(ctx context.Context, c *Client, namespace, name string, timeout time.Duration) (BucketBinding, error) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last *unstructured.Unstructured
	for {
		u, err := GetOBC(ctx, c, namespace, name)
		if err != nil && !apierrors.IsNotFound(err) && ctx.Err() == nil {
			return BucketBinding{}, err
		}
		if err == nil {
			last = u
			phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
			switch phase {
			case "Bound":
				return readBinding(ctx, c, namespace, name, u)
			case "Failed", "Released":
				return BucketBinding{}, fmt.Errorf("OBC %s phase %s: %s", name, phase, stuckText(namespace, name, "", u, c))
			}
		}
		select {
		case <-ctx.Done():
			return BucketBinding{}, fmt.Errorf("OBC %s did not become Bound within %s: %s", name, timeout, stuckText(namespace, name, "", last, c))
		case <-ticker.C:
		}
	}
}

func readBinding(ctx context.Context, c *Client, namespace, obcName string, u *unstructured.Unstructured) (BucketBinding, error) {
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	obName, _, _ := unstructured.NestedString(u.Object, "status", "objectBucketName")
	status, _ := json.Marshal(nested(u, "status"))
	cm, err := c.Clientset.CoreV1().ConfigMaps(namespace).Get(ctx, obcName, metav1.GetOptions{})
	if err != nil {
		return BucketBinding{}, fmt.Errorf("OBC %s bound but configmap missing: %w", obcName, err)
	}
	if _, err := c.Clientset.CoreV1().Secrets(namespace).Get(ctx, obcName, metav1.GetOptions{}); err != nil {
		return BucketBinding{}, fmt.Errorf("OBC %s bound but secret missing: %w", obcName, err)
	}
	host := cm.Data["BUCKET_HOST"]
	port := cm.Data["BUCKET_PORT"]
	bucket := cm.Data["BUCKET_NAME"]
	if bucket == "" {
		bucket, _, _ = unstructured.NestedString(u.Object, "spec", "bucketName")
	}
	region := cm.Data["BUCKET_REGION"]
	if region == "" {
		region = "us-east-1"
	}
	return BucketBinding{
		Name:             bucket,
		OBCName:          obcName,
		ObjectBucketName: obName,
		Phase:            phase,
		Endpoint:         EndpointFromHostPort(host, port),
		Region:           region,
		SecretName:       obcName,
		ConfigMapName:    obcName,
		StatusJSON:       string(status),
	}, nil
}

// EndpointFromHostPort builds the worker's S3 base URL from an OBC ConfigMap.
func EndpointFromHostPort(host, port string) string {
	if host == "" {
		return ""
	}
	scheme := "https"
	if port == "80" {
		scheme = "http"
	}
	if port == "" || port == "443" || port == "80" {
		return scheme + "://" + host
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

// DeleteOBC removes the claim. NotFound is success.
func DeleteOBC(ctx context.Context, c *Client, namespace, name string) error {
	dc, err := dynamicClient(c)
	if err != nil {
		return err
	}
	err = dc.Resource(OBCGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete OBC %s: %w", name, err)
	}
	return nil
}

// WaitOBCGone waits until the claim and its ObjectBucket are both gone.
// On timeout the error includes finalizers, status, and events.
func WaitOBCGone(ctx context.Context, c *Client, namespace, name, obName string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last *unstructured.Unstructured
	for {
		gone, u, err := obcGone(ctx, c, namespace, name, obName)
		if err != nil && ctx.Err() == nil {
			return err
		}
		if gone {
			return nil
		}
		if u != nil {
			last = u
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("OBC %s did not disappear within %s: %s", name, timeout, stuckText(namespace, name, obName, last, c))
		case <-ticker.C:
		}
	}
}

func obcGone(ctx context.Context, c *Client, namespace, name, obName string) (bool, *unstructured.Unstructured, error) {
	dc, err := dynamicClient(c)
	if err != nil {
		return false, nil, err
	}
	u, err := dc.Resource(OBCGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		if ctx.Err() != nil {
			return false, nil, nil
		}
		return false, nil, err
	}
	if err == nil {
		return false, u, nil
	}
	if obName == "" {
		return true, nil, nil
	}
	_, obErr := dc.Resource(OBGVR).Get(ctx, obName, metav1.GetOptions{})
	if apierrors.IsNotFound(obErr) {
		return true, nil, nil
	}
	if obErr != nil && ctx.Err() != nil {
		return false, nil, nil
	}
	if obErr != nil {
		return false, nil, obErr
	}
	return false, nil, nil
}

func stuckText(namespace, name, obName string, u *unstructured.Unstructured, c *Client) string {
	readCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if u == nil && c != nil {
		got, err := GetOBC(readCtx, c, namespace, name)
		if err == nil {
			u = got
		}
	}
	var b strings.Builder
	if u != nil {
		fins, _, _ := unstructured.NestedStringSlice(u.Object, "metadata", "finalizers")
		status, _ := json.Marshal(nested(u, "status"))
		fmt.Fprintf(&b, "finalizers=%v status=%s", fins, status)
	} else {
		b.WriteString("claim not readable")
	}
	if c != nil {
		if ev := Events(readCtx, c, namespace, name, "ObjectBucketClaim"); ev != "" {
			fmt.Fprintf(&b, " events=[%s]", ev)
		}
		if obName != "" {
			if ob, err := objectBucket(readCtx, c, obName); err == nil && ob != nil {
				fins, _, _ := unstructured.NestedStringSlice(ob.Object, "metadata", "finalizers")
				fmt.Fprintf(&b, " objectBucketFinalizers=%v", fins)
			}
		}
	}
	return b.String()
}

func objectBucket(ctx context.Context, c *Client, name string) (*unstructured.Unstructured, error) {
	dc, err := dynamicClient(c)
	if err != nil {
		return nil, err
	}
	return dc.Resource(OBGVR).Get(ctx, name, metav1.GetOptions{})
}

func nested(u *unstructured.Unstructured, field string) interface{} {
	if u == nil {
		return nil
	}
	v, _, _ := unstructured.NestedFieldNoCopy(u.Object, field)
	return v
}

// Events returns a short rendering of events for a named object.
func Events(ctx context.Context, c *Client, namespace, name, kind string) string {
	if c == nil || c.Clientset == nil {
		return ""
	}
	list, err := c.Clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=%s", name, kind),
	})
	if err != nil || list == nil {
		return ""
	}
	parts := make([]string, 0, len(list.Items))
	for _, ev := range list.Items {
		parts = append(parts, fmt.Sprintf("%s %s", ev.Reason, ev.Message))
	}
	return strings.Join(parts, "; ")
}
