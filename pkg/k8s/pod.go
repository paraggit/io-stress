package k8s

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

type PodSpec struct {
	Name       string
	Namespace  string
	Image      string
	PVCName    string
	VolumeMode corev1.PersistentVolumeMode
	Labels     map[string]string
	Privileged bool
}

func CreatePod(ctx context.Context, c *Client, spec PodSpec) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:            "fio",
					Image:           spec.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"sleep", "infinity"},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: spec.PVCName,
						},
					},
				},
			},
		},
	}

	if spec.VolumeMode == corev1.PersistentVolumeFilesystem {
		pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: "data", MountPath: "/mnt/data"},
		}
	} else {
		priv := true
		pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			Privileged: &priv,
		}
		pod.Spec.Containers[0].VolumeDevices = []corev1.VolumeDevice{
			{Name: "data", DevicePath: "/dev/rbdblock"},
		}
	}

	_, err := c.Clientset.CoreV1().Pods(spec.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create pod %s: %w", spec.Name, err)
	}
	return nil
}

func WaitPodReady(ctx context.Context, c *Client, namespace, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	watcher, err := c.Clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + name,
	})
	if err != nil {
		return fmt.Errorf("watch pod %s: %w", name, err)
	}
	defer watcher.Stop()

	for event := range watcher.ResultChan() {
		if event.Type == watch.Error {
			return fmt.Errorf("watch error for pod %s", name)
		}
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return nil
			}
		}
	}
	return fmt.Errorf("pod %s did not become Ready within %v", name, timeout)
}

func DeletePod(ctx context.Context, c *Client, namespace, name string) error {
	gracePeriod := int64(0)
	err := c.Clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete pod %s: %w", name, err)
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	watcher, err := c.Clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + name,
	})
	if err != nil {
		return nil
	}
	defer watcher.Stop()

	for event := range watcher.ResultChan() {
		if event.Type == watch.Deleted {
			return nil
		}
	}
	return nil
}
