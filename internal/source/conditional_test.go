package source_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/source"
)

func TestConditionalFetchAndValidatorRemoval(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
				t.Error("first request was conditional")
			}
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
			_, _ = w.Write([]byte("first"))
		case 2:
			if r.Header.Get("If-None-Match") != `"v1"` || r.Header.Get("If-Modified-Since") != "Wed, 21 Oct 2015 07:28:00 GMT" {
				t.Error("missing validators")
			}
			w.Header().Set("ETag", `"v2"`)
			w.WriteHeader(http.StatusNotModified)
		default:
			if r.Header.Get("If-None-Match") != `"v1"` {
				t.Error("missing ETag")
			}
			_, _ = w.Write([]byte("second"))
		}
	}))
	defer server.Close()
	s, err := source.NewHTTP(sourceID(t, "conditional"), server.URL, source.HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, AllowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Fetch(context.Background(), "", "")
	if err != nil || first.NotModified || string(first.Payload.Bytes()) != "first" || first.ETag != `"v1"` || first.LastModified == "" {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := s.Fetch(context.Background(), first.ETag, first.LastModified)
	if err != nil || !second.NotModified || len(second.Payload.Bytes()) != 0 || second.ETag != `"v2"` {
		t.Fatalf("second = %+v, %v", second, err)
	}
	third, err := s.Fetch(context.Background(), first.ETag, "")
	if err != nil || third.NotModified || string(third.Payload.Bytes()) != "second" || third.ETag != "" || third.LastModified != "" {
		t.Fatalf("third = %+v, %v", third, err)
	}
}

func TestRetryFetchClassificationAndCancellation(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	s, _ := source.NewHTTP(sourceID(t, "retry"), server.URL, source.HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, AllowLoopback: true})
	result, err := source.RetryFetch(context.Background(), func(ctx context.Context) (source.FetchResult, error) { return s.Fetch(ctx, "", "") }, func(int) time.Duration { return 0 })
	if err != nil || string(result.Payload.Bytes()) != "ok" || calls.Load() != 3 {
		t.Fatalf("retry = %v, %v, calls=%d", result, err, calls.Load())
	}
	calls.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	_, err = source.RetryFetch(ctx, func(ctx context.Context) (source.FetchResult, error) {
		return s.Fetch(ctx, "", "")
	}, func(int) time.Duration { cancel(); return time.Second })
	if err == nil || calls.Load() != 1 {
		t.Fatalf("cancel = %v, calls=%d", err, calls.Load())
	}
}

func TestHTTPSVerificationNeedsExplicitOptIn(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer server.Close()
	secure, err := source.NewHTTP(sourceID(t, "tls-secure"), server.URL, source.HTTPOptions{AllowPrivateNetworks: true, AllowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secure.Fetch(context.Background(), "", ""); err == nil || source.Retryable(err) {
		t.Fatalf("untrusted TLS was accepted or retried: %v", err)
	}
	insecure, err := source.NewHTTP(sourceID(t, "tls-opt-in"), server.URL, source.HTTPOptions{AllowPrivateNetworks: true, AllowLoopback: true, AllowInsecureTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := insecure.Fetch(context.Background(), "", "")
	if err != nil || string(result.Payload.Bytes()) != "ok" {
		t.Fatalf("explicit TLS opt-in = %v, %v", result, err)
	}
}

func TestHTTPRetryStatusClassification(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(http.StatusTooManyRequests)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(int(status.Load()))
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	s, _ := source.NewHTTP(sourceID(t, "status-retry"), server.URL, source.HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, AllowLoopback: true})
	result, err := source.RetryFetch(context.Background(), func(ctx context.Context) (source.FetchResult, error) { return s.Fetch(ctx, "", "") }, func(int) time.Duration { return 0 })
	if err != nil || string(result.Payload.Bytes()) != "ok" || calls.Load() != 2 {
		t.Fatalf("429 retry = %v, %v, %d", result, err, calls.Load())
	}
	calls.Store(0)
	status.Store(http.StatusUnauthorized)
	_, err = source.RetryFetch(context.Background(), func(ctx context.Context) (source.FetchResult, error) { return s.Fetch(ctx, "", "") }, func(int) time.Duration { return 0 })
	if err == nil || calls.Load() != 1 || source.Retryable(err) {
		t.Fatalf("401 retry = %v, %d", err, calls.Load())
	}
}
