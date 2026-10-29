package workload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/objstore"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
	"github.com/red-hat-storage/odf-io-stress/pkg/worker"
)

// Disrupt is a seam for a future disruption phase. v1 leaves it nil.
var Disrupt func(ctx context.Context, phase string) error

// EnsureObjectSeed fills a random seed when the object workload is on and
// none was set. The same seed is logged and stored on the config so the report
// can reproduce the run.
func EnsureObjectSeed(cfg *config.Config) error {
	if cfg == nil || !cfg.Tools.Object.Enabled || strings.TrimSpace(cfg.Tools.Object.Seed) != "" {
		return nil
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Errorf("generate object seed: %w", err)
	}
	cfg.Tools.Object.Seed = hex.EncodeToString(b[:])
	log.Printf("object seed: %s", cfg.Tools.Object.Seed)
	return nil
}

type bucketBind struct {
	Name             string
	OBCName          string
	ObjectBucketName string
	Ordinal          int
	Versioned        bool
	DeletePath       string
	Endpoint         string
	Region           string
	Secret           string
	Capabilities     map[string]string
	ProbeDetail      map[string]string
}

type waveJob struct {
	Worker  int
	Workers int
	Ordinal int
	Buckets []bucketBind
}

type objectRun struct {
	ctx       context.Context
	cfg       *config.Config
	client    *k8s.Client
	collector *report.Collector
	seed      [32]byte
	buckets   []bucketBind
	deleteRan bool
	backend   objectBackend
	mu        sync.Mutex
	usageErr  error
}

func runObject(ctx context.Context, cfg *config.Config, client *k8s.Client, collector *report.Collector) error {
	backend, err := ResolveObjectBackend(ctx, client, cfg)
	if err != nil {
		return err
	}
	if err := EnsureObjectSeed(cfg); err != nil {
		return err
	}
	obj := cfg.Tools.Object
	seed, err := objstore.ParseSeed(obj.Seed)
	if err != nil {
		return &UsageError{Err: err}
	}
	phases, err := config.OrderedPhases(obj.Phases)
	if err != nil {
		return &UsageError{Err: err}
	}
	if len(plannedPods(cfg)) == 0 {
		if err := ensurePodCapacity(ctx, client, podsNeeded(cfg)); err != nil {
			return err
		}
	}
	o := &objectRun{ctx: ctx, cfg: cfg, client: client, collector: collector, seed: seed, backend: backend}
	log.Printf("Object workload: profile=%s buckets=%d objects=%d phases=%s seed=%s", obj.Profile, obj.Buckets, obj.ObjectsPerBucket, strings.Join(phases, ","), obj.Seed)
	if obj.InsecureTLS {
		log.Printf("WARNING: object TLS verification is disabled (--object-insecure-tls). The worker will not authenticate the S3 endpoint.")
	} else if err := o.prepareTLS(ctx); err != nil {
		return err
	}
	if obj.Resume {
		if err := o.loadExisting(ctx); err != nil {
			return err
		}
	} else if !containsPhase(phases, "create") {
		return &UsageError{Err: fmt.Errorf("object phases omit create; pass --object-resume --object-only --object-seed to reuse buckets, or include create")}
	}
	for _, phase := range phases {
		if err := ctx.Err(); err != nil {
			return err
		}
		if Disrupt != nil {
			if err := Disrupt(ctx, phase); err != nil {
				return fmt.Errorf("disrupt before %s: %w", phase, err)
			}
		}
		var phaseErr error
		switch phase {
		case "create":
			if !obj.Resume {
				phaseErr = o.provision(ctx)
			}
			if phaseErr == nil {
				phaseErr = o.setup(ctx)
			}
		case "write":
			if phaseErr = o.preflight(ctx); phaseErr == nil {
				phaseErr = o.sharded(ctx, "write", "write")
			}
		case "read":
			phaseErr = o.sharded(ctx, "read", "read")
		case "verify":
			phaseErr = o.sharded(ctx, "verify", "verify")
		case "list":
			phaseErr = o.perBucket(ctx, "list", "list", o.buckets, false)
		case "copy":
			phaseErr = o.sharded(ctx, "copy", "copy")
		case "delete":
			phaseErr = o.delete(ctx)
			if phaseErr == nil {
				o.deleteRan = true
			}
		case "teardown":
			phaseErr = o.teardown(ctx)
		default:
			phaseErr = fmt.Errorf("unknown object phase %q", phase)
		}
		if phaseErr != nil {
			return phaseErr
		}
	}
	return nil
}

// prepareTLS installs the service-CA bundle for both backends.
// An http endpoint does not use the mounted bundle.
func (o *objectRun) prepareTLS(ctx context.Context) error {
	ns := o.cfg.Cluster.Namespace
	if err := k8s.EnsureServiceCA(ctx, o.client, ns); err != nil {
		return err
	}
	if err := k8s.WaitServiceCA(ctx, o.client, ns, 2*time.Minute); err != nil {
		return err
	}
	return nil
}

func (o *objectRun) preflight(ctx context.Context) error {
	obj := o.cfg.Tools.Object
	if !obj.CapacityPreflight || obj.Force {
		if obj.Force {
			log.Printf("object capacity preflight overridden by --object-force")
		}
		return nil
	}
	projected, err := obj.ProjectedBytes()
	if err != nil {
		return err
	}
	ns := obj.ODFNamespace
	free, found, readErr := o.readFreeBytes(ctx, ns)
	if readErr != nil || !found {
		log.Printf("WARNING: could not read %s free capacity in %s (%v); continuing", o.capacitySource(), ns, readErr)
		return nil
	}
	if o.backend.Name == config.BackendRGW {
		// CephFreeBytes does not return the CephCluster name, and pkg/k8s does
		// not log. Name the namespace here. Do not list CephClusters again
		// only to print an object name.
		log.Printf("capacity preflight: namespace %s: projected logical %d bytes versus raw bytesAvailable %d bytes (logical projected bytes versus raw bytesAvailable; optimistic under replication)", ns, projected, free)
		if projected > free {
			return &UsageError{Err: fmt.Errorf("projected logical %d bytes exceed raw bytesAvailable %d bytes; pass --object-force to continue", projected, free)}
		}
		return nil
	}
	log.Printf("capacity preflight: projected %d bytes, free %d bytes", projected, free)
	if projected > free {
		return &UsageError{Err: fmt.Errorf("projected %d bytes exceeds NooBaa free capacity %d bytes; pass --object-force to continue", projected, free)}
	}
	return nil
}

func (o *objectRun) readFreeBytes(ctx context.Context, namespace string) (int64, bool, error) {
	if o.backend.Name == config.BackendRGW {
		return k8s.CephFreeBytes(ctx, o.client, namespace)
	}
	return k8s.NooBaaFreeBytes(ctx, o.client, namespace)
}

func (o *objectRun) capacitySource() string {
	if o.backend.Name == config.BackendRGW {
		return "Ceph"
	}
	return "NooBaa"
}

func (o *objectRun) provision(ctx context.Context) error {
	obj := o.cfg.Tools.Object
	if obj.Provisioning == "s3" {
		return o.provisionS3(ctx)
	}
	class := o.backend.StorageClass
	if class == "" {
		class = obj.StorageClass
	}
	if err := k8s.StorageClassExists(ctx, o.client, class); err != nil {
		return fmt.Errorf("backend %s: %w", o.backend.Name, err)
	}
	o.buckets = make([]bucketBind, obj.Buckets)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(obj.BucketCreateConcurrency)
	var mu sync.Mutex
	for ordinal := 0; ordinal < obj.Buckets; ordinal++ {
		ordinal := ordinal
		g.Go(func() error {
			name := objstore.BucketName(obj.KeyPrefix, o.seed, ordinal)
			start := time.Now()
			err := k8s.CreateOBC(gctx, o.client, k8s.OBCSpec{
				Name:         name,
				Namespace:    o.cfg.Cluster.Namespace,
				StorageClass: class,
				BucketName:   name,
				BucketClass:  o.backend.BucketClass,
				Labels:       map[string]string{"app": o.cfg.Cluster.Prefix, "ordinal": fmt.Sprintf("%d", ordinal)},
			})
			if err != nil {
				if strings.Contains(err.Error(), "pick a different seed") {
					return &UsageError{Err: err}
				}
				return err
			}
			bind, err := k8s.WaitOBCBound(gctx, o.client, o.cfg.Cluster.Namespace, name, obj.PhaseTimeout.Duration())
			if err != nil {
				o.record(name, "create", report.ObjectResult{Bucket: name, Phase: "create", Seed: obj.Seed, Worker: ordinal}, "fail", time.Since(start), err.Error())
				return err
			}
			b := o.binding(ordinal, name, bind)
			mu.Lock()
			o.buckets[ordinal] = b
			mu.Unlock()
			ms := float64(time.Since(start)) / float64(time.Millisecond)
			o.record(name, "create", report.ObjectResult{
				Bucket: name, Phase: "create", Seed: obj.Seed, Worker: ordinal, ObjectsOK: 1,
				Ops: map[string]report.OpStat{"bind": {Count: 1, P50Ms: ms, P95Ms: ms, P99Ms: ms, MaxMs: ms}},
			}, "pass", time.Since(start), "")
			log.Printf("OBC %s Bound in %s", name, time.Since(start).Round(time.Millisecond))
			return nil
		})
	}
	return g.Wait()
}

func (o *objectRun) provisionS3(ctx context.Context) error {
	obj := o.cfg.Tools.Object
	secret := o.adminSecret()
	if err := k8s.CopySecret(ctx, o.client, obj.NooBaaNamespace, "noobaa-admin", o.cfg.Cluster.Namespace, secret, []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}); err != nil {
		return fmt.Errorf("copy noobaa-admin: %w", err)
	}
	endpoint := obj.Endpoint
	if endpoint == "" {
		endpoint = o.backend.FallbackEndpoint
	}
	o.buckets = make([]bucketBind, obj.Buckets)
	for ordinal := 0; ordinal < obj.Buckets; ordinal++ {
		name := objstore.BucketName(obj.KeyPrefix, o.seed, ordinal)
		o.buckets[ordinal] = bucketBind{
			Name:       name,
			Ordinal:    ordinal,
			Versioned:  objstore.Versioned(ordinal, obj.VersionedBuckets),
			DeletePath: objstore.DeletePath(obj.DeleteMode, ordinal, obj.Buckets),
			Endpoint:   endpoint,
			Region:     obj.Region,
			Secret:     secret,
		}
	}
	return nil
}

func (o *objectRun) loadExisting(ctx context.Context) error {
	obj := o.cfg.Tools.Object
	if obj.Provisioning == "s3" {
		secret := o.adminSecret()
		if _, err := o.client.Clientset.CoreV1().Secrets(o.cfg.Cluster.Namespace).Get(ctx, secret, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				return &UsageError{Err: fmt.Errorf("resume: secret %s is missing; s3 buckets from this seed are not available", secret)}
			}
			return err
		}
		return o.provisionS3(ctx)
	}
	o.buckets = make([]bucketBind, obj.Buckets)
	for ordinal := 0; ordinal < obj.Buckets; ordinal++ {
		name := objstore.BucketName(obj.KeyPrefix, o.seed, ordinal)
		u, err := k8s.GetOBC(ctx, o.client, o.cfg.Cluster.Namespace, name)
		if apierrors.IsNotFound(err) {
			return &UsageError{Err: fmt.Errorf("resume: OBC %s is not in namespace %s; it must already be Bound", name, o.cfg.Cluster.Namespace)}
		}
		if err != nil {
			return err
		}
		phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
		if phase != "Bound" {
			return &UsageError{Err: fmt.Errorf("resume: OBC %s phase is %q, want Bound", name, phase)}
		}
		bind, err := k8s.WaitOBCBound(ctx, o.client, o.cfg.Cluster.Namespace, name, obj.PhaseTimeout.Duration())
		if err != nil {
			return &UsageError{Err: fmt.Errorf("resume: OBC %s: %w", name, err)}
		}
		o.buckets[ordinal] = o.binding(ordinal, name, bind)
	}
	log.Printf("object resume: reusing %d Bound bucket(s) in %s", len(o.buckets), o.cfg.Cluster.Namespace)
	return nil
}

func (o *objectRun) binding(ordinal int, obcName string, bind k8s.BucketBinding) bucketBind {
	obj := o.cfg.Tools.Object
	endpoint := obj.Endpoint
	if endpoint == "" {
		endpoint = bind.Endpoint
	}
	if endpoint == "" {
		endpoint = o.backend.FallbackEndpoint
	}
	region := bind.Region
	if region == "" {
		region = obj.Region
	}
	secret := bind.SecretName
	if secret == "" {
		secret = obcName
	}
	name := bind.Name
	if name == "" {
		name = obcName
	}
	return bucketBind{
		Name:             name,
		OBCName:          obcName,
		ObjectBucketName: bind.ObjectBucketName,
		Ordinal:          ordinal,
		Versioned:        objstore.Versioned(ordinal, obj.VersionedBuckets),
		DeletePath:       objstore.DeletePath(obj.DeleteMode, ordinal, obj.Buckets),
		Endpoint:         endpoint,
		Region:           region,
		Secret:           secret,
	}
}

func (o *objectRun) setup(ctx context.Context) error {
	results, err := o.runWave(ctx, "create", "setup", []waveJob{{
		Worker: 0, Workers: 1, Buckets: o.buckets,
	}}, false)
	if err != nil {
		return err
	}
	byName := map[string]report.ObjectResult{}
	for _, res := range results {
		byName[res.Bucket] = res
	}
	for i := range o.buckets {
		res, ok := byName[o.buckets[i].Name]
		if !ok {
			continue
		}
		o.buckets[i].Capabilities = res.Capabilities
		o.buckets[i].ProbeDetail = res.ProbeDetail
	}
	return nil
}

func (o *objectRun) sharded(ctx context.Context, phase, role string) error {
	n := o.cfg.Tools.Object.Workers
	if n < 1 {
		n = 1
	}
	jobs := make([]waveJob, n)
	for i := 0; i < n; i++ {
		jobs[i] = waveJob{Worker: i, Workers: n, Buckets: o.buckets}
	}
	_, err := o.runWave(ctx, phase, role, jobs, false)
	return err
}

func (o *objectRun) perBucket(ctx context.Context, phase, role string, buckets []bucketBind, cleanupFail bool) error {
	if len(buckets) == 0 {
		return nil
	}
	jobs := make([]waveJob, len(buckets))
	for i, b := range buckets {
		jobs[i] = waveJob{Worker: 0, Workers: 1, Ordinal: b.Ordinal, Buckets: []bucketBind{b}}
	}
	_, err := o.runWave(ctx, phase, role, jobs, cleanupFail)
	return err
}

func (o *objectRun) delete(ctx context.Context) error {
	var recursive []bucketBind
	for _, b := range o.buckets {
		if b.DeletePath == "recursive" {
			recursive = append(recursive, b)
			continue
		}
		o.record("", "delete", report.ObjectResult{
			Bucket: b.Name, Phase: "delete", Seed: o.cfg.Tools.Object.Seed, Worker: b.Ordinal,
			SkipReason: "obc-only: OBC will be deleted while the bucket is still full",
		}, "skip", 0, "")
	}
	if len(recursive) == 0 {
		return nil
	}
	n := o.cfg.Tools.Object.Workers
	if n < 1 {
		n = 1
	}
	jobs := make([]waveJob, n)
	for i := 0; i < n; i++ {
		jobs[i] = waveJob{Worker: i, Workers: n, Buckets: recursive}
	}
	if _, err := o.runWave(ctx, "delete", "delete-keys", jobs, false); err != nil {
		return err
	}
	return o.perBucket(ctx, "delete", "delete-purge", recursive, false)
}

func (o *objectRun) teardown(ctx context.Context) error {
	if o.deleteRan {
		var recursive []bucketBind
		for _, b := range o.buckets {
			if b.DeletePath == "recursive" {
				recursive = append(recursive, b)
			}
		}
		if err := o.perBucket(ctx, "teardown", "assert", recursive, false); err != nil {
			return err
		}
	}
	obj := o.cfg.Tools.Object
	if o.cfg.Cluster.NoCleanup {
		for _, b := range o.buckets {
			o.record("", "teardown", report.ObjectResult{
				Bucket: b.Name, Phase: "teardown", Seed: obj.Seed,
				SkipReason: "no-cleanup: bucket left in place",
			}, "skip", 0, "")
		}
		return nil
	}
	if obj.Provisioning == "s3" {
		return o.perBucket(ctx, "teardown", "delete-bucket", o.buckets, true)
	}
	for _, b := range o.buckets {
		start := time.Now()
		name := b.OBCName
		if name == "" {
			name = b.Name
		}
		if err := k8s.DeleteOBC(ctx, o.client, o.cfg.Cluster.Namespace, name); err != nil {
			o.record(name, "teardown", report.ObjectResult{Bucket: b.Name, Phase: "teardown", Seed: obj.Seed}, "cleanup-fail", time.Since(start), err.Error())
			continue
		}
		if err := k8s.WaitOBCGone(ctx, o.client, o.cfg.Cluster.Namespace, name, b.ObjectBucketName, obj.OBCDeleteTimeout.Duration()); err != nil {
			o.record(name, "teardown", report.ObjectResult{Bucket: b.Name, Phase: "teardown", Seed: obj.Seed}, "cleanup-fail", time.Since(start), err.Error())
			continue
		}
		o.record(name, "teardown", report.ObjectResult{Bucket: b.Name, Phase: "teardown", Seed: obj.Seed, ObjectsOK: 1}, "pass", time.Since(start), "")
		log.Printf("OBC %s deleted in %s", name, time.Since(start).Round(time.Millisecond))
	}
	return nil
}

func (o *objectRun) runWave(ctx context.Context, phase, role string, jobs []waveJob, cleanupFail bool) ([]report.ObjectResult, error) {
	if len(jobs) == 0 {
		return nil, nil
	}
	obj := o.cfg.Tools.Object
	pctx, cancel := context.WithTimeout(ctx, obj.PhaseTimeout.Duration())
	defer cancel()
	g, gctx := errgroup.WithContext(pctx)
	if n := o.cfg.Cluster.MaxParallelPods; n > 0 {
		g.SetLimit(n)
	}
	var mu sync.Mutex
	var all []report.ObjectResult
	for _, job := range jobs {
		job := job
		g.Go(func() error {
			results, err := o.runPod(gctx, phase, role, job, cleanupFail)
			mu.Lock()
			all = append(all, results...)
			mu.Unlock()
			if err != nil && errors.Is(err, context.Canceled) && o.sawUsage() {
				return nil
			}
			return err
		})
	}
	err := g.Wait()
	o.mu.Lock()
	usage := o.usageErr
	o.mu.Unlock()
	if usage != nil {
		return all, &UsageError{Err: usage}
	}
	return all, err
}

func (o *objectRun) runPod(ctx context.Context, phase, role string, job waveJob, cleanupFail bool) ([]report.ObjectResult, error) {
	spec, mounts, err := o.jobSpec(phase, role, job)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	ns := o.cfg.Cluster.Namespace
	podName := workerPodName(o.cfg.Cluster.Prefix, role, job.Ordinal, job.Worker)
	if err := k8s.UpsertConfigMap(ctx, o.client, ns, podName, map[string]string{"spec.json": string(raw)}, nil); err != nil {
		return nil, err
	}
	obj := o.cfg.Tools.Object
	start := time.Now()
	if err := k8s.CreateWorkerPod(ctx, o.client, k8s.WorkerPodSpec{
		Name:          podName,
		Namespace:     ns,
		Image:         obj.Image,
		SpecConfigMap: podName,
		Creds:         mounts,
		ServiceCA:     !obj.InsecureTLS,
		CPURequest:    obj.WorkerResources.Requests.CPU,
		MemoryRequest: obj.WorkerResources.Requests.Memory,
		CPULimit:      obj.WorkerResources.Limits.CPU,
		MemoryLimit:   obj.WorkerResources.Limits.Memory,
		Deadline:      obj.PhaseTimeout.Duration(),
		Labels: map[string]string{
			"app": o.cfg.Cluster.Prefix, "component": "object-worker", "phase": phase,
		},
	}); err != nil {
		return nil, err
	}
	pod, waitErr := k8s.WaitPodFinished(ctx, o.client, ns, podName, obj.PhaseTimeout.Duration())
	logs, logErr := k8s.PodLogs(context.Background(), o.client, ns, podName)
	keep := logErr != nil
	defer func() {
		if keep {
			log.Printf("keeping worker pod %s for inspection", podName)
			return
		}
		delCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = k8s.DeletePod(delCtx, o.client, ns, podName)
		_ = o.client.Clientset.CoreV1().ConfigMaps(ns).Delete(delCtx, podName, metav1.DeleteOptions{})
	}()
	if strings.Contains(logs, "specVersion mismatch") {
		mismatch := fmt.Errorf("worker specVersion mismatch on %s: the worker image does not match this control plane", podName)
		o.noteUsage(mismatch)
		return nil, &UsageError{Err: mismatch}
	}
	infra := k8s.PodInfraReason(pod)
	results, parseErr := worker.ParseResults(logs)
	if infra != "" {
		o.record(podName, phase, report.ObjectResult{Phase: phase, Seed: obj.Seed, Worker: job.Worker, Error: infra}, "infra-fail", time.Since(start), infra)
		return nil, nil
	}
	if parseErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if logErr != nil {
			keep = true
		}
		msg := parseErr.Error()
		if logErr != nil {
			msg = logErr.Error()
		}
		if waitErr != nil {
			msg = waitErr.Error() + ": " + msg
		}
		o.record(podName, phase, report.ObjectResult{Phase: phase, Seed: obj.Seed, Worker: job.Worker}, "infra-fail", time.Since(start), "worker exited without a final result: "+msg)
		return nil, nil
	}
	for _, res := range results {
		status := objectJobStatus(res)
		if cleanupFail && status == "fail" {
			status = "cleanup-fail"
		}
		o.record(podName, phase, res, status, time.Since(start), "")
	}
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	if waitErr != nil {
		return results, waitErr
	}
	return results, nil
}

func (o *objectRun) jobSpec(phase, role string, job waveJob) (worker.Spec, []k8s.CredMount, error) {
	obj := o.cfg.Tools.Object
	mounts, dirs := credMounts(job.Buckets)
	threshold, err := config.ParseByteSize(obj.Multipart.Threshold)
	if err != nil && obj.Multipart.Enabled {
		return worker.Spec{}, nil, err
	}
	part, err := config.ParseByteSize(obj.Multipart.PartSize)
	if err != nil && obj.Multipart.Enabled {
		return worker.Spec{}, nil, err
	}
	buckets := make([]worker.BucketSpec, len(job.Buckets))
	for i, b := range job.Buckets {
		buckets[i] = worker.BucketSpec{
			Name:         b.Name,
			Ordinal:      b.Ordinal,
			Versioned:    b.Versioned,
			DeletePath:   b.DeletePath,
			Endpoint:     b.Endpoint,
			Region:       b.Region,
			CredDir:      dirs[b.Ordinal],
			Capabilities: b.Capabilities,
			ProbeDetail:  b.ProbeDetail,
		}
	}
	ca := ""
	if !obj.InsecureTLS {
		ca = k8s.ServiceCAMount + "/" + k8s.ServiceCAKey
	}
	return worker.Spec{
		SpecVersion:           worker.SpecVersion,
		Phase:                 phase,
		Role:                  role,
		Worker:                job.Worker,
		Workers:               job.Workers,
		Seed:                  obj.Seed,
		Buckets:               buckets,
		ObjectsPerBucket:      obj.ObjectsPerBucket,
		KeyPrefix:             obj.KeyPrefix,
		KeyLayout:             obj.KeyLayout,
		SizeMix:               obj.SizeMix,
		Concurrency:           obj.WorkerConcurrency,
		VerifyMode:            obj.VerifyMode,
		VerifySamplePct:       obj.VerifySamplePct,
		RangeReadsPct:         obj.RangeReadsPct,
		MultipartEnabled:      obj.Multipart.Enabled,
		MultipartThreshold:    threshold,
		MultipartPartSize:     part,
		AbortCases:            obj.Multipart.AbortCases,
		OpTimeout:             obj.OpTimeout,
		MaxRetries:            obj.MaxRetries,
		InsecureTLS:           obj.InsecureTLS,
		CAFile:                ca,
		VersionsPerKey:        obj.VersionsPerKey,
		ErrorRateThresholdPct: obj.ErrorRateThresholdPct,
		Region:                obj.Region,
		Provisioning:          obj.Provisioning,
		Resume:                obj.Resume,
	}, mounts, nil
}

func credMounts(buckets []bucketBind) ([]k8s.CredMount, map[int]string) {
	seen := map[string]string{}
	var mounts []k8s.CredMount
	dirs := map[int]string{}
	for _, b := range buckets {
		path, ok := seen[b.Secret]
		if !ok {
			path = fmt.Sprintf("/etc/objstress/creds/%d", b.Ordinal)
			mounts = append(mounts, k8s.CredMount{
				Volume:    fmt.Sprintf("cred-%d", len(mounts)),
				Secret:    b.Secret,
				MountPath: path,
			})
			seen[b.Secret] = path
		}
		dirs[b.Ordinal] = path
	}
	return mounts, dirs
}

func (o *objectRun) adminSecret() string {
	return o.cfg.Cluster.Prefix + "-noobaa-admin"
}

func (o *objectRun) noteUsage(err error) {
	o.mu.Lock()
	if o.usageErr == nil {
		o.usageErr = err
	}
	o.mu.Unlock()
}

func (o *objectRun) sawUsage() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.usageErr != nil
}

func (o *objectRun) record(pod, phase string, res report.ObjectResult, status string, dur time.Duration, errText string) {
	if res.Phase == "" {
		res.Phase = phase
	}
	if res.Seed == "" {
		res.Seed = o.cfg.Tools.Object.Seed
	}
	if errText == "" {
		errText = res.Error
	}
	if res.Ops == nil {
		res.Ops = map[string]report.OpStat{}
	}
	if res.S3Errors == nil {
		res.S3Errors = map[string]uint64{}
	}
	exit := 0
	switch status {
	case "fail":
		exit = 1
	case "cleanup-fail":
		exit = 2
	case "infra-fail":
		exit = 3
	}
	o.collector.Add(report.JobResult{
		Pod:          pod,
		Job:          phase,
		Category:     "object-" + phase,
		Storage:      o.backend.ReportStorage,
		VolumeMode:   "Object",
		Status:       status,
		ExitCode:     exit,
		Duration:     dur,
		ObjectOutput: &res,
		Error:        errText,
	})
}

func objectJobStatus(res report.ObjectResult) string {
	if res.ObjectsFail > 0 || len(res.Mismatches) > 0 || res.MismatchMore > 0 || res.Error != "" {
		return "fail"
	}
	if res.SkipReason != "" && res.ObjectsOK == 0 {
		return "skip"
	}
	return "pass"
}

func workerPodName(prefix, role string, ordinal, worker int) string {
	name := fmt.Sprintf("%s-obj-%s-%d-%d", prefix, role, ordinal, worker)
	name = strings.ToLower(name)
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	return name
}

func containsPhase(phases []string, name string) bool {
	for _, p := range phases {
		if p == name {
			return true
		}
	}
	return false
}
