package k8s

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

func ExecInPod(ctx context.Context, c *Client, namespace, pod, container string, cmd []string) ([]byte, []byte, int, error) {
	req := c.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.RestConfig, "POST", req.URL())
	if err != nil {
		return nil, nil, -1, fmt.Errorf("create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(interface{ ExitStatus() int }); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			return stdout.Bytes(), stderr.Bytes(), -1, err
		}
	}

	return stdout.Bytes(), stderr.Bytes(), exitCode, nil
}
