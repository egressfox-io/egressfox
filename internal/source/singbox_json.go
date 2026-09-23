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
	if kind != "vless" && kind != "trojan" && kind != "vmess" && kind != "shadowsocks" {
		return unsupported("unsupported_protocol")
	}
	for key := range object {
		switch key {
		case "type", "tag", "server", "server_port", "uuid", "password", "method", "security", "alter_id", "network", "tls", "transport", "flow":
		default:
			return unsupported("singbox_outbound_option")
		}
	}
	var record struct {
		Tag, Server, UUID, Password, Method, Security, Network, Flow string
		ServerPort, AlterID                                          int
	}
	for _, item := range []struct {
		key    string
		target *string
	}{{"tag", &record.Tag}, {"server", &record.Server}, {"uuid", &record.UUID}, {"password", &record.Password}, {"method", &record.Method}, {"security", &record.Security}, {"network", &record.Network}, {"flow", &record.Flow}} {
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
	if record.Flow != "" || record.AlterID != 0 {
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
		if !onlyJSONKeys(raw, "type", "path", "headers") {
			return unsupported("singbox_transport_option")
		}
		var settings struct {
			Type, Path string
			Headers    map[string]string
		}
		if json.Unmarshal(raw, &settings) != nil || settings.Type != "ws" {
			return unsupported("unsupported_transport")
		}
		host := ""
		for key, value := range settings.Headers {
			if !strings.EqualFold(key, "host") {
				return unsupported("singbox_transport_option")
			}
			host = value
		}
		transport, err = endpoint.NewWebSocketTransportWithHost(settings.Path, host)
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_transport"}
		}
	}
	tls := endpoint.DisabledTLS()
	if raw := object["tls"]; raw != nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if !onlyJSONKeys(raw, "enabled", "server_name", "insecure") {
			return unsupported("singbox_tls_option")
		}
		var settings struct {
			Enabled    bool   `json:"enabled"`
			ServerName string `json:"server_name"`
			Insecure   bool   `json:"insecure"`
		}
		if json.Unmarshal(raw, &settings) != nil || !settings.Enabled {
			return unsupported("singbox_tls_option")
		}
		tls, err = endpoint.NewTLS(settings.ServerName, settings.Insecure)
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_tls"}
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
			configuration, err = endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, credential, transport, tls)
		}
		if err != nil {
			return parseInput{kind: DiagnosticInvalid, code: "invalid_credential"}
		}
	case "trojan":
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
	}
	return parseInput{configuration: configuration, alias: record.Tag}
}
