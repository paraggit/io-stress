package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFreePodSlotsSkipsMastersAndFullWorkers(t *testing.T) {
	nodes := []corev1.Node{
		testNode("master", 250, corev1.Taint{
			Key: "node-role.kubernetes.io/control-plane", Effect: corev1.TaintEffectNoSchedule,
		}),
		testNode("worker-a", 250),
		testNode("worker-b", 250),
		testNode("cordoned", 250),
	}
	nodes[3].Spec.Unschedulable = true

	var pods []corev1.Pod
	for i := 0; i < 248; i++ {
		pods = append(pods, scheduledPod("worker-a", corev1.PodRunning))
	}
	pods = append(pods, scheduledPod("worker-a", corev1.PodSucceeded))
	pods = append(pods, scheduledPod("worker-b", corev1.PodRunning))
	pods = append(pods, corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}})

	gotNodes, free := freePodSlots(nodes, pods)
	if gotNodes != 2 {
		t.Fatalf("schedulable nodes = %d, want 2", gotNodes)
	}
	// worker-a has 2 free (succeeded does not count); worker-b has 249.
	if free != 251 {
		t.Fatalf("free slots = %d, want 251", free)
	}
}

func TestUnschedulablePodTooManyPods(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "odf-io-cephfs-pod-494"},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{
				Type:    corev1.PodScheduled,
				Status:  corev1.ConditionFalse,
				Message: "0/9 nodes are available: 3 node(s) had untolerated taint(s), 6 Too many pods.",
			}},
		},
	}
	err := unschedulablePod(pod)
	if err == nil || err.Error() == "" {
		t.Fatal("expected unschedulable error")
	}
	unsched, ok := err.(*UnschedulableError)
	if !ok || unsched.Name != pod.Name {
		t.Fatalf("err = %#v", err)
	}
}

func testNode(name string, pods int, taints ...corev1.Taint) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.NodeSpec{Taints: taints},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourcePods: *resource.NewQuantity(int64(pods), resource.DecimalSI),
			},
		},
	}
}

func scheduledPod(node string, phase corev1.PodPhase) corev1.Pod {
	return corev1.Pod{
		Spec:   corev1.PodSpec{NodeName: node},
		Status: corev1.PodStatus{Phase: phase},
	}
}
