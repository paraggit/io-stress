package k8s

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

type PVCSpec struct {
	Name         string
	Namespace    string
	StorageClass string
	Size         string
	VolumeMode   corev1.PersistentVolumeMode
	AccessModes  []corev1.PersistentVolumeAccessMode
	Labels       map[string]string
	DataSource   *corev1.TypedLocalObjectReference
}

func CreatePVC(ctx context.Context, c *Client, spec PVCSpec) error {
	quantity, err := resource.ParseQuantity(spec.Size)
	if err != nil {
		return fmt.Errorf("parse PVC size %q: %w", spec.Size, err)
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      spec.AccessModes,
			VolumeMode:       &spec.VolumeMode,
			StorageClassName: &spec.StorageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: quantity,
				},
			},
			DataSource: spec.DataSource,
		},
	}
	_, err = c.Clientset.CoreV1().PersistentVolumeClaims(spec.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create PVC %s: %w", spec.Name, err)
	}
	return nil
}

func WaitPVCBound(ctx context.Context, c *Client, namespace, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	watcher, err := c.Clientset.CoreV1().PersistentVolumeClaims(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + name,
	})
	if err != nil {
		return fmt.Errorf("watch PVC %s: %w", name, err)
	}
	defer watcher.Stop()

	for event := range watcher.ResultChan() {
		if event.Type == watch.Error {
			return fmt.Errorf("watch error for PVC %s", name)
		}
		pvc, ok := event.Object.(*corev1.PersistentVolumeClaim)
		if !ok {
			continue
		}
		if pvc.Status.Phase == corev1.ClaimBound {
			return nil
		}
	}
	return fmt.Errorf("PVC %s did not reach Bound within %v", name, timeout)
}

func PatchPVCSize(ctx context.Context, c *Client, namespace, name, newSize string) error {
	patch := fmt.Sprintf(`{"spec":{"resources":{"requests":{"storage":"%s"}}}}`, newSize)
	_, err := c.Clientset.CoreV1().PersistentVolumeClaims(namespace).Patch(
		ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patch PVC %s size to %s: %w", name, newSize, err)
	}
	return nil
}

func GetPVCCapacity(ctx context.Context, c *Client, namespace, name string) (string, error) {
	pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get PVC %s: %w", name, err)
	}
	q := pvc.Status.Capacity[corev1.ResourceStorage]
	return q.String(), nil
}
