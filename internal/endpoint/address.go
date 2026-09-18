package endpoint

import (
	"net"
	"net/netip"
	"strings"
)

type hostKind uint8

const (
	hostInvalid hostKind = iota
	hostDNS
	hostIPv4
	hostIPv6
)

// Address is a validated, canonical endpoint network address.
type Address struct {
	host string
	port uint16
	kind hostKind
}

// NewAddress validates and canonicalizes a DNS name or IP address and port.
func NewAddress(host string, port int) (Address, error) {
	normalized, kind, err := normalizeHost(host, "address.host")
	if err != nil {
		return Address{}, err
	}
	if port < 1 || port > 65535 {
		return Address{}, invalid("address.port", "must be between 1 and 65535")
	}
	return Address{host: normalized, port: uint16(port), kind: kind}, nil
}

// Host returns the canonical host without IPv6 brackets.
func (a Address) Host() string { return a.host }

// Port returns the endpoint port.
func (a Address) Port() uint16 { return a.port }

func (a Address) valid() bool {
	return a.host != "" && a.port != 0 && a.kind >= hostDNS && a.kind <= hostIPv6
}

// String returns host:port using brackets for IPv6.
func (a Address) String() string {
	if !a.valid() {
		return "<invalid-address>"
	}
	return net.JoinHostPort(a.host, decimalPort(a.port))
}

func normalizeHost(raw, field string) (string, hostKind, error) {
	if raw == "" {
		return "", hostInvalid, invalid(field, "must not be empty")
	}
	if strings.TrimSpace(raw) != raw {
		return "", hostInvalid, invalid(field, "must not contain surrounding whitespace")
	}

	host := raw
	bracketed := false
	if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
		if len(host) < 3 || !strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]") {
			return "", hostInvalid, invalid(field, "has invalid IP address brackets")
		}
		bracketed = true
		host = host[1 : len(host)-1]
	}

	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" {
			return "", hostInvalid, invalid(field, "IPv6 zones are not supported")
		}
		if bracketed && address.Is4() {
			return "", hostInvalid, invalid(field, "brackets are only valid for IPv6 addresses")
		}
		kind := hostIPv6
		if address.Is4() {
			kind = hostIPv4
		}
		return address.String(), kind, nil
	}
	if strings.ContainsAny(host, ":[]") {
		return "", hostInvalid, invalid(field, "must be a valid DNS name or IP address")
	}

	if strings.HasSuffix(host, ".") {
		host = strings.TrimSuffix(host, ".")
	}
	host = strings.ToLower(host)
	if len(host) == 0 || len(host) > 253 {
		return "", hostInvalid, invalid(field, "must satisfy DNS length limits")
	}

	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 {
			return "", hostInvalid, invalid(field, "must contain valid DNS labels")
		}
		for index := range len(label) {
			character := label[index]
			alphanumeric := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
			if !alphanumeric && character != '-' {
				return "", hostInvalid, invalid(field, "must contain ASCII DNS labels")
			}
			if character == '-' && (index == 0 || index == len(label)-1) {
				return "", hostInvalid, invalid(field, "DNS labels must not start or end with a hyphen")
			}
		}
	}

	return host, hostDNS, nil
}

func decimalPort(port uint16) string {
	const digits = "0123456789"
	var buffer [5]byte
	index := len(buffer)
	for port > 0 {
		index--
		buffer[index] = digits[port%10]
		port /= 10
	}
	return string(buffer[index:])
}
