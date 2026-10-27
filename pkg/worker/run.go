package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/objstore"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

// Run executes spec and writes NDJSON to w. The last lines are type=result,
// one per bucket. A non-nil error is returned after those lines are written.
func Run(ctx context.Context, spec *Spec, w io.Writer) error {
	if spec == nil {
		return fmt.Errorf("nil worker spec")
	}
	if spec.SpecVersion != SpecVersion {
		return fmt.Errorf("specVersion mismatch: worker speaks %d, spec is %d", SpecVersion, spec.SpecVersion)
	}
	if spec.Role == "" {
		spec.Role = spec.Phase
	}
	seed, err := objstore.ParseSeed(spec.Seed)
	if err != nil {
		return err
	}
	mix, err := mixOf(spec)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rt := &runtime{
		ctx:    ctx,
		cancel: cancel,
		spec:   spec,
		seed:   seed,
		mix:    mix,
		em:     &emitter{enc: json.NewEncoder(w)},
	}
	results, runErr := rt.dispatch()
	for _, res := range results {
		rt.em.result(res)
	}
	if runErr != nil {
		return runErr
	}
	for _, res := range results {
		if res.ObjectsFail > 0 || len(res.Mismatches) > 0 || res.MismatchMore > 0 {
			return fmt.Errorf("phase %s worker %d: %s", spec.Phase, spec.Worker, firstErr(res))
		}
	}
	return nil
}

func firstErr(res report.ObjectResult) string {
	if res.Error != "" {
		return res.Error
	}
	if len(res.Mismatches) > 0 {
		m := res.Mismatches[0]
		return fmt.Sprintf("verification mismatch key=%s offset=%d", m.Key, m.FirstBadByte)
	}
	return "object workload failed"
}

// ParseResults returns every type=result object in worker logs.
// kubectl may merge other lines; those are ignored. An empty set is an error.
func ParseResults(logs string) ([]report.ObjectResult, error) {
	var out []report.ObjectResult
	sc := bufio.NewScanner(strings.NewReader(logs))
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var env struct {
			Type   string              `json:"type"`
			Result report.ObjectResult `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Type == "result" {
			out = append(out, env.Result)
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("worker logs contained no final result")
	}
	return out, nil
}

type emitter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (e *emitter) progress(phase string, worker int, ok uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_ = e.enc.Encode(map[string]any{
		"type":      "progress",
		"phase":     phase,
		"worker":    worker,
		"objectsOk": ok,
	})
}

func (e *emitter) result(res report.ObjectResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_ = e.enc.Encode(struct {
		Type   string              `json:"type"`
		Result report.ObjectResult `json:"result"`
	}{Type: "result", Result: res})
}

type runtime struct {
	ctx    context.Context
	cancel context.CancelFunc
	spec   *Spec
	seed   [32]byte
	mix    []objstore.SizeWeight
	em     *emitter
}

func (rt *runtime) dispatch() ([]report.ObjectResult, error) {
	switch rt.spec.Role {
	case "setup", "create":
		return rt.each(rt.setupBucket)
	case "write":
		return rt.each(rt.writeBucket)
	case "read":
		return rt.each(rt.readBucket)
	case "verify":
		return rt.each(rt.verifyBucket)
	case "list":
		return rt.each(rt.listBucket)
	case "copy":
		return rt.each(rt.copyBucket)
	case "delete", "delete-keys":
		return rt.each(rt.deleteKeysBucket)
	case "delete-purge":
		return rt.each(rt.purgeBucket)
	case "assert":
		return rt.each(rt.assertBucket)
	case "delete-bucket":
		return rt.each(rt.deleteBucket)
	default:
		return nil, fmt.Errorf("unknown worker role %q", rt.spec.Role)
	}
}

func (rt *runtime) each(fn func(context.Context, *objstore.Client, *acc) error) ([]report.ObjectResult, error) {
	var out []report.ObjectResult
	for _, b := range rt.spec.Buckets {
		if rt.ctx.Err() != nil {
			break
		}
		stats := objstore.NewStats()
		a := rt.newAcc(b, stats)
		if skipClient(rt.spec.Role, b) {
			a.skip(skipClientReason(rt.spec.Role))
			out = append(out, a.finish())
			continue
		}
		c, err := rt.open(b, stats)
		if err != nil {
			a.fail(err)
			out = append(out, a.finish())
			continue
		}
		if err := fn(a.ctx, c, a); err != nil {
			a.fail(err)
		}
		out = append(out, a.finish())
	}
	if rt.ctx.Err() != nil && !errors.Is(rt.ctx.Err(), context.Canceled) {
		return out, rt.ctx.Err()
	}
	return out, nil
}

func skipClient(role string, b BucketSpec) bool {
	if b.DeletePath != "obc-only" {
		return false
	}
	switch role {
	case "delete", "delete-keys", "delete-purge", "assert":
		return true
	default:
		return false
	}
}

func skipClientReason(role string) string {
	if role == "assert" {
		return "obc-only: bucket left full for the OBC finalizer"
	}
	return "obc-only: bucket left full; OBC delete is the finalizer case"
}

func (rt *runtime) open(b BucketSpec, stats *objstore.Stats) (*objstore.Client, error) {
	ak, sk, err := readCreds(b)
	if err != nil {
		return nil, err
	}
	region := b.Region
	if region == "" {
		region = rt.spec.Region
	}
	if region == "" {
		region = "us-east-1"
	}
	ca := rt.spec.CAFile
	if rt.spec.InsecureTLS {
		ca = ""
	}
	return objstore.NewClient(rt.ctx, objstore.Options{
		Endpoint:    b.Endpoint,
		Region:      region,
		AccessKey:   ak,
		SecretKey:   sk,
		CAFile:      ca,
		InsecureTLS: rt.spec.InsecureTLS,
		MaxRetries:  rt.spec.MaxRetries,
		OpTimeout:   rt.spec.OpTimeout.Duration(),
		Stats:       stats,
	})
}

func readCreds(b BucketSpec) (string, string, error) {
	if b.CredDir != "" {
		ak, err1 := os.ReadFile(filepath.Join(b.CredDir, "AWS_ACCESS_KEY_ID"))
		sk, err2 := os.ReadFile(filepath.Join(b.CredDir, "AWS_SECRET_ACCESS_KEY"))
		if err1 == nil && err2 == nil {
			return strings.TrimSpace(string(ak)), strings.TrimSpace(string(sk)), nil
		}
	}
	ak := strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	sk := strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if ak == "" || sk == "" {
		return "", "", fmt.Errorf("bucket %s: missing AWS credentials", b.Name)
	}
	return ak, sk, nil
}

func mixOf(spec *Spec) ([]objstore.SizeWeight, error) {
	if len(spec.SizeMix) == 0 {
		return nil, fmt.Errorf("size mix is empty")
	}
	out := make([]objstore.SizeWeight, 0, len(spec.SizeMix))
	for _, sw := range spec.SizeMix {
		n, err := config.ParseByteSize(sw.Size)
		if err != nil {
			return nil, err
		}
		out = append(out, objstore.SizeWeight{Bytes: n, Weight: sw.Weight})
	}
	return out, nil
}

type acc struct {
	ctx     context.Context
	cancel  context.CancelFunc
	rt      *runtime
	bucket  BucketSpec
	stats   *objstore.Stats
	mu      sync.Mutex
	res     report.ObjectResult
	stopped bool
}

func (rt *runtime) newAcc(b BucketSpec, stats *objstore.Stats) *acc {
	return &acc{
		ctx:    rt.ctx,
		cancel: rt.cancel,
		rt:     rt,
		bucket: b,
		stats:  stats,
		res: report.ObjectResult{
			Bucket:       b.Name,
			Phase:        rt.spec.Phase,
			Worker:       rt.spec.Worker,
			Seed:         rt.spec.Seed,
			Capabilities: b.Capabilities,
			ProbeDetail:  b.ProbeDetail,
			Ops:          map[string]report.OpStat{},
			S3Errors:     map[string]uint64{},
		},
	}
}

func (a *acc) ok(n, bytes uint64) {
	a.mu.Lock()
	a.res.ObjectsOK += n
	a.res.BytesMoved += bytes
	ok := a.res.ObjectsOK
	a.mu.Unlock()
	if n > 0 && ok%256 == 0 {
		a.rt.em.progress(a.rt.spec.Phase, a.rt.spec.Worker, ok)
	}
}

func (a *acc) fail(err error) {
	if err == nil {
		return
	}
	if objstore.IsCapacity(err) {
		a.mu.Lock()
		if !a.stopped {
			a.stopped = true
			a.res.Error = fmt.Sprintf("capacity stop: %s", err.Error())
			if a.res.ObjectsFail == 0 {
				a.res.ObjectsFail = 1
			}
		}
		a.mu.Unlock()
		a.cancel()
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	a.res.ObjectsFail++
	if a.res.Error == "" {
		a.res.Error = fmt.Sprintf("%s (code=%s status=%d requestId=%s)", err.Error(), objstore.Code(err), objstore.StatusCode(err), objstore.RequestID(err))
	}
}

func (a *acc) mismatch(m report.Mismatch) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.res.ObjectsFail++
	if len(a.res.Mismatches) < 100 {
		a.res.Mismatches = append(a.res.Mismatches, m)
	} else {
		a.res.MismatchMore++
	}
	if a.res.Error == "" {
		a.res.Error = fmt.Sprintf("verification mismatch bucket=%s key=%s offset=%d requestId=%s", a.bucket.Name, m.Key, m.FirstBadByte, m.RequestID)
	}
}

func (a *acc) skip(reason string) {
	if reason == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.res.SkipReason == "" {
		a.res.SkipReason = reason
		return
	}
	if strings.Contains(a.res.SkipReason, reason) {
		return
	}
	a.res.SkipReason += "; " + reason
}

func (a *acc) setProbe(p objstore.ProbeResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.bucket.Capabilities = p.Capabilities
	a.bucket.ProbeDetail = p.Detail
	a.res.Capabilities = p.Capabilities
	a.res.ProbeDetail = p.Detail
}

func (a *acc) finish() report.ObjectResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stats != nil {
		a.res.Ops = convertOps(a.stats.Ops())
		a.res.S3Errors = a.stats.Errors()
		a.res.S3ErrorSamples = a.stats.ErrorSamples()
		a.res.Retries = a.stats.Retries()
	}
	if a.res.Ops == nil {
		a.res.Ops = map[string]report.OpStat{}
	}
	if a.res.S3Errors == nil {
		a.res.S3Errors = map[string]uint64{}
	}
	if a.rt.spec.ErrorRateThresholdPct > 0 && a.res.Retries > 0 {
		var ops uint64
		for _, st := range a.res.Ops {
			ops += st.Count
		}
		if ops > 0 {
			rate := float64(a.res.Retries) / float64(ops) * 100
			if rate > a.rt.spec.ErrorRateThresholdPct {
				msg := fmt.Sprintf("retried S3 error rate %.2f%% exceeds %.2f%%", rate, a.rt.spec.ErrorRateThresholdPct)
				if a.res.Error == "" {
					a.res.Error = msg
				}
				if a.res.ObjectsFail == 0 {
					a.res.ObjectsFail = 1
				}
			}
		}
	}
	return a.res
}

func convertOps(in map[string]objstore.OpStat) map[string]report.OpStat {
	out := make(map[string]report.OpStat, len(in))
	for k, v := range in {
		out[k] = report.OpStat{Count: v.Count, P50Ms: v.P50Ms, P95Ms: v.P95Ms, P99Ms: v.P99Ms, MaxMs: v.MaxMs}
	}
	return out
}

func supported(b BucketSpec, cap string) bool {
	return (objstore.ProbeResult{Capabilities: b.Capabilities}).Supported(cap)
}

func unsupportedDetail(b BucketSpec, cap string) string {
	if b.ProbeDetail != nil && b.ProbeDetail[cap] != "" {
		return b.ProbeDetail[cap]
	}
	if b.Capabilities != nil && b.Capabilities[cap] != "" {
		return b.Capabilities[cap]
	}
	return "unsupported"
}
