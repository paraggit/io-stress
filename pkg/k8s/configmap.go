package k8s

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ServiceCAConfigMap is the ConfigMap the service-CA operator fills in.
	ServiceCAConfigMap = "openshift-service-ca.crt"
	// ServiceCAKey is the data key OpenShift injects.
	ServiceCAKey = "service-ca.crt"
	// ServiceCAAnnotation asks the service-CA operator to inject the bundle.
	ServiceCAAnnotation = "service.beta.openshift.io/inject-cabundle"
	// ServiceCAMount is the worker mount path. The cert file is ServiceCAKey.
	ServiceCAMount = "/etc/ssl/service-ca"
)

// UpsertConfigMap creates or replaces a ConfigMap's data.
func UpsertConfigMap(ctx context.Context, c *Client, namespace, name string, data map[string]string, annotations map[string]string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Data: data,
	}
	_, err := c.Clientset.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := c.Clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		if existing.Data == nil {
			existing.Data = map[string]string{}
		}
		for k, v := range data {
			existing.Data[k] = v
		}
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		for k, v := range annotations {
			existing.Annotations[k] = v
		}
		_, err = c.Clientset.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("upsert configmap %s: %w", name, err)
	}
	return nil
}

// EnsureServiceCA creates the ConfigMap the OpenShift service-CA operator fills.
func EnsureServiceCA(ctx context.Context, c *Client, namespace string) error {
	return UpsertConfigMap(ctx, c, namespace, ServiceCAConfigMap, nil, map[string]string{
		ServiceCAAnnotation: "true",
	})
}

// WaitServiceCA waits until the injected bundle is present.
func WaitServiceCA(ctx context.Context, c *Client, namespace string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cm, err := c.Clientset.CoreV1().ConfigMaps(namespace).Get(ctx, ServiceCAConfigMap, metav1.GetOptions{})
		if err == nil && cm.Data[ServiceCAKey] != "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("service CA configmap %s/%s was not injected within %s", namespace, ServiceCAConfigMap, timeout)
		case <-ticker.C:
		}
	}
}

// CopySecret copies selected keys from one namespace into another.
// The worker cannot mount a secret from a different namespace.
func CopySecret(ctx context.Context, c *Client, srcNS, srcName, dstNS, dstName string, keys []string) error {
	src, err := c.Clientset.CoreV1().Secrets(srcNS).Get(ctx, srcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read secret %s/%s: %w", srcNS, srcName, err)
	}
	data := map[string][]byte{}
	for _, key := range keys {
		val, ok := src.Data[key]
		if !ok {
			return fmt.Errorf("secret %s/%s has no key %s", srcNS, srcName, key)
		}
		data[key] = val
	}
	dst := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: dstName, Namespace: dstNS},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
	_, err = c.Clientset.CoreV1().Secrets(dstNS).Create(ctx, dst, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := c.Clientset.CoreV1().Secrets(dstNS).Get(ctx, dstName, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		if existing.Data == nil {
			existing.Data = map[string][]byte{}
		}
		for k, v := range data {
			existing.Data[k] = v
		}
		_, err = c.Clientset.CoreV1().Secrets(dstNS).Update(ctx, existing, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("copy secret to %s/%s: %w", dstNS, dstName, err)
	}
	return nil
}
