package mihomo

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/policy"
)

type Renderer struct{}

func (Renderer) Profile() artifact.Profile { return artifact.Mihomo11931 }

type configuration struct {
	SOCKSPort      uint16       `yaml:"socks-port"`
	BindAddress    string       `yaml:"bind-address"`
	AllowLAN       bool         `yaml:"allow-lan"`
	Authentication []string     `yaml:"authentication,omitempty"`
	Mode           string       `yaml:"mode"`
	LogLevel       string       `yaml:"log-level"`
	Proxies        []proxy      `yaml:"proxies"`
	Groups         []proxyGroup `yaml:"proxy-groups"`
	Rules          []string     `yaml:"rules"`
}

type proxy struct {
	Name              string         `yaml:"name"`
	Type              string         `yaml:"type"`
	Server            string         `yaml:"server"`
	Port              uint16         `yaml:"port"`
	UUID              string         `yaml:"uuid,omitempty"`
	Password          string         `yaml:"password,omitempty"`
	Username          string         `yaml:"username,omitempty"`
	Cipher            string         `yaml:"cipher,omitempty"`
	AlterID           *int           `yaml:"alterId,omitempty"`
	TLS               bool           `yaml:"tls"`
	ServerName        string         `yaml:"servername,omitempty"`
	SNI               string         `yaml:"sni,omitempty"`
	SkipCertVerify    bool           `yaml:"skip-cert-verify"`
	Network           string         `yaml:"network"`
	WebSocket         *webSocketOpts `yaml:"ws-opts,omitempty"`
	HTTP2             *http2Opts     `yaml:"h2-opts,omitempty"`
	GRPC              *grpcOpts      `yaml:"grpc-opts,omitempty"`
	XHTTP             *xhttpOpts     `yaml:"xhttp-opts,omitempty"`
	Flow              string         `yaml:"flow,omitempty"`
	ALPN              []string       `yaml:"alpn,omitempty"`
	ClientFingerprint string         `yaml:"client-fingerprint,omitempty"`
	RealityOpts       *realityOpts   `yaml:"reality-opts,omitempty"`
	Up                string         `yaml:"up,omitempty"`
	Down              string         `yaml:"down,omitempty"`
	Obfs              string         `yaml:"obfs,omitempty"`
	ObfsPassword      string         `yaml:"obfs-password,omitempty"`
	Ports             string         `yaml:"ports,omitempty"`
}

type realityOpts struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

type webSocketOpts struct {
	Path        string            `yaml:"path"`
	Headers     map[string]string `yaml:"headers,omitempty"`
	HTTPUpgrade bool              `yaml:"v2ray-http-upgrade,omitempty"`
}
type http2Opts struct {
	Host []string `yaml:"host,omitempty"`
	Path string   `yaml:"path"`
}
type grpcOpts struct {
	ServiceName string `yaml:"grpc-service-name"`
}
type xhttpOpts struct {
	Path string `yaml:"path"`
	Host string `yaml:"host,omitempty"`
	Mode string `yaml:"mode"`
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
		AllowLAN: gateway.Listener().Managed(), Mode: "rule", LogLevel: "silent", Proxies: proxies,
		Groups: []proxyGroup{{Name: "egressfox", Type: "select", Proxies: names}},
		Rules:  []string{"MATCH,egressfox"},
	}
	if gateway.Listener().Managed() {
		model.Authentication = []string{gateway.Listener().Username() + ":" + gateway.Listener().Password()}
	}
	content, err := yaml.Marshal(model)
	if err != nil {
		return artifact.Candidate{}, fmt.Errorf("mihomo serialization failed")
	}
	return artifact.NewCandidate(r.Profile(), content)
}

func (r Renderer) renderProxy(item engine.NamedRecord) (proxy, error) {
	configuration := item.Record.Configuration()
	if err := engine.CheckEndpoint(r.Profile(), configuration); err != nil {
		return proxy{}, err
	}
	result := proxy{
		Name: item.Name, Server: configuration.Address().Host(), Port: configuration.Address().Port(),
		TLS: configuration.TLS().Enabled(), SkipCertVerify: configuration.TLS().InsecureSkipVerify(), Network: "tcp",
	}
	security := configuration.SecurityOptions()
	result.Flow = configuration.Flow().String()
	result.ALPN = security.ALPN()
	result.ClientFingerprint = security.Fingerprint()
	if reality, enabled := security.Reality(); enabled {
		result.RealityOpts = &realityOpts{PublicKey: reality.PublicKey(), ShortID: reality.RevealShortID()}
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
	case endpoint.ProtocolVMess:
		result.Type = "vmess"
		result.UUID = configuration.Credential().Reveal()
		result.Cipher = configuration.Method()
		zero := 0
		result.AlterID = &zero
		if configuration.TLS().Enabled() {
			result.ServerName = configuration.TLS().ServerName()
		}
	case endpoint.ProtocolShadowsocks:
		result.Type = "ss"
		result.Cipher = configuration.Method()
		result.Password = configuration.Credential().Reveal()
	case endpoint.ProtocolSOCKS5:
		result.Type = "socks5"
		result.Username = configuration.Credential().RevealUsername()
		result.Password = configuration.Credential().Reveal()
	case endpoint.ProtocolHTTPProxy:
		result.Type = "http"
		result.Username = configuration.Credential().RevealUsername()
		result.Password = configuration.Credential().Reveal()
		if configuration.TLS().Enabled() {
			result.SNI = configuration.TLS().ServerName()
		}
	case endpoint.ProtocolHysteria2:
		result.Type = "hysteria2"
		result.Password = configuration.Credential().Reveal()
		result.SNI = configuration.TLS().ServerName()
		result.Network = ""
		if options, ok := configuration.Hysteria2(); ok {
			if ports := options.PortRanges(); len(ports) != 0 {
				for i := range ports {
					ports[i] = strings.ReplaceAll(ports[i], ":", "-")
				}
				result.Ports = strings.Join(ports, ",")
			}
			if options.UpMbps() != 0 {
				result.Up = fmt.Sprintf("%d Mbps", options.UpMbps())
				result.Down = fmt.Sprintf("%d Mbps", options.DownMbps())
			}
			if secret := options.RevealSalamanderPassword(); secret != "" {
				result.Obfs = "salamander"
				result.ObfsPassword = secret
			}
		}
	default:
		return proxy{}, &engine.CapabilityError{Profile: r.Profile(), Field: "inventory.protocol", Feature: configuration.Protocol().String()}
	}
	switch configuration.Transport().Kind() {
	case endpoint.TransportTCP:
	case endpoint.TransportQUIC:
	case endpoint.TransportWebSocket:
		result.Network = "ws"
		result.WebSocket = &webSocketOpts{Path: configuration.Transport().WebSocketPath()}
		if host := configuration.Transport().WebSocketHost(); host != "" {
			result.WebSocket.Headers = map[string]string{"Host": host}
		}
	case endpoint.TransportHTTP2:
		result.Network = "h2"
		result.HTTP2 = &http2Opts{Path: configuration.Transport().Path()}
		if host := configuration.Transport().Host(); host != "" {
			result.HTTP2.Host = []string{host}
		}
	case endpoint.TransportHTTPUpgrade:
		result.Network = "ws"
		result.WebSocket = &webSocketOpts{Path: configuration.Transport().Path(), HTTPUpgrade: true}
		if host := configuration.Transport().Host(); host != "" {
			result.WebSocket.Headers = map[string]string{"Host": host}
		}
	case endpoint.TransportGRPC:
		result.Network = "grpc"
		result.GRPC = &grpcOpts{ServiceName: configuration.Transport().Service()}
	case endpoint.TransportXHTTP:
		result.Network = "xhttp"
		result.XHTTP = &xhttpOpts{Path: configuration.Transport().Path(), Host: configuration.Transport().Host(), Mode: configuration.Transport().Mode()}
	default:
		return proxy{}, &engine.CapabilityError{Profile: r.Profile(), Field: "inventory.transport", Feature: configuration.Transport().Kind().String()}
	}
	return result, nil
}

var _ engine.Renderer = Renderer{}
