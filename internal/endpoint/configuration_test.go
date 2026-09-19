package endpoint_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

const syntheticVLESSUserID = "7ae477a8-3884-4dad-a5a8-a5106778cbbb"

func TestIdentityCanonicalizationAndStability(t *testing.T) {
	t.Parallel()

	canonical := mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), mustTLS(t, "edge.example.com", false))
	equivalent := mustVLESSConfiguration(t, "EDGE.EXAMPLE.COM.", strings.ToUpper(syntheticVLESSUserID), endpoint.NewTCPTransport(), mustTLS(t, "", false))

	if !canonical.Identity().Equal(equivalent.Identity()) {
		t.Fatalf("equivalent configurations have different identities: %s != %s", canonical.Identity(), equivalent.Identity())
	}
	if !canonical.Equivalent(equivalent) {
		t.Fatal("equivalent canonical configurations compare unequal")
	}

	const wantID = "ef1_fq56uvsashikgrfhfvnqckuxoctbrgbknbuef5bliqcuigsks33a"
	if got := canonical.ID().String(); got != wantID {
		t.Fatalf("ID() = %q, want versioned golden %q", got, wantID)
	}
}

func TestLogicalIdentityIncludesNonSecretConnectionSemantics(t *testing.T) {
	t.Parallel()

	baseTLS := mustTLS(t, "edge.example.com", false)
	base := mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), baseTLS)
	webSocket := mustWebSocket(t, "/proxy")

	tests := []struct {
		name   string
		config endpoint.Configuration
	}{
		{name: "host", config: mustVLESSConfiguration(t, "other.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), mustTLS(t, "edge.example.com", false))},
		{name: "port", config: mustConfiguration(t, endpoint.ProtocolVLESS, mustAddress(t, "edge.example.com", 8443), mustVLESSCredential(t, syntheticVLESSUserID), endpoint.NewTCPTransport(), baseTLS)},
		{name: "protocol", config: mustTrojanConfiguration(t, "edge.example.com", "synthetic-password", endpoint.NewTCPTransport(), baseTLS)},
		{name: "transport kind", config: mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, webSocket, baseTLS)},
		{name: "WebSocket path", config: mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, mustWebSocket(t, "/Proxy"), baseTLS)},
		{name: "TLS enabled", config: mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), endpoint.DisabledTLS())},
		{name: "TLS server name", config: mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), mustTLS(t, "tls.example.com", false))},
		{name: "TLS verification", config: mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), mustTLS(t, "edge.example.com", true))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if base.ID() == test.config.ID() {
				t.Fatalf("meaningful %s difference did not change logical ID", test.name)
			}
			if base.Identity().Equal(test.config.Identity()) {
				t.Fatalf("meaningful %s difference did not change connection identity", test.name)
			}
		})
	}
}

func TestCredentialRotationPreservesLogicalIDAndChangesRevision(t *testing.T) {
	t.Parallel()

	first := mustTrojanConfiguration(t, "edge.example.com", "first-synthetic-password", endpoint.NewTCPTransport(), mustTLS(t, "", false))
	rotated := mustTrojanConfiguration(t, "EDGE.EXAMPLE.COM.", "second-synthetic-password", endpoint.NewTCPTransport(), mustTLS(t, "edge.example.com", false))

	if first.ID() != rotated.ID() {
		t.Fatalf("credential rotation changed logical ID: %s != %s", first.ID(), rotated.ID())
	}
	if first.Identity().Equal(rotated.Identity()) {
		t.Fatal("credential rotation reused the complete connection identity")
	}
	if !first.Identity().SameEndpoint(rotated.Identity()) {
		t.Fatal("credential rotation lost logical endpoint continuity")
	}
}

func TestIdentityPersistenceBoundariesRoundTripWithoutFormattingRevision(t *testing.T) {
	t.Parallel()
	configuration := mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), mustTLS(t, "edge.example.com", false))
	identity := configuration.Identity()
	restoredID, err := endpoint.ParseID(identity.ID().String())
	if err != nil || restoredID != identity.ID() {
		t.Fatalf("ParseID() = %v, %v", restoredID, err)
	}
	protected, err := identity.Revision().RevealForPersistence()
	if err != nil {
		t.Fatal(err)
	}
	restoredRevision, err := endpoint.RestoreRevision(protected)
	if err != nil || !restoredRevision.Equal(identity.Revision()) {
		t.Fatalf("RestoreRevision() error = %v", err)
	}
	formatted := fmt.Sprintf("%v %+v %#v", identity.Revision(), identity.Revision(), identity.Revision())
	if strings.Contains(formatted, hex.EncodeToString(protected)) {
		t.Fatal("revision formatting exposed persistence bytes")
	}
}

func TestConfigurationValidation(t *testing.T) {
	t.Parallel()

	address := mustAddress(t, "edge.example.com", 443)
	vless := mustVLESSCredential(t, syntheticVLESSUserID)
	trojan := mustTrojanCredential(t, "synthetic-password")
	tls := mustTLS(t, "", false)

	tests := []struct {
		name string
		make func() error
	}{
		{name: "unknown protocol", make: func() error {
			_, err := endpoint.NewConfiguration(endpoint.Protocol(200), address, vless, endpoint.NewTCPTransport(), tls)
			return err
		}},
		{name: "zero address", make: func() error {
			_, err := endpoint.NewConfiguration(endpoint.ProtocolVLESS, endpoint.Address{}, vless, endpoint.NewTCPTransport(), tls)
			return err
		}},
		{name: "mismatched credential", make: func() error {
			_, err := endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, trojan, endpoint.NewTCPTransport(), tls)
			return err
		}},
		{name: "zero transport", make: func() error {
			_, err := endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, vless, endpoint.Transport{}, tls)
			return err
		}},
		{name: "Trojan without TLS", make: func() error {
			_, err := endpoint.NewConfiguration(endpoint.ProtocolTrojan, address, trojan, endpoint.NewTCPTransport(), endpoint.DisabledTLS())
			return err
		}},
		{name: "invalid VLESS UUID", make: func() error { _, err := endpoint.NewVLESSCredential("not-a-uuid"); return err }},
		{name: "nil VLESS UUID", make: func() error {
			_, err := endpoint.NewVLESSCredential("00000000-0000-0000-0000-000000000000")
			return err
		}},
		{name: "empty Trojan password", make: func() error { _, err := endpoint.NewTrojanCredential(""); return err }},
		{name: "unsupported WebSocket default", make: func() error { _, err := endpoint.NewWebSocketTransport(""); return err }},
		{name: "relative WebSocket path", make: func() error { _, err := endpoint.NewWebSocketTransport("proxy"); return err }},
		{name: "invalid TLS server name", make: func() error { _, err := endpoint.NewTLS("https://edge.example.com", false); return err }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.make(); !errors.Is(err, endpoint.ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCredentialAndConfigurationDiagnosticsAreRedacted(t *testing.T) {
	t.Parallel()

	const secret = "trojan://user:synthetic-canary@edge.example.com:443/private"
	credential := mustTrojanCredential(t, secret)
	config := mustTrojanConfiguration(t, "edge.example.com", secret, mustWebSocket(t, "/path?token=not-a-secret"), mustTLS(t, "", false))

	values := []string{config.ID().String()}
	formats := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%o", "%p"}
	for _, value := range []any{credential, config.Transport(), config.TLS(), config, config.Identity()} {
		for _, format := range formats {
			values = append(values, fmt.Sprintf(format, value))
		}
	}
	values = append(values,
		fmt.Sprintf("%#v", []endpoint.Configuration{config}),
		fmt.Sprintf("%d", []endpoint.Configuration{config}),
		fmt.Sprintf("%d", []endpoint.Identity{config.Identity()}),
	)
	for _, value := range values {
		assertDoesNotContain(t, value, secret)
	}

	encodedCredential, err := json.Marshal(credential)
	if err != nil {
		t.Fatalf("json.Marshal(Credential) error = %v", err)
	}
	assertDoesNotContain(t, string(encodedCredential), secret)

	for _, value := range []any{config, config.Identity()} {
		encoded, marshalErr := json.Marshal(value)
		assertDoesNotContain(t, string(encoded), secret)
		if marshalErr == nil {
			t.Fatalf("json.Marshal(%T) unexpectedly succeeded", value)
		}
		assertDoesNotContain(t, marshalErr.Error(), secret)
	}

	_, validationErr := endpoint.NewConfiguration(endpoint.ProtocolVLESS, config.Address(), credential, config.Transport(), config.TLS())
	if validationErr == nil {
		t.Fatal("mismatched secret credential unexpectedly accepted")
	}
	assertDoesNotContain(t, validationErr.Error(), secret)

	if got := credential.Reveal(); got != secret {
		t.Fatalf("Reveal() did not return the credential at the explicit boundary")
	}
}

func TestValidationErrorsDoNotEchoCompleteURIs(t *testing.T) {
	t.Parallel()

	const proxyURI = "vless://7ae477a8-3884-4dad-a5a8-a5106778cbbb@example.com:443?token=synthetic"
	_, err := endpoint.NewAddress(proxyURI, 443)
	if err == nil {
		t.Fatal("complete proxy URI unexpectedly accepted as a host")
	}
	assertDoesNotContain(t, err.Error(), proxyURI)
}

func assertDoesNotContain(t testing.TB, value, secret string) {
	t.Helper()
	if strings.Contains(value, secret) {
		t.Fatal("diagnostic contained a redaction canary")
	}
}

func mustVLESSConfiguration(t testing.TB, host, userID string, transport endpoint.Transport, tls endpoint.TLSConfig) endpoint.Configuration {
	t.Helper()
	return mustConfiguration(t, endpoint.ProtocolVLESS, mustAddress(t, host, 443), mustVLESSCredential(t, userID), transport, tls)
}

func mustTrojanConfiguration(t testing.TB, host, password string, transport endpoint.Transport, tls endpoint.TLSConfig) endpoint.Configuration {
	t.Helper()
	return mustConfiguration(t, endpoint.ProtocolTrojan, mustAddress(t, host, 443), mustTrojanCredential(t, password), transport, tls)
}

func mustConfiguration(t testing.TB, protocol endpoint.Protocol, address endpoint.Address, credential endpoint.Credential, transport endpoint.Transport, tls endpoint.TLSConfig) endpoint.Configuration {
	t.Helper()
	configuration, err := endpoint.NewConfiguration(protocol, address, credential, transport, tls)
	if err != nil {
		t.Fatalf("NewConfiguration() error = %v", err)
	}
	return configuration
}

func mustVLESSCredential(t testing.TB, userID string) endpoint.Credential {
	t.Helper()
	credential, err := endpoint.NewVLESSCredential(userID)
	if err != nil {
		t.Fatalf("NewVLESSCredential() error = %v", err)
	}
	return credential
}

func mustTrojanCredential(t testing.TB, password string) endpoint.Credential {
	t.Helper()
	credential, err := endpoint.NewTrojanCredential(password)
	if err != nil {
		t.Fatalf("NewTrojanCredential() error = %v", err)
	}
	return credential
}

func mustWebSocket(t testing.TB, path string) endpoint.Transport {
	t.Helper()
	transport, err := endpoint.NewWebSocketTransport(path)
	if err != nil {
		t.Fatalf("NewWebSocketTransport() error = %v", err)
	}
	return transport
}

func mustTLS(t testing.TB, serverName string, insecure bool) endpoint.TLSConfig {
	t.Helper()
	tls, err := endpoint.NewTLS(serverName, insecure)
	if err != nil {
		t.Fatalf("NewTLS() error = %v", err)
	}
	return tls
}
