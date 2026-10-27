package objstore

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Capability names recorded by the probe and copied onto later worker specs.
const (
	CapVersioning    = "versioning"
	CapMultipart     = "multipart"
	CapListVersions  = "list-object-versions"
	CapListMultipart = "list-multipart-uploads"
	CapCopy          = "copy"
	CapBatchDelete   = "batch-delete"
)

// ProbeResult is the R10 capability map for one bucket.
// Values are "supported" or "unsupported". Detail holds the probe response
// for anything unsupported, so a skip can name the reason.
type ProbeResult struct {
	Capabilities map[string]string
	Detail       map[string]string
}

// Supported reports whether the probe marked cap as supported.
// A missing entry is treated as supported so a phase still attempts the call
// when no probe ran.
func (p ProbeResult) Supported(cap string) bool {
	if p.Capabilities == nil {
		return true
	}
	v, ok := p.Capabilities[cap]
	if !ok {
		return true
	}
	return v != "unsupported" && !strings.HasPrefix(v, "unsupported")
}

// Probe runs one cheap call per optional capability. NotImplemented,
// MethodNotAllowed, and HTTP 501/405 mark the capability unsupported.
// Any other error fails the probe.
func Probe(ctx context.Context, c *Client, bucket string) (ProbeResult, error) {
	out := ProbeResult{
		Capabilities: map[string]string{},
		Detail:       map[string]string{},
	}
	const probeKey = "objstress-probe/object"
	const probeCopy = "objstress-probe/copy"

	if _, err := c.PutObject(ctx, bucket, probeKey, 4, nil, func() io.Reader { return strings.NewReader("prob") }); err != nil {
		return out, fmt.Errorf("probe put %s: %w", bucket, err)
	}
	defer func() {
		_ = c.DeleteObject(context.Background(), bucket, probeKey, "")
		_ = c.DeleteObject(context.Background(), bucket, probeCopy, "")
	}()

	if _, err := c.HeadObject(ctx, bucket, probeKey, ""); err != nil && !IsUnsupported(err) {
		return out, fmt.Errorf("probe head %s: %w", bucket, err)
	}

	if err := out.mark(CapCopy, copyErr(ctx, c, bucket, probeKey, probeCopy)); err != nil {
		return out, fmt.Errorf("probe copy %s: %w", bucket, err)
	}
	if err := out.mark(CapMultipart, abortErr(ctx, c, bucket, probeKey)); err != nil {
		return out, fmt.Errorf("probe multipart %s: %w", bucket, err)
	}
	if err := out.mark(CapListMultipart, listUploadErr(ctx, c, bucket, probeKey)); err != nil {
		return out, fmt.Errorf("probe list multipart %s: %w", bucket, err)
	}
	if err := out.mark(CapVersioning, versioningErr(ctx, c, bucket)); err != nil {
		return out, fmt.Errorf("probe versioning %s: %w", bucket, err)
	}
	if err := out.mark(CapListVersions, listVerErr(ctx, c, bucket, probeKey)); err != nil {
		return out, fmt.Errorf("probe list versions %s: %w", bucket, err)
	}
	if err := out.mark(CapBatchDelete, batchErr(ctx, c, bucket, probeCopy)); err != nil {
		return out, fmt.Errorf("probe batch delete %s: %w", bucket, err)
	}
	return out, nil
}

func (p *ProbeResult) mark(cap string, err error) error {
	if err == nil {
		p.Capabilities[cap] = "supported"
		return nil
	}
	if IsUnsupported(err) || IsUnsupportedCode(Code(err)) {
		p.Capabilities[cap] = "unsupported"
		p.Detail[cap] = fmt.Sprintf("%s HTTP %d: %s", Code(err), StatusCode(err), err.Error())
		return nil
	}
	return err
}

func copyErr(ctx context.Context, c *Client, bucket, src, dst string) error {
	_, err := c.CopyObject(ctx, bucket, src, dst)
	return err
}

func abortErr(ctx context.Context, c *Client, bucket, key string) error {
	_, err := c.AbortMultipart(ctx, bucket, key+"-abort")
	return err
}

func listUploadErr(ctx context.Context, c *Client, bucket, prefix string) error {
	_, err := c.ListMultipartUploads(ctx, bucket, prefix)
	return err
}

func versioningErr(ctx context.Context, c *Client, bucket string) error {
	_, err := c.GetBucketVersioning(ctx, bucket)
	return err
}

func listVerErr(ctx context.Context, c *Client, bucket, prefix string) error {
	_, err := c.ListObjectVersions(ctx, bucket, prefix, "", "", 1)
	return err
}

func batchErr(ctx context.Context, c *Client, bucket, key string) error {
	errs, err := c.DeleteKeys(ctx, bucket, []ObjectID{{Key: key}})
	if err != nil {
		return err
	}
	for _, e := range errs {
		if e.Code == "" || e.Code == "NoSuchKey" {
			continue
		}
		return &apiErr{code: e.Code, msg: e.Code + ": " + e.Message}
	}
	return nil
}

// IsUnsupportedCode reports a per-key error code that means "not implemented".
func IsUnsupportedCode(code string) bool {
	return code == "NotImplemented" || code == "MethodNotAllowed"
}
