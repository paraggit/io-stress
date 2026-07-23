package k8s

import (
	"context"
	"encoding/base64"
	"fmt"
)

// WriteFileInPod writes data to a file inside a pod container using base64 encoding.
func WriteFileInPod(ctx context.Context, c *Client, namespace, pod, container, path string, data []byte) error {
	b64 := base64.StdEncoding.EncodeToString(data)
	cmd := []string{"sh", "-c", fmt.Sprintf("echo %s | base64 -d > %s", b64, path)}
	
	_, stderr, exitCode, err := ExecInPod(ctx, c, namespace, pod, container, cmd)
	if err != nil {
		return fmt.Errorf("exec failed: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("write file failed (exit %d): %s", exitCode, string(stderr))
	}
	
	return nil
}