package source

import (
	"encoding/json"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

type xrayProxyUser struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

type xrayProxyServer struct {
	Address string          `json:"address"`
	Port    int             `json:"port"`
	User    string          `json:"user"`
	Pass    string          `json:"pass"`
	Users   []xrayProxyUser `json:"users"`
}

func parseXrayProxyOutbound(object map[string]json.RawMessage, protocol string, remaining int) []parseInput {
	unsupported := func(code string) []parseInput { return []parseInput{{kind: DiagnosticUnsupported, code: code}} }
	for key, value := range object {
		if key != "protocol" && key != "tag" && key != "settings" && key != "streamSettings" && !emptyJSON(value) {
			return unsupported("xray_outbound_option")
		}
	}
	transport, tls, code := parseXrayStream(object["streamSettings"])
	if code != "" {
		return unsupported(code)
	}
	if transport.Kind() != endpoint.TransportTCP || (protocol == "socks" && tls.Enabled()) {
		return unsupported("xray_proxy_transport")
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(object["settings"], &settings) != nil {
		return []parseInput{{kind: DiagnosticMalformed, code: "xray_settings"}}
	}
	var servers []xrayProxyServer
	if raw, exists := settings["servers"]; exists {
		if !onlyJSONKeys(object["settings"], "servers") || json.Unmarshal(raw, &servers) != nil {
			return unsupported("xray_settings_option")
		}
		var rawServers []json.RawMessage
		if json.Unmarshal(raw, &rawServers) != nil {
			return []parseInput{{kind: DiagnosticMalformed, code: "xray_servers"}}
		}
		for _, rawServer := range rawServers {
			if !onlyJSONKeys(rawServer, "address", "port", "users") {
				return unsupported("xray_server_option")
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(rawServer, &fields)
			var users []json.RawMessage
			if fields["users"] != nil && json.Unmarshal(fields["users"], &users) != nil {
				return []parseInput{{kind: DiagnosticMalformed, code: "xray_users"}}
			}
			for _, user := range users {
				if !onlyJSONKeys(user, "user", "pass", "level", "email") {
					return unsupported("xray_user_option")
				}
			}
		}
	} else {
		if !onlyJSONKeys(object["settings"], "address", "port", "user", "pass", "level", "email") {
			return unsupported("xray_settings_option")
		}
		var server xrayProxyServer
		if json.Unmarshal(object["settings"], &server) != nil {
			return []parseInput{{kind: DiagnosticMalformed, code: "xray_settings"}}
		}
		servers = []xrayProxyServer{server}
	}
	if len(servers) == 0 || len(servers) > remaining {
		return unsupported("xray_server_count")
	}
	var alias string
	_ = json.Unmarshal(object["tag"], &alias)
	inputs := make([]parseInput, 0)
	for _, server := range servers {
		users := server.Users
		if len(users) == 0 {
			users = []xrayProxyUser{{User: server.User, Pass: server.Pass}}
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
			kind := endpoint.ProtocolHTTPProxy
			if protocol == "socks" {
				kind = endpoint.ProtocolSOCKS5
			}
			credential, err := endpoint.NewProxyCredential(kind, user.User, user.Pass)
			if err != nil {
				inputs = append(inputs, parseInput{kind: DiagnosticUnsupported, code: "xray_proxy_auth"})
				continue
			}
			configuration, err := endpoint.NewExtendedConfiguration(kind, address, credential, transport, tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
			if err != nil {
				inputs = append(inputs, parseInput{kind: DiagnosticInvalid, code: "invalid_endpoint"})
				continue
			}
			inputs = append(inputs, parseInput{configuration: configuration, alias: alias})
		}
	}
	return inputs
}
