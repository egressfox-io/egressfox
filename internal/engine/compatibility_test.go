package engine_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/policy"
	"github.com/egressfox-io/egressfox/internal/source"
)

func compatibilityGateway(t *testing.T) policy.Gateway {
	t.Helper()
	vmess, _ := json.Marshal(map[string]string{"v": "2", "add": "vm.example.com", "port": "443", "id": "11111111-1111-4111-8111-111111111111", "aid": "0", "net": "ws", "type": "none", "path": "/ws", "host": "front.example.com", "tls": "tls", "sni": "vm.example.com", "scy": "auto"})
	lines := []string{"vmess://" + base64.StdEncoding.EncodeToString(vmess), "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:synthetic-password")) + "@ss.example.com:8388"}
	id, err := endpoint.NewSourceID("compat-render")
	if err != nil {
		t.Fatal(err)
	}
	inline, err := source.NewInline(id, []byte(strings.Join(lines, "\n")), source.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := inline.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := source.Parse(payload, source.ParseOptions{Format: source.FormatURIList})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := endpoint.Deduplicate(snapshot.Records())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := policy.NewSOCKSListener("127.0.0.1", 1080)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := policy.NewGateway(inventory, listener)
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func TestVMessAndShadowsocksRendererFields(t *testing.T) {
	gateway := compatibilityGateway(t)
	mihomoCandidate, err := (mihomo.Renderer{}).Render(gateway)
	if err != nil {
		t.Fatal(err)
	}
	var mihomoModel struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(mihomoCandidate.Reveal(), &mihomoModel); err != nil {
		t.Fatal(err)
	}
	if len(mihomoModel.Proxies) != 2 {
		t.Fatal("missing Mihomo proxies")
	}
	var foundVMess, foundSS bool
	for _, proxy := range mihomoModel.Proxies {
		switch proxy["type"] {
		case "vmess":
			foundVMess = proxy["cipher"] == "auto" && proxy["alterId"] == 0 && proxy["network"] == "ws"
		case "ss":
			foundSS = proxy["cipher"] == "aes-256-gcm" && proxy["password"] == "synthetic-password"
		}
	}
	if !foundVMess || !foundSS {
		t.Fatal("Mihomo protocol fields lost")
	}
	singCandidate, err := (singbox.Renderer{}).Render(gateway)
	if err != nil {
		t.Fatal(err)
	}
	var singModel struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(singCandidate.Reveal(), &singModel); err != nil {
		t.Fatal(err)
	}
	foundVMess, foundSS = false, false
	for _, outbound := range singModel.Outbounds {
		switch outbound["type"] {
		case "vmess":
			foundVMess = outbound["security"] == "auto" && outbound["alter_id"] == float64(0)
		case "shadowsocks":
			foundSS = outbound["method"] == "aes-256-gcm" && outbound["password"] == "synthetic-password"
		}
	}
	if !foundVMess || !foundSS {
		t.Fatal("sing-box protocol fields lost")
	}
}
