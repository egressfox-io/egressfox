package source

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func parseSingBoxHysteria2(object map[string]json.RawMessage) parseInput {
	unsupported := func() parseInput { return parseInput{kind: DiagnosticUnsupported, code: "singbox_hysteria2_option"} }
	for key := range object {
		switch key {
		case "type", "tag", "server", "server_port", "server_ports", "password", "tls", "obfs", "up_mbps", "down_mbps":
		default:
			return unsupported()
		}
	}
	var record struct {
		Tag      string `json:"tag"`
		Server   string `json:"server"`
		Port     int    `json:"server_port"`
		Password string `json:"password"`
		Up       int    `json:"up_mbps"`
		Down     int    `json:"down_mbps"`
	}
	fields := []struct {
		key    string
		target any
	}{{"tag", &record.Tag}, {"server", &record.Server}, {"server_port", &record.Port}, {"password", &record.Password}, {"up_mbps", &record.Up}, {"down_mbps", &record.Down}}
	for _, field := range fields {
		if raw := object[field.key]; raw != nil && json.Unmarshal(raw, field.target) != nil {
			return parseInput{kind: DiagnosticMalformed, code: "singbox_hysteria2_field"}
		}
	}
	if object["server_ports"] != nil && object["server_port"] != nil {
		return unsupported()
	}
	if raw := object["server_ports"]; raw != nil && record.Port == 0 {
		var ports []string
		if json.Unmarshal(raw, &ports) != nil || len(ports) == 0 {
			return parseInput{kind: DiagnosticMalformed, code: "hysteria2_ports"}
		}
		first := strings.Split(ports[0], ":")[0]
		record.Port, _ = strconv.Atoi(first)
	}
	address, err := endpoint.NewAddress(record.Server, record.Port)
	if err != nil {
		return parseInput{kind: DiagnosticInvalid, code: "invalid_address"}
	}
	credential, err := endpoint.NewHysteria2Credential(record.Password)
	if err != nil {
		return parseInput{kind: DiagnosticInvalid, code: "invalid_credential"}
	}
	if !onlyJSONKeys(object["tls"], "enabled", "server_name", "insecure", "alpn") {
		return unsupported()
	}
	var tlsValue struct {
		Enabled    bool     `json:"enabled"`
		ServerName string   `json:"server_name"`
		Insecure   bool     `json:"insecure"`
		ALPN       []string `json:"alpn"`
	}
	if json.Unmarshal(object["tls"], &tlsValue) != nil || !tlsValue.Enabled {
		return parseInput{kind: DiagnosticInvalid, code: "hysteria2_tls_required"}
	}
	tls, err := endpoint.NewTLS(tlsValue.ServerName, tlsValue.Insecure)
	if err != nil {
		return parseInput{kind: DiagnosticInvalid, code: "invalid_tls"}
	}
	security, err := endpoint.NewSecurityOptions(tlsValue.ALPN, "", nil)
	if err != nil {
		return parseInput{kind: DiagnosticInvalid, code: "invalid_alpn"}
	}
	secret := ""
	if raw := object["obfs"]; raw != nil {
		if !onlyJSONKeys(raw, "type", "password") {
			return unsupported()
		}
		var obfs struct{ Type, Password string }
		if json.Unmarshal(raw, &obfs) != nil {
			return parseInput{kind: DiagnosticMalformed, code: "hysteria2_obfs"}
		}
		if obfs.Type != "salamander" || obfs.Password == "" {
			return unsupported()
		}
		secret = obfs.Password
	}
	options, err := endpoint.NewHysteria2Options(record.Up, record.Down, secret)
	if raw := object["server_ports"]; raw != nil {
		var ports []string
		if json.Unmarshal(raw, &ports) != nil {
			return parseInput{kind: DiagnosticMalformed, code: "hysteria2_ports"}
		}
		options, err = endpoint.NewHysteria2OptionsWithPorts(record.Up, record.Down, secret, ports)
	}
	if err != nil {
		return parseInput{kind: DiagnosticInvalid, code: "hysteria2_options"}
	}
	if !options.IncludesPort(address.Port()) {
		return unsupported()
	}
	configuration, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, address, credential, endpoint.NewQUICTransport(), tls, security, endpoint.FlowNone, &options)
	if err != nil {
		return parseInput{kind: DiagnosticInvalid, code: "invalid_endpoint"}
	}
	return parseInput{configuration: configuration, alias: record.Tag}
}
