package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var snapshotGVR = schema.GroupVersionResource{
	Group:    "snapshot.storage.k8s.io",
	Version:  "v1",
	Resource: "volumesnapshots",
}

var snapshotClassGVR = schema.GroupVersionResource{
	Group:    "snapshot.storage.k8s.io",
	Version:  "v1",
	Resource: "volumesnapshotclasses",
}

func dynamicClient(c *Client) (dynamic.Interface, error) {
	return dynamic.NewForConfig(c.RestConfig)
}

func CreateSnapshot(ctx context.Context, c *Client, namespace, name, pvcName, snapshotClass string) error {
	dc, err := dynamicClient(c)
	if err != nil {
		return err
	}
	snap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "snapshot.storage.k8s.io/v1",
			"kind":       "VolumeSnapshot",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"volumeSnapshotClassName": snapshotClass,
				"source": map[string]interface{}{
					"persistentVolumeClaimName": pvcName,
				},
			},
		},
	}
	_, err = dc.Resource(snapshotGVR).Namespace(namespace).Create(ctx, snap, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create snapshot %s: %w", name, err)
	}
	return nil
}

func WaitSnapshotReady(ctx context.Context, c *Client, namespace, name string, timeout time.Duration) error {
	dc, err := dynamicClient(c)
	if err != nil {
		return err
	}
	deadline := time.After(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("snapshot %s not readyToUse within %v", name, timeout)
		case <-ticker.C:
			snap, err := dc.Resource(snapshotGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			status, _, _ := unstructured.NestedMap(snap.Object, "status")
			if status == nil {
				continue
			}
			ready, found, _ := unstructured.NestedBool(snap.Object, "status", "readyToUse")
			if found && ready {
				return nil
			}
		}
	}
}

func DetectSnapshotClass(ctx context.Context, c *Client, csiDriver string) (string, error) {
	dc, err := dynamicClient(c)
	if err != nil {
		return "", err
	}
	list, err := dc.Resource(snapshotClassGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list snapshot classes: %w", err)
	}
	for _, item := range list.Items {
		data, _ := json.Marshal(item.Object)
		var sc map[string]interface{}
		json.Unmarshal(data, &sc)
		driver, _, _ := unstructured.NestedString(item.Object, "driver")
		if driver == csiDriver {
			return item.GetName(), nil
		}
	}
	return "", fmt.Errorf("no VolumeSnapshotClass found for driver %s", csiDriver)
}

func DeleteSnapshot(ctx context.Context, c *Client, namespace, name string) error {
	dc, err := dynamicClient(c)
	if err != nil {
		return err
	}
	err = dc.Resource(snapshotGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete snapshot %s: %w", name, err)
	}
	return nil
}
