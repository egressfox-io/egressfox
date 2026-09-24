package engine_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/policy"
)

func TestExactProfileCapabilityGate(t *testing.T) {
	address, err := endpoint.NewAddress("edge.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	user, err := endpoint.NewVLESSCredential("7ae477a8-3884-4dad-a5a8-a5106778cbbb")
	if err != nil {
		t.Fatal(err)
	}
	tls, err := endpoint.NewTLS("", false)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, user, endpoint.NewTCPTransport(), tls)
	if err != nil {
		t.Fatal(err)
	}
	grpc, err := endpoint.NewGRPCTransport("test-service")
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, address, user, grpc, tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	vision, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, address, user, endpoint.NewTCPTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowVision, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyAuth, err := endpoint.NewProxyCredential(endpoint.ProtocolHTTPProxy, "private-user", "private-password")
	if err != nil {
		t.Fatal(err)
	}
	httpProxy, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHTTPProxy, address, proxyAuth, endpoint.NewTCPTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []artifact.Profile{artifact.Mihomo11931, artifact.SingBox1141} {
		if err := engine.CheckEndpoint(profile, legacy); err != nil {
			t.Fatalf("legacy %s: %v", profile, err)
		}
		if err := engine.CheckEndpoint(profile, httpProxy); err != nil {
			t.Fatalf("HTTP CONNECT %s: %v", profile, err)
		}
		for _, configuration := range []endpoint.Configuration{advanced, vision} {
			err := engine.CheckEndpoint(profile, configuration)
			if !errors.Is(err, engine.ErrUnsupported) {
				t.Fatalf("%s: want capability rejection, got %v", profile, err)
			}
			if strings.Contains(err.Error(), "private-user") || strings.Contains(err.Error(), "private-password") {
				t.Fatal("capability error leaked credentials")
			}
		}
	}
	wrong := artifact.Mihomo11931
	wrong.Version = "1.19.32"
	if err := engine.CheckEndpoint(wrong, legacy); !errors.Is(err, engine.ErrUnsupported) {
		t.Fatal("unqualified engine version accepted")
	}
	if err := engine.CheckEndpoint(artifact.Mihomo11931, endpoint.Configuration{}); !errors.Is(err, endpoint.ErrInvalid) || errors.Is(err, engine.ErrUnsupported) {
		t.Fatal("invalid and unsupported were conflated")
	}
}

func TestRenderersDoNotDropAdvancedSemantics(t *testing.T) {
	address, _ := endpoint.NewAddress("edge.example.com", 443)
	credential, _ := endpoint.NewVLESSCredential("7ae477a8-3884-4dad-a5a8-a5106778cbbb")
	tls, _ := endpoint.NewTLS("", false)
	transport, _ := endpoint.NewHTTPUpgradeTransport("/upgrade", "front.example.com")
	configuration, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, address, credential, transport, tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := endpoint.NewSourceID("advanced")
	recordID, _ := endpoint.NewRecordID("node")
	provenance, _ := endpoint.NewProvenance(source, recordID)
	record, err := endpoint.NewRecord(configuration, provenance)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := endpoint.Deduplicate([]endpoint.Record{record})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := policy.NewSOCKSListener("127.0.0.1", 1080)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := policy.NewGateway(inventory, listener)
	if err != nil {
		t.Fatal(err)
	}
	for _, renderer := range []engine.Renderer{mihomo.Renderer{}, singbox.Renderer{}} {
		if _, err := renderer.Render(gateway); !errors.Is(err, engine.ErrUnsupported) {
			t.Fatalf("%s rendered unsupported HTTPUpgrade as another transport: %v", renderer.Profile(), err)
		}
	}
}

func TestPinnedBuildAsymmetryIsExplicit(t *testing.T) {
	address, _ := endpoint.NewAddress("edge.example.com", 443)
	user, _ := endpoint.NewVLESSCredential("7ae477a8-3884-4dad-a5a8-a5106778cbbb")
	tls, _ := endpoint.NewTLS("edge.example.com", false)
	reality, err := endpoint.NewReality(base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), "a1b2")
	if err != nil {
		t.Fatal(err)
	}
	security, err := endpoint.NewSecurityOptions(nil, "chrome", &reality)
	if err != nil {
		t.Fatal(err)
	}
	vless, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, address, user, endpoint.NewTCPTransport(), tls, security, endpoint.FlowVision, nil)
	if err != nil {
		t.Fatal(err)
	}
	hyAuth, _ := endpoint.NewHysteria2Credential("hy-password")
	hyOptions, _ := endpoint.NewHysteria2Options(100, 100, "")
	hysteria, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, address, hyAuth, endpoint.NewQUICTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, &hyOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, configuration := range []endpoint.Configuration{vless, hysteria} {
		mihomo, err := engine.AssessEndpoint(artifact.Mihomo11931, configuration)
		if err != nil || mihomo.Build != engine.BuildAvailable || mihomo.Implemented {
			t.Fatalf("Mihomo assessment: %+v, %v", mihomo, err)
		}
		singbox, err := engine.AssessEndpoint(artifact.SingBox1141, configuration)
		if err != nil || singbox.Build != engine.BuildUnavailable || singbox.Implemented {
			t.Fatalf("sing-box assessment: %+v, %v", singbox, err)
		}
	}
}
