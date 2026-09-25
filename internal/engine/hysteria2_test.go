package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/policy"
	"github.com/egressfox-io/egressfox/internal/source"
)

func TestHysteria2RenderersPreserveConnectionOptions(t *testing.T) {
	id, _ := endpoint.NewSourceID("hysteria2")
	uri := "hy2://auth-canary@edge.example.com:443?sni=front.example.com&alpn=h3&obfs=salamander&obfs-password=obfs-canary&upmbps=20&downmbps=40&ports=443-444%2C8443"
	inline, err := source.NewInline(id, []byte(uri), source.DefaultLimits())
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
	for _, variant := range []struct {
		profile  artifact.Profile
		renderer engine.Renderer
		fields   []string
	}{
		{artifact.Mihomo11931, mihomo.Renderer{}, []string{"type: hysteria2", "ports: 443-444,8443", "obfs-password: obfs-canary", "sni: front.example.com"}},
		{artifact.SingBox1141, singbox.Renderer{}, []string{`"type": "hysteria2"`, `"server_ports": [`, `"443:444"`, `"8443:8443"`, `"password": "obfs-canary"`, `"server_name": "front.example.com"`}},
	} {
		if err := engine.CheckEndpoint(variant.profile, snapshot.Records()[0].Configuration()); err != nil {
			t.Fatal(err)
		}
		candidate, err := variant.renderer.Render(gateway)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range variant.fields {
			if !strings.Contains(string(candidate.Reveal()), field) {
				t.Fatalf("%s missing %s", variant.profile, field)
			}
		}
	}
}
