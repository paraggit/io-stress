package objstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
)

type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return false }

func TestRetryableTransport(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	reset := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	for _, err := range []error{
		refused, reset, io.EOF, io.ErrUnexpectedEOF,
		fmt.Errorf("read: %w", io.ErrUnexpectedEOF),
		timeoutNetErr{},
	} {
		if !retryable(err) {
			t.Fatalf("retryable(%v) = false, want true", err)
		}
	}
	if retryable(context.Canceled) {
		t.Fatal("context.Canceled must not be retryable")
	}
	if retryable(&apiErr{code: "AccessDenied", msg: "denied", status: 403}) {
		t.Fatal("AccessDenied must not be retryable")
	}
	if !retryable(context.DeadlineExceeded) {
		t.Fatal("context.DeadlineExceeded should stay retryable")
	}
}

func TestIsCopyLimit(t *testing.T) {
	for _, code := range []string{"EntityTooLarge", "InvalidRequest"} {
		if !IsCopyLimit(&apiErr{code: code, msg: code, status: 400}) {
			t.Fatalf("IsCopyLimit(%s) = false, want true", code)
		}
	}
	if IsCopyLimit(&apiErr{code: "SlowDown", msg: "slow", status: 503}) {
		t.Fatal("SlowDown must not be a copy limit")
	}
	if IsCopyLimit(errors.New("plain")) {
		t.Fatal("non-API error must not be a copy limit")
	}
}
