package source_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/source"
)

func TestHTTPAcquisition(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Error("missing authorization")
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "trojan://secret@example.com:443")
	}))
	defer server.Close()
	httpSource, err := source.NewHTTP(sourceID(t, "http-source"), server.URL, source.HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, Headers: http.Header{"Authorization": {"Bearer synthetic"}}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := httpSource.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Bytes()) == 0 || payload.ContentType() != "text/plain" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestHTTPFailuresAreBoundedAndRedacted(t *testing.T) {
	t.Parallel()
	secret := "source-token-canary"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("x", 100), http.StatusUnauthorized)
	}))
	defer server.Close()
	httpSource, err := source.NewHTTP(sourceID(t, "http-source"), server.URL+"?token="+secret, source.HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, Headers: http.Header{"Authorization": {"Bearer " + secret}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = httpSource.Acquire(context.Background())
	if err == nil || strings.Contains(fmt.Sprintf("%v %+v %#v", err, httpSource, httpSource), secret) {
		t.Fatal("HTTP diagnostic leaked or missing")
	}
	blocked, _ := source.NewHTTP(sourceID(t, "blocked"), server.URL, source.HTTPOptions{AllowHTTP: true})
	if _, err = blocked.Acquire(context.Background()); err == nil {
		t.Fatal("private destination unexpectedly allowed")
	}
}

func TestHTTPSizeTimeoutAndRedirectPolicy(t *testing.T) {
	t.Parallel()
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "123456") }))
	defer large.Close()
	limits := source.DefaultLimits()
	limits.MaxSourceBytes = 5
	s, _ := source.NewHTTP(sourceID(t, "large"), large.URL, source.HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, Limits: limits})
	if _, err := s.Acquire(context.Background()); err == nil {
		t.Fatal("oversized response accepted")
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	s, _ = source.NewHTTP(sourceID(t, "slow"), slow.URL, source.HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, Timeout: 20 * time.Millisecond})
	if _, err := s.Acquire(context.Background()); err == nil {
		t.Fatal("timeout accepted")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	s, _ = source.NewHTTP(sourceID(t, "redirect"), redirect.URL, source.HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true})
	if _, err := s.Acquire(context.Background()); err == nil {
		t.Fatal("cross-origin redirect accepted")
	}
}
