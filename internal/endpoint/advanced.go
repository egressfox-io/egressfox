package endpoint

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Advanced transports have distinct typed constructors. XHTTP is reserved for
// C3: its mode and parameter contract is not guessed from a share URI in C1.
type advancedTransport struct {
	path, host, service string
}

func (t *advancedTransport) valid(kind TransportKind) bool {
	if t == nil {
		return false
	}
	switch kind {
	case TransportHTTP2, TransportHTTPUpgrade:
		return t.path != "" && t.service == ""
	case TransportGRPC:
		return t.service != "" && t.path == "" && t.host == ""
	case TransportQUIC:
		return t.path == "" && t.host == "" && t.service == ""
	default:
		return false
	}
}

func (t *advancedTransport) equal(other *advancedTransport) bool {
	if t == nil || other == nil {
		return t == nil && other == nil
	}
	return *t == *other
}

func newHTTPTransport(kind TransportKind, path, host string) (Transport, error) {
	if path == "" || path[0] != '/' || len(path) > 2048 || !utf8.ValidString(path) {
		return Transport{}, invalid("transport.path", "must be an explicit UTF-8 absolute path")
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return Transport{}, invalid("transport.path", "must not contain control characters")
		}
	}
	if host != "" {
		var err error
		host, _, err = normalizeHost(host, "transport.host")
		if err != nil {
			return Transport{}, err
		}
	}
	return Transport{kind: kind, advanced: &advancedTransport{path: path, host: host}}, nil
}

func NewHTTP2Transport(path, host string) (Transport, error) {
	return newHTTPTransport(TransportHTTP2, path, host)
}

func NewHTTPUpgradeTransport(path, host string) (Transport, error) {
	return newHTTPTransport(TransportHTTPUpgrade, path, host)
}

func NewGRPCTransport(service string) (Transport, error) {
	if service == "" || len(service) > 1024 || !utf8.ValidString(service) || strings.ContainsAny(service, "\x00\r\n") {
		return Transport{}, invalid("transport.grpc.service", "must be a nonempty bounded service name")
	}
	return Transport{kind: TransportGRPC, advanced: &advancedTransport{service: service}}, nil
}

// NewQUICTransport represents Hysteria2's UDP/QUIC network transport. It is
// deliberately distinct from V2Ray's similarly named transport option.
func NewQUICTransport() Transport {
	return Transport{kind: TransportQUIC, advanced: &advancedTransport{}}
}

func (t Transport) Path() string {
	if t.advanced == nil {
		return ""
	}
	return t.advanced.path
}

func (t Transport) Host() string {
	if t.advanced == nil {
		return ""
	}
	return t.advanced.host
}

func (t Transport) Service() string {
	if t.advanced == nil {
		return ""
	}
	return t.advanced.service
}

// Reality is client authentication for a Reality TLS handshake, not an
// alternative certificate verifier. Short ID is kept out of public identity.
type Reality struct {
	publicKey string
	shortID   string
}

func NewReality(publicKey, shortID string) (Reality, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != publicKey {
		return Reality{}, invalid("reality.public_key", "must be a canonical 32-byte base64url key")
	}
	if len(shortID) > 16 || len(shortID)%2 != 0 {
		return Reality{}, invalid("reality.short_id", "must contain 0–8 hexadecimal bytes")
	}
	if _, err := hex.DecodeString(shortID); err != nil {
		return Reality{}, invalid("reality.short_id", "must be hexadecimal")
	}
	return Reality{publicKey: publicKey, shortID: strings.ToLower(shortID)}, nil
}

func (r Reality) PublicKey() string              { return r.publicKey }
func (r Reality) RevealShortID() string          { return r.shortID }
func (Reality) String() string                   { return "reality(<redacted-short-id>)" }
func (Reality) GoString() string                 { return "endpoint.Reality(<redacted>)" }
func (r Reality) Format(state fmt.State, _ rune) { writeSafeFormat(state, r.String()) }

// SecurityOptions augments ordinary TLS with connection-critical options.
// These options are not rendered until a later slice explicitly qualifies them.
type SecurityOptions struct {
	alpn        []string
	fingerprint string
	reality     *Reality
}

func NewSecurityOptions(alpn []string, fingerprint string, reality *Reality) (SecurityOptions, error) {
	if len(alpn) > 8 {
		return SecurityOptions{}, invalid("security.alpn", "too many protocol names")
	}
	seen := make(map[string]bool, len(alpn))
	copyALPN := make([]string, len(alpn))
	for i, value := range alpn {
		if value == "" || len(value) > 255 || !utf8.ValidString(value) || seen[value] {
			return SecurityOptions{}, invalid("security.alpn", "must contain distinct bounded protocol names")
		}
		for _, r := range value {
			if r < 0x21 || r > 0x7e {
				return SecurityOptions{}, invalid("security.alpn", "must contain printable ASCII without spaces")
			}
		}
		seen[value] = true
		copyALPN[i] = value
	}
	if fingerprint != "" {
		switch fingerprint {
		case "chrome", "firefox", "safari", "ios", "android", "edge":
		default:
			return SecurityOptions{}, invalid("security.fingerprint", "is not in the admitted common set")
		}
	}
	if reality != nil && (reality.publicKey == "" || fingerprint == "") {
		return SecurityOptions{}, invalid("security.reality", "requires a public key and client fingerprint")
	}
	var copied *Reality
	if reality != nil {
		value := *reality
		copied = &value
	}
	return SecurityOptions{alpn: copyALPN, fingerprint: fingerprint, reality: copied}, nil
}

func (s SecurityOptions) ALPN() []string      { return append([]string(nil), s.alpn...) }
func (s SecurityOptions) Fingerprint() string { return s.fingerprint }
func (s SecurityOptions) Reality() (Reality, bool) {
	if s.reality == nil {
		return Reality{}, false
	}
	return *s.reality, true
}
func (SecurityOptions) String() string                   { return "security-options(<redacted>)" }
func (SecurityOptions) GoString() string                 { return "endpoint.SecurityOptions(<redacted>)" }
func (s SecurityOptions) Format(state fmt.State, _ rune) { writeSafeFormat(state, s.String()) }

func (s SecurityOptions) equal(other SecurityOptions) bool {
	if s.fingerprint != other.fingerprint || len(s.alpn) != len(other.alpn) || (s.reality == nil) != (other.reality == nil) {
		return false
	}
	for i := range s.alpn {
		if s.alpn[i] != other.alpn[i] {
			return false
		}
	}
	return s.reality == nil || *s.reality == *other.reality
}

type VLESSFlow uint8

const (
	FlowNone VLESSFlow = iota
	FlowVision
)

func (f VLESSFlow) String() string {
	if f == FlowVision {
		return "xtls-rprx-vision"
	}
	return ""
}

type Hysteria2Options struct {
	upMbps, downMbps int
	obfsPassword     *credentialSecret
}

func NewHysteria2Options(upMbps, downMbps int, salamanderPassword string) (Hysteria2Options, error) {
	if upMbps < 0 || downMbps < 0 || upMbps > 100000 || downMbps > 100000 || (upMbps == 0) != (downMbps == 0) {
		return Hysteria2Options{}, invalid("hysteria2.bandwidth", "must be both zero or both positive and bounded")
	}
	if len(salamanderPassword) > 4096 || !utf8.ValidString(salamanderPassword) {
		return Hysteria2Options{}, invalid("hysteria2.obfs", "must be bounded UTF-8")
	}
	var obfs *credentialSecret
	if salamanderPassword != "" {
		obfs = &credentialSecret{value: salamanderPassword}
	}
	return Hysteria2Options{upMbps: upMbps, downMbps: downMbps, obfsPassword: obfs}, nil
}

func (h Hysteria2Options) UpMbps() int   { return h.upMbps }
func (h Hysteria2Options) DownMbps() int { return h.downMbps }
func (h Hysteria2Options) RevealSalamanderPassword() string {
	if h.obfsPassword == nil {
		return ""
	}
	return h.obfsPassword.value
}
func (Hysteria2Options) String() string                   { return "hysteria2-options(<redacted>)" }
func (Hysteria2Options) GoString() string                 { return "endpoint.Hysteria2Options(<redacted>)" }
func (h Hysteria2Options) Format(state fmt.State, _ rune) { writeSafeFormat(state, h.String()) }

type advancedConnection struct {
	flow     VLESSFlow
	security SecurityOptions
	hysteria *Hysteria2Options
}

func (a *advancedConnection) valid(c Configuration) bool {
	if a == nil {
		return c.protocol != ProtocolSOCKS5 && c.protocol != ProtocolHTTPProxy && c.protocol != ProtocolHysteria2 && c.transport.advanced == nil
	}
	if a.flow != FlowNone && (a.flow != FlowVision || c.protocol != ProtocolVLESS || c.transport.kind != TransportTCP || !c.tls.enabled) {
		return false
	}
	if (len(a.security.alpn) > 0 || a.security.fingerprint != "" || a.security.reality != nil) && !c.tls.enabled {
		return false
	}
	if a.security.reality != nil && (c.protocol != ProtocolVLESS || c.tls.insecureSkipVerify || c.tls.serverName == "") {
		return false
	}
	if a.security.reality != nil && c.transport.kind != TransportTCP {
		return false
	}
	if c.transport.kind == TransportHTTP2 && !c.tls.enabled {
		return false // sing-box would otherwise negotiate HTTP/1.1
	}
	if c.protocol == ProtocolTrojan && c.transport.kind == TransportHTTP2 {
		return false // pinned Mihomo Trojan has no HTTP/2 transport
	}
	if c.protocol == ProtocolSOCKS5 || c.protocol == ProtocolHTTPProxy {
		return c.transport.kind == TransportTCP && (c.protocol == ProtocolHTTPProxy || !c.tls.enabled) && a.flow == FlowNone && a.hysteria == nil && a.security.reality == nil && a.security.fingerprint == "" && len(a.security.alpn) == 0
	}
	if c.protocol == ProtocolHysteria2 {
		return c.transport.kind == TransportQUIC && a.hysteria != nil && a.flow == FlowNone && a.security.reality == nil
	}
	return a.hysteria == nil && c.transport.kind != TransportQUIC
}

func (a *advancedConnection) equal(other *advancedConnection) bool {
	if a == nil || other == nil {
		return a == nil && other == nil
	}
	if a.flow != other.flow || !a.security.equal(other.security) || (a.hysteria == nil) != (other.hysteria == nil) {
		return false
	}
	if a.hysteria == nil {
		return true
	}
	return a.hysteria.upMbps == other.hysteria.upMbps && a.hysteria.downMbps == other.hysteria.downMbps && a.hysteria.RevealSalamanderPassword() == other.hysteria.RevealSalamanderPassword()
}

// NewExtendedConfiguration admits only typed semantics. The engine capability
// boundary still rejects every newly modeled form until its implementation slice.
func NewExtendedConfiguration(protocol Protocol, address Address, credential Credential, transport Transport, tls TLSConfig, security SecurityOptions, flow VLESSFlow, hysteria *Hysteria2Options) (Configuration, error) {
	return newExtendedConfiguration(protocol, address, credential, transport, tls, security, flow, hysteria, "")
}

// NewExtendedVMessConfiguration retains VMess payload security independently
// of TLS and transport security.
func NewExtendedVMessConfiguration(address Address, credential Credential, transport Transport, tls TLSConfig, security SecurityOptions, method string) (Configuration, error) {
	if method != "auto" && method != "aes-128-gcm" && method != "chacha20-poly1305" && method != "none" {
		return Configuration{}, invalid("vmess.security", "is unsupported")
	}
	return newExtendedConfiguration(ProtocolVMess, address, credential, transport, tls, security, FlowNone, nil, method)
}

func newExtendedConfiguration(protocol Protocol, address Address, credential Credential, transport Transport, tls TLSConfig, security SecurityOptions, flow VLESSFlow, hysteria *Hysteria2Options, method string) (Configuration, error) {
	if !address.valid() || !credential.valid() || credential.protocol != protocol || !transport.valid() {
		return Configuration{}, invalid("configuration", "contains invalid connection fields")
	}
	// An old connection retains its original canonical representation regardless
	// of which constructor a caller uses. Otherwise identical inventories could
	// split their observations across ef1/ef2 and ef3 identities.
	if transport.advanced == nil && flow == FlowNone && hysteria == nil && len(security.alpn) == 0 && security.fingerprint == "" && security.reality == nil {
		switch protocol {
		case ProtocolVLESS, ProtocolTrojan:
			return NewConfiguration(protocol, address, credential, transport, tls)
		case ProtocolVMess:
			return NewVMessConfiguration(address, credential, transport, tls, method)
		}
	}
	if tls.enabled && tls.serverName == "" {
		tls.serverName = address.host
	}
	var copied *Hysteria2Options
	if hysteria != nil {
		value := *hysteria
		copied = &value
	}
	security.alpn = append([]string(nil), security.alpn...)
	if security.reality != nil {
		value := *security.reality
		security.reality = &value
	}
	c := Configuration{protocol: protocol, address: address, credential: credential, transport: transport, tls: tls, method: method,
		advanced: &advancedConnection{flow: flow, security: security, hysteria: copied}}
	if !c.valid() {
		return Configuration{}, invalid("configuration", "has contradictory protocol, transport or security options")
	}
	return c, nil
}

func (c Configuration) Flow() VLESSFlow {
	if c.advanced == nil {
		return FlowNone
	}
	return c.advanced.flow
}

func (c Configuration) SecurityOptions() SecurityOptions {
	if c.advanced == nil {
		return SecurityOptions{}
	}
	s := c.advanced.security
	s.alpn = append([]string(nil), s.alpn...)
	if s.reality != nil {
		value := *s.reality
		s.reality = &value
	}
	return s
}

func (c Configuration) Hysteria2() (Hysteria2Options, bool) {
	if c.advanced == nil || c.advanced.hysteria == nil {
		return Hysteria2Options{}, false
	}
	return *c.advanced.hysteria, true
}

// WithAddress reconstructs an execution-only literal destination without
// changing any other connection semantic. The caller retains original identity.
func (c Configuration) WithAddress(address Address) (Configuration, error) {
	if !c.valid() || !address.valid() {
		return Configuration{}, invalid("configuration.address", "is invalid")
	}
	c.address = address
	return c, nil
}
