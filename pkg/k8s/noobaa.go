package k8s

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	noobaaGVR  = schema.GroupVersionResource{Group: "noobaa.io", Version: "v1alpha1", Resource: "noobaas"}
	backingGVR = schema.GroupVersionResource{Group: "noobaa.io", Version: "v1alpha1", Resource: "backingstores"}
	freeKeys   = map[string]bool{
		"freecapacity": true, "freecapacitybytes": true, "availablebytes": true,
		"freespace": true, "freebytes": true, "actualfreecapacity": true,
	}
)

// NooBaaFreeBytes reads free capacity from the NooBaa CR and backing stores.
// found is false when no numeric free-capacity field is present; that is a
// warning for the caller, not a failure.
func NooBaaFreeBytes(ctx context.Context, c *Client, namespace string) (free int64, found bool, err error) {
	dc, err := dynamicClient(c)
	if err != nil {
		return 0, false, err
	}
	var nums []int64
	collect := func(gvr schema.GroupVersionResource, namespaced bool) error {
		var list *unstructured.UnstructuredList
		var listErr error
		if namespaced {
			list, listErr = dc.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		} else {
			list, listErr = dc.Resource(gvr).List(ctx, metav1.ListOptions{})
		}
		if listErr != nil {
			return listErr
		}
		for i := range list.Items {
			nums = append(nums, freeNumbers(list.Items[i].Object)...)
		}
		return nil
	}
	if err := collect(noobaaGVR, true); err != nil {
		return 0, false, fmt.Errorf("list noobaas: %w", err)
	}
	if err := collect(backingGVR, true); err != nil {
		return 0, false, fmt.Errorf("list backingstores: %w", err)
	}
	if len(nums) == 0 {
		return 0, false, nil
	}
	min := nums[0]
	for _, n := range nums[1:] {
		if n < min {
			min = n
		}
	}
	return min, true, nil
}

func freeNumbers(v interface{}) []int64 {
	var out []int64
	var walk func(interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			for k, val := range t {
				if freeKeys[strings.ToLower(strings.ReplaceAll(k, "_", ""))] || freeKeys[strings.ToLower(k)] {
					if n, ok := asInt64(val); ok {
						out = append(out, n)
						continue
					}
				}
				walk(val)
			}
		case []interface{}:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(v)
	return out
}

func asInt64(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	case jsonNumber:
		n, err := t.Int64()
		return n, err == nil
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// jsonNumber matches encoding/json's number type without importing it into the walk hot path's signature.
type jsonNumber interface {
	Int64() (int64, error)
}
