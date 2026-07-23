package workload

import (
	"fmt"
	"log"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func DryRun(cfg *config.Config) error {
	log.Printf("DRY_RUN — emitting manifests for %d RBD + %d CephFS PVCs", cfg.Cluster.RBD.NumPVC, cfg.Cluster.CephFS.NumPVC)

	for i := 1; i <= cfg.Cluster.RBD.NumPVC; i++ {
		volumeMode := "Filesystem"
		accessMode := "ReadWriteOnce"
		if i%2 == 0 {
			volumeMode = "Block"
		}
		pvcName := fmt.Sprintf("%s-rbd-pvc-%d", cfg.Cluster.Prefix, i)
		podName := fmt.Sprintf("%s-rbd-pod-%d", cfg.Cluster.Prefix, i)
		emitPVCYAML(pvcName, cfg.Cluster.Namespace, cfg.Cluster.RBD.StorageClass, cfg.Cluster.PVCSize, volumeMode, accessMode, cfg.Cluster.Prefix)
		emitPodYAML(podName, pvcName, cfg.Cluster.Namespace, cfg.Tools.FIO.Image, volumeMode, cfg.Cluster.Prefix)
	}

	for i := 1; i <= cfg.Cluster.CephFS.NumPVC; i++ {
		pvcName := fmt.Sprintf("%s-cephfs-pvc-%d", cfg.Cluster.Prefix, i)
		podName := fmt.Sprintf("%s-cephfs-pod-%d", cfg.Cluster.Prefix, i)
		emitPVCYAML(pvcName, cfg.Cluster.Namespace, cfg.Cluster.CephFS.StorageClass, cfg.Cluster.PVCSize, "Filesystem", "ReadWriteMany", cfg.Cluster.Prefix)
		emitPodYAML(podName, pvcName, cfg.Cluster.Namespace, cfg.Tools.FIO.Image, "Filesystem", cfg.Cluster.Prefix)
	}

	return nil
}

func emitPVCYAML(name, namespace, sc, size, volumeMode, accessMode, prefix string) {
	fmt.Printf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  accessModes:
    - %s
  volumeMode: %s
  resources:
    requests:
      storage: %s
  storageClassName: %s
---
`, name, namespace, prefix, accessMode, volumeMode, size, sc)
}

func emitPodYAML(name, pvcName, namespace, image, volumeMode, prefix string) {
	if volumeMode == "Filesystem" {
		fmt.Printf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  restartPolicy: Never
  containers:
    - name: iotool
      image: %s
      imagePullPolicy: IfNotPresent
      command: ["sleep", "infinity"]
      volumeMounts:
        - name: data
          mountPath: /mnt/data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
---
`, name, namespace, prefix, image, pvcName)
	} else {
		fmt.Printf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  restartPolicy: Never
  containers:
    - name: iotool
      image: %s
      imagePullPolicy: IfNotPresent
      command: ["sleep", "infinity"]
      securityContext:
        privileged: true
      volumeDevices:
        - name: data
          devicePath: /dev/rbdblock
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
---
`, name, namespace, prefix, image, pvcName)
	}
}
