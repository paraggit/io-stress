package objstore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/middleware"
)

// VersionMeta is the user metadata key that records the payload version ordinal.
const VersionMeta = "odf-version"

// PutResult is a finished PutObject or multipart upload.
type PutResult struct {
	ETag      string
	VersionID string
	RequestID string
}

// GetResult is a GetObject response. The caller closes Body.
type GetResult struct {
	Body      io.ReadCloser
	ETag      string
	Size      int64
	VersionID string
	RequestID string
	Metadata  map[string]string
}

// HeadResult is a HeadObject response.
type HeadResult struct {
	ETag      string
	Size      int64
	VersionID string
	RequestID string
	Metadata  map[string]string
}

// ObjectInfo is one ListObjectsV2 entry.
type ObjectInfo struct {
	Key  string
	Size int64
	ETag string
}

// VersionInfo is one ListObjectVersions entry, including delete markers.
type VersionInfo struct {
	Key          string
	VersionID    string
	IsLatest     bool
	Size         int64
	ETag         string
	DeleteMarker bool
}

// ObjectID identifies a key and, when set, a version.
type ObjectID struct {
	Key       string
	VersionID string
}

// KeyError is one DeleteObjects per-key failure inside an HTTP 200.
type KeyError struct {
	Key       string
	VersionID string
	Code      string
	Message   string
}

// PutObject writes size bytes from body(). body is a factory so a retry
// starts the stream over instead of buffering the object.
func (c *Client) PutObject(ctx context.Context, bucket, key string, size int64, meta map[string]string, body func() io.Reader) (PutResult, error) {
	var out PutResult
	err := c.withRetry(ctx, "PutObject", func(ctx context.Context) error {
		in := &s3.PutObjectInput{
			Bucket:        aws.String(bucket),
			Key:           aws.String(key),
			Body:          body(),
			ContentLength: aws.Int64(size),
		}
		if len(meta) > 0 {
			in.Metadata = meta
		}
		res, err := c.api.PutObject(ctx, in)
		if err != nil {
			return err
		}
		out = PutResult{
			ETag:      NormalizeETag(aws.ToString(res.ETag)),
			VersionID: aws.ToString(res.VersionId),
			RequestID: metaRequestID(res.ResultMetadata),
		}
		return nil
	})
	return out, err
}

// GetObject fetches an object or a byte range. rangeHeader is an HTTP Range
// value such as "bytes=0-99", or empty for the full object.
func (c *Client) GetObject(ctx context.Context, bucket, key, versionID, rangeHeader string) (GetResult, error) {
	var out GetResult
	err := c.withRetry(ctx, "GetObject", func(ctx context.Context) error {
		in := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
		if versionID != "" {
			in.VersionId = aws.String(versionID)
		}
		if rangeHeader != "" {
			in.Range = aws.String(rangeHeader)
		}
		res, err := c.api.GetObject(ctx, in)
		if err != nil {
			return err
		}
		size := aws.ToInt64(res.ContentLength)
		out = GetResult{
			Body:      res.Body,
			ETag:      NormalizeETag(aws.ToString(res.ETag)),
			Size:      size,
			VersionID: aws.ToString(res.VersionId),
			RequestID: metaRequestID(res.ResultMetadata),
			Metadata:  res.Metadata,
		}
		return nil
	})
	return out, err
}

// HeadObject checks size and ETag without the body.
func (c *Client) HeadObject(ctx context.Context, bucket, key, versionID string) (HeadResult, error) {
	var out HeadResult
	err := c.withRetry(ctx, "HeadObject", func(ctx context.Context) error {
		in := &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
		if versionID != "" {
			in.VersionId = aws.String(versionID)
		}
		res, err := c.api.HeadObject(ctx, in)
		if err != nil {
			return err
		}
		out = HeadResult{
			ETag:      NormalizeETag(aws.ToString(res.ETag)),
			Size:      aws.ToInt64(res.ContentLength),
			VersionID: aws.ToString(res.VersionId),
			RequestID: metaRequestID(res.ResultMetadata),
			Metadata:  res.Metadata,
		}
		return nil
	})
	return out, err
}

// ListPage is one ListObjectsV2 page.
type ListPage struct {
	Objects   []ObjectInfo
	Prefixes  []string
	NextToken string
	Truncated bool
	KeyCount  int
}

// ListObjectsV2 fetches one page. maxKeys <= 0 lets the service pick.
func (c *Client) ListObjectsV2(ctx context.Context, bucket, prefix, delimiter, token string, maxKeys int32) (ListPage, error) {
	var page ListPage
	err := c.withRetry(ctx, "ListObjectsV2", func(ctx context.Context) error {
		in := &s3.ListObjectsV2Input{Bucket: aws.String(bucket)}
		if prefix != "" {
			in.Prefix = aws.String(prefix)
		}
		if delimiter != "" {
			in.Delimiter = aws.String(delimiter)
		}
		if token != "" {
			in.ContinuationToken = aws.String(token)
		}
		if maxKeys > 0 {
			in.MaxKeys = aws.Int32(maxKeys)
		}
		res, err := c.api.ListObjectsV2(ctx, in)
		if err != nil {
			return err
		}
		page = ListPage{Truncated: aws.ToBool(res.IsTruncated), NextToken: aws.ToString(res.NextContinuationToken)}
		if res.KeyCount != nil {
			page.KeyCount = int(*res.KeyCount)
		}
		for _, o := range res.Contents {
			page.Objects = append(page.Objects, ObjectInfo{
				Key:  aws.ToString(o.Key),
				Size: aws.ToInt64(o.Size),
				ETag: NormalizeETag(aws.ToString(o.ETag)),
			})
		}
		for _, p := range res.CommonPrefixes {
			page.Prefixes = append(page.Prefixes, aws.ToString(p.Prefix))
		}
		return nil
	})
	return page, err
}

// VersionPage is one ListObjectVersions page.
type VersionPage struct {
	Versions  []VersionInfo
	NextKey   string
	NextVer   string
	Truncated bool
}

// ListObjectVersions fetches one page of versions and delete markers.
func (c *Client) ListObjectVersions(ctx context.Context, bucket, prefix, keyMarker, verMarker string, maxKeys int32) (VersionPage, error) {
	var page VersionPage
	err := c.withRetry(ctx, "ListObjectVersions", func(ctx context.Context) error {
		in := &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)}
		if prefix != "" {
			in.Prefix = aws.String(prefix)
		}
		if keyMarker != "" {
			in.KeyMarker = aws.String(keyMarker)
		}
		if verMarker != "" {
			in.VersionIdMarker = aws.String(verMarker)
		}
		if maxKeys > 0 {
			in.MaxKeys = aws.Int32(maxKeys)
		}
		res, err := c.api.ListObjectVersions(ctx, in)
		if err != nil {
			return err
		}
		page = VersionPage{Truncated: aws.ToBool(res.IsTruncated), NextKey: aws.ToString(res.NextKeyMarker), NextVer: aws.ToString(res.NextVersionIdMarker)}
		for _, v := range res.Versions {
			page.Versions = append(page.Versions, VersionInfo{
				Key:       aws.ToString(v.Key),
				VersionID: aws.ToString(v.VersionId),
				IsLatest:  aws.ToBool(v.IsLatest),
				Size:      aws.ToInt64(v.Size),
				ETag:      NormalizeETag(aws.ToString(v.ETag)),
			})
		}
		for _, m := range res.DeleteMarkers {
			page.Versions = append(page.Versions, VersionInfo{
				Key:          aws.ToString(m.Key),
				VersionID:    aws.ToString(m.VersionId),
				IsLatest:     aws.ToBool(m.IsLatest),
				DeleteMarker: true,
			})
		}
		return nil
	})
	return page, err
}

// DeleteObject removes the current version (or the version id, when set).
func (c *Client) DeleteObject(ctx context.Context, bucket, key, versionID string) error {
	return c.withRetry(ctx, "DeleteObject", func(ctx context.Context) error {
		in := &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
		if versionID != "" {
			in.VersionId = aws.String(versionID)
		}
		_, err := c.api.DeleteObject(ctx, in)
		return err
	})
}

// DeleteKeys deletes ids in batches of 1000. Per-key errors from an HTTP 200
// are returned and are not treated as success. A transport error is err.
func (c *Client) DeleteKeys(ctx context.Context, bucket string, ids []ObjectID) ([]KeyError, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = id.Key + "\x00" + id.VersionID
	}
	var all []KeyError
	for _, batch := range Chunk(keys, 1000) {
		objs := make([]types.ObjectIdentifier, len(batch))
		for i, packed := range batch {
			key, ver, _ := splitID(packed)
			objs[i] = types.ObjectIdentifier{Key: aws.String(key)}
			if ver != "" {
				objs[i].VersionId = aws.String(ver)
			}
		}
		var pageErrs []KeyError
		err := c.withRetry(ctx, "DeleteObjects", func(ctx context.Context) error {
			res, err := c.api.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucket),
				Delete: &types.Delete{Objects: objs, Quiet: aws.Bool(false)},
			})
			if err != nil {
				return err
			}
			pageErrs = pageErrs[:0]
			for _, e := range res.Errors {
				pageErrs = append(pageErrs, KeyError{
					Key:       aws.ToString(e.Key),
					VersionID: aws.ToString(e.VersionId),
					Code:      aws.ToString(e.Code),
					Message:   aws.ToString(e.Message),
				})
			}
			return nil
		})
		if err != nil {
			return all, err
		}
		all = append(all, pageErrs...)
	}
	return all, nil
}

func splitID(packed string) (string, string, bool) {
	for i := 0; i < len(packed); i++ {
		if packed[i] == 0 {
			return packed[:i], packed[i+1:], true
		}
	}
	return packed, "", false
}

// CopyObject server-side copies sourceKey onto destKey in the same bucket.
func (c *Client) CopyObject(ctx context.Context, bucket, sourceKey, destKey string) (PutResult, error) {
	var out PutResult
	err := c.withRetry(ctx, "CopyObject", func(ctx context.Context) error {
		src := url.PathEscape(bucket) + "/" + encodeKey(sourceKey)
		res, err := c.api.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket:     aws.String(bucket),
			Key:        aws.String(destKey),
			CopySource: aws.String(src),
		})
		if err != nil {
			return err
		}
		etag := ""
		if res.CopyObjectResult != nil {
			etag = NormalizeETag(aws.ToString(res.CopyObjectResult.ETag))
		}
		out = PutResult{ETag: etag, VersionID: aws.ToString(res.VersionId), RequestID: metaRequestID(res.ResultMetadata)}
		return nil
	})
	return out, err
}

func encodeKey(key string) string {
	// PathEscape keeps slashes in the key from being read as extra path
	// segments in the CopySource header.
	return url.PathEscape(key)
}

// CreateBucket creates one bucket. BucketAlreadyExists is returned as-is.
func (c *Client) CreateBucket(ctx context.Context, bucket string) error {
	return c.withRetry(ctx, "CreateBucket", func(ctx context.Context) error {
		_, err := c.api.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
		return err
	})
}

// DeleteBucket removes an empty bucket. BucketNotEmpty is returned as-is.
func (c *Client) DeleteBucket(ctx context.Context, bucket string) error {
	return c.withRetry(ctx, "DeleteBucket", func(ctx context.Context) error {
		_, err := c.api.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
		return err
	})
}

// PutBucketVersioning enables versioning on the bucket.
func (c *Client) PutBucketVersioning(ctx context.Context, bucket string) error {
	return c.withRetry(ctx, "PutBucketVersioning", func(ctx context.Context) error {
		_, err := c.api.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: aws.String(bucket),
			VersioningConfiguration: &types.VersioningConfiguration{
				Status: types.BucketVersioningStatusEnabled,
			},
		})
		return err
	})
}

// GetBucketVersioning returns the raw status string (Enabled, Suspended, or empty).
func (c *Client) GetBucketVersioning(ctx context.Context, bucket string) (string, error) {
	var status string
	err := c.withRetry(ctx, "GetBucketVersioning", func(ctx context.Context) error {
		res, err := c.api.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
		if err != nil {
			return err
		}
		status = string(res.Status)
		return nil
	})
	return status, err
}

// MultipartResult is a completed multipart upload.
type MultipartResult struct {
	PutResult
	PartETags []string
}

// UploadMultipart streams the body as parts of partSize and completes the upload.
// body must yield exactly size bytes and is consumed once. Retries restart
// from a fresh body factory.
func (c *Client) UploadMultipart(ctx context.Context, bucket, key string, size, partSize int64, meta map[string]string, body func() io.Reader) (MultipartResult, error) {
	var out MultipartResult
	err := c.withRetry(ctx, "Multipart", func(ctx context.Context) error {
		res, err := c.uploadMultipartOnce(ctx, bucket, key, size, partSize, meta, body())
		if err != nil {
			return err
		}
		out = res
		return nil
	})
	return out, err
}

func (c *Client) uploadMultipartOnce(ctx context.Context, bucket, key string, size, partSize int64, meta map[string]string, body io.Reader) (MultipartResult, error) {
	createIn := &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	if len(meta) > 0 {
		createIn.Metadata = meta
	}
	created, err := c.api.CreateMultipartUpload(ctx, createIn)
	if err != nil {
		return MultipartResult{}, err
	}
	uploadID := aws.ToString(created.UploadId)
	abort := true
	defer func() {
		if abort {
			_, _ = c.api.AbortMultipartUpload(context.Background(), &s3.AbortMultipartUploadInput{
				Bucket: aws.String(bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
			})
		}
	}()
	var parts []types.CompletedPart
	var partETags []string
	var partNum int32
	left := size
	for left > 0 {
		n := partSize
		if n > left {
			n = left
		}
		partNum++
		up, err := c.api.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(bucket),
			Key:           aws.String(key),
			UploadId:      aws.String(uploadID),
			PartNumber:    aws.Int32(partNum),
			Body:          io.LimitReader(body, n),
			ContentLength: aws.Int64(n),
		})
		if err != nil {
			return MultipartResult{}, err
		}
		etag := aws.ToString(up.ETag)
		parts = append(parts, types.CompletedPart{ETag: aws.String(etag), PartNumber: aws.Int32(partNum)})
		partETags = append(partETags, NormalizeETag(etag))
		left -= n
	}
	done, err := c.api.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return MultipartResult{}, err
	}
	abort = false
	return MultipartResult{
		PutResult: PutResult{
			ETag:      NormalizeETag(aws.ToString(done.ETag)),
			VersionID: aws.ToString(done.VersionId),
			RequestID: metaRequestID(done.ResultMetadata),
		},
		PartETags: partETags,
	}, nil
}

// AbortMultipart creates an upload, aborts it, and returns the upload id.
func (c *Client) AbortMultipart(ctx context.Context, bucket, key string) (string, error) {
	var uploadID string
	err := c.withRetry(ctx, "AbortMultipartUpload", func(ctx context.Context) error {
		created, err := c.api.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(bucket), Key: aws.String(key),
		})
		if err != nil {
			return err
		}
		uploadID = aws.ToString(created.UploadId)
		_, err = c.api.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket: aws.String(bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
		})
		return err
	})
	return uploadID, err
}

// ListMultipartUploads returns upload ids currently open for the bucket.
func (c *Client) ListMultipartUploads(ctx context.Context, bucket, prefix string) ([]string, error) {
	var ids []string
	err := c.withRetry(ctx, "ListMultipartUploads", func(ctx context.Context) error {
		ids = nil
		var marker *string
		for {
			in := &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket)}
			if prefix != "" {
				in.Prefix = aws.String(prefix)
			}
			if marker != nil {
				in.KeyMarker = marker
			}
			res, err := c.api.ListMultipartUploads(ctx, in)
			if err != nil {
				return err
			}
			for _, u := range res.Uploads {
				ids = append(ids, aws.ToString(u.UploadId))
			}
			if !aws.ToBool(res.IsTruncated) {
				return nil
			}
			marker = res.NextKeyMarker
			if marker == nil || aws.ToString(marker) == "" {
				return nil
			}
		}
	})
	return ids, err
}

// VersionOrdinal reads the payload version stored in object metadata.
// Missing metadata returns 0, false.
func VersionOrdinal(meta map[string]string) (int64, bool) {
	if meta == nil {
		return 0, false
	}
	for _, k := range []string{VersionMeta, "Odf-Version", "odf-version"} {
		if v, ok := meta[k]; ok {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

func metaRequestID(md middleware.Metadata) string {
	id, _ := awsmiddleware.GetRequestIDMetadata(md)
	return id
}

// EndpointFromHostPort builds an https URL from an OBC configmap.
func EndpointFromHostPort(host, port string) string {
	if host == "" {
		return ""
	}
	scheme := "https"
	if port == "80" {
		scheme = "http"
	}
	if port == "" || port == "443" || port == "80" {
		return scheme + "://" + host
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

// DefaultEndpoint is the in-cluster MCG service for a NooBaa namespace.
// The namespace is a parameter so a later RGW or provider-mode endpoint is
// a config change rather than a hard-coded openshift-storage string.
func DefaultEndpoint(namespace string) string {
	if namespace == "" {
		namespace = "openshift-storage"
	}
	return "https://s3." + namespace + ".svc:443"
}
