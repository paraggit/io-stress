package workload

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestPodsNeededIncludesCephFSRWX(t *testing.T) {
	cfg := config.NewDefault()
	cfg.Cluster.RBD.NumPVC = 2
	cfg.Cluster.CephFS.NumPVC = 3
	if got := podsNeeded(cfg); got != 8 {
		t.Fatalf("standard suite pods = %d, want 8 (2 rbd + 3 cephfs + 3 rwx)", got)
	}
	cfg.Cluster.SkipFIOStress = true
	if got := podsNeeded(cfg); got != 5 {
		t.Fatalf("skip fio pods = %d, want 5", got)
	}
	cfg.Cluster.SkipFIOStress = false
	cfg.Cluster.AppTypes = []string{"vm"}
	if got := podsNeeded(cfg); got != 5 {
		t.Fatalf("app-type pods = %d, want 5", got)
	}
}

func TestPlannedPodsVolumeModes(t *testing.T) {
	cfg := config.NewDefault()
	cfg.Cluster.RBD.NumPVC = 2
	cfg.Cluster.CephFS.NumPVC = 1

	pods := plannedPods(cfg)
	if len(pods) != 3 {
		t.Fatalf("len = %d, want 3", len(pods))
	}
	if pods[0].StorageType != "rbd" || pods[0].VolumeMode != corev1.PersistentVolumeFilesystem || pods[0].Target != "/mnt/data/fio.dat" {
		t.Fatalf("rbd odd = %+v", pods[0])
	}
	if pods[1].VolumeMode != corev1.PersistentVolumeBlock || pods[1].Target != "/dev/rbdblock" {
		t.Fatalf("rbd even = %+v", pods[1])
	}
	if pods[2].StorageType != "cephfs" || pods[2].VolumeMode != corev1.PersistentVolumeFilesystem || pods[2].PVCName != cfg.Cluster.Prefix+"-cephfs-pvc-1" {
		t.Fatalf("cephfs = %+v", pods[2])
	}

	rbd := pvcSpec(cfg, pods[0])
	if rbd.StorageClass != cfg.Cluster.RBD.StorageClass || rbd.AccessModes[0] != corev1.ReadWriteOnce {
		t.Fatalf("rbd spec = %+v", rbd)
	}
	ceph := pvcSpec(cfg, pods[2])
	if ceph.StorageClass != cfg.Cluster.CephFS.StorageClass || ceph.AccessModes[0] != corev1.ReadWriteMany {
		t.Fatalf("cephfs spec = %+v", ceph)
	}
}

func TestNewProvisionGroupCapsInflightWork(t *testing.T) {
	cfg := config.NewDefault()
	cfg.Cluster.MaxParallelProvision = 2

	g, _ := newProvisionGroup(context.Background(), cfg)
	var current, maxInflight atomic.Int32
	for i := 0; i < 8; i++ {
		g.Go(func() error {
			n := current.Add(1)
			for {
				seen := maxInflight.Load()
				if n <= seen || maxInflight.CompareAndSwap(seen, n) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			current.Add(-1)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := maxInflight.Load(); got > 2 {
		t.Fatalf("in flight = %d, want <= 2", got)
	}
}
