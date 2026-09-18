package source

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

const defaultHTTPTimeout = 15 * time.Second

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type HTTPOptions struct {
	Limits               Limits
	Timeout              time.Duration
	MaxRedirects         int
	AllowHTTP            bool
	AllowPrivateNetworks bool
	Headers              http.Header
	Resolver             Resolver
}

type HTTP struct {
	source   endpoint.SourceID
	location *url.URL
	options  HTTPOptions
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
	ctx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	transport := &http.Transport{
		DisableCompression: true,
		DialContext:        s.dialContext,
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
		return Payload{}, failure(s.source, ErrAcquire, "request")
	}
	request.Header = s.options.Headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		code := "request"
		if ctx.Err() == context.DeadlineExceeded {
			code = "timeout"
		}
		if ctx.Err() == context.Canceled {
			code = "cancelled"
		}
		return Payload{}, failure(s.source, ErrAcquire, code)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		result := failure(s.source, ErrAcquire, "http_status").(*Failure)
		result.status = response.StatusCode
		return Payload{}, result
	}
	reader := io.LimitReader(response.Body, int64(s.options.Limits.MaxSourceBytes)+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return Payload{}, failure(s.source, ErrAcquire, "read")
	}
	if len(data) > s.options.Limits.MaxSourceBytes {
		return Payload{}, failure(s.source, ErrAcquire, "source_too_large")
	}
	return newPayload(s.source, data, response.Header.Get("Content-Type")), nil
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
		if !s.options.AllowPrivateNetworks && !publicAddress(candidate) {
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
	if address.Is4() {
		value := address.As4()
		if value[0] == 100 && value[1]&0xc0 == 64 {
			return false
		}
	}
	return true
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
