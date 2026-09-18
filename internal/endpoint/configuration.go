package endpoint

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Protocol is an admitted endpoint protocol.
type Protocol uint8

const (
	ProtocolUnknown Protocol = iota
	ProtocolVLESS
	ProtocolTrojan
)

func (p Protocol) String() string {
	switch p {
	case ProtocolVLESS:
		return "vless"
	case ProtocolTrojan:
		return "trojan"
	default:
		return "unknown"
	}
}

// Credential holds canonical secret-bearing authentication material. Its normal
// formatting and JSON representation are always redacted; Reveal is the explicit
// boundary for a future renderer.
type Credential struct {
	protocol      Protocol
	secret        *credentialSecret
	nonComparable []struct{}
}

type credentialSecret struct {
	value string
}

// NewVLESSCredential validates and canonicalizes a VLESS UUID user ID.
func NewVLESSCredential(userID string) (Credential, error) {
	canonical, err := canonicalUUID(userID)
	if err != nil {
		return Credential{}, err
	}
	return Credential{protocol: ProtocolVLESS, secret: &credentialSecret{value: canonical}}, nil
}

// NewTrojanCredential validates a Trojan password without changing its bytes.
func NewTrojanCredential(password string) (Credential, error) {
	if password == "" {
		return Credential{}, invalid("trojan.password", "must not be empty")
	}
	if len(password) > 4096 {
		return Credential{}, invalid("trojan.password", "must not exceed 4096 bytes")
	}
	if !utf8.ValidString(password) {
		return Credential{}, invalid("trojan.password", "must be valid UTF-8")
	}
	return Credential{protocol: ProtocolTrojan, secret: &credentialSecret{value: password}}, nil
}

func canonicalUUID(raw string) (string, error) {
	if len(raw) != 36 || raw[8] != '-' || raw[13] != '-' || raw[18] != '-' || raw[23] != '-' {
		return "", invalid("vless.user_id", "must be a hyphenated UUID")
	}
	compact := strings.ReplaceAll(raw, "-", "")
	decoded := make([]byte, 16)
	if _, err := hex.Decode(decoded, []byte(compact)); err != nil {
		return "", invalid("vless.user_id", "must be a hyphenated UUID")
	}
	allZero := true
	for _, value := range decoded {
		allZero = allZero && value == 0
	}
	if allZero {
		return "", invalid("vless.user_id", "must not be the nil UUID")
	}
	encoded := hex.EncodeToString(decoded)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

// Reveal returns the credential for the narrow boundary that must render it.
func (c Credential) Reveal() string {
	if c.secret == nil {
		return ""
	}
	return c.secret.value
}

func (c Credential) valid() bool {
	return c.secret != nil && c.secret.value != "" && (c.protocol == ProtocolVLESS || c.protocol == ProtocolTrojan)
}

func (c Credential) equal(other Credential) bool {
	if !c.valid() || !other.valid() || c.protocol != other.protocol || len(c.secret.value) != len(other.secret.value) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.secret.value), []byte(other.secret.value)) == 1
}

func (Credential) String() string   { return "<redacted-credential>" }
func (Credential) GoString() string { return "endpoint.Credential(<redacted>)" }

func (Credential) Format(state fmt.State, _ rune) {
	writeSafeFormat(state, "<redacted-credential>")
}

func (Credential) MarshalJSON() ([]byte, error) {
	return json.Marshal("<redacted-credential>")
}

// TransportKind identifies an admitted connection transport.
type TransportKind uint8

const (
	TransportUnknown TransportKind = iota
	TransportTCP
	TransportWebSocket
)

func (kind TransportKind) String() string {
	switch kind {
	case TransportTCP:
		return "tcp"
	case TransportWebSocket:
		return "websocket"
	default:
		return "unknown"
	}
}

// Transport contains the typed options for an admitted transport.
type Transport struct {
	kind          TransportKind
	webSocketPath *string
	nonComparable []struct{}
}

// NewTCPTransport constructs direct TCP transport.
func NewTCPTransport() Transport { return Transport{kind: TransportTCP} }

// NewWebSocketTransport validates an explicit WebSocket request path.
func NewWebSocketTransport(path string) (Transport, error) {
	if path == "" || path[0] != '/' {
		return Transport{}, invalid("transport.websocket.path", "must be explicit and start with a slash")
	}
	if len(path) > 2048 {
		return Transport{}, invalid("transport.websocket.path", "must not exceed 2048 bytes")
	}
	if !utf8.ValidString(path) {
		return Transport{}, invalid("transport.websocket.path", "must be valid UTF-8")
	}
	for _, character := range path {
		if character < 0x20 || character == 0x7f {
			return Transport{}, invalid("transport.websocket.path", "must not contain control characters")
		}
	}
	return Transport{kind: TransportWebSocket, webSocketPath: &path}, nil
}

// Kind returns the transport kind.
func (t Transport) Kind() TransportKind { return t.kind }

// WebSocketPath returns the exact path for WebSocket transport and an empty
// string for TCP transport.
func (t Transport) WebSocketPath() string {
	if t.webSocketPath == nil {
		return ""
	}
	return *t.webSocketPath
}

func (t Transport) valid() bool {
	return t.kind == TransportTCP && t.webSocketPath == nil ||
		t.kind == TransportWebSocket && t.webSocketPath != nil && *t.webSocketPath != ""
}

func (t Transport) equal(other Transport) bool {
	return t.kind == other.kind && t.WebSocketPath() == other.WebSocketPath()
}

func (t Transport) String() string   { return t.kind.String() }
func (t Transport) GoString() string { return "endpoint.Transport(" + t.kind.String() + ")" }

func (t Transport) Format(state fmt.State, _ rune) { writeSafeFormat(state, t.String()) }

// TLSConfig describes the admitted TLS behavior. The zero value disables TLS.
type TLSConfig struct {
	enabled            bool
	serverName         string
	insecureSkipVerify bool
}

// DisabledTLS returns a TLS-disabled configuration.
func DisabledTLS() TLSConfig { return TLSConfig{} }

// NewTLS validates an optional server name. An empty name is resolved to the
// endpoint host by NewConfiguration.
func NewTLS(serverName string, insecureSkipVerify bool) (TLSConfig, error) {
	if serverName != "" {
		normalized, _, err := normalizeHost(serverName, "tls.server_name")
		if err != nil {
			return TLSConfig{}, err
		}
		serverName = normalized
	}
	return TLSConfig{enabled: true, serverName: serverName, insecureSkipVerify: insecureSkipVerify}, nil
}

// Enabled reports whether TLS is enabled.
func (c TLSConfig) Enabled() bool { return c.enabled }

// ServerName returns the effective canonical TLS server name.
func (c TLSConfig) ServerName() string { return c.serverName }

// InsecureSkipVerify reports whether certificate verification is disabled.
func (c TLSConfig) InsecureSkipVerify() bool { return c.insecureSkipVerify }

func (c TLSConfig) String() string {
	if !c.enabled {
		return "tls-disabled"
	}
	if c.insecureSkipVerify {
		return "tls-enabled(unverified)"
	}
	return "tls-enabled(verified)"
}

func (c TLSConfig) GoString() string { return "endpoint.TLSConfig(" + c.String() + ")" }

func (c TLSConfig) Format(state fmt.State, _ rune) { writeSafeFormat(state, c.String()) }

// Configuration is an immutable normalized endpoint connection configuration.
type Configuration struct {
	protocol   Protocol
	address    Address
	credential Credential
	transport  Transport
	tls        TLSConfig
}

// NewConfiguration validates a configuration in the M1 semantic subset.
func NewConfiguration(
	protocol Protocol,
	address Address,
	credential Credential,
	transport Transport,
	tls TLSConfig,
) (Configuration, error) {
	if protocol != ProtocolVLESS && protocol != ProtocolTrojan {
		return Configuration{}, invalid("protocol", "is not supported")
	}
	if !address.valid() {
		return Configuration{}, invalid("address", "must be constructed with NewAddress")
	}
	if !credential.valid() || credential.protocol != protocol {
		return Configuration{}, invalid("credential", "does not match the endpoint protocol")
	}
	if !transport.valid() {
		return Configuration{}, invalid("transport", "is not supported")
	}
	if protocol == ProtocolTrojan && !tls.enabled {
		return Configuration{}, invalid("tls", "is required for Trojan")
	}
	if tls.enabled && tls.serverName == "" {
		tls.serverName = address.host
	}

	return Configuration{
		protocol:   protocol,
		address:    address,
		credential: credential,
		transport:  transport,
		tls:        tls,
	}, nil
}

// Protocol returns the endpoint protocol.
func (c Configuration) Protocol() Protocol { return c.protocol }

// Address returns the canonical network address.
func (c Configuration) Address() Address { return c.address }

// Credential returns the redacting credential value. Reveal must be called
// explicitly to access secret material.
func (c Configuration) Credential() Credential { return c.credential }

// Transport returns the normalized transport.
func (c Configuration) Transport() Transport { return c.transport }

// TLS returns the normalized TLS behavior.
func (c Configuration) TLS() TLSConfig { return c.tls }

// Equivalent reports complete configuration equality, including credentials.
func (c Configuration) Equivalent(other Configuration) bool {
	return c.protocol == other.protocol &&
		c.address == other.address &&
		c.credential.equal(other.credential) &&
		c.transport.equal(other.transport) &&
		c.tls == other.tls
}

func (c Configuration) valid() bool {
	if c.protocol != ProtocolVLESS && c.protocol != ProtocolTrojan {
		return false
	}
	if !c.address.valid() || !c.credential.valid() || c.credential.protocol != c.protocol || !c.transport.valid() {
		return false
	}
	if c.protocol == ProtocolTrojan && !c.tls.enabled {
		return false
	}
	return !c.tls.enabled || c.tls.serverName != ""
}

func (c Configuration) String() string {
	if !c.valid() {
		return "endpoint configuration <invalid>"
	}
	return fmt.Sprintf("endpoint configuration %s protocol=%s credential=<redacted>", c.Identity(), c.protocol)
}

func (c Configuration) GoString() string { return c.String() }

func (c Configuration) Format(state fmt.State, _ rune) { writeSafeFormat(state, c.String()) }

func (Configuration) MarshalJSON() ([]byte, error) {
	return nil, errorsForJSON("endpoint configuration")
}

func errorsForJSON(kind string) error {
	return fmt.Errorf("%s JSON serialization is disabled because it contains confidential identity data", kind)
}

func writeSafeFormat(state fmt.State, value string) {
	_, _ = state.Write([]byte(value))
}
