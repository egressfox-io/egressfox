package probe

import (
	"context"
	"net"
	"net/netip"
	"sort"

	"github.com/egressfox-io/egressfox/internal/observation"
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type netResolver struct{ resolver *net.Resolver }

func (resolver netResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return resolver.resolver.LookupNetIP(ctx, network, host)
}

type authorizedTarget struct {
	target  observation.HTTPTarget
	address netip.Addr
}

var sharedOrControlPrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

var metadataAddresses = map[netip.Addr]struct{}{
	netip.MustParseAddr("169.254.169.254"): {},
	netip.MustParseAddr("169.254.170.2"):   {},
	netip.MustParseAddr("100.100.100.200"): {},
	netip.MustParseAddr("fd00:ec2::254"):   {},
}

func authorizeTarget(ctx context.Context, resolver Resolver, target observation.HTTPTarget) (authorizedTarget, error) {
	requestURL := target.ExecutionURL()
	address, err := authorizeHost(ctx, resolver, requestURL.Hostname(), target.AllowsPrivateDestinations(), "target")
	if err != nil {
		return authorizedTarget{}, err
	}
	return authorizedTarget{target: target, address: address}, nil
}

func authorizeHost(ctx context.Context, resolver Resolver, host string, allowPrivate bool, subject string) (netip.Addr, error) {
	addresses := make([]netip.Addr, 0, 1)
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = append(addresses, literal)
	} else {
		resolved, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return netip.Addr{}, executionFailure(contextCode(ctx, subject+"_resolution"))
		}
		addresses = append(addresses, resolved...)
	}
	if len(addresses) == 0 {
		return netip.Addr{}, executionFailure(subject + "_resolution_empty")
	}
	for index := range addresses {
		addresses[index] = addresses[index].Unmap()
		if !authorizedAddress(addresses[index], allowPrivate) {
			return netip.Addr{}, executionFailure(subject + "_address_denied")
		}
	}
	sort.Slice(addresses, func(left, right int) bool { return addresses[left].Compare(addresses[right]) < 0 })
	return addresses[0], nil
}

func authorizedAddress(address netip.Addr, allowPrivate bool) bool {
	address = address.Unmap()
	if _, metadata := metadataAddresses[address]; metadata {
		return false
	}
	if !address.IsValid() || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || isSharedOrControlAddress(address) {
		return false
	}
	if !allowPrivate && (address.IsPrivate() || address.IsLoopback()) {
		return false
	}
	return true
}

func isSharedOrControlAddress(address netip.Addr) bool {
	for _, prefix := range sharedOrControlPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
