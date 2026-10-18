package provider

import (
	"context"
	stderrors "errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func okResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// newStubClient 前 len(errs) 次请求依次返回 errs 中的错误，之后返回 200
func newStubClient(maxRetries int, errs ...error) (*HTTPClient, *atomic.Int32) {
	var calls atomic.Int32
	c := NewHTTPClient("https://example.test", time.Second)
	c.SetMaxRetries(maxRetries)
	c.retryBackoff = time.Millisecond
	c.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		n := int(calls.Add(1))
		if n <= len(errs) {
			return nil, errs[n-1]
		}
		return okResponse("ok"), nil
	})
	return c, &calls
}

func dnsTimeout() error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{
		Err: "read udp 127.0.0.1:1->127.0.0.11:53: i/o timeout", Name: "api.example.cn", Server: "127.0.0.11:53", IsTimeout: true,
	}}
}

func TestHTTPClientRetriesNetworkErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"dns timeout", dnsTimeout()},
		{"connection refused", &net.OpError{Op: "dial", Net: "tcp", Err: stderrors.New("connect: connection refused")}},
		{"connection reset", &net.OpError{Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}}},
		{"unexpected eof", io.ErrUnexpectedEOF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, calls := newStubClient(2, tc.err, tc.err)
			data, err := c.Get(context.Background(), "/x", nil)
			if err != nil {
				t.Fatalf("Get() error = %v, want success after retries", err)
			}
			if string(data) != "ok" {
				t.Fatalf("Get() = %q, want ok", data)
			}
			if got := calls.Load(); got != 3 {
				t.Fatalf("calls = %d, want 3", got)
			}
		})
	}
}

func TestHTTPClientDoesNotRetryPermanentErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"no such host", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}}},
		{"client timeout", &timeoutError{}},
		{"tls error", stderrors.New("tls: failed to verify certificate")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, calls := newStubClient(2, tc.err)
			if _, err := c.Get(context.Background(), "/x", nil); err == nil {
				t.Fatal("Get() error = nil, want error")
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("calls = %d, want 1", got)
			}
		})
	}
}

func TestHTTPClientGivesUpAfterMaxRetries(t *testing.T) {
	c, calls := newStubClient(2, dnsTimeout(), dnsTimeout(), dnsTimeout())
	_, err := c.Get(context.Background(), "/x", nil)
	if err == nil || !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("Get() error = %v, want the last dns error", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3 (1 + 2 retries)", got)
	}
}

func TestHTTPClientZeroRetries(t *testing.T) {
	c, calls := newStubClient(0, dnsTimeout())
	if _, err := c.Get(context.Background(), "/x", nil); err == nil {
		t.Fatal("Get() error = nil, want error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestHTTPClientRetryResendsBody(t *testing.T) {
	var bodies []string
	c := NewHTTPClient("https://example.test", time.Second)
	c.SetMaxRetries(1)
	c.retryBackoff = time.Millisecond
	c.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			return nil, dnsTimeout()
		}
		return okResponse("ok"), nil
	})

	if _, err := c.Post(context.Background(), "/x", strings.NewReader(`{"a":1}`)); err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if len(bodies) != 2 || bodies[0] != `{"a":1}` || bodies[1] != `{"a":1}` {
		t.Fatalf("bodies = %q, want the same body sent twice", bodies)
	}
}

func TestHTTPClientStopsRetryingWhenContextDone(t *testing.T) {
	c, calls := newStubClient(5, dnsTimeout(), dnsTimeout(), dnsTimeout())
	c.retryBackoff = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, "/x", nil); err == nil {
		t.Fatal("Get() error = nil, want error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

// 用真实 TCP 连接验证：服务端直接 RST 时会被识别为可重试错误
func TestHTTPClientRetriesRealConnectionReset(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.(*net.TCPConn).SetLinger(0)
			conn.Close()
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, time.Second)
	c.SetMaxRetries(1)
	c.retryBackoff = time.Millisecond

	data, err := c.Get(context.Background(), "/x", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(data) != "ok" || calls.Load() != 2 {
		t.Fatalf("data = %q, calls = %d, want ok after 2 calls", data, calls.Load())
	}
}

type timeoutError struct{}

func (*timeoutError) Error() string   { return "Client.Timeout exceeded while awaiting headers" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }
