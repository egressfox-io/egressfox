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
}
type outbound struct {
	Type       string     `json:"type"`
	Tag        string     `json:"tag"`
	Server     string     `json:"server,omitempty"`
	ServerPort uint16     `json:"server_port,omitempty"`
	UUID       string     `json:"uuid,omitempty"`
	Password   string     `json:"password,omitempty"`
	Network    string     `json:"network,omitempty"`
	TLS        *tlsConfig `json:"tls,omitempty"`
	Transport  *transport `json:"transport,omitempty"`
	Outbounds  []string   `json:"outbounds,omitempty"`
	Default    string     `json:"default,omitempty"`
}
type tlsConfig struct {
	Enabled    bool   `json:"enabled"`
	ServerName string `json:"server_name"`
	Insecure   bool   `json:"insecure"`
}
type transport struct {
	Type string `json:"type"`
	Path string `json:"path"`
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
	model := configuration{
		Log:       logConfig{Disabled: true},
		DNS:       dnsConfig{Servers: []dnsServer{{Type: "local", Tag: "local"}}},
		Inbounds:  []inbound{{Type: "socks", Tag: "egressfox-in", Listen: gateway.Listener().Address(), ListenPort: gateway.Listener().Port()}},
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
	result := outbound{Tag: item.Name, Server: configuration.Address().Host(), ServerPort: configuration.Address().Port(), Network: "tcp"}
	switch configuration.Protocol() {
	case endpoint.ProtocolVLESS:
		result.Type = "vless"
		result.UUID = configuration.Credential().Reveal()
	case endpoint.ProtocolTrojan:
		result.Type = "trojan"
		result.Password = configuration.Credential().Reveal()
	default:
		return outbound{}, &engine.CapabilityError{Profile: r.Profile(), Field: "inventory.protocol", Feature: configuration.Protocol().String()}
	}
	if configuration.TLS().Enabled() {
		result.TLS = &tlsConfig{Enabled: true, ServerName: configuration.TLS().ServerName(), Insecure: configuration.TLS().InsecureSkipVerify()}
	}
	switch configuration.Transport().Kind() {
	case endpoint.TransportTCP:
	case endpoint.TransportWebSocket:
		result.Transport = &transport{Type: "ws", Path: configuration.Transport().WebSocketPath()}
	default:
		return outbound{}, &engine.CapabilityError{Profile: r.Profile(), Field: "inventory.transport", Feature: configuration.Transport().Kind().String()}
	}
	return result, nil
}

var _ engine.Renderer = Renderer{}
