package k8s

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ProvisionTimeoutError is a clone/restore Bound wait that expired while the
// provisioner was still working (or stalled). Callers should record it as
// slow/retryable rather than a hard product failure.
type ProvisionTimeoutError struct {
	Name     string
	Duration time.Duration
	State    string
}

func (e *ProvisionTimeoutError) Error() string {
	msg := fmt.Sprintf("PVC %s did not reach Bound within %v", e.Name, e.Duration)
	if e.State != "" {
		msg += ": " + e.State
	}
	return msg
}

func IsProvisionTimeout(err error) bool {
	var e *ProvisionTimeoutError
	return errors.As(err, &e)
}

// ExtendProvisionDeadline pushes deadline to now+stall when the PVC is still
// making progress, capped at maxEnd.
func ExtendProvisionDeadline(now, deadline, maxEnd time.Time, stall time.Duration) time.Time {
	extended := now.Add(stall)
	if extended.After(maxEnd) {
		extended = maxEnd
	}
	if extended.After(deadline) {
		return extended
	}
	return deadline
}

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
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create PVC %s: %w", spec.Name, err)
	}
	return nil
}

func WaitPVCBound(ctx context.Context, c *Client, namespace, name string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Poll with Get. One Watch per PVC fails once the apiserver drops
	// connections, which happens as soon as many claims are created together.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	bound, err := pvcIsBound(ctx, c, namespace, name)
	if bound || err != nil {
		if bound {
			return nil
		}
		return err
	}
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("PVC %s did not reach Bound within %v", name, timeout)
			}
			return ctx.Err()
		case <-ticker.C:
			bound, err := pvcIsBound(ctx, c, namespace, name)
			if bound {
				return nil
			}
			if err != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return fmt.Errorf("PVC %s did not reach Bound within %v", name, timeout)
				}
				return err
			}
		}
	}
}

func pvcIsBound(ctx context.Context, c *Client, namespace, name string) (bool, error) {
	pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, fmt.Errorf("get PVC %s: %w", name, err)
	}
	return pvc.Status.Phase == corev1.ClaimBound, nil
}

// WaitPVCBoundProgress waits for Bound, extending the deadline while the PVC's
// resourceVersion continues to change (clone in progress). A stall of `timeout`
// without progress, or 3×timeout elapsed, returns ProvisionTimeoutError.
func WaitPVCBoundProgress(ctx context.Context, c *Client, namespace, name string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	start := time.Now()
	deadline := start.Add(timeout)
	maxEnd := start.Add(timeout * 3)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var prevRV string
	inspect := func() bool {
		pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false
		}
		if pvc.Status.Phase == corev1.ClaimBound {
			return true
		}
		rv := pvc.ResourceVersion
		if prevRV != "" && rv != prevRV {
			deadline = ExtendProvisionDeadline(time.Now(), deadline, maxEnd, timeout)
		}
		prevRV = rv
		return false
	}

	if inspect() {
		return nil
	}
	for {
		now := time.Now()
		if !now.Before(deadline) || !now.Before(maxEnd) {
			return &ProvisionTimeoutError{
				Name:     name,
				Duration: timeout,
				State:    PVCDebugState(ctx, c, namespace, name),
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if inspect() {
				return nil
			}
		}
	}
}

// PVCDebugState captures PVC phase, CSI annotations, conditions, and recent events.
func PVCDebugState(ctx context.Context, c *Client, namespace, name string) string {
	pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("get PVC: %v", err)
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("phase=%s", pvc.Status.Phase))
	if q, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok && !q.IsZero() {
		parts = append(parts, fmt.Sprintf("capacity=%s", q.String()))
	}
	for k, v := range pvc.Annotations {
		if strings.Contains(k, "provisioner") || strings.Contains(k, "csi") || strings.Contains(k, "ceph") {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	}
	for _, cond := range pvc.Status.Conditions {
		parts = append(parts, fmt.Sprintf("cond=%s:%s:%s", cond.Type, cond.Status, cond.Message))
	}
	events, err := c.Clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=PersistentVolumeClaim", name),
	})
	if err == nil {
		n := len(events.Items)
		start := 0
		if n > 5 {
			start = n - 5
		}
		for _, ev := range events.Items[start:] {
			parts = append(parts, fmt.Sprintf("event=%s:%s", ev.Reason, ev.Message))
		}
	}
	return strings.Join(parts, "; ")
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

func DeletePVC(ctx context.Context, c *Client, namespace, name string) error {
	err := c.Clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete PVC %s: %w", name, err)
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

// GetPVCRequestedSize returns the PVC's current storage request (updates immediately on expand patch).
func GetPVCRequestedSize(ctx context.Context, c *Client, namespace, name string) (string, error) {
	pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get PVC %s: %w", name, err)
	}
	q, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		return "", fmt.Errorf("PVC %s has no storage request", name)
	}
	return q.String(), nil
}
