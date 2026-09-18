package endpoint_test

import (
	"errors"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func TestNewAddressCanonicalizesEquivalentHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "DNS case and root dot", raw: "Edge.Example.COM.", want: "edge.example.com"},
		{name: "IPv4", raw: "192.0.2.10", want: "192.0.2.10"},
		{name: "IPv4 with root dot", raw: "192.0.2.10.", want: "192.0.2.10"},
		{name: "expanded IPv6", raw: "2001:0db8:0:0:0:0:0:1", want: "2001:db8::1"},
		{name: "bracketed IPv6", raw: "[2001:db8::1]", want: "2001:db8::1"},
		{name: "IPv4-mapped IPv6", raw: "::ffff:192.0.2.10", want: "::ffff:192.0.2.10"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			address, err := endpoint.NewAddress(test.raw, 443)
			if err != nil {
				t.Fatalf("NewAddress() error = %v", err)
			}
			if got := address.Host(); got != test.want {
				t.Fatalf("Host() = %q, want %q", got, test.want)
			}
			if got := address.Port(); got != 443 {
				t.Fatalf("Port() = %d, want 443", got)
			}
		})
	}
}

func TestNewAddressRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		port int
	}{
		{name: "empty host", port: 443},
		{name: "leading whitespace", host: " edge.example.com", port: 443},
		{name: "URI", host: "https://edge.example.com/secret", port: 443},
		{name: "empty DNS label", host: "edge..example.com", port: 443},
		{name: "DNS underscore", host: "edge_name.example.com", port: 443},
		{name: "DNS leading hyphen", host: "-edge.example.com", port: 443},
		{name: "Unicode without IDNA", host: "egress.例子", port: 443},
		{name: "unbalanced brackets", host: "[2001:db8::1", port: 443},
		{name: "bracketed IPv4", host: "[192.0.2.1]", port: 443},
		{name: "IPv6 zone", host: "fe80::1%en0", port: 443},
		{name: "zero port", host: "edge.example.com"},
		{name: "large port", host: "edge.example.com", port: 65536},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := endpoint.NewAddress(test.host, test.port)
			if !errors.Is(err, endpoint.ErrInvalid) {
				t.Fatalf("NewAddress() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestIPv4MappedIPv6RemainsDistinct(t *testing.T) {
	t.Parallel()

	ipv4 := mustAddress(t, "192.0.2.10", 443)
	mapped := mustAddress(t, "::ffff:192.0.2.10", 443)
	if ipv4 == mapped {
		t.Fatal("IPv4-mapped IPv6 address collapsed into IPv4")
	}
}

func FuzzAddressCanonicalRoundTrip(f *testing.F) {
	f.Add("Edge.Example.COM.", 443)
	f.Add("[2001:0db8::1]", 8443)
	f.Add("192.0.2.10", 1)
	f.Add("not a host", -1)

	f.Fuzz(func(t *testing.T, host string, port int) {
		address, err := endpoint.NewAddress(host, port)
		if err != nil {
			return
		}
		roundTrip, err := endpoint.NewAddress(address.Host(), int(address.Port()))
		if err != nil {
			t.Fatalf("canonical address rejected: %v", err)
		}
		if address != roundTrip {
			t.Fatalf("round trip changed address: %v != %v", address, roundTrip)
		}
	})
}

func mustAddress(t testing.TB, host string, port int) endpoint.Address {
	t.Helper()
	address, err := endpoint.NewAddress(host, port)
	if err != nil {
		t.Fatalf("NewAddress() error = %v", err)
	}
	return address
}
