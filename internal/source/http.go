package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

const defaultHTTPTimeout = 15 * time.Second

var sourceSpecialUsePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// These known metadata destinations must be rejected before an opt-in for
// ordinary private/ULA sources. This list is deliberately not exhaustive;
// deployment egress policy remains the broader network boundary.
var sourceMetadataAddresses = map[netip.Addr]struct{}{
	netip.MustParseAddr("169.254.169.254"): {},
	netip.MustParseAddr("169.254.170.2"):   {},
	netip.MustParseAddr("100.100.100.200"): {},
	netip.MustParseAddr("fd00:ec2::254"):   {},
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type HTTPOptions struct {
	Limits               Limits
	Timeout              time.Duration
	MaxRedirects         int
	AllowHTTP            bool
	AllowPrivateNetworks bool
	AllowLoopback        bool
	AllowInsecureTLS     bool
	Headers              http.Header
	Resolver             Resolver
}

func (o HTTPOptions) String() string {
	return fmt.Sprintf("HTTP options timeout=%s redirects=%d headers=<redacted>", o.Timeout, o.MaxRedirects)
}

func (o HTTPOptions) GoString() string { return o.String() }

func (o HTTPOptions) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(o.String()))
}

func (HTTPOptions) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("HTTP source options JSON serialization is disabled because it may contain credentials")
}

type HTTP struct {
	source   endpoint.SourceID
	location *url.URL
	options  HTTPOptions
}

// FetchResult distinguishes an accepted response body from a conditional 304.
// Validators are provider-controlled metadata, not content identities.
type FetchResult struct {
	Payload      Payload
	NotModified  bool
	ETag         string
	LastModified string
}

func NewHTTP(source endpoint.SourceID, rawURL string, options HTTPOptions) (HTTP, error) {
	if source.String() == "<invalid-source-id>" {
		return HTTP{}, failure(source, ErrAcquire, "invalid_source_id")
	}
	limits, err := options.Limits.normalized()
	if err != nil {
		return HTTP{}, failure(source, ErrAcquire, "invalid_limits")
	}
	location, err := url.Parse(rawURL)
	if err != nil || location.Hostname() == "" || location.User != nil {
		return HTTP{}, failure(source, ErrAcquire, "invalid_url")
	}
	if location.Scheme != "https" && !(options.AllowHTTP && location.Scheme == "http") {
		return HTTP{}, failure(source, ErrAcquire, "scheme_not_allowed")
	}
	if len(rawURL) > 4096 || location.Fragment != "" {
		return HTTP{}, failure(source, ErrAcquire, "invalid_url")
	}
	options.Limits = limits
	if options.Timeout == 0 {
		options.Timeout = defaultHTTPTimeout
	}
	if options.Timeout < 0 {
		return HTTP{}, failure(source, ErrAcquire, "invalid_timeout")
	}
	if options.MaxRedirects == 0 {
		options.MaxRedirects = 3
	}
	if options.MaxRedirects < 0 {
		return HTTP{}, failure(source, ErrAcquire, "invalid_redirect_limit")
	}
	options.Headers = options.Headers.Clone()
	if options.Resolver == nil {
		options.Resolver = net.DefaultResolver
	}
	return HTTP{source: source, location: location, options: options}, nil
}

func (s HTTP) String() string                 { return fmt.Sprintf("http source=%s location=<redacted>", s.source) }
func (s HTTP) GoString() string               { return s.String() }
func (s HTTP) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(s.String())) }

func (s HTTP) Acquire(ctx context.Context) (Payload, error) {
	result, err := s.Fetch(ctx, "", "")
	if err == nil && result.NotModified {
		return Payload{}, failure(s.source, ErrAcquire, "unexpected_not_modified")
	}
	return result.Payload, err
}

func (s HTTP) Fetch(ctx context.Context, etag, lastModified string) (FetchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	transport := &http.Transport{
		DisableCompression:     true,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 16 << 10,
		DialContext:            s.dialContext,
	}
	if s.options.AllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
	}
	defer transport.CloseIdleConnections()
	origin := canonicalOrigin(s.location)
	client := &http.Client{Transport: transport}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > s.options.MaxRedirects {
			return failure(s.source, ErrAcquire, "redirect_limit")
		}
		if canonicalOrigin(request.URL) != origin {
			return failure(s.source, ErrAcquire, "redirect_origin")
		}
		request.Header.Del("Referer")
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.location.String(), nil)
	if err != nil {
		return FetchResult{}, failure(s.source, ErrAcquire, "request_config")
	}
	request.Header = s.options.Headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Del("If-None-Match")
	request.Header.Del("If-Modified-Since")
	if etag != "" && safeValidator(etag) {
		request.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" && safeValidator(lastModified) {
		request.Header.Set("If-Modified-Since", lastModified)
	}
	response, err := client.Do(request)
	if err != nil {
		var nested *Failure
		if errors.As(err, &nested) {
			return FetchResult{}, nested
		}
		var certificateError *tls.CertificateVerificationError
		var authorityError x509.UnknownAuthorityError
		if errors.As(err, &certificateError) || errors.As(err, &authorityError) {
			return FetchResult{}, failure(s.source, ErrAcquire, "tls_verification")
		}
		code := "request"
		if ctx.Err() == context.DeadlineExceeded {
			code = "timeout"
		}
		if ctx.Err() == context.Canceled {
			code = "cancelled"
		}
		return FetchResult{}, failure(s.source, ErrAcquire, code)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		result := FetchResult{NotModified: true}
		if value := response.Header.Get("ETag"); safeValidator(value) {
			result.ETag = value
		}
		if value := response.Header.Get("Last-Modified"); safeValidator(value) {
			result.LastModified = value
		}
		return result, nil
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		result := failure(s.source, ErrAcquire, "http_status").(*Failure)
		result.status = response.StatusCode
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			result.retryAfter = boundedRetryAfter(response.Header.Get("Retry-After"), time.Now())
		}
		return FetchResult{}, result
	}
	reader := io.LimitReader(response.Body, int64(s.options.Limits.MaxSourceBytes)+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return FetchResult{}, failure(s.source, ErrAcquire, "read")
	}
	if len(data) > s.options.Limits.MaxSourceBytes {
		return FetchResult{}, failure(s.source, ErrAcquire, "source_too_large")
	}
	contentType := response.Header.Get("Content-Type")
	if len(contentType) > 256 {
		contentType = ""
	}
	result := FetchResult{Payload: newPayload(s.source, data, contentType)}
	if value := response.Header.Get("ETag"); safeValidator(value) {
		result.ETag = value
	}
	if value := response.Header.Get("Last-Modified"); safeValidator(value) {
		result.LastModified = value
	}
	return result, nil
}

func boundedRetryAfter(value string, now time.Time) time.Duration {
	var delay time.Duration
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		if seconds > 2 {
			return 2 * time.Second
		}
		delay = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(value); err == nil {
		delay = at.Sub(now)
	}
	if delay < 0 {
		return 0
	}
	if delay > 2*time.Second {
		return 2 * time.Second
	}
	return delay
}

func safeValidator(value string) bool {
	if len(value) > 1024 {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

func (s HTTP) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, failure(s.source, ErrAcquire, "invalid_destination")
	}
	addresses, err := s.options.Resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, failure(s.source, ErrAcquire, "resolve")
	}
	for _, candidate := range addresses {
		candidate = candidate.Unmap()
		if !destinationAllowed(candidate, s.options.AllowPrivateNetworks, s.options.AllowLoopback) {
			continue
		}
		connection, dialErr := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialErr == nil {
			return connection, nil
		}
	}
	return nil, failure(s.source, ErrAcquire, "destination_not_allowed")
}

func publicAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range sourceSpecialUsePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func destinationAllowed(address netip.Addr, allowPrivate, allowLoopback bool) bool {
	address = address.Unmap()
	if _, metadata := sourceMetadataAddresses[address]; metadata {
		return false
	}
	if !address.IsValid() || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.IsLoopback() {
		return allowLoopback
	}
	if address.IsPrivate() {
		return allowPrivate
	}
	return publicAddress(address)
}

func canonicalOrigin(location *url.URL) string {
	port := location.Port()
	if port == "" {
		if location.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return strings.ToLower(location.Scheme) + "://" + strings.ToLower(location.Hostname()) + ":" + port
}
