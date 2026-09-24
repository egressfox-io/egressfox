package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

// decodeXrayProfiles reads only the outbounds of complete Xray/V2Ray client
// configurations. Routing, DNS, inbounds and UI metadata are never interpreted.
func decodeXrayProfiles(profiles []json.RawMessage, limits Limits) ([]parseInput, error) {
	if len(profiles) > limits.MaxRecords {
		return nil, errors.New("too many profiles")
	}
	inputs := make([]parseInput, 0)
	totalOutbounds := 0
	for _, raw := range profiles {
		var profile map[string]json.RawMessage
		if json.Unmarshal(raw, &profile) != nil || profile["outbounds"] == nil {
			return nil, errors.New("invalid xray profile")
		}
		var outbounds []json.RawMessage
		if json.Unmarshal(profile["outbounds"], &outbounds) != nil {
			return nil, errors.New("invalid xray outbounds")
		}
		totalOutbounds += len(outbounds)
		if totalOutbounds > limits.MaxRecords {
			return nil, errors.New("too many xray outbounds")
		}
		for _, outbound := range outbounds {
			if len(outbound) > limits.MaxRecordBytes {
				return nil, errors.New("oversized xray outbound")
			}
			var object map[string]json.RawMessage
			if json.Unmarshal(outbound, &object) != nil {
				return nil, errors.New("invalid xray outbound")
			}
			var protocol string
			if json.Unmarshal(object["protocol"], &protocol) != nil {
				var kind string
				if json.Unmarshal(object["type"], &kind) != nil {
					return nil, errors.New("unknown client outbound schema")
				}
				if isServiceOutbound(kind) {
					continue
				}
				inputs = append(inputs, parseSingBoxOutbound(object, kind))
				continue
			}
			if isServiceOutbound(protocol) {
				continue
			}
			parsed := parseXrayOutbound(object, protocol, limits.MaxRecords-len(inputs))
			inputs = append(inputs, parsed...)
			if len(inputs) > limits.MaxRecords {
				return nil, errors.New("too many extracted records")
			}
		}
	}
	return inputs, nil
}

func isServiceOutbound(kind string) bool {
	switch kind {
	case "freedom", "direct", "blackhole", "block", "dns", "loopback", "balancer", "selector", "urltest":
		return true
	default:
		return false
	}
}

type xrayServer struct {
	Address    string `json:"address"`
	Port       int    `json:"port"`
	ID         string `json:"id"`
	Password   string `json:"password"`
	Method     string `json:"method"`
	Security   string `json:"security"`
	Encryption string `json:"encryption"`
	Flow       string `json:"flow"`
	AlterID    int    `json:"alterId"`
	Users      []struct {
		ID         string `json:"id"`
		Password   string `json:"password"`
		Encryption string `json:"encryption"`
		Security   string `json:"security"`
		AlterID    int    `json:"alterId"`
		Flow       string `json:"flow"`
	} `json:"users"`
}

func parseXrayOutbound(object map[string]json.RawMessage, protocol string, remaining int) []parseInput {
	unsupported := func(code string) []parseInput { return []parseInput{{kind: DiagnosticUnsupported, code: code}} }
	if protocol == "socks" || protocol == "http" {
		return parseXrayProxyOutbound(object, protocol, remaining)
	}
	if protocol != "vless" && protocol != "vmess" && protocol != "trojan" && protocol != "shadowsocks" {
		return unsupported("unsupported_protocol")
	}
	for key, value := range object {
		if key != "protocol" && key != "tag" && key != "settings" && key != "streamSettings" && !emptyJSON(value) {
			return unsupported("xray_outbound_option")
		}
	}
	var alias string
	_ = json.Unmarshal(object["tag"], &alias)
	transport, tls, code := parseXrayStream(object["streamSettings"])
	if code != "" {
		return unsupported(code)
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(object["settings"], &settings) != nil {
		return []parseInput{{kind: DiagnosticMalformed, code: "xray_settings"}}
	}
	if _, exists := settings["vnext"]; exists {
		if !onlyJSONKeys(object["settings"], "vnext") {
			return unsupported("xray_settings_option")
		}
	} else if _, exists := settings["servers"]; exists {
		if !onlyJSONKeys(object["settings"], "servers") {
			return unsupported("xray_settings_option")
		}
	} else if !onlyJSONKeys(object["settings"], "address", "port", "id", "password", "method", "security", "encryption", "flow", "alterId", "level", "email") {
		return unsupported("xray_settings_option")
	}
	servers := make([]xrayServer, 0)
	if raw, exists := settings["vnext"]; exists {
		if protocol != "vless" && protocol != "vmess" || json.Unmarshal(raw, &servers) != nil {
			return []parseInput{{kind: DiagnosticMalformed, code: "xray_vnext"}}
		}
	} else if raw, exists := settings["servers"]; exists {
		if protocol != "trojan" && protocol != "shadowsocks" || json.Unmarshal(raw, &servers) != nil {
			return []parseInput{{kind: DiagnosticMalformed, code: "xray_servers"}}
		}
	} else {
		var one xrayServer
		if json.Unmarshal(object["settings"], &one) != nil {
			return []parseInput{{kind: DiagnosticMalformed, code: "xray_settings"}}
		}
		servers = append(servers, one)
	}
	if raw := settings["vnext"]; raw != nil {
		var serverRaw []json.RawMessage
		_ = json.Unmarshal(raw, &serverRaw)
		for _, item := range serverRaw {
			if !onlyJSONKeys(item, "address", "port", "users") {
				return unsupported("xray_server_option")
			}
			var serverFields map[string]json.RawMessage
			_ = json.Unmarshal(item, &serverFields)
			var userRaw []json.RawMessage
			_ = json.Unmarshal(serverFields["users"], &userRaw)
			for _, user := range userRaw {
				if !onlyJSONKeys(user, "id", "password", "encryption", "security", "alterId", "flow", "email", "level") {
					return unsupported("xray_user_option")
				}
			}
		}
	}
	if raw := settings["servers"]; raw != nil {
		var serverRaw []json.RawMessage
		_ = json.Unmarshal(raw, &serverRaw)
		for _, item := range serverRaw {
			if !onlyJSONKeys(item, "address", "port", "password", "method", "email", "level") {
				return unsupported("xray_server_option")
			}
		}
	}
	if len(servers) == 0 || len(servers) > remaining {
		return unsupported("xray_server_count")
	}
	inputs := make([]parseInput, 0)
	for _, server := range servers {
		users := server.Users
		if len(users) == 0 {
			// Newer Xray shorthand and legacy Trojan/Shadowsocks settings.
			users = append(users, struct {
				ID         string `json:"id"`
				Password   string `json:"password"`
				Encryption string `json:"encryption"`
				Security   string `json:"security"`
				AlterID    int    `json:"alterId"`
				Flow       string `json:"flow"`
			}{ID: server.ID, Password: server.Password, Encryption: server.Encryption, Security: server.Security, AlterID: server.AlterID, Flow: server.Flow})
		}
		if len(users) > remaining-len(inputs) {
			return unsupported("xray_user_count")
		}
		for _, user := range users {
			address, err := endpoint.NewAddress(server.Address, server.Port)
			if err != nil {
				inputs = append(inputs, parseInput{kind: DiagnosticInvalid, code: "invalid_address"})
				continue
			}
			if user.Flow != "" || user.AlterID != 0 {
				inputs = append(inputs, parseInput{kind: DiagnosticUnsupported, code: "xray_user_option"})
				continue
			}
			var configuration endpoint.Configuration
			switch protocol {
			case "vless":
				if user.Encryption != "" && user.Encryption != "none" {
					inputs = append(inputs, parseInput{kind: DiagnosticUnsupported, code: "xray_encryption"})
					continue
				}
				credential, err := endpoint.NewVLESSCredential(user.ID)
				if err == nil {
					configuration, err = endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, credential, transport, tls)
				}
				if err != nil {
					inputs = append(inputs, parseInput{kind: DiagnosticInvalid, code: "xray_credential"})
					continue
				}
			case "vmess":
				credential, err := endpoint.NewVMessCredential(user.ID)
				if err == nil {
					configuration, err = endpoint.NewVMessConfiguration(address, credential, transport, tls, valueOr(user.Security, "auto"))
				}
				if err != nil {
					inputs = append(inputs, parseInput{kind: DiagnosticUnsupported, code: "xray_vmess_option"})
					continue
				}
			case "trojan":
				credential, err := endpoint.NewTrojanCredential(user.Password)
				if err == nil {
					configuration, err = endpoint.NewConfiguration(endpoint.ProtocolTrojan, address, credential, transport, tls)
				}
				if err != nil {
					inputs = append(inputs, parseInput{kind: DiagnosticInvalid, code: "xray_trojan_option"})
					continue
				}
			case "shadowsocks":
				method := server.Method
				credential, err := endpoint.NewShadowsocksCredential(user.Password)
				if err == nil {
					configuration, err = endpoint.NewShadowsocksConfiguration(address, credential, method)
				}
				if err != nil || transport.Kind() != endpoint.TransportTCP || tls.Enabled() {
					inputs = append(inputs, parseInput{kind: DiagnosticUnsupported, code: "xray_ss_option"})
					continue
				}
			}
			inputs = append(inputs, parseInput{configuration: configuration, alias: alias})
		}
	}
	return inputs
}

func parseXrayStream(raw json.RawMessage) (endpoint.Transport, endpoint.TLSConfig, string) {
	transport, tls := endpoint.NewTCPTransport(), endpoint.DisabledTLS()
	if len(raw) == 0 || emptyJSON(raw) {
		return transport, tls, ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return transport, tls, "xray_stream"
	}
	var security, network, method string
	_ = json.Unmarshal(fields["security"], &security)
	_ = json.Unmarshal(fields["network"], &network)
	_ = json.Unmarshal(fields["method"], &method)
	if method != "" && network != "" && method != network {
		return transport, tls, "xray_transport_conflict"
	}
	if method != "" {
		network = method
	}
	if security == "reality" {
		return transport, tls, "reality_unsupported"
	}
	if security != "" && security != "none" && security != "tls" {
		return transport, tls, "unsupported_security"
	}
	for key, value := range fields {
		if key != "security" && key != "network" && key != "method" && key != "wsSettings" && key != "tlsSettings" && !emptyJSON(value) {
			return transport, tls, "xray_stream_option"
		}
	}
	if security == "tls" {
		if !onlyJSONKeys(fields["tlsSettings"], "serverName", "allowInsecure", "alpn", "fingerprint") {
			return transport, tls, "xray_tls_option"
		}
		var settings struct {
			ServerName    string   `json:"serverName"`
			AllowInsecure bool     `json:"allowInsecure"`
			ALPN          []string `json:"alpn"`
			Fingerprint   string   `json:"fingerprint"`
		}
		if json.Unmarshal(fields["tlsSettings"], &settings) != nil || len(settings.ALPN) != 0 || settings.Fingerprint != "" {
			return transport, tls, "xray_tls_option"
		}
		var err error
		tls, err = endpoint.NewTLS(settings.ServerName, settings.AllowInsecure)
		if err != nil {
			return transport, tls, "invalid_tls"
		}
	} else if !emptyJSON(fields["tlsSettings"]) {
		return transport, tls, "tls_option_without_tls"
	}
	switch network {
	case "", "tcp", "raw":
		if !emptyJSON(fields["wsSettings"]) {
			return transport, tls, "xray_transport_option"
		}
	case "ws", "websocket":
		if !onlyJSONKeys(fields["wsSettings"], "path", "headers") {
			return transport, tls, "xray_ws_option"
		}
		var settings struct {
			Path    string            `json:"path"`
			Headers map[string]string `json:"headers"`
		}
		if json.Unmarshal(fields["wsSettings"], &settings) != nil {
			return transport, tls, "xray_ws_option"
		}
		host := ""
		for key, value := range settings.Headers {
			if strings.EqualFold(key, "host") {
				host = value
			} else {
				return transport, tls, "xray_ws_option"
			}
		}
		var err error
		transport, err = endpoint.NewWebSocketTransportWithHost(settings.Path, host)
		if err != nil {
			return transport, tls, "invalid_transport"
		}
	default:
		return transport, tls, "unsupported_transport"
	}
	return transport, tls, ""
}

func onlyJSONKeys(raw json.RawMessage, allowed ...string) bool {
	if len(raw) == 0 {
		return true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return false
	}
	for key := range object {
		found := false
		for _, permitted := range allowed {
			found = found || key == permitted
		}
		if !found {
			return false
		}
	}
	return true
}

func emptyJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) || bytes.Equal(trimmed, []byte("[]"))
}
