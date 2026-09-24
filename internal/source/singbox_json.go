package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func decodeSingBoxRecords(records []json.RawMessage, limits Limits) ([]parseInput, error) {
	if len(records) > limits.MaxRecords {
		return nil, errors.New("too many records")
	}
	inputs := make([]parseInput, 0, len(records))
	for _, raw := range records {
		if len(raw) > limits.MaxRecordBytes {
			return nil, errors.New("oversized record")
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return nil, errors.New("invalid record")
		}
		var kind string
		if json.Unmarshal(object["type"], &kind) != nil {
			return nil, errors.New("unknown record schema")
		}
		if isServiceOutbound(kind) {
			continue
		}
		inputs = append(inputs, parseSingBoxOutbound(object, kind))
	}
	return inputs, nil
}

func parseSingBoxOutbound(object map[string]json.RawMessage, kind string) parseInput {
	unsupported := func(code string) parseInput { return parseInput{kind: DiagnosticUnsupported, code: code} }
	if kind != "vless" && kind != "trojan" && kind != "vmess" && kind != "shadowsocks" && kind != "socks" && kind != "http" {
		return unsupported("unsupported_protocol")
	}
	for key := range object {
		switch key {
		case "type", "tag", "server", "server_port", "uuid", "password", "method", "security", "alter_id", "network", "tls", "transport", "flow", "username", "version":
		default:
			return unsupported("singbox_outbound_option")
		}
	}
	if kind == "socks" || kind == "http" {
		for key := range object {
			switch key {
			case "type", "tag", "server", "server_port", "username", "password":
			case "version", "network":
				if kind != "socks" {
					return unsupported("singbox_proxy_option")
				}
			case "tls":
				if kind != "http" {
					return unsupported("singbox_proxy_option")
				}
			default:
				return unsupported("singbox_proxy_option")
			}
		}
	}
	var record struct {
		Tag, Server, UUID, Password, Method, Security, Network, Flow, Username, Version string
		ServerPort, AlterID                                                             int
	}
	for _, item := range []struct {
		key    string
		target *string
	}{{"tag", &record.Tag}, {"server", &record.Server}, {"uuid", &record.UUID}, {"password", &record.Password}, {"method", &record.Method}, {"security", &record.Security}, {"network", &record.Network}, {"flow", &record.Flow}, {"username", &record.Username}, {"version", &record.Version}} {
		if raw := object[item.key]; raw != nil && json.Unmarshal(raw, item.target) != nil {
			return parseInput{kind: DiagnosticMalformed, code: "singbox_field_type"}
		}
	}
	if json.Unmarshal(object["server_port"], &record.ServerPort) != nil {
		return parseInput{kind: DiagnosticMalformed, code: "singbox_port"}
	}
	if raw := object["alter_id"]; raw != nil && json.Unmarshal(raw, &record.AlterID) != nil {
		return parseInput{kind: DiagnosticMalformed, code: "singbox_alter_id"}
	}
	if record.AlterID != 0 || record.Flow != "" && (kind != "vless" || record.Flow != endpoint.FlowVision.String()) {
		return unsupported("singbox_protocol_option")
	}
	if kind != "socks" && object["version"] != nil || kind != "socks" && kind != "http" && object["username"] != nil {
		return unsupported("singbox_protocol_option")
	}
	if record.Network != "" && record.Network != "tcp" {
		return unsupported("singbox_network")
	}
	address, err := endpoint.NewAddress(record.Server, record.ServerPort)
	if err != nil {
		return parseInput{kind: DiagnosticInvalid, code: "invalid_address"}
	}
	transport := endpoint.NewTCPTransport()
	if raw := object["transport"]; raw != nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if !onlyJSONKeys(raw, "type", "path", "headers", "host", "service_name") {
			return unsupported("singbox_transport_option")
		}
		var settings struct {
			Type        string            `json:"type"`
			Path        string            `json:"path"`
			ServiceName string            `json:"service_name"`
			Host        json.RawMessage   `json:"host"`
			Headers     map[string]string `json:"headers"`
		}
		if json.Unmarshal(raw, &settings) != nil {
			return unsupported("unsupported_transport")
		}
		host := ""
		for key, value := range settings.Headers {
			if !strings.EqualFold(key, "host") {
				return unsupported("singbox_transport_option")
			}
			host = value
		}
		switch settings.Type {
		case "ws":
			if len(settings.Host) != 0 || settings.ServiceName != "" {
				return unsupported("singbox_transport_option")
			}
			transport, err = endpoint.NewWebSocketTransportWithHost(settings.Path, host)
		case "http":
			if len(settings.Headers) != 0 || settings.ServiceName != "" {
				return unsupported("singbox_transport_option")
			}
			if len(settings.Host) != 0 {
				var hosts []string
				if json.Unmarshal(settings.Host, &hosts) != nil || len(hosts) != 1 {
					return unsupported("singbox_http_host")
				}
				host = hosts[0]
			}
			transport, err = endpoint.NewHTTP2Transport(settings.Path, host)
		case "httpupgrade":
			if len(settings.Headers) != 0 || settings.ServiceName != "" {
				return unsupported("singbox_transport_option")
			}
			if len(settings.Host) != 0 && json.Unmarshal(settings.Host, &host) != nil {
				return unsupported("singbox_upgrade_host")
			}
			transport, err = endpoint.NewHTTPUpgradeTransport(settings.Path, host)
		case "grpc":
			if settings.Path != "" || len(settings.Host) != 0 || len(settings.Headers) != 0 {
				return unsupported("singbox_transport_option")
			}
			transport, err = endpoint.NewGRPCTransport(settings.ServiceName)
		default:
			return unsupported("unsupported_transport")
		}
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_transport"}
		}
	}
	tls := endpoint.DisabledTLS()
	securityOptions := endpoint.SecurityOptions{}
	if raw := object["tls"]; raw != nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if !onlyJSONKeys(raw, "enabled", "server_name", "insecure", "alpn", "utls", "reality") {
			return unsupported("singbox_tls_option")
		}
		var settings struct {
			Enabled    bool            `json:"enabled"`
			ServerName string          `json:"server_name"`
			Insecure   bool            `json:"insecure"`
			ALPN       []string        `json:"alpn"`
			UTLS       json.RawMessage `json:"utls"`
			Reality    json.RawMessage `json:"reality"`
		}
		if json.Unmarshal(raw, &settings) != nil || !settings.Enabled {
			return unsupported("singbox_tls_option")
		}
		tls, err = endpoint.NewTLS(settings.ServerName, settings.Insecure)
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_tls"}
		}
		fingerprint := ""
		if len(settings.UTLS) != 0 {
			if !onlyJSONKeys(settings.UTLS, "enabled", "fingerprint") {
				return unsupported("singbox_utls_option")
			}
			var utls struct {
				Enabled     bool
				Fingerprint string
			}
			if json.Unmarshal(settings.UTLS, &utls) != nil || !utls.Enabled {
				return unsupported("singbox_utls_option")
			}
			fingerprint = utls.Fingerprint
		}
		var reality *endpoint.Reality
		if len(settings.Reality) != 0 {
			if !onlyJSONKeys(settings.Reality, "enabled", "public_key", "short_id") {
				return unsupported("singbox_reality_option")
			}
			var value struct {
				Enabled   bool   `json:"enabled"`
				PublicKey string `json:"public_key"`
				ShortID   string `json:"short_id"`
			}
			if json.Unmarshal(settings.Reality, &value) != nil || !value.Enabled || settings.ServerName == "" || settings.Insecure {
				return unsupported("singbox_reality_option")
			}
			parsed, parseErr := endpoint.NewReality(value.PublicKey, value.ShortID)
			if parseErr != nil {
				return parseInput{kind: DiagnosticInvalid, code: "invalid_reality"}
			}
			reality = &parsed
		}
		securityOptions, err = endpoint.NewSecurityOptions(settings.ALPN, fingerprint, reality)
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_security_option"}
		}
	}
	var configuration endpoint.Configuration
	switch kind {
	case "vless":
		if record.Password != "" || record.Method != "" || record.Security != "" {
			return unsupported("singbox_protocol_option")
		}
		credential, err := endpoint.NewVLESSCredential(record.UUID)
		if err == nil {
			flow := endpoint.FlowNone
			if record.Flow != "" {
				flow = endpoint.FlowVision
			}
			configuration, err = endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, address, credential, transport, tls, securityOptions, flow, nil)
		}
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_credential"}
		}
	case "trojan":
		if len(securityOptions.ALPN()) != 0 || securityOptions.Fingerprint() != "" {
			return unsupported("singbox_trojan_security_option")
		}
		if record.UUID != "" || record.Method != "" || record.Security != "" {
			return unsupported("singbox_protocol_option")
		}
		credential, err := endpoint.NewTrojanCredential(record.Password)
		if err == nil {
			configuration, err = endpoint.NewConfiguration(endpoint.ProtocolTrojan, address, credential, transport, tls)
		}
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_credential"}
		}
	case "vmess":
		if len(securityOptions.ALPN()) != 0 || securityOptions.Fingerprint() != "" {
			return unsupported("singbox_vmess_security_option")
		}
		if record.Password != "" || record.Method != "" {
			return unsupported("singbox_protocol_option")
		}
		credential, err := endpoint.NewVMessCredential(record.UUID)
		if err == nil {
			configuration, err = endpoint.NewVMessConfiguration(address, credential, transport, tls, valueOr(record.Security, "auto"))
		}
		if err != nil {
			return unsupported("singbox_vmess_option")
		}
	case "shadowsocks":
		if record.UUID != "" || record.Security != "" || transport.Kind() != endpoint.TransportTCP || tls.Enabled() {
			return unsupported("singbox_ss_option")
		}
		credential, err := endpoint.NewShadowsocksCredential(record.Password)
		if err == nil {
			configuration, err = endpoint.NewShadowsocksConfiguration(address, credential, record.Method)
		}
		if err != nil {
			return unsupported("singbox_ss_option")
		}
	case "socks", "http":
		if record.UUID != "" || record.Method != "" || record.Security != "" || object["transport"] != nil && !emptyJSON(object["transport"]) {
			return unsupported("singbox_protocol_option")
		}
		if kind == "socks" && record.Version != "" && record.Version != "5" {
			return unsupported("singbox_socks_version")
		}
		if kind == "socks" && tls.Enabled() || kind == "http" && object["network"] != nil {
			return unsupported("singbox_proxy_option")
		}
		protocol := endpoint.ProtocolHTTPProxy
		if kind == "socks" {
			protocol = endpoint.ProtocolSOCKS5
		}
		credential, err := endpoint.NewProxyCredential(protocol, record.Username, record.Password)
		if err != nil {
			return unsupported("singbox_proxy_auth")
		}
		configuration, err = endpoint.NewExtendedConfiguration(protocol, address, credential, transport, tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_endpoint"}
		}
	}
	return parseInput{configuration: configuration, alias: record.Tag}
}
