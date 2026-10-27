package worker

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/red-hat-storage/odf-io-stress/pkg/objstore"
	"github.com/red-hat-storage/odf-io-stress/pkg/report"
)

func (rt *runtime) setupBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	b := a.bucket
	if rt.spec.Provisioning == "s3" && !rt.spec.Resume {
		if err := c.CreateBucket(ctx, b.Name); err != nil {
			code := objstore.Code(err)
			if code == "BucketAlreadyExists" || code == "BucketAlreadyOwnedByYou" {
				a.fail(fmt.Errorf("bucket %s already exists; pick a different seed or clean up", b.Name))
				return nil
			}
			a.fail(err)
			return nil
		}
	}
	probe, err := objstore.Probe(ctx, c, b.Name)
	if err != nil {
		a.fail(err)
		return nil
	}
	a.setProbe(probe)
	for cap, state := range probe.Capabilities {
		if state == "unsupported" || (len(state) >= 12 && state[:12] == "unsupported") {
			a.skip(cap + " unsupported: " + unsupportedDetail(BucketSpec{ProbeDetail: probe.Detail, Capabilities: probe.Capabilities}, cap))
		}
	}
	if b.Versioned {
		if !probe.Supported(objstore.CapVersioning) {
			a.skip("versioning unsupported: " + unsupportedDetail(BucketSpec{ProbeDetail: probe.Detail}, objstore.CapVersioning))
		} else if err := c.PutBucketVersioning(ctx, b.Name); err != nil {
			a.fail(fmt.Errorf("enable versioning on %s: %w", b.Name, err))
			return nil
		}
	}
	a.ok(1, 0)
	return nil
}

func (rt *runtime) writeBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	b := a.bucket
	if b.Versioned && !supported(b, objstore.CapVersioning) {
		a.skip("versioning unsupported: " + unsupportedDetail(b, objstore.CapVersioning) + "; writing a single version")
	}
	_ = a.walk(func(ctx context.Context, i uint64) error {
		return rt.putIndex(ctx, c, a, b, i)
	})
	rt.abortCases(ctx, c, a, b)
	return nil
}

func (rt *runtime) putIndex(ctx context.Context, c *objstore.Client, a *acc, b BucketSpec, i uint64) error {
	key, err := objstore.Key(rt.spec.KeyLayout, rt.spec.KeyPrefix, i)
	if err != nil {
		a.fail(err)
		return nil
	}
	size := objstore.SizeFor(rt.seed, i, rt.mix)
	count, first := versionSpan(rt.spec, b)
	multipart := rt.useMultipart(b, size)
	var moved uint64
	for v := 0; v < count; v++ {
		ver := first
		if first > 0 {
			ver = int64(v + 1)
		}
		meta := map[string]string{}
		if ver > 0 {
			meta[objstore.VersionMeta] = strconv.FormatInt(ver, 10)
		}
		body := func() io.Reader {
			return objstore.PayloadReader(rt.seed, b.Ordinal, key, ver, size)
		}
		var put objstore.PutResult
		if multipart {
			mp, err := c.UploadMultipart(ctx, b.Name, key, size, rt.spec.MultipartPartSize, meta, body)
			if err != nil {
				a.fail(err)
				return err
			}
			put = mp.PutResult
		} else {
			put, err = c.PutObject(ctx, b.Name, key, size, meta, body)
			if err != nil {
				a.fail(err)
				return err
			}
		}
		moved += uint64(size)
		if objstore.Hit(rt.seed, b.Ordinal, key, "raw", 1) {
			rt.verifyGet(ctx, c, a, b, key, ver, size, put.VersionID, put.ETag, multipart)
		}
	}
	a.ok(1, moved)
	return nil
}

func (rt *runtime) abortCases(ctx context.Context, c *objstore.Client, a *acc, b BucketSpec) {
	if rt.spec.Worker != 0 || rt.spec.AbortCases <= 0 || !rt.spec.MultipartEnabled {
		return
	}
	if !supported(b, objstore.CapMultipart) {
		a.skip("multipart abort skipped: " + unsupportedDetail(b, objstore.CapMultipart))
		return
	}
	for n := 0; n < rt.spec.AbortCases; n++ {
		key := fmt.Sprintf("%s/abort/%d", rt.spec.KeyPrefix, n)
		if _, err := c.AbortMultipart(ctx, b.Name, key); err != nil {
			if objstore.IsUnsupported(err) {
				a.skip("multipart abort skipped: " + err.Error())
				return
			}
			a.fail(err)
			return
		}
	}
	if !supported(b, objstore.CapListMultipart) {
		a.skip("list-multipart-uploads unsupported: " + unsupportedDetail(b, objstore.CapListMultipart) + "; aborted-upload check skipped")
		return
	}
	ids, err := c.ListMultipartUploads(ctx, b.Name, rt.spec.KeyPrefix+"/abort/")
	if err != nil {
		if objstore.IsUnsupported(err) {
			a.skip("list-multipart-uploads unsupported: " + err.Error())
			return
		}
		a.fail(err)
		return
	}
	if len(ids) != 0 {
		a.fail(fmt.Errorf("aborted multipart uploads still listed on %s: %d", b.Name, len(ids)))
	}
}

func (rt *runtime) readBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	b := a.bucket
	_ = a.walk(func(ctx context.Context, i uint64) error {
		key, err := objstore.Key(rt.spec.KeyLayout, rt.spec.KeyPrefix, i)
		if err != nil {
			a.fail(err)
			return nil
		}
		size := objstore.SizeFor(rt.seed, i, rt.mix)
		ver := latestVersion(rt.spec, b)
		if _, err := c.HeadObject(ctx, b.Name, key, ""); err != nil {
			a.fail(err)
			return err
		}
		header, start, length, ranged := rangeHeader(rt.seed, b.Ordinal, key, size, rt.spec.RangeReadsPct)
		if ranged {
			got, err := c.GetObject(ctx, b.Name, key, "", header)
			if err != nil {
				a.fail(err)
				return err
			}
			mm, ok, cmpErr := compareSkip(objstore.PayloadReader(rt.seed, b.Ordinal, key, ver, size), got.Body, start, length)
			if cmpErr != nil {
				a.fail(cmpErr)
				return cmpErr
			}
			if !ok {
				a.mismatch(mismatchOf(key, "", size, mm, "", got.ETag, got.RequestID))
			}
		} else {
			rt.verifyGet(ctx, c, a, b, key, ver, size, "", "", rt.useMultipart(b, size))
		}
		a.ok(1, uint64(size))
		return nil
	})
	return nil
}

func (rt *runtime) verifyBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	b := a.bucket
	_ = a.walk(func(ctx context.Context, i uint64) error {
		key, err := objstore.Key(rt.spec.KeyLayout, rt.spec.KeyPrefix, i)
		if err != nil {
			a.fail(err)
			return nil
		}
		size := objstore.SizeFor(rt.seed, i, rt.mix)
		full := rt.fullVerify(b, key)
		if b.Versioned && supported(b, objstore.CapListVersions) && full {
			if err := rt.verifyVersions(ctx, c, a, b, key, size); err != nil {
				return err
			}
		} else if full {
			ver := latestVersion(rt.spec, b)
			rt.verifyGet(ctx, c, a, b, key, ver, size, "", "", rt.useMultipart(b, size))
		} else {
			rt.verifyETag(ctx, c, a, b, key, latestVersion(rt.spec, b), size)
		}
		if b.Versioned && !supported(b, objstore.CapListVersions) {
			a.skip("list-object-versions unsupported: " + unsupportedDetail(b, objstore.CapListVersions) + "; verified the current object only")
		}
		a.ok(1, 0)
		return nil
	})
	return nil
}

func (rt *runtime) verifyVersions(ctx context.Context, c *objstore.Client, a *acc, b BucketSpec, key string, size int64) error {
	page, err := c.ListObjectVersions(ctx, b.Name, key, "", "", int32(rt.spec.VersionsPerKey)+5)
	if err != nil {
		if objstore.IsUnsupported(err) {
			a.skip("list-object-versions unsupported: " + err.Error())
			rt.verifyGet(ctx, c, a, b, key, latestVersion(rt.spec, b), size, "", "", rt.useMultipart(b, size))
			return nil
		}
		a.fail(err)
		return err
	}
	seen := 0
	for _, v := range page.Versions {
		if v.Key != key || v.DeleteMarker {
			continue
		}
		head, err := c.HeadObject(ctx, b.Name, key, v.VersionID)
		if err != nil {
			a.fail(err)
			return err
		}
		ver, ok := objstore.VersionOrdinal(head.Metadata)
		if !ok {
			a.fail(fmt.Errorf("object %s version %s is missing %s metadata", key, v.VersionID, objstore.VersionMeta))
			continue
		}
		seen++
		rt.verifyGet(ctx, c, a, b, key, ver, size, v.VersionID, head.ETag, rt.useMultipart(b, size))
	}
	want := rt.spec.VersionsPerKey
	if want < 1 {
		want = 1
	}
	if seen != want {
		a.fail(fmt.Errorf("key %s has %d versions, want %d", key, seen, want))
	}
	return nil
}

func (rt *runtime) listBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	if rt.spec.Worker != 0 {
		a.skip("list runs on worker 0 only")
		return nil
	}
	b := a.bucket
	prefix := rt.spec.KeyPrefix + "/"
	count, err := countObjects(ctx, c, b.Name, prefix)
	if err != nil {
		a.fail(err)
		return nil
	}
	if count != rt.spec.ObjectsPerBucket {
		a.fail(fmt.Errorf("list %s: count %d, want %d", b.Name, count, rt.spec.ObjectsPerBucket))
	} else {
		a.ok(uint64(count), 0)
	}
	if rt.spec.ObjectsPerBucket > 2 {
		page, err := c.ListObjectsV2(ctx, b.Name, prefix, "", "", 2)
		if err != nil {
			a.fail(err)
		} else if len(page.Objects) > 2 || !page.Truncated {
			a.fail(fmt.Errorf("list %s maxKeys=2: objects=%d truncated=%v", b.Name, len(page.Objects), page.Truncated))
		}
	}
	if rt.spec.KeyLayout != "flat" {
		page, err := c.ListObjectsV2(ctx, b.Name, prefix, "/", "", 1000)
		if err != nil {
			a.fail(err)
		} else if len(page.Prefixes) == 0 {
			a.fail(fmt.Errorf("list %s delimiter: no common prefixes", b.Name))
		}
	}
	if b.Versioned {
		if !supported(b, objstore.CapListVersions) {
			a.skip("list-object-versions unsupported: " + unsupportedDetail(b, objstore.CapListVersions))
		} else {
			versions, markers, err := countVersions(ctx, c, b.Name, prefix)
			if err != nil {
				if objstore.IsUnsupported(err) {
					a.skip("list-object-versions unsupported: " + err.Error())
				} else {
					a.fail(err)
				}
			} else {
				want := rt.spec.ObjectsPerBucket * rt.spec.VersionsPerKey
				if versions != want || markers != 0 {
					a.fail(fmt.Errorf("list versions %s: versions %d want %d, delete markers %d", b.Name, versions, want, markers))
				}
			}
		}
	}
	if supported(b, objstore.CapListMultipart) {
		ids, err := c.ListMultipartUploads(ctx, b.Name, rt.spec.KeyPrefix+"/abort/")
		if err != nil {
			if objstore.IsUnsupported(err) {
				a.skip("list-multipart-uploads unsupported: " + err.Error())
			} else {
				a.fail(err)
			}
		} else if len(ids) != 0 {
			a.fail(fmt.Errorf("aborted multipart uploads still listed on %s: %d", b.Name, len(ids)))
		}
	} else if rt.spec.AbortCases > 0 {
		a.skip("list-multipart-uploads unsupported: " + unsupportedDetail(b, objstore.CapListMultipart))
	}
	return nil
}

func (rt *runtime) copyBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	b := a.bucket
	if !supported(b, objstore.CapCopy) {
		a.skip("copy unsupported: " + unsupportedDetail(b, objstore.CapCopy))
		return nil
	}
	var copied atomic.Int32
	_ = a.walk(func(ctx context.Context, i uint64) error {
		if copied.Load() >= 32 {
			return nil
		}
		key, err := objstore.Key(rt.spec.KeyLayout, rt.spec.KeyPrefix, i)
		if err != nil {
			a.fail(err)
			return nil
		}
		pct := rt.spec.VerifySamplePct
		if pct <= 0 {
			pct = 1
		}
		start, end := objstore.Shard(rt.spec.Worker, workersOf(rt.spec), uint64(rt.spec.ObjectsPerBucket))
		force := i == start && end > start
		if !force && !objstore.Hit(rt.seed, b.Ordinal, key, "copy", pct) {
			return nil
		}
		if copied.Add(1) > 32 {
			return nil
		}
		size := objstore.SizeFor(rt.seed, i, rt.mix)
		dest := "copies/" + key
		if _, err := c.CopyObject(ctx, b.Name, key, dest); err != nil {
			if objstore.IsUnsupported(err) {
				a.skip("copy unsupported: " + err.Error())
				return nil
			}
			a.fail(err)
			return nil
		}
		rt.verifyGet(ctx, c, a, b, dest, latestVersion(rt.spec, b), size, "", "", rt.useMultipart(b, size))
		a.ok(1, uint64(size))
		return nil
	})
	return nil
}

func (rt *runtime) deleteKeysBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	b := a.bucket
	start, end := objstore.Shard(rt.spec.Worker, workersOf(rt.spec), uint64(rt.spec.ObjectsPerBucket))
	var batch []objstore.ObjectID
	var firstKey string
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := a.deleteIDs(ctx, c, b, batch); err != nil {
			return err
		}
		a.ok(uint64(len(batch)), 0)
		batch = batch[:0]
		return nil
	}
	for i := start; i < end; i++ {
		if ctx.Err() != nil {
			return nil
		}
		key, err := objstore.Key(rt.spec.KeyLayout, rt.spec.KeyPrefix, i)
		if err != nil {
			a.fail(err)
			return nil
		}
		if firstKey == "" {
			firstKey = key
		}
		batch = append(batch, objstore.ObjectID{Key: key})
		if len(batch) == 1000 {
			if err := flush(); err != nil {
				return nil
			}
		}
	}
	if err := flush(); err != nil {
		return nil
	}
	if b.Versioned && firstKey != "" && rt.spec.Worker == 0 {
		rt.assertDeleteMarker(ctx, c, a, b, firstKey)
	}
	return nil
}

func (rt *runtime) assertDeleteMarker(ctx context.Context, c *objstore.Client, a *acc, b BucketSpec, key string) {
	page, err := c.ListObjectsV2(ctx, b.Name, key, "", "", 10)
	if err != nil {
		a.fail(err)
		return
	}
	for _, obj := range page.Objects {
		if obj.Key == key {
			a.fail(fmt.Errorf("plain list still contains %s after delete without version id", key))
			break
		}
	}
	if !supported(b, objstore.CapListVersions) {
		a.skip("list-object-versions unsupported: " + unsupportedDetail(b, objstore.CapListVersions) + "; delete-marker check skipped")
		return
	}
	vers, err := c.ListObjectVersions(ctx, b.Name, key, "", "", 20)
	if err != nil {
		if objstore.IsUnsupported(err) {
			a.skip("list-object-versions unsupported: " + err.Error())
			return
		}
		a.fail(err)
		return
	}
	found := false
	for _, v := range vers.Versions {
		if v.Key == key && v.DeleteMarker {
			found = true
			break
		}
	}
	if !found {
		a.fail(fmt.Errorf("version list for %s has no delete marker after delete without version id", key))
	}
}

func (rt *runtime) purgeBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	if rt.spec.Worker != 0 {
		a.skip("purge runs on worker 0 only")
		return nil
	}
	b := a.bucket
	if b.Versioned && !supported(b, objstore.CapListVersions) {
		a.skip("list-object-versions unsupported: " + unsupportedDetail(b, objstore.CapListVersions) + "; version purge skipped")
	}
	versioned := b.Versioned && supported(b, objstore.CapListVersions)
	for pass := 0; pass < 64; pass++ {
		n, err := deletePass(ctx, c, a, b, versioned)
		if err != nil {
			return nil
		}
		if n == 0 {
			break
		}
	}
	left, err := countObjects(ctx, c, b.Name, "")
	if err != nil {
		a.fail(err)
		return nil
	}
	if left != 0 {
		a.fail(fmt.Errorf("bucket %s still has %d objects after purge", b.Name, left))
		return nil
	}
	if versioned {
		versions, markers, err := countVersions(ctx, c, b.Name, "")
		if err != nil {
			a.fail(err)
			return nil
		}
		if versions != 0 || markers != 0 {
			a.fail(fmt.Errorf("bucket %s still has %d versions and %d delete markers", b.Name, versions, markers))
			return nil
		}
	}
	a.ok(1, 0)
	return nil
}

func (rt *runtime) assertBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	if rt.spec.Worker != 0 {
		a.skip("assert runs on worker 0 only")
		return nil
	}
	b := a.bucket
	left, err := countObjects(ctx, c, b.Name, "")
	if err != nil {
		a.fail(err)
		return nil
	}
	if left != 0 {
		a.fail(fmt.Errorf("bucket %s is not empty: %d objects", b.Name, left))
		return nil
	}
	if b.Versioned && supported(b, objstore.CapListVersions) {
		versions, markers, err := countVersions(ctx, c, b.Name, "")
		if err != nil {
			if objstore.IsUnsupported(err) {
				a.skip("list-object-versions unsupported: " + err.Error())
			} else {
				a.fail(err)
				return nil
			}
		} else if versions != 0 || markers != 0 {
			a.fail(fmt.Errorf("bucket %s is not version-empty: %d versions, %d delete markers", b.Name, versions, markers))
			return nil
		}
	}
	a.ok(1, 0)
	return nil
}

func (rt *runtime) deleteBucket(ctx context.Context, c *objstore.Client, a *acc) error {
	if err := c.DeleteBucket(ctx, a.bucket.Name); err != nil {
		a.fail(fmt.Errorf("delete bucket %s: %w", a.bucket.Name, err))
		return nil
	}
	a.ok(1, 0)
	return nil
}

func (a *acc) walk(fn func(context.Context, uint64) error) error {
	workers := workersOf(a.rt.spec)
	start, end := objstore.Shard(a.rt.spec.Worker, workers, uint64(a.rt.spec.ObjectsPerBucket))
	conc := a.rt.spec.Concurrency
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var first atomic.Value
loop:
	for i := start; i < end; i++ {
		select {
		case <-a.ctx.Done():
			break loop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i uint64) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(a.ctx, i); err != nil && first.Load() == nil {
				first.Store(err)
			}
		}(i)
	}
	wg.Wait()
	if v := first.Load(); v != nil {
		return v.(error)
	}
	return a.ctx.Err()
}

func (a *acc) deleteIDs(ctx context.Context, c *objstore.Client, b BucketSpec, ids []objstore.ObjectID) error {
	if len(ids) == 0 {
		return nil
	}
	if supported(b, objstore.CapBatchDelete) {
		errs, err := c.DeleteKeys(ctx, b.Name, ids)
		if err == nil {
			for _, e := range errs {
				if e.Code == "" || e.Code == "NoSuchKey" {
					continue
				}
				a.fail(fmt.Errorf("delete %s version %s: %s: %s", e.Key, e.VersionID, e.Code, e.Message))
			}
			return nil
		}
		if !objstore.IsUnsupported(err) && !objstore.IsUnsupportedCode(objstore.Code(err)) {
			a.fail(err)
			return err
		}
		a.skip("batch-delete unsupported: " + err.Error())
	}
	for _, id := range ids {
		if err := c.DeleteObject(ctx, b.Name, id.Key, id.VersionID); err != nil && objstore.Code(err) != "NoSuchKey" {
			a.fail(err)
			return err
		}
	}
	return nil
}

func deletePass(ctx context.Context, c *objstore.Client, a *acc, b BucketSpec, versioned bool) (int, error) {
	if versioned {
		page, err := c.ListObjectVersions(ctx, b.Name, "", "", "", 1000)
		if err != nil {
			a.fail(err)
			return 0, err
		}
		if len(page.Versions) == 0 {
			return 0, nil
		}
		ids := make([]objstore.ObjectID, 0, len(page.Versions))
		for _, v := range page.Versions {
			ids = append(ids, objstore.ObjectID{Key: v.Key, VersionID: v.VersionID})
		}
		if err := a.deleteIDs(ctx, c, b, ids); err != nil {
			return 0, err
		}
		return len(ids), nil
	}
	page, err := c.ListObjectsV2(ctx, b.Name, "", "", "", 1000)
	if err != nil {
		a.fail(err)
		return 0, err
	}
	if len(page.Objects) == 0 {
		return 0, nil
	}
	ids := make([]objstore.ObjectID, 0, len(page.Objects))
	for _, obj := range page.Objects {
		ids = append(ids, objstore.ObjectID{Key: obj.Key})
	}
	if err := a.deleteIDs(ctx, c, b, ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (rt *runtime) verifyGet(ctx context.Context, c *objstore.Client, a *acc, b BucketSpec, key string, ver, size int64, versionID, etag string, multipart bool) {
	got, err := c.GetObject(ctx, b.Name, key, versionID, "")
	if err != nil {
		a.fail(err)
		return
	}
	defer got.Body.Close()
	payloadKey := key
	if len(key) > len("copies/") && key[:len("copies/")] == "copies/" {
		payloadKey = key[len("copies/"):]
	}
	mm, ok, err := objstore.CompareStreams(objstore.PayloadReader(rt.seed, b.Ordinal, payloadKey, ver, size), got.Body)
	if err != nil {
		a.fail(err)
		return
	}
	if ok {
		return
	}
	expETag := etag
	if exp, good, err := objstore.ExpectedETag(rt.seed, b.Ordinal, payloadKey, ver, size, rt.spec.MultipartPartSize, multipart); err == nil && good {
		expETag = exp
	}
	a.mismatch(mismatchOf(key, versionID, size, mm, expETag, got.ETag, got.RequestID))
}

func (rt *runtime) verifyETag(ctx context.Context, c *objstore.Client, a *acc, b BucketSpec, key string, ver, size int64) {
	multipart := rt.useMultipart(b, size)
	head, err := c.HeadObject(ctx, b.Name, key, "")
	if err != nil {
		a.fail(err)
		return
	}
	exp, ok, err := objstore.ExpectedETag(rt.seed, b.Ordinal, key, ver, size, rt.spec.MultipartPartSize, multipart)
	if err != nil {
		a.fail(err)
		return
	}
	if !ok {
		a.skip("etag check skipped for multipart object " + key)
		return
	}
	if objstore.NormalizeETag(head.ETag) == exp {
		return
	}
	rt.verifyGet(ctx, c, a, b, key, ver, size, "", exp, multipart)
}

func (rt *runtime) fullVerify(b BucketSpec, key string) bool {
	switch rt.spec.VerifyMode {
	case "etag":
		return false
	case "sample":
		pct := rt.spec.VerifySamplePct
		if pct <= 0 {
			pct = 1
		}
		return objstore.Hit(rt.seed, b.Ordinal, key, "verify", pct)
	default:
		return true
	}
}

func (rt *runtime) useMultipart(b BucketSpec, size int64) bool {
	return rt.spec.MultipartEnabled && rt.spec.MultipartPartSize > 0 && size >= rt.spec.MultipartThreshold && supported(b, objstore.CapMultipart)
}

func versionSpan(spec *Spec, b BucketSpec) (count int, first int64) {
	if b.Versioned && supported(b, objstore.CapVersioning) {
		n := spec.VersionsPerKey
		if n < 1 {
			n = 1
		}
		return n, 1
	}
	return 1, 0
}

func latestVersion(spec *Spec, b BucketSpec) int64 {
	count, first := versionSpan(spec, b)
	if first == 0 {
		return 0
	}
	return int64(count)
}

func workersOf(spec *Spec) int {
	if spec.Workers < 1 {
		return 1
	}
	return spec.Workers
}

func mismatchOf(key, versionID string, size int64, mm objstore.StreamMismatch, expETag, actETag, req string) report.Mismatch {
	return report.Mismatch{
		Key:          key,
		VersionID:    versionID,
		Size:         size,
		ExpectedSHA:  mm.ExpectedSHA,
		ActualSHA:    mm.ActualSHA,
		FirstBadByte: mm.FirstBadByte,
		ExpectedHex:  mm.ExpectedHex,
		ActualHex:    mm.ActualHex,
		ExpectedETag: expETag,
		ActualETag:   actETag,
		RequestID:    req,
	}
}

func rangeHeader(seed [32]byte, ordinal int, key string, size int64, pct float64) (string, int64, int64, bool) {
	if pct <= 0 || size <= 1 || !objstore.Hit(seed, ordinal, key, "range", pct) {
		return "", 0, size, false
	}
	h := sha256.New()
	_, _ = h.Write(seed[:])
	_ = binary.Write(h, binary.BigEndian, int64(ordinal))
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte("range-off"))
	sum := h.Sum(nil)
	start := int64(binary.BigEndian.Uint64(sum[:8]) % uint64(size))
	length := size - start
	if length > 64*1024 {
		length = 64 * 1024
	}
	if length < 1 {
		length = 1
	}
	return fmt.Sprintf("bytes=%d-%d", start, start+length-1), start, length, true
}

func compareSkip(expected io.Reader, actual io.ReadCloser, skip, n int64) (objstore.StreamMismatch, bool, error) {
	defer actual.Close()
	if err := discard(expected, skip); err != nil {
		return objstore.StreamMismatch{}, false, err
	}
	return objstore.CompareStreams(io.LimitReader(expected, n), actual)
}

func discard(r io.Reader, n int64) error {
	buf := make([]byte, 1<<20)
	for n > 0 {
		chunk := int64(len(buf))
		if chunk > n {
			chunk = n
		}
		got, err := io.ReadFull(r, buf[:chunk])
		n -= int64(got)
		if err != nil {
			return err
		}
	}
	return nil
}

func countObjects(ctx context.Context, c *objstore.Client, bucket, prefix string) (int, error) {
	var count int
	var token string
	for pages := 0; pages < 1000000; pages++ {
		page, err := c.ListObjectsV2(ctx, bucket, prefix, "", token, 1000)
		if err != nil {
			return count, err
		}
		count += len(page.Objects)
		if !page.Truncated || page.NextToken == "" {
			return count, nil
		}
		token = page.NextToken
	}
	return count, fmt.Errorf("list %s did not terminate", bucket)
}

func countVersions(ctx context.Context, c *objstore.Client, bucket, prefix string) (versions, markers int, err error) {
	var keyMarker, verMarker string
	for pages := 0; pages < 1000000; pages++ {
		page, err := c.ListObjectVersions(ctx, bucket, prefix, keyMarker, verMarker, 1000)
		if err != nil {
			return versions, markers, err
		}
		for _, v := range page.Versions {
			if v.DeleteMarker {
				markers++
			} else {
				versions++
			}
		}
		if !page.Truncated {
			return versions, markers, nil
		}
		keyMarker, verMarker = page.NextKey, page.NextVer
	}
	return versions, markers, fmt.Errorf("list versions %s did not terminate", bucket)
}
