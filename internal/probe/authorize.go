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
}

func authorizeTarget(ctx context.Context, resolver Resolver, target observation.HTTPTarget) (authorizedTarget, error) {
	requestURL := target.ExecutionURL()
	host := requestURL.Hostname()
	addresses := make([]netip.Addr, 0, 1)
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = append(addresses, literal)
	} else {
		resolved, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return authorizedTarget{}, executionFailure(contextCode(ctx, "target_resolution"))
		}
		addresses = append(addresses, resolved...)
	}
	if len(addresses) == 0 {
		return authorizedTarget{}, executionFailure("target_resolution_empty")
	}
	for index := range addresses {
		addresses[index] = addresses[index].Unmap()
		if !authorizedAddress(addresses[index], target.AllowsPrivateDestinations()) {
			return authorizedTarget{}, executionFailure("target_address_denied")
		}
	}
	sort.Slice(addresses, func(left, right int) bool { return addresses[left].Compare(addresses[right]) < 0 })
	return authorizedTarget{target: target, address: addresses[0]}, nil
}

func authorizedAddress(address netip.Addr, allowPrivate bool) bool {
	if !address.IsValid() || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return false
	}
	if !allowPrivate && (address.IsPrivate() || address.IsLoopback() || isSharedOrControlAddress(address)) {
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
