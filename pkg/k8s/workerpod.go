package k8s

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CredMount is one bucket secret mounted into a worker pod.
type CredMount struct {
	Volume    string
	Secret    string
	MountPath string
}

// WorkerPodSpec is a run-to-completion s3-worker pod.
type WorkerPodSpec struct {
	Name          string
	Namespace     string
	Image         string
	SpecConfigMap string
	Creds         []CredMount
	ServiceCA     bool
	CPURequest    string
	MemoryRequest string
	CPULimit      string
	MemoryLimit   string
	Deadline      time.Duration
	Labels        map[string]string
}

// BuildWorkerPod renders the pod. The container runs s3-worker and exits.
func BuildWorkerPod(spec WorkerPodSpec) *corev1.Pod {
	mounts := []corev1.VolumeMount{{
		Name:      "spec",
		MountPath: "/etc/objstress",
		ReadOnly:  true,
	}}
	volumes := []corev1.Volume{{
		Name: "spec",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: spec.SpecConfigMap},
				Items:                []corev1.KeyToPath{{Key: "spec.json", Path: "spec.json"}},
			},
		},
	}}
	for _, cred := range spec.Creds {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      cred.Volume,
			MountPath: cred.MountPath,
			ReadOnly:  true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: cred.Volume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: cred.Secret},
			},
		})
	}
	if spec.ServiceCA {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "service-ca",
			MountPath: ServiceCAMount,
			ReadOnly:  true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: "service-ca",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ServiceCAConfigMap},
				},
			},
		})
	}
	var envFrom []corev1.EnvFromSource
	if len(spec.Creds) == 1 {
		envFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: spec.Creds[0].Secret},
			},
		}}
	}
	deadline := int64(spec.Deadline.Seconds())
	if deadline < 1 {
		deadline = 1
	}
	nonRoot := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:        &deadline,
			AutomountServiceAccountToken: boolPtr(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &nonRoot,
				RunAsUser:    int64Ptr(fioNonRootUID),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{{
				Name:    "worker",
				Image:   spec.Image,
				Command: []string{"/usr/local/bin/odf-io-stress", "s3-worker", "--spec", "/etc/objstress/spec.json"},
				EnvFrom: envFrom,
				Resources: corev1.ResourceRequirements{
					Requests: quantityList(spec.CPURequest, spec.MemoryRequest),
					Limits:   quantityList(spec.CPULimit, spec.MemoryLimit),
				},
				VolumeMounts: mounts,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: boolPtr(false),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
				},
			}},
			Volumes: volumes,
		},
	}
}

func quantityList(cpu, mem string) corev1.ResourceList {
	out := corev1.ResourceList{}
	if q, err := resource.ParseQuantity(cpu); err == nil && cpu != "" {
		out[corev1.ResourceCPU] = q
	}
	if q, err := resource.ParseQuantity(mem); err == nil && mem != "" {
		out[corev1.ResourceMemory] = q
	}
	return out
}

// CreateWorkerPod creates a run-to-completion worker, replacing a finished pod of the same name.
func CreateWorkerPod(ctx context.Context, c *Client, spec WorkerPodSpec) error {
	pod := BuildWorkerPod(spec)
	_, err := c.Clientset.CoreV1().Pods(spec.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if apierrors.IsAlreadyExists(err) {
		if delErr := DeletePod(ctx, c, spec.Namespace, spec.Name); delErr != nil {
			return delErr
		}
		if waitErr := WaitPodDeleted(ctx, c, spec.Namespace, spec.Name, 2*time.Minute); waitErr != nil {
			return waitErr
		}
		_, err = c.Clientset.CoreV1().Pods(spec.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("create worker pod %s: %w", spec.Name, err)
	}
	return nil
}

// WaitPodFinished waits until the pod Succeeded or Failed.
func WaitPodFinished(ctx context.Context, c *Client, namespace, name string, timeout time.Duration) (*corev1.Pod, error) {
	if timeout <= 0 {
		timeout = 6 * time.Hour
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		pod, err := c.Clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("pod %s did not finish within %s: %w", name, timeout, ctx.Err())
			}
			return nil, err
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded, corev1.PodFailed:
			return pod, nil
		}
		select {
		case <-ctx.Done():
			return pod, fmt.Errorf("pod %s did not finish within %s: %w", name, timeout, ctx.Err())
		case <-ticker.C:
		}
	}
}

// PodLogs reads the worker container log without timestamps so JSON lines stay intact.
func PodLogs(ctx context.Context, c *Client, namespace, name string) (string, error) {
	req := c.Clientset.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{
		Container: "worker",
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("logs for %s: %w", name, err)
	}
	defer stream.Close()
	body, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("read logs for %s: %w", name, err)
	}
	return string(body), nil
}

// PodInfraReason reports OOMKilled or Evicted. An empty string means neither.
func PodInfraReason(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	if pod.Status.Reason == "Evicted" || pod.Status.Reason == "OOMKilled" {
		return pod.Status.Reason
	}
	statuses := append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...)
	statuses = append(statuses, pod.Status.InitContainerStatuses...)
	for _, cs := range statuses {
		if cs.State.Terminated != nil {
			switch cs.State.Terminated.Reason {
			case "OOMKilled", "Evicted":
				return cs.State.Terminated.Reason
			}
		}
	}
	return ""
}
