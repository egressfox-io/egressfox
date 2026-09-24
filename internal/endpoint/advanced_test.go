package endpoint_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func advancedAddress(t *testing.T) endpoint.Address {
	t.Helper()
	a, err := endpoint.NewAddress("EDGE.Example.COM.", 443)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func advancedVLESS(t *testing.T, transport endpoint.Transport, security endpoint.SecurityOptions, flow endpoint.VLESSFlow, credential string) endpoint.Configuration {
	t.Helper()
	a := advancedAddress(t)
	c, err := endpoint.NewVLESSCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	tls, err := endpoint.NewTLS("", false)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, a, c, transport, tls, security, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	return configuration
}

func TestExtendedIdentityAndCredentialIsolation(t *testing.T) {
	t.Parallel()
	transport, err := endpoint.NewGRPCTransport("Service")
	if err != nil {
		t.Fatal(err)
	}
	first := advancedVLESS(t, transport, endpoint.SecurityOptions{}, endpoint.FlowNone, syntheticVLESSUserID)
	second := advancedVLESS(t, transport, endpoint.SecurityOptions{}, endpoint.FlowNone, "94515a2d-6681-4df5-933c-ab937e3539ca")
	if !strings.HasPrefix(first.ID().String(), "ef3_") || first.ID() != second.ID() || first.Identity().Equal(second.Identity()) {
		t.Fatal("new semantics did not use isolated v3 logical/private identities")
	}
	parsed, err := endpoint.ParseID(first.ID().String())
	if err != nil || parsed != first.ID() {
		t.Fatal("v3 identity cannot round-trip")
	}
	changedTransport, err := endpoint.NewGRPCTransport("OtherService")
	if err != nil {
		t.Fatal(err)
	}
	third := advancedVLESS(t, changedTransport, endpoint.SecurityOptions{}, endpoint.FlowNone, syntheticVLESSUserID)
	if third.ID() == first.ID() {
		t.Fatal("gRPC service disappeared from identity")
	}
	repeated := advancedVLESS(t, transport, endpoint.SecurityOptions{}, endpoint.FlowNone, syntheticVLESSUserID)
	if !first.Identity().Equal(repeated.Identity()) || !first.Equivalent(repeated) {
		t.Fatal("v3 identity is not deterministic")
	}
}

func TestExtendedConstructorPreservesLegacyIdentity(t *testing.T) {
	t.Parallel()
	address := advancedAddress(t)
	credential, err := endpoint.NewVLESSCredential(syntheticVLESSUserID)
	if err != nil {
		t.Fatal(err)
	}
	tls, err := endpoint.NewTLS("", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, transport := range []endpoint.Transport{endpoint.NewTCPTransport(), mustWebSocketTransport(t)} {
		legacy, err := endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, credential, transport, tls)
		if err != nil {
			t.Fatal(err)
		}
		extended, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, address, credential, transport, tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
		if err != nil || !legacy.Identity().Equal(extended.Identity()) || !legacy.Equivalent(extended) {
			t.Fatalf("legacy identity changed through extended constructor: %v", err)
		}
	}
}

func mustWebSocketTransport(t *testing.T) endpoint.Transport {
	t.Helper()
	transport, err := endpoint.NewWebSocketTransport("/ws")
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func TestRealityAndVisionIdentity(t *testing.T) {
	t.Parallel()
	key := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	firstReality, err := endpoint.NewReality(key, "a1b2")
	if err != nil {
		t.Fatal(err)
	}
	secondReality, err := endpoint.NewReality(key, "c3d4")
	if err != nil {
		t.Fatal(err)
	}
	firstSecurity, err := endpoint.NewSecurityOptions([]string{"h2", "http/1.1"}, "chrome", &firstReality)
	if err != nil {
		t.Fatal(err)
	}
	secondSecurity, err := endpoint.NewSecurityOptions([]string{"h2", "http/1.1"}, "chrome", &secondReality)
	if err != nil {
		t.Fatal(err)
	}
	first := advancedVLESS(t, endpoint.NewTCPTransport(), firstSecurity, endpoint.FlowVision, syntheticVLESSUserID)
	rotated := advancedVLESS(t, endpoint.NewTCPTransport(), secondSecurity, endpoint.FlowVision, syntheticVLESSUserID)
	if first.ID() != rotated.ID() || first.Identity().Equal(rotated.Identity()) {
		t.Fatal("short ID must rotate only private revision")
	}
	if first.ID() == advancedVLESS(t, endpoint.NewTCPTransport(), firstSecurity, endpoint.FlowNone, syntheticVLESSUserID).ID() {
		t.Fatal("Vision flow lost from identity")
	}
	if first.ID() == advancedVLESS(t, endpoint.NewTCPTransport(), endpoint.SecurityOptions{}, endpoint.FlowVision, syntheticVLESSUserID).ID() {
		t.Fatal("Reality public key lost from identity")
	}
	canary := "a1b2"
	for _, value := range []any{first, firstReality, firstSecurity} {
		formatted := fmt.Sprintf("%v %+v %#v", value, value, value)
		if strings.Contains(formatted, canary) || strings.Contains(formatted, syntheticVLESSUserID) {
			t.Fatal("security or credentials leaked through formatting")
		}
	}
	if _, err := json.Marshal(first); err == nil {
		t.Fatal("configuration JSON unexpectedly enabled")
	}
}

func TestProxyAndHysteriaDomainValidity(t *testing.T) {
	t.Parallel()
	a := advancedAddress(t)
	proxyAuth, err := endpoint.NewProxyCredential(endpoint.ProtocolHTTPProxy, "user", "first-password")
	if err != nil {
		t.Fatal(err)
	}
	rotatedAuth, err := endpoint.NewProxyCredential(endpoint.ProtocolHTTPProxy, "user", "other-password")
	if err != nil {
		t.Fatal(err)
	}
	first, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHTTPProxy, a, proxyAuth, endpoint.NewTCPTransport(), endpoint.DisabledTLS(), endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHTTPProxy, a, rotatedAuth, endpoint.NewTCPTransport(), endpoint.DisabledTLS(), endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != rotated.ID() || first.Identity().Equal(rotated.Identity()) {
		t.Fatal("proxy password rotation was not private")
	}
	tls, err := endpoint.NewTLS("edge.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	https, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHTTPProxy, a, proxyAuth, endpoint.NewTCPTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
	if err != nil || https.ID() == first.ID() {
		t.Fatal("HTTPS proxy security was not distinct")
	}
	h2, err := endpoint.NewHTTP2Transport("/path", "front.example.com")
	if err != nil {
		t.Fatal(err)
	}
	vless, err := endpoint.NewVLESSCredential(syntheticVLESSUserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, a, vless, h2, endpoint.DisabledTLS(), endpoint.SecurityOptions{}, endpoint.FlowNone, nil); !errors.Is(err, endpoint.ErrInvalid) {
		t.Fatal("HTTP/2 without TLS would downgrade to HTTP/1.1")
	}
	if _, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolSOCKS5, a, proxyAuth, endpoint.NewTCPTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil); !errors.Is(err, endpoint.ErrInvalid) {
		t.Fatal("mismatched SOCKS5 credential accepted")
	}
	hyCredential, err := endpoint.NewHysteria2Credential("hy-password")
	if err != nil {
		t.Fatal(err)
	}
	hyOptions, err := endpoint.NewHysteria2Options(100, 200, "obfs-password")
	if err != nil {
		t.Fatal(err)
	}
	hy, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, a, hyCredential, endpoint.NewQUICTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, &hyOptions)
	if err != nil || hy.ID().String()[:4] != "ef3_" {
		t.Fatal("valid Hysteria2 domain form rejected")
	}
	if _, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, a, hyCredential, endpoint.NewTCPTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, &hyOptions); !errors.Is(err, endpoint.ErrInvalid) {
		t.Fatal("Hysteria2 over TCP accepted")
	}
	if strings.Contains(fmt.Sprintf("%+v", hy), "obfs-password") {
		t.Fatal("obfuscation password leaked")
	}
}

func TestExtendedDeduplicationRetainsProvenanceAndPrivateRevisions(t *testing.T) {
	t.Parallel()
	address := advancedAddress(t)
	tls, _ := endpoint.NewTLS("", false)
	firstCredential, _ := endpoint.NewHysteria2Credential("password-one")
	secondCredential, _ := endpoint.NewHysteria2Credential("password-two")
	options, _ := endpoint.NewHysteria2Options(100, 200, "obfs-one")
	first, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, address, firstCredential, endpoint.NewQUICTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, &options)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, address, secondCredential, endpoint.NewQUICTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, &options)
	if err != nil {
		t.Fatal(err)
	}
	changedObfs, _ := endpoint.NewHysteria2Options(100, 200, "obfs-two")
	third, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, address, firstCredential, endpoint.NewQUICTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, &changedObfs)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != rotated.ID() || first.ID() != third.ID() || first.Identity().Equal(rotated.Identity()) || first.Identity().Equal(third.Identity()) {
		t.Fatal("Hysteria2 auth/obfs rotation did not preserve public ID and split private revisions")
	}
	sourceA, _ := endpoint.NewSourceID("source-a")
	sourceB, _ := endpoint.NewSourceID("source-b")
	idA, _ := endpoint.NewRecordID("record-a")
	idB, _ := endpoint.NewRecordID("record-b")
	provenanceA, _ := endpoint.NewProvenance(sourceA, idA)
	provenanceB, _ := endpoint.NewProvenance(sourceB, idB)
	recordA, _ := endpoint.NewRecord(first, provenanceA)
	recordB, _ := endpoint.NewRecord(first, provenanceB)
	recordRotated, _ := endpoint.NewRecord(rotated, provenanceA)
	for _, records := range [][]endpoint.Record{{recordA, recordB, recordRotated}, {recordRotated, recordB, recordA}} {
		inventory, err := endpoint.Deduplicate(records)
		if err != nil || inventory.Len() != 2 {
			t.Fatalf("deduplication = %d, %v", inventory.Len(), err)
		}
		for _, record := range inventory.Records() {
			if record.Identity().Equal(first.Identity()) && len(record.Provenance()) != 2 {
				t.Fatal("equivalent endpoint lost provenance")
			}
		}
	}
}

func FuzzExtendedIdentityDeterminism(f *testing.F) {
	f.Add("Service")
	f.Add("other/service")
	f.Fuzz(func(t *testing.T, service string) {
		transport, err := endpoint.NewGRPCTransport(service)
		if err != nil {
			return
		}
		first := advancedVLESS(t, transport, endpoint.SecurityOptions{}, endpoint.FlowNone, syntheticVLESSUserID)
		second := advancedVLESS(t, transport, endpoint.SecurityOptions{}, endpoint.FlowNone, syntheticVLESSUserID)
		if !first.Identity().Equal(second.Identity()) {
			t.Fatal("identity changed for identical input")
		}
	})
}
