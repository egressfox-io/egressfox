package singbox

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/policy"
)

type Renderer struct{}

func (Renderer) Profile() artifact.Profile { return artifact.SingBox1141 }

type configuration struct {
	Log       logConfig   `json:"log"`
	DNS       dnsConfig   `json:"dns"`
	Inbounds  []inbound   `json:"inbounds"`
	Outbounds []outbound  `json:"outbounds"`
	Route     routeConfig `json:"route"`
}
type logConfig struct {
	Disabled bool `json:"disabled"`
}
type dnsConfig struct {
	Servers []dnsServer `json:"servers"`
}
type dnsServer struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}
type inbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort uint16 `json:"listen_port"`
	Users      []user `json:"users,omitempty"`
}
type user struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type outbound struct {
	Type        string     `json:"type"`
	Tag         string     `json:"tag"`
	Server      string     `json:"server,omitempty"`
	ServerPort  uint16     `json:"server_port,omitempty"`
	UUID        string     `json:"uuid,omitempty"`
	Password    string     `json:"password,omitempty"`
	Username    string     `json:"username,omitempty"`
	Version     string     `json:"version,omitempty"`
	Security    string     `json:"security,omitempty"`
	AlterID     *int       `json:"alter_id,omitempty"`
	Method      string     `json:"method,omitempty"`
	Network     string     `json:"network,omitempty"`
	Flow        string     `json:"flow,omitempty"`
	TLS         *tlsConfig `json:"tls,omitempty"`
	Transport   *transport `json:"transport,omitempty"`
	Outbounds   []string   `json:"outbounds,omitempty"`
	Default     string     `json:"default,omitempty"`
	UpMbps      int        `json:"up_mbps,omitempty"`
	DownMbps    int        `json:"down_mbps,omitempty"`
	Obfs        *hy2Obfs   `json:"obfs,omitempty"`
	ServerPorts []string   `json:"server_ports,omitempty"`
}
type hy2Obfs struct {
	Type     string `json:"type"`
	Password string `json:"password"`
}
type tlsConfig struct {
	Enabled    bool           `json:"enabled"`
	ServerName string         `json:"server_name"`
	Insecure   bool           `json:"insecure"`
	ALPN       []string       `json:"alpn,omitempty"`
	UTLS       *utlsConfig    `json:"utls,omitempty"`
	Reality    *realityConfig `json:"reality,omitempty"`
}
type utlsConfig struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint"`
}
type realityConfig struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id"`
}
type transport struct {
	Type        string            `json:"type"`
	Path        string            `json:"path,omitempty"`
	Host        any               `json:"host,omitempty"`
	ServiceName string            `json:"service_name,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}
type routeConfig struct {
	Final                 string `json:"final"`
	DefaultDomainResolver string `json:"default_domain_resolver"`
}

func (r Renderer) Render(gateway policy.Gateway) (artifact.Candidate, error) {
	named := engine.AssignNames(gateway.Inventory())
	outbounds := make([]outbound, 0, len(named)+1)
	names := make([]string, 0, len(named))
	for _, item := range named {
		value, err := r.renderOutbound(item)
		if err != nil {
			return artifact.Candidate{}, err
		}
		outbounds = append(outbounds, value)
		names = append(names, item.Name)
	}
	outbounds = append(outbounds, outbound{Type: "selector", Tag: "egressfox", Outbounds: names, Default: names[0]})
	inboundModel := inbound{Type: "socks", Tag: "egressfox-in", Listen: gateway.Listener().Address(), ListenPort: gateway.Listener().Port()}
	if gateway.Listener().Managed() {
		inboundModel.Users = []user{{Username: gateway.Listener().Username(), Password: gateway.Listener().Password()}}
	}
	model := configuration{
		Log:       logConfig{Disabled: true},
		DNS:       dnsConfig{Servers: []dnsServer{{Type: "local", Tag: "local"}}},
		Inbounds:  []inbound{inboundModel},
		Outbounds: outbounds,
		Route:     routeConfig{Final: "egressfox", DefaultDomainResolver: "local"},
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(model); err != nil {
		return artifact.Candidate{}, fmt.Errorf("sing-box serialization failed")
	}
	return artifact.NewCandidate(r.Profile(), buffer.Bytes())
}

func (r Renderer) renderOutbound(item engine.NamedRecord) (outbound, error) {
	configuration := item.Record.Configuration()
	if err := engine.CheckEndpoint(r.Profile(), configuration); err != nil {
		return outbound{}, err
	}
	result := outbound{Tag: item.Name, Server: configuration.Address().Host(), ServerPort: configuration.Address().Port(), Network: "tcp"}
	security := configuration.SecurityOptions()
	result.Flow = configuration.Flow().String()
	switch configuration.Protocol() {
	case endpoint.ProtocolVLESS:
		result.Type = "vless"
		result.UUID = configuration.Credential().Reveal()
	case endpoint.ProtocolTrojan:
		result.Type = "trojan"
		result.Password = configuration.Credential().Reveal()
	case endpoint.ProtocolVMess:
		result.Type = "vmess"
		result.UUID = configuration.Credential().Reveal()
		result.Security = configuration.Method()
		zero := 0
		result.AlterID = &zero
	case endpoint.ProtocolShadowsocks:
		result.Type = "shadowsocks"
		result.Method = configuration.Method()
		result.Password = configuration.Credential().Reveal()
	case endpoint.ProtocolSOCKS5:
		result.Type = "socks"
		result.Version = "5"
		result.Username = configuration.Credential().RevealUsername()
		result.Password = configuration.Credential().Reveal()
	case endpoint.ProtocolHTTPProxy:
		result.Type = "http"
		result.Username = configuration.Credential().RevealUsername()
		result.Password = configuration.Credential().Reveal()
		result.Network = ""
	case endpoint.ProtocolHysteria2:
		result.Type = "hysteria2"
		result.Password = configuration.Credential().Reveal()
		result.Network = ""
		if options, ok := configuration.Hysteria2(); ok {
			result.ServerPorts = options.PortRanges()
			if len(result.ServerPorts) != 0 {
				result.ServerPort = 0
			}
			result.UpMbps = options.UpMbps()
			result.DownMbps = options.DownMbps()
			if secret := options.RevealSalamanderPassword(); secret != "" {
				result.Obfs = &hy2Obfs{Type: "salamander", Password: secret}
			}
		}
	default:
		return outbound{}, &engine.CapabilityError{Profile: r.Profile(), Field: "inventory.protocol", Feature: configuration.Protocol().String()}
	}
	if configuration.TLS().Enabled() {
		result.TLS = &tlsConfig{Enabled: true, ServerName: configuration.TLS().ServerName(), Insecure: configuration.TLS().InsecureSkipVerify(), ALPN: security.ALPN()}
		if security.Fingerprint() != "" {
			result.TLS.UTLS = &utlsConfig{Enabled: true, Fingerprint: security.Fingerprint()}
		}
		if reality, enabled := security.Reality(); enabled {
			result.TLS.Reality = &realityConfig{Enabled: true, PublicKey: reality.PublicKey(), ShortID: reality.RevealShortID()}
		}
	}
	switch configuration.Transport().Kind() {
	case endpoint.TransportTCP:
	case endpoint.TransportQUIC:
	case endpoint.TransportWebSocket:
		result.Transport = &transport{Type: "ws", Path: configuration.Transport().WebSocketPath()}
		if host := configuration.Transport().WebSocketHost(); host != "" {
			result.Transport.Headers = map[string]string{"Host": host}
		}
	case endpoint.TransportHTTP2:
		result.Transport = &transport{Type: "http", Path: configuration.Transport().Path()}
		if host := configuration.Transport().Host(); host != "" {
			result.Transport.Host = []string{host}
		}
	case endpoint.TransportHTTPUpgrade:
		result.Transport = &transport{Type: "httpupgrade", Path: configuration.Transport().Path(), Host: configuration.Transport().Host()}
	case endpoint.TransportGRPC:
		result.Transport = &transport{Type: "grpc", ServiceName: configuration.Transport().Service()}
	default:
		return outbound{}, &engine.CapabilityError{Profile: r.Profile(), Field: "inventory.transport", Feature: configuration.Transport().Kind().String()}
	}
	return result, nil
}

var _ engine.Renderer = Renderer{}
