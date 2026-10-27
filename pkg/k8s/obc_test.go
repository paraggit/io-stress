package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestBuildOBCUsesBucketName(t *testing.T) {
	u := BuildOBC(OBCSpec{
		Name:         "objstress-4f2a9c1d-007",
		Namespace:    "odf-io-stress",
		StorageClass: "openshift-storage.noobaa.io",
		BucketName:   "objstress-4f2a9c1d-007",
		BucketClass:  "noobaa-default-bucket-class",
	})
	got, _, _ := unstructured.NestedString(u.Object, "spec", "bucketName")
	if got != "objstress-4f2a9c1d-007" {
		t.Fatalf("bucketName = %q", got)
	}
	if _, found, _ := unstructured.NestedString(u.Object, "spec", "generateBucketName"); found {
		t.Fatal("generateBucketName must not be set")
	}
	class, _, _ := unstructured.NestedString(u.Object, "spec", "additionalConfig", "bucketclass")
	if class != "noobaa-default-bucket-class" {
		t.Fatalf("bucketclass = %q", class)
	}
}

func TestWaitOBCGoneReportsFinalizers(t *testing.T) {
	scheme := runtime.NewScheme()
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "objectbucket.io/v1alpha1",
		"kind":       "ObjectBucketClaim",
		"metadata": map[string]interface{}{
			"name":       "stuck",
			"namespace":  "ns",
			"finalizers": []interface{}{"noobaa.io/obc-finalizer"},
		},
		"spec": map[string]interface{}{"bucketName": "stuck"},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		OBCGVR: "ObjectBucketClaimList",
		OBGVR:  "ObjectBucketList",
	}, obj)
	c := &Client{Clientset: k8sfake.NewSimpleClientset(&corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "stuck.1", Namespace: "ns"},
		InvolvedObject: corev1.ObjectReference{
			Kind: "ObjectBucketClaim", Name: "stuck", Namespace: "ns",
		},
		Reason: "Failed", Message: "finalizer stuck",
	}), Dynamic: dyn}
	err := WaitOBCGone(context.Background(), c, "ns", "stuck", "", 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "noobaa.io/obc-finalizer") {
		t.Fatalf("error = %v", err)
	}
}

func TestWorkerPodRunsToCompletion(t *testing.T) {
	pod := BuildWorkerPod(WorkerPodSpec{
		Name:          "obj-write-0-0",
		Namespace:     "ns",
		Image:         "example/odf-io-stress:latest",
		SpecConfigMap: "obj-write-0-0",
		Creds:         []CredMount{{Volume: "cred-0", Secret: "bucket", MountPath: "/etc/objstress/creds/0"}},
		ServiceCA:     true,
		CPURequest:    "200m",
		MemoryRequest: "256Mi",
		CPULimit:      "2",
		MemoryLimit:   "2Gi",
		Deadline:      time.Hour,
	})
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("restart = %s", pod.Spec.RestartPolicy)
	}
	cmd := strings.Join(pod.Spec.Containers[0].Command, " ")
	if !strings.Contains(cmd, "s3-worker") || !strings.Contains(cmd, "/etc/objstress/spec.json") {
		t.Fatalf("command = %s", cmd)
	}
	if pod.Spec.Containers[0].SecurityContext == nil || pod.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation == nil || *pod.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("worker must not allow privilege escalation")
	}
	if PodInfraReason(&corev1.Pod{Status: corev1.PodStatus{Reason: "Evicted"}}) != "Evicted" {
		t.Fatal("evicted pod should be infra")
	}
}
