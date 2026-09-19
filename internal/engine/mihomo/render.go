package mihomo

import (
	"fmt"

	"go.yaml.in/yaml/v3"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/policy"
)

type Renderer struct{}

func (Renderer) Profile() artifact.Profile { return artifact.Mihomo11931 }

type configuration struct {
	SOCKSPort   uint16       `yaml:"socks-port"`
	BindAddress string       `yaml:"bind-address"`
	AllowLAN    bool         `yaml:"allow-lan"`
	Mode        string       `yaml:"mode"`
	LogLevel    string       `yaml:"log-level"`
	Proxies     []proxy      `yaml:"proxies"`
	Groups      []proxyGroup `yaml:"proxy-groups"`
	Rules       []string     `yaml:"rules"`
}

type proxy struct {
	Name           string         `yaml:"name"`
	Type           string         `yaml:"type"`
	Server         string         `yaml:"server"`
	Port           uint16         `yaml:"port"`
	UUID           string         `yaml:"uuid,omitempty"`
	Password       string         `yaml:"password,omitempty"`
	TLS            bool           `yaml:"tls"`
	ServerName     string         `yaml:"servername,omitempty"`
	SNI            string         `yaml:"sni,omitempty"`
	SkipCertVerify bool           `yaml:"skip-cert-verify"`
	Network        string         `yaml:"network"`
	WebSocket      *webSocketOpts `yaml:"ws-opts,omitempty"`
}

type webSocketOpts struct {
	Path string `yaml:"path"`
}
type proxyGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

func (r Renderer) Render(gateway policy.Gateway) (artifact.Candidate, error) {
	named := engine.AssignNames(gateway.Inventory())
	proxies := make([]proxy, 0, len(named))
	names := make([]string, 0, len(named))
	for _, item := range named {
		value, err := r.renderProxy(item)
		if err != nil {
			return artifact.Candidate{}, err
		}
		proxies = append(proxies, value)
		names = append(names, item.Name)
	}
	model := configuration{
		SOCKSPort: gateway.Listener().Port(), BindAddress: gateway.Listener().Address(),
		AllowLAN: false, Mode: "rule", LogLevel: "silent", Proxies: proxies,
		Groups: []proxyGroup{{Name: "egressfox", Type: "select", Proxies: names}},
		Rules:  []string{"MATCH,egressfox"},
	}
	content, err := yaml.Marshal(model)
	if err != nil {
		return artifact.Candidate{}, fmt.Errorf("mihomo serialization failed")
	}
	return artifact.NewCandidate(r.Profile(), content)
}

func (r Renderer) renderProxy(item engine.NamedRecord) (proxy, error) {
	configuration := item.Record.Configuration()
	result := proxy{
		Name: item.Name, Server: configuration.Address().Host(), Port: configuration.Address().Port(),
		TLS: configuration.TLS().Enabled(), SkipCertVerify: configuration.TLS().InsecureSkipVerify(), Network: "tcp",
	}
	switch configuration.Protocol() {
	case endpoint.ProtocolVLESS:
		result.Type = "vless"
		result.UUID = configuration.Credential().Reveal()
		if configuration.TLS().Enabled() {
			result.ServerName = configuration.TLS().ServerName()
		}
	case endpoint.ProtocolTrojan:
		result.Type = "trojan"
		result.Password = configuration.Credential().Reveal()
		result.SNI = configuration.TLS().ServerName()
	default:
		return proxy{}, &engine.CapabilityError{Profile: r.Profile(), Field: "inventory.protocol", Feature: configuration.Protocol().String()}
	}
	switch configuration.Transport().Kind() {
	case endpoint.TransportTCP:
	case endpoint.TransportWebSocket:
		result.Network = "ws"
		result.WebSocket = &webSocketOpts{Path: configuration.Transport().WebSocketPath()}
	default:
		return proxy{}, &engine.CapabilityError{Profile: r.Profile(), Field: "inventory.transport", Feature: configuration.Transport().Kind().String()}
	}
	return result, nil
}

var _ engine.Renderer = Renderer{}
