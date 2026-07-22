package k8s

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func EnsureNamespace(ctx context.Context, c *Client, namespace string) error {
	_, err := c.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %s: %w", namespace, err)
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}
	_, err = c.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", namespace, err)
	}
	return nil
}

func DeleteNamespace(ctx context.Context, c *Client, namespace string) error {
	err := c.Clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete namespace %s: %w", namespace, err)
	}
	return nil
}

// WaitNamespaceDeleted blocks until the namespace is fully gone or timeout.
func WaitNamespaceDeleted(ctx context.Context, c *Client, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		_, err := c.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil && ctx.Err() != nil {
			return fmt.Errorf("wait namespace %s deleted: %w", namespace, ctx.Err())
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("namespace %s still present after %v", namespace, timeout)
		case <-ticker.C:
		}
	}
}

// ResetNamespace deletes the namespace (cascading leftovers) and recreates it.
func ResetNamespace(ctx context.Context, c *Client, namespace string, timeout time.Duration) error {
	if err := DeleteNamespace(ctx, c, namespace); err != nil {
		return err
	}
	if err := WaitNamespaceDeleted(ctx, c, namespace, timeout); err != nil {
		return err
	}
	return EnsureNamespace(ctx, c, namespace)
}
