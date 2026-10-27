package objstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListObjectsV2Paginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") != "2" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		token := r.URL.Query().Get("continuation-token")
		if token == "" {
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bkt</Name><KeyCount>1</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>true</IsTruncated>
  <NextContinuationToken>tok-2</NextContinuationToken>
  <Contents><Key>a</Key><Size>1</Size><ETag>&quot;aaa&quot;</ETag><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`)
			return
		}
		if token != "tok-2" {
			t.Errorf("continuation token = %q", token)
		}
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bkt</Name><KeyCount>1</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated>
  <Contents><Key>b</Key><Size>2</Size><ETag>&quot;bbb&quot;</ETag><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`)
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, 2)
	var keys []string
	token := ""
	for {
		page, err := c.ListObjectsV2(context.Background(), "bkt", "", "", token, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range page.Objects {
			keys = append(keys, o.Key)
		}
		if !page.Truncated {
			break
		}
		token = page.NextToken
	}
	if strings.Join(keys, ",") != "a,b" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestDeleteObjectsPerKeyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("delete") == "" && !strings.Contains(r.URL.RawQuery, "delete") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Deleted><Key>ok</Key></Deleted>
  <Error><Key>bad</Key><Code>InternalError</Code><Message>nope</Message></Error>
</DeleteResult>`)
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, 0)
	errs, err := c.DeleteKeys(context.Background(), "bkt", []ObjectID{{Key: "ok"}, {Key: "bad"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 || errs[0].Key != "bad" || errs[0].Code != "InternalError" {
		t.Fatalf("errs = %+v", errs)
	}
}

func TestListObjectVersionsDeleteMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["versions"]; !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bkt</Name><IsTruncated>false</IsTruncated><MaxKeys>1000</MaxKeys>
  <Version><Key>k</Key><VersionId>v1</VersionId><IsLatest>false</IsLatest>
    <LastModified>2020-01-01T00:00:00.000Z</LastModified><ETag>&quot;abc&quot;</ETag><Size>4</Size><StorageClass>STANDARD</StorageClass></Version>
  <DeleteMarker><Key>k</Key><VersionId>dm1</VersionId><IsLatest>true</IsLatest>
    <LastModified>2020-01-01T00:00:00.000Z</LastModified></DeleteMarker>
</ListVersionsResult>`)
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, 0)
	page, err := c.ListObjectVersions(context.Background(), "bkt", "", "", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Versions) != 2 {
		t.Fatalf("versions = %+v", page.Versions)
	}
	var sawVer, sawMarker bool
	for _, v := range page.Versions {
		if v.DeleteMarker && v.VersionID == "dm1" {
			sawMarker = true
		}
		if !v.DeleteMarker && v.VersionID == "v1" {
			sawVer = true
		}
	}
	if !sawVer || !sawMarker {
		t.Fatalf("versions = %+v", page.Versions)
	}
}

func TestSlowDownIsRetried(t *testing.T) {
	var puts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		puts++
		if puts == 1 {
			w.Header().Set("Content-Type", "application/xml")
			w.Header().Set("x-amz-request-id", "req-slow")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>SlowDown</Code><Message>slow</Message><RequestId>req-slow</RequestId></Error>`)
			return
		}
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("x-amz-request-id", "req-ok")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, 3)
	_, err := c.PutObject(context.Background(), "bkt", "k", 4, nil, func() io.Reader { return strings.NewReader("prob") })
	if err != nil {
		t.Fatal(err)
	}
	if puts != 2 {
		t.Fatalf("puts = %d, want 2", puts)
	}
	if c.Stats().Retries() != 1 {
		t.Fatalf("retries = %d", c.Stats().Retries())
	}
	if c.Stats().Errors()["SlowDown"] != 1 {
		t.Fatalf("errors = %+v", c.Stats().Errors())
	}
}

func TestProbeNotImplementedSkipsCapability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Copy-Source") != "" {
			w.Header().Set("Content-Type", "application/xml")
			w.Header().Set("x-amz-request-id", "req-copy")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>NotImplemented</Code><Message>no copy</Message><RequestId>req-copy</RequestId></Error>`)
			return
		}
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPut && q.Has("versioning"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && q.Has("versioning"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
		case r.Method == http.MethodPost && q.Has("uploads"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Bucket>bkt</Bucket><Key>k</Key><UploadId>u1</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodDelete && q.Has("uploadId"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && q.Has("uploads"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListMultipartUploadsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>`)
		case r.Method == http.MethodGet && q.Has("versions"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>bkt</Name><IsTruncated>false</IsTruncated></ListVersionsResult>`)
		case r.Method == http.MethodPost && q.Has("delete"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></DeleteResult>`)
		case r.Method == http.MethodHead:
			w.Header().Set("Content-Length", "4")
			w.Header().Set("ETag", `"abc"`)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("ETag", `"abc"`)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, 1)
	got, err := Probe(context.Background(), c, "bkt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Capabilities[CapCopy] != "unsupported" {
		t.Fatalf("copy = %q detail %q", got.Capabilities[CapCopy], got.Detail[CapCopy])
	}
	if !strings.Contains(got.Detail[CapCopy], "NotImplemented") {
		t.Fatalf("detail = %q", got.Detail[CapCopy])
	}
	if got.Capabilities[CapMultipart] != "supported" {
		t.Fatalf("multipart = %q", got.Capabilities[CapMultipart])
	}
	if got.Supported(CapCopy) {
		t.Fatal("unsupported copy must not report supported")
	}
}

func testClient(t *testing.T, endpoint string, retries int) *Client {
	t.Helper()
	c, err := NewClient(context.Background(), Options{
		Endpoint:   endpoint,
		Region:     "us-east-1",
		AccessKey:  "AKID",
		SecretKey:  "SECRET",
		MaxRetries: retries,
		OpTimeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
