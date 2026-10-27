package workload

import (
	"fmt"
	"log"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/objstore"
	sigsyaml "sigs.k8s.io/yaml"
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

	if cfg.Tools.Object.Enabled {
		if err := emitObjectDryRun(cfg); err != nil {
			return err
		}
	}

	if cfg.Cluster.SetupOnly {
		log.Printf("Setup-only mode: manifests only; a real run would create these PVCs/pods and exit without FIO")
	} else if cfg.Cluster.WriteVerify {
		log.Printf("Write-verify mode: each PVC/pod will run a single sequential write+immediate crc32c verify (no stress suite, no lifecycle)")
	} else if len(cfg.Cluster.AppTypes) > 0 {
		log.Printf("App-type mode: profiles %v will run on every PVC/pod above (no extra PVCs)", cfg.Cluster.AppTypes)
	}

	return nil
}

func emitObjectDryRun(cfg *config.Config) error {
	if err := EnsureObjectSeed(cfg); err != nil {
		return err
	}
	obj := cfg.Tools.Object
	seed, err := objstore.ParseSeed(obj.Seed)
	if err != nil {
		return err
	}
	log.Printf("DRY_RUN — object workload profile=%s buckets=%d seed=%s", obj.Profile, obj.Buckets, obj.Seed)
	if !obj.InsecureTLS {
		fmt.Printf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
  annotations:
    %s: "true"
---
`, k8s.ServiceCAConfigMap, cfg.Cluster.Namespace, k8s.ServiceCAAnnotation)
	}
	var creds []k8s.CredMount
	for ordinal := 0; ordinal < obj.Buckets; ordinal++ {
		name := objstore.BucketName(obj.KeyPrefix, seed, ordinal)
		if obj.Provisioning == "obc" {
			raw, err := sigsyaml.Marshal(k8s.BuildOBC(k8s.OBCSpec{
				Name:         name,
				Namespace:    cfg.Cluster.Namespace,
				StorageClass: obj.StorageClass,
				BucketName:   name,
				BucketClass:  obj.BucketClass,
				Labels:       map[string]string{"app": cfg.Cluster.Prefix},
			}))
			if err != nil {
				return err
			}
			fmt.Printf("%s---\n", raw)
		}
		if ordinal == 0 {
			creds = append(creds, k8s.CredMount{Volume: "cred-0", Secret: name, MountPath: "/etc/objstress/creds/0"})
		}
	}
	pod := k8s.BuildWorkerPod(k8s.WorkerPodSpec{
		Name:          workerPodName(cfg.Cluster.Prefix, "write", 0, 0),
		Namespace:     cfg.Cluster.Namespace,
		Image:         obj.Image,
		SpecConfigMap: workerPodName(cfg.Cluster.Prefix, "write", 0, 0),
		Creds:         creds,
		ServiceCA:     !obj.InsecureTLS,
		CPURequest:    obj.WorkerResources.Requests.CPU,
		MemoryRequest: obj.WorkerResources.Requests.Memory,
		CPULimit:      obj.WorkerResources.Limits.CPU,
		MemoryLimit:   obj.WorkerResources.Limits.Memory,
		Deadline:      obj.PhaseTimeout.Duration(),
		Labels:        map[string]string{"app": cfg.Cluster.Prefix, "component": "object-worker"},
	})
	raw, err := sigsyaml.Marshal(pod)
	if err != nil {
		return err
	}
	fmt.Printf("%s---\n", raw)
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
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 1000
    fsGroupChangePolicy: OnRootMismatch
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: fio
      image: %s
      imagePullPolicy: IfNotPresent
      command: ["sleep", "infinity"]
      securityContext:
        allowPrivilegeEscalation: false
        runAsNonRoot: true
        runAsUser: 1000
        capabilities:
          drop: ["ALL"]
        seccompProfile:
          type: RuntimeDefault
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
