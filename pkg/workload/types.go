package workload

import corev1 "k8s.io/api/core/v1"

type PodInfo struct {
	Index       int
	Name        string
	StorageType string
	VolumeMode  corev1.PersistentVolumeMode
	Target      string
	PVCName     string
}

func (p PodInfo) VolumeModeStr() string {
	if p.VolumeMode == corev1.PersistentVolumeFilesystem {
		return "Filesystem"
	}
	return "Block"
}
