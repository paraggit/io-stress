package objstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeAccessDeniedIsUnsupported(t *testing.T) {
	if !IsUnsupported(&apiErr{code: "AccessDenied", msg: "denied", status: 403}) {
		t.Fatal("AccessDenied API error should be unsupported")
	}
	if !IsUnsupported(&apiErr{code: "Forbidden", msg: "forbidden", status: 403}) {
		t.Fatal("HTTP 403 should be unsupported")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method == http.MethodGet && q.Has("versioning") {
			w.Header().Set("Content-Type", "application/xml")
			w.Header().Set("x-amz-request-id", "req-ver")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Access Denied</Message><RequestId>req-ver</RequestId></Error>`)
			return
		}
		switch {
		case r.Header.Get("X-Amz-Copy-Source") != "":
			w.WriteHeader(http.StatusOK)
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
	if got.Capabilities[CapVersioning] != "unsupported" {
		t.Fatalf("versioning = %q detail %q", got.Capabilities[CapVersioning], got.Detail[CapVersioning])
	}
	if !strings.Contains(got.Detail[CapVersioning], "AccessDenied") {
		t.Fatalf("detail = %q", got.Detail[CapVersioning])
	}
}
