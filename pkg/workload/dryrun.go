package workload

import (
	"fmt"
	"log"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func DryRun(cfg *config.Config) error {
	log.Printf("DRY_RUN — emitting manifests for %d RBD + %d CephFS PVCs", cfg.NumPVC, cfg.NumPVC)

	for i := 1; i <= cfg.NumPVC; i++ {
		volumeMode := "Filesystem"
		accessMode := "ReadWriteOnce"
		if i%2 == 0 {
			volumeMode = "Block"
		}
		pvcName := fmt.Sprintf("%s-rbd-pvc-%d", cfg.Prefix, i)
		podName := fmt.Sprintf("%s-rbd-pod-%d", cfg.Prefix, i)
		emitPVCYAML(pvcName, cfg.Namespace, cfg.RBDStorageClass, cfg.PVCSize, volumeMode, accessMode, cfg.Prefix)
		emitPodYAML(podName, pvcName, cfg.Namespace, cfg.FIOImage, volumeMode, cfg.Prefix)
	}

	for i := 1; i <= cfg.NumPVC; i++ {
		pvcName := fmt.Sprintf("%s-cephfs-pvc-%d", cfg.Prefix, i)
		podName := fmt.Sprintf("%s-cephfs-pod-%d", cfg.Prefix, i)
		emitPVCYAML(pvcName, cfg.Namespace, cfg.CephFSStorageClass, cfg.PVCSize, "Filesystem", "ReadWriteMany", cfg.Prefix)
		emitPodYAML(podName, pvcName, cfg.Namespace, cfg.FIOImage, "Filesystem", cfg.Prefix)
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
    - name: fio
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
    - name: fio
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
