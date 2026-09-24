package source

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

// The official share URI puts a comma-separated port set in the authority.
// net/url rejects that non-RFC port syntax, so normalize only that bounded
// authority form before using its usual percent-decoding and host parsing.
func parseHysteria2Raw(raw string) (endpoint.Configuration, string, DiagnosticKind, string) {
	start := strings.Index(raw, "://") + 3
	if start < 3 {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_hysteria2_uri"
	}
	rest := raw[start:]
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority := rest[:end]
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		hostport := authority[at+1:]
		colon := strings.LastIndex(hostport, ":")
		if strings.HasPrefix(hostport, "[") {
			close := strings.Index(hostport, "]")
			if close < 0 || colon != close+1 {
				colon = -1
			}
		}
		if colon >= 0 {
			portText := hostport[colon+1:]
			if strings.ContainsAny(portText, ",-") {
				entries := strings.Split(strings.ReplaceAll(portText, "-", ":"), ",")
				first := strings.Split(entries[0], ":")[0]
				if _, err := endpoint.NewHysteria2OptionsWithPorts(0, 0, "", entries); err != nil {
					return endpoint.Configuration{}, "", DiagnosticInvalid, "hysteria2_ports"
				}
				prefix := raw[:start+at+1+colon+1]
				suffix := rest[end:]
				separator := "?"
				if strings.Contains(suffix, "?") {
					separator = "&"
				}
				fragment := strings.Index(suffix, "#")
				if fragment >= 0 {
					suffix = suffix[:fragment] + separator + "ports=" + url.QueryEscape(strings.Join(entries, ",")) + suffix[fragment:]
				} else {
					suffix += separator + "ports=" + url.QueryEscape(strings.Join(entries, ","))
				}
				raw = prefix + first + suffix
			}
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_hysteria2_uri"
	}
	return parseHysteria2URI(u)
}

// parseHysteria2URI admits a bounded, explicit share-link subset. Every query
// field that can alter the connection is either mapped or rejected.
func parseHysteria2URI(u *url.URL) (endpoint.Configuration, string, DiagnosticKind, string) {
	if u.Opaque != "" || u.User == nil || u.User.Username() == "" || u.Hostname() == "" || u.Path != "" && u.Path != "/" {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_hysteria2_uri"
	}
	auth := u.User.Username()
	if password, hasPassword := u.User.Password(); hasPassword {
		auth += ":" + password
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "hysteria2_query"
	}
	known := map[string]bool{"sni": true, "insecure": true, "allowInsecure": true, "alpn": true, "obfs": true, "obfs-password": true, "obfsParam": true, "upmbps": true, "downmbps": true, "ports": true}
	for key, values := range query {
		if !known[key] {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "hysteria2_uri_option"
		}
		if len(values) != 1 {
			return endpoint.Configuration{}, "", DiagnosticMalformed, "hysteria2_query_duplicate"
		}
	}
	if query.Has("insecure") && query.Has("allowInsecure") || query.Has("obfs-password") && query.Has("obfsParam") {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "hysteria2_query_duplicate"
	}
	port, err := strconv.Atoi(valueOr(u.Port(), "443"))
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_port"
	}
	address, err := endpoint.NewAddress(u.Hostname(), port)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_address"
	}
	credential, err := endpoint.NewHysteria2Credential(auth)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_credential"
	}
	insecure, valid := parseBool(valueOr(query.Get("insecure"), query.Get("allowInsecure")))
	if !valid {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_boolean"
	}
	tls, err := endpoint.NewTLS(query.Get("sni"), insecure)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_tls"
	}
	var alpn []string
	if value := query.Get("alpn"); value != "" {
		alpn = strings.Split(value, ",")
	}
	security, err := endpoint.NewSecurityOptions(alpn, "", nil)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_alpn"
	}
	secret := valueOr(query.Get("obfs-password"), query.Get("obfsParam"))
	if query.Get("obfs") != "" && query.Get("obfs") != "salamander" || query.Get("obfs") == "" && secret != "" || query.Get("obfs") == "salamander" && secret == "" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "hysteria2_obfs"
	}
	up, upErr := strconv.Atoi(valueOr(query.Get("upmbps"), "0"))
	down, downErr := strconv.Atoi(valueOr(query.Get("downmbps"), "0"))
	if upErr != nil || downErr != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "hysteria2_bandwidth"
	}
	options, err := endpoint.NewHysteria2Options(up, down, secret)
	if query.Has("ports") {
		options, err = endpoint.NewHysteria2OptionsWithPorts(up, down, secret, strings.Split(strings.ReplaceAll(query.Get("ports"), "-", ":"), ","))
	}
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "hysteria2_options"
	}
	if !options.IncludesPort(address.Port()) {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "hysteria2_port_mismatch"
	}
	configuration, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, address, credential, endpoint.NewQUICTransport(), tls, security, endpoint.FlowNone, &options)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_endpoint"
	}
	return configuration, u.Fragment, 0, ""
}
