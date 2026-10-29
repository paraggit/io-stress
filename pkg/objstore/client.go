package objstore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// ErrMismatch is a verification failure. It is never retried.
var ErrMismatch = errors.New("verification mismatch")

// apiErr is a smithy.APIError used for probe results synthesized from a
// per-key DeleteObjects error (those are not SDK errors).
type apiErr struct {
	code   string
	msg    string
	status int
}

func (e *apiErr) Error() string                 { return e.msg }
func (e *apiErr) ErrorCode() string             { return e.code }
func (e *apiErr) ErrorMessage() string          { return e.msg }
func (e *apiErr) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }
func (e *apiErr) HTTPStatusCode() int           { return e.status }

// Options builds a path-style S3 client. The control plane does not use this;
// only the in-cluster worker does.
type Options struct {
	Endpoint    string
	Region      string
	AccessKey   string
	SecretKey   string
	CAFile      string
	InsecureTLS bool
	MaxRetries  int
	OpTimeout   time.Duration
	Stats       *Stats
}

// Client is a path-style S3 client with its own retry counter.
type Client struct {
	api        *s3.Client
	stats      *Stats
	maxRetries int
	opTimeout  time.Duration
}

var insecureOnce sync.Once

// NewClient builds a path-style client. Virtual-host addressing does not work
// against the in-cluster service DNS name.
func NewClient(ctx context.Context, opt Options) (*Client, error) {
	if opt.Endpoint == "" {
		return nil, fmt.Errorf("s3 endpoint is empty")
	}
	if opt.Region == "" {
		opt.Region = "us-east-1"
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if opt.InsecureTLS {
		tlsConfig.InsecureSkipVerify = true
		insecureOnce.Do(func() {
			log.Printf("WARNING: object TLS verification is disabled (--object-insecure-tls / insecure_tls). The worker is not authenticating the S3 endpoint.")
		})
	} else if opt.CAFile != "" {
		pem, err := os.ReadFile(opt.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read service CA %s: %w", opt.CAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("service CA %s contained no certificates", opt.CAFile)
		}
		tlsConfig.RootCAs = pool
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(opt.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(opt.AccessKey, opt.SecretKey, "")),
		config.WithHTTPClient(&http.Client{Transport: transport}),
		config.WithRetryer(func() aws.Retryer { return aws.NopRetryer{} }),
	)
	if err != nil {
		return nil, fmt.Errorf("s3 config: %w", err)
	}
	endpoint := opt.Endpoint
	api := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(endpoint)
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	if opt.Stats == nil {
		opt.Stats = NewStats()
	}
	return &Client{api: api, stats: opt.Stats, maxRetries: opt.MaxRetries, opTimeout: opt.OpTimeout}, nil
}

// Stats returns the counters this client records into.
func (c *Client) Stats() *Stats { return c.stats }

func (c *Client) withRetry(ctx context.Context, op string, fn func(context.Context) error) error {
	max := c.maxRetries
	if max < 0 {
		max = 0
	}
	var last error
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		callCtx, cancel := c.callContext(ctx)
		start := time.Now()
		last = fn(callCtx)
		d := time.Since(start)
		cancel()
		c.stats.Observe(op, d)
		if last == nil {
			return nil
		}
		if !retryable(last) || attempt >= max {
			c.stats.NoteError(apiCode(last), RequestID(last))
			return last
		}
		c.stats.NoteRetry(apiCode(last), RequestID(last))
		timer := time.NewTimer(backoff(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.opTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.opTimeout)
}

func backoff(attempt int) time.Duration {
	d := 200 * time.Millisecond * time.Duration(1<<attempt)
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	jitter := time.Duration(rand.Int64N(int64(d/2) + 1))
	return d/2 + jitter
}

func retryable(err error) bool {
	if err == nil || errors.Is(err, ErrMismatch) || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	switch apiCode(err) {
	case "SlowDown", "ServiceUnavailable", "InternalError", "RequestTimeout", "RequestTimeTooSkewed",
		"Throttling", "ThrottlingException", "ProvisionedThroughputExceededException":
		return true
	case "NotImplemented", "MethodNotAllowed", "NoSuchKey", "NoSuchBucket", "AccessDenied",
		"InvalidArgument", "BucketAlreadyExists", "BucketAlreadyOwnedByYou", "NoSpace", "QuotaExceeded":
		return false
	}
	switch httpStatus(err) {
	case 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// IsCapacity reports a full-cluster stop, not an error to tally per object.
func IsCapacity(err error) bool {
	switch apiCode(err) {
	case "NoSpace", "QuotaExceeded":
		return true
	default:
		return false
	}
}

// IsUnsupported reports a capability this NooBaa build does not implement.
func IsUnsupported(err error) bool {
	switch apiCode(err) {
	case "NotImplemented", "MethodNotAllowed", "AccessDenied":
		return true
	}
	switch httpStatus(err) {
	case 501, 405, 403:
		return true
	default:
		return false
	}
}

// IsCopyLimit reports an object size or request shape RGW rejects for CopyObject.
func IsCopyLimit(err error) bool {
	switch apiCode(err) {
	case "EntityTooLarge", "InvalidRequest":
		return true
	default:
		return false
	}
}

func apiCode(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) && api.ErrorCode() != "" {
		return api.ErrorCode()
	}
	if st := httpStatus(err); st != 0 {
		return fmt.Sprintf("HTTP%d", st)
	}
	return "Unknown"
}

func httpStatus(err error) int {
	var re interface{ HTTPStatusCode() int }
	if errors.As(err, &re) {
		return re.HTTPStatusCode()
	}
	return 0
}

// RequestID returns the S3 request id carried on an SDK error, if any.
func RequestID(err error) string {
	var re interface{ ServiceRequestID() string }
	if errors.As(err, &re) {
		return re.ServiceRequestID()
	}
	var resp *smithyhttp.ResponseError
	if errors.As(err, &resp) && resp.Response != nil {
		return resp.Response.Header.Get("x-amz-request-id")
	}
	return ""
}

// Code returns the S3 error code for err.
func Code(err error) string { return apiCode(err) }

// StatusCode returns the HTTP status on err, or 0.
func StatusCode(err error) int { return httpStatus(err) }
