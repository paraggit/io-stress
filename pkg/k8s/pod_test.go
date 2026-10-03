package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestBuildPod_FilesystemRestricted(t *testing.T) {
	pod := buildPod(PodSpec{
		Name:       "fio-pod",
		Namespace:  "odf-io-stress",
		Image:      "quay.io/ocsci/nginx:fio",
		PVCName:    "pvc",
		VolumeMode: corev1.PersistentVolumeFilesystem,
	})
	sc := pod.Spec.SecurityContext
	if sc == nil || sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Fatal("pod must set runAsNonRoot=true")
	}
	if sc.RunAsUser == nil || *sc.RunAsUser != fioNonRootUID {
		t.Fatalf("pod runAsUser = %v, want %d", sc.RunAsUser, fioNonRootUID)
	}
	if sc.FSGroup == nil || *sc.FSGroup != fioNonRootUID {
		t.Fatalf("pod fsGroup = %v, want %d", sc.FSGroup, fioNonRootUID)
	}
	if sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("pod seccomp = %#v, want RuntimeDefault", sc.SeccompProfile)
	}

	csc := pod.Spec.Containers[0].SecurityContext
	if csc == nil {
		t.Fatal("container securityContext missing")
	}
	if csc.Privileged != nil && *csc.Privileged {
		t.Error("filesystem pods must not be privileged")
	}
	if csc.AllowPrivilegeEscalation == nil || *csc.AllowPrivilegeEscalation {
		t.Error("allowPrivilegeEscalation must be false")
	}
	if csc.RunAsNonRoot == nil || !*csc.RunAsNonRoot {
		t.Error("container must set runAsNonRoot=true")
	}
	if csc.Capabilities == nil || len(csc.Capabilities.Drop) != 1 || csc.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("capabilities.drop = %#v, want [ALL]", csc.Capabilities)
	}
	if csc.SeccompProfile == nil || csc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("container seccomp = %#v, want RuntimeDefault", csc.SeccompProfile)
	}
}

func TestBuildPod_BlockPrivileged(t *testing.T) {
	pod := buildPod(PodSpec{
		Name:       "fio-block",
		Namespace:  "odf-io-stress",
		Image:      "quay.io/ocsci/nginx:fio",
		PVCName:    "pvc",
		VolumeMode: corev1.PersistentVolumeBlock,
		Privileged: true,
	})
	csc := pod.Spec.Containers[0].SecurityContext
	if csc == nil || csc.Privileged == nil || !*csc.Privileged {
		t.Fatal("block pods must stay privileged for raw device access")
	}
	if pod.Spec.SecurityContext != nil {
		t.Error("privileged block pods should not set restricted pod securityContext")
	}
}
