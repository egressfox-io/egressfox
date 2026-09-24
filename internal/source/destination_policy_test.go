package source

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func TestDestinationPolicySpecialUse(t *testing.T) {
	for _, test := range []struct {
		address                   string
		public, private, loopback bool
	}{
		{"8.8.8.8", true, true, true},
		{"2606:4700:4700::1111", true, true, true},
		{"10.24.0.8", false, true, true},
		{"::ffff:10.24.0.8", false, true, true},
		{"172.20.0.8", false, true, true},
		{"192.168.1.8", false, true, true},
		{"fd00::8", false, true, true},
		{"fd00:ec2::254", false, false, false},
		{"127.0.0.1", false, false, true},
		{"::1", false, false, true},
		{"::ffff:127.0.0.1", false, false, true},
		{"169.254.169.254", false, false, false},
		{"169.254.170.2", false, false, false},
		{"169.254.1.2", false, false, false},
		{"fe80::1", false, false, false},
		{"::ffff:169.254.169.254", false, false, false},
		{"::ffff:100.100.100.200", false, false, false},
		{"100.100.100.200", false, false, false},
		{"224.0.0.1", false, false, false},
		{"ff02::1", false, false, false},
		{"0.0.0.0", false, false, false},
		{"::", false, false, false},
		{"198.18.0.1", false, false, false},
		{"192.0.2.1", false, false, false},
		{"2001:db8::1", false, false, false},
	} {
		t.Run(test.address, func(t *testing.T) {
			address := netip.MustParseAddr(test.address)
			for _, option := range []struct {
				private, loopback, want bool
			}{{false, false, test.public}, {true, false, test.private}, {true, true, test.loopback}} {
				if got := destinationAllowed(address, option.private, option.loopback); got != option.want {
					t.Fatalf("destinationAllowed(%s, %v, %v) = %v, want %v", test.address, option.private, option.loopback, got, option.want)
				}
			}
		})
	}
}

type changingResolver struct {
	addresses []netip.Addr
	lookups   atomic.Int32
}

func (r *changingResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	index := int(r.lookups.Add(1)) - 1
	if index >= len(r.addresses) {
		index = len(r.addresses) - 1
	}
	return []netip.Addr{r.addresses[index]}, nil
}

func TestDialAuthorizationRechecksRedirectAndDNS(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/redirect" {
			w.Header().Set("Connection", "close")
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		if r.URL.Path == "/retry" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("synthetic"))
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	loopback := netip.MustParseAddr(host)
	for _, prohibited := range []string{"169.254.169.254", "fd00:ec2::254"} {
		for _, path := range []string{"/direct", "/redirect", "/retry"} {
			resolver := &changingResolver{addresses: []netip.Addr{loopback, netip.MustParseAddr(prohibited)}}
			sourceID, err := endpoint.NewSourceID("controlled")
			if err != nil {
				t.Fatal(err)
			}
			location := "http://provider.test:" + port + path
			fetcher, err := NewHTTP(sourceID, location, HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, AllowLoopback: true, Resolver: resolver})
			if err != nil {
				t.Fatal(err)
			}
			if path == "/direct" {
				if _, err := fetcher.Acquire(context.Background()); err != nil {
					t.Fatal(err)
				}
				if _, err := fetcher.Acquire(context.Background()); err == nil {
					t.Fatal("changed DNS answer reached prohibited address")
				}
			} else if path == "/redirect" {
				if _, err := fetcher.Acquire(context.Background()); err == nil {
					t.Fatal("redirect bypassed changed DNS authorization")
				}
			} else {
				if _, err := RetryFetch(context.Background(), func(ctx context.Context) (FetchResult, error) {
					return fetcher.Fetch(ctx, "", "")
				}, func(int) time.Duration { return 0 }); err == nil {
					t.Fatal("retry bypassed changed DNS authorization")
				}
			}
			if resolver.lookups.Load() != 2 {
				t.Fatalf("expected two dial-time resolutions, got %d", resolver.lookups.Load())
			}
		}
	}
	if requests.Load() != 6 {
		t.Fatalf("unexpected controlled request count: %d", requests.Load())
	}
}

func TestMetadataHostnameCannotDialWithPrivatePermission(t *testing.T) {
	sourceID, _ := endpoint.NewSourceID("metadata-hostname")
	for _, value := range []string{"169.254.169.254", "fd00:ec2::254"} {
		resolver := &changingResolver{addresses: []netip.Addr{netip.MustParseAddr(value)}}
		fetcher, err := NewHTTP(sourceID, "http://provider.test/subscription", HTTPOptions{AllowHTTP: true, AllowPrivateNetworks: true, AllowLoopback: true, Resolver: resolver})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fetcher.Acquire(context.Background()); err == nil || resolver.lookups.Load() != 1 {
			t.Fatalf("metadata hostname dial = %v, lookups=%d", err, resolver.lookups.Load())
		}
	}
}
