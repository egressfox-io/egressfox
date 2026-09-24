package source

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

// HTTP(S) proxy URLs require an explicit URI-list source. The SOCKS5 scheme
// is unambiguous and can also appear in auto-detected lists and JSON URI arrays.
func parseProxyURI(u *url.URL, scheme string) (endpoint.Configuration, string, DiagnosticKind, string) {
	if u.Opaque != "" || u.Hostname() == "" || u.Port() == "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "proxy_uri_option"
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_port"
	}
	address, err := endpoint.NewAddress(u.Hostname(), port)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_address"
	}
	protocol := endpoint.ProtocolHTTPProxy
	if scheme == "socks5" {
		protocol = endpoint.ProtocolSOCKS5
	}
	username, password := "", ""
	if u.User != nil {
		username = u.User.Username()
		var hasPassword bool
		password, hasPassword = u.User.Password()
		if !hasPassword || username == "" || password == "" {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "proxy_auth_form"
		}
		if protocol == endpoint.ProtocolHTTPProxy && strings.Contains(username, ":") {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "proxy_auth_form"
		}
	}
	credential, err := endpoint.NewProxyCredential(protocol, username, password)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_credential"
	}
	tls := endpoint.DisabledTLS()
	if scheme == "https" {
		tls, err = endpoint.NewTLS("", false)
		if err != nil {
			return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_tls"
		}
	}
	configuration, err := endpoint.NewExtendedConfiguration(protocol, address, credential, endpoint.NewTCPTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_endpoint"
	}
	return configuration, u.Fragment, 0, ""
}
