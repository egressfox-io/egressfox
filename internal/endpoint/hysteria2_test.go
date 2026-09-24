package endpoint_test

import (
	"errors"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func TestHysteria2PortBoundsAndEf3Identity(t *testing.T) {
	for _, ranges := range [][]string{{}, {"0"}, {"65536"}, {"1:257"}, {"20:10"}, {"01"}} {
		if _, err := endpoint.NewHysteria2OptionsWithPorts(20, 40, "secret", ranges); !errors.Is(err, endpoint.ErrInvalid) {
			t.Fatalf("accepted invalid ports %v: %v", ranges, err)
		}
	}
	address, _ := endpoint.NewAddress("edge.example.com", 443)
	credential, _ := endpoint.NewHysteria2Credential("auth-canary")
	tls, _ := endpoint.NewTLS("front.example.com", false)
	base, _ := endpoint.NewHysteria2Options(20, 40, "obfs-canary")
	hop, err := endpoint.NewHysteria2OptionsWithPorts(20, 40, "obfs-canary", []string{"443:444", "8443"})
	if err != nil || !hop.IncludesPort(444) || hop.IncludesPort(445) {
		t.Fatalf("port bounds: %v", err)
	}
	makeConfig := func(options endpoint.Hysteria2Options) endpoint.Configuration {
		value, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, address, credential, endpoint.NewQUICTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, &options)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	first, second := makeConfig(base), makeConfig(hop)
	if first.Identity().Equal(second.Identity()) || first.ID() == second.ID() {
		t.Fatal("port hopping reused endpoint identity")
	}
	ports := hop.PortRanges()
	ports[0] = "1"
	if hop.PortRanges()[0] != "443:444" {
		t.Fatal("mutable port alias escaped")
	}
}
