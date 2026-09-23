package source

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func decodeShareBase64(value string) ([]byte, bool) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil && len(decoded) <= 16384 {
			return decoded, true
		}
	}
	return nil, false
}

func parseVMessURI(raw string) (endpoint.Configuration, string, DiagnosticKind, string) {
	encoded := strings.TrimPrefix(raw, "vmess://")
	if strings.ContainsAny(encoded, "?#/") {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "vmess_uri_variant"
	}
	decoded, ok := decodeShareBase64(encoded)
	if !ok || !boundedJSONDepth(decoded, 8) {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_vmess_payload"
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(decoded, &fields) != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_vmess_payload"
	}
	known := map[string]bool{"v": true, "ps": true, "add": true, "port": true, "id": true, "aid": true, "net": true, "type": true, "host": true, "path": true, "tls": true, "sni": true, "scy": true, "fp": true, "alpn": true, "allowInsecure": true}
	for key := range fields {
		if !known[key] {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "vmess_unknown_field"
		}
	}
	get := func(key string) (string, bool) {
		rawValue, exists := fields[key]
		if !exists {
			return "", true
		}
		var value string
		if json.Unmarshal(rawValue, &value) == nil {
			return value, true
		}
		var number json.Number
		if json.Unmarshal(rawValue, &number) == nil {
			return string(number), true
		}
		return "", false
	}
	values := map[string]string{}
	for key := range known {
		value, valid := get(key)
		if !valid {
			return endpoint.Configuration{}, "", DiagnosticMalformed, "vmess_field_type"
		}
		values[key] = value
	}
	if values["v"] != "" && values["v"] != "2" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "vmess_version"
	}
	if values["aid"] != "" && values["aid"] != "0" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "vmess_alter_id"
	}
	if values["type"] != "" && values["type"] != "none" || values["fp"] != "" || values["alpn"] != "" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "vmess_option"
	}
	port, err := strconv.Atoi(values["port"])
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_port"
	}
	address, err := endpoint.NewAddress(values["add"], port)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_address"
	}
	credential, err := endpoint.NewVMessCredential(values["id"])
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_credential"
	}
	transport := endpoint.NewTCPTransport()
	switch valueOr(values["net"], "tcp") {
	case "tcp":
		if values["path"] != "" || values["host"] != "" {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "vmess_transport_option"
		}
	case "ws":
		transport, err = endpoint.NewWebSocketTransportWithHost(values["path"], values["host"])
		if err != nil {
			return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_transport"
		}
	default:
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_transport"
	}
	tls := endpoint.DisabledTLS()
	switch values["tls"] {
	case "":
		if values["sni"] != "" || values["allowInsecure"] != "" {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "tls_option_without_tls"
		}
	case "tls":
		insecure, valid := parseBool(values["allowInsecure"])
		if !valid {
			return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_boolean"
		}
		tls, err = endpoint.NewTLS(values["sni"], insecure)
		if err != nil {
			return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_tls"
		}
	default:
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_security"
	}
	configuration, err := endpoint.NewVMessConfiguration(address, credential, transport, tls, valueOr(values["scy"], "auto"))
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "vmess_security"
	}
	return configuration, values["ps"], 0, ""
}

func parseShadowsocksURI(raw string) (endpoint.Configuration, string, DiagnosticKind, string) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ss" {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_ss_uri"
	}
	if u.Path != "" && u.Path != "/" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_ss_path"
	}
	if u.RawQuery != "" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "ss_plugin_or_option"
	}
	var userinfo, hostport string
	if u.Opaque != "" || (u.User == nil && u.Host != "") {
		encoded := u.Opaque
		if encoded == "" {
			encoded = u.Host
		}
		decoded, valid := decodeShareBase64(encoded)
		if !valid {
			return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_ss_base64"
		}
		parts := strings.SplitN(string(decoded), "@", 2)
		if len(parts) != 2 {
			return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_ss_authority"
		}
		userinfo, hostport = parts[0], parts[1]
	} else {
		if u.User == nil {
			return endpoint.Configuration{}, "", DiagnosticMalformed, "missing_ss_userinfo"
		}
		userinfo, hostport = u.User.String(), u.Host
		if decoded, valid := decodeShareBase64(userinfo); valid {
			userinfo = string(decoded)
		} else {
			userinfo, err = url.PathUnescape(userinfo)
			if err != nil {
				return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_ss_userinfo"
			}
		}
	}
	parts := strings.SplitN(userinfo, ":", 2)
	if len(parts) != 2 {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_ss_userinfo"
	}
	method, password := parts[0], parts[1]
	hostURL, err := url.Parse("ss://" + hostport)
	if err != nil || hostURL.Hostname() == "" || hostURL.Port() == "" {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_ss_authority"
	}
	port, err := strconv.Atoi(hostURL.Port())
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_port"
	}
	address, err := endpoint.NewAddress(hostURL.Hostname(), port)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_address"
	}
	credential, err := endpoint.NewShadowsocksCredential(password)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_credential"
	}
	configuration, err := endpoint.NewShadowsocksConfiguration(address, credential, method)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "ss_method"
	}
	return configuration, u.Fragment, 0, ""
}
