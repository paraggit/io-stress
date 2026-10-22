package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UnschedulableError is a pod the scheduler has already rejected. Waiting
// longer will not place it.
type UnschedulableError struct {
	Name    string
	Message string
}

func (e *UnschedulableError) Error() string {
	return fmt.Sprintf("pod %s cannot be scheduled: %s", e.Name, e.Message)
}

// SchedulablePodSlots is how many more pods can be placed on nodes that accept
// this workload. Masters and other NoSchedule nodes are excluded. Pods already
// on a node consume its allocatable pod count ("Too many pods").
func SchedulablePodSlots(ctx context.Context, c *Client) (nodes, free int, err error) {
	nodeList, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("list nodes: %w", err)
	}
	podList, err := c.Clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("list pods: %w", err)
	}
	nodes, free = freePodSlots(nodeList.Items, podList.Items)
	return nodes, free, nil
}

func freePodSlots(nodes []corev1.Node, pods []corev1.Pod) (schedulableNodes, free int) {
	used := map[string]int{}
	for i := range pods {
		pod := &pods[i]
		if pod.Spec.NodeName == "" {
			continue
		}
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		used[pod.Spec.NodeName]++
	}
	for i := range nodes {
		node := &nodes[i]
		if !nodeAcceptsWorkload(*node) {
			continue
		}
		schedulableNodes++
		capacity := int(node.Status.Allocatable.Pods().Value())
		remain := capacity - used[node.Name]
		if remain > 0 {
			free += remain
		}
	}
	return schedulableNodes, free
}

// nodeAcceptsWorkload is true when a pod with only the default not-ready and
// unreachable tolerations can be scheduled here.
func nodeAcceptsWorkload(node corev1.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect != corev1.TaintEffectNoSchedule && taint.Effect != corev1.TaintEffectNoExecute {
			continue
		}
		if !defaultPodTolerates(taint) {
			return false
		}
	}
	return true
}

func defaultPodTolerates(taint corev1.Taint) bool {
	switch taint.Key {
	case "node.kubernetes.io/not-ready", "node.kubernetes.io/unreachable":
		return taint.Effect == corev1.TaintEffectNoExecute
	default:
		return false
	}
}

func unschedulablePod(pod *corev1.Pod) error {
	for _, cond := range pod.Status.Conditions {
		if cond.Type != corev1.PodScheduled || cond.Status != corev1.ConditionFalse {
			continue
		}
		if strings.Contains(cond.Message, "Too many pods") {
			return &UnschedulableError{Name: pod.Name, Message: cond.Message}
		}
	}
	return nil
}
