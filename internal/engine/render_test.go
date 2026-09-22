package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/policy"
	"github.com/egressfox-io/egressfox/internal/source"
)

func TestRenderersMatchGoldenAndAreDeterministic(t *testing.T) {
	t.Parallel()
	forward := testGateway(t, false)
	reverse := testGateway(t, true)
	tests := []struct {
		name     string
		renderer engine.Renderer
		golden   string
	}{
		{"mihomo", mihomo.Renderer{}, "mihomo.yaml"},
		{"sing-box", singbox.Renderer{}, "sing-box.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate, err := test.renderer.Render(forward)
			if err != nil {
				t.Fatal(err)
			}
			permuted, err := test.renderer.Render(reverse)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(candidate.Reveal(), permuted.Reveal()) {
				t.Fatal("input order changed rendered bytes")
			}
			want, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if !reflect.DeepEqual(candidate.Reveal(), want) {
				t.Fatalf("rendered artifact differs from %s", test.golden)
			}
			if test.name == "mihomo" {
				var parsed any
				if err := yaml.Unmarshal(candidate.Reveal(), &parsed); err != nil {
					t.Fatalf("invalid YAML: %v", err)
				}
			} else if !json.Valid(candidate.Reveal()) {
				t.Fatal("invalid JSON")
			}
		})
	}
}

func TestManagedRenderersRequireAuthentication(t *testing.T) {
	t.Parallel()
	const username = "egressfox"
	const password = "synthetic-password-0123456789"
	gateway := testGatewayWithListener(t, false, func(t testing.TB) policy.Listener {
		listener, err := policy.NewManagedSOCKSListener(1080, username, password)
		if err != nil {
			t.Fatal(err)
		}
		return listener
	})

	mihomoCandidate, err := (mihomo.Renderer{}).Render(gateway)
	if err != nil {
		t.Fatal(err)
	}
	var mihomoConfig map[string]any
	if err := yaml.Unmarshal(mihomoCandidate.Reveal(), &mihomoConfig); err != nil {
		t.Fatal(err)
	}
	if mihomoConfig["allow-lan"] != true || mihomoConfig["bind-address"] != "0.0.0.0" {
		t.Fatalf("managed Mihomo listener = %#v", mihomoConfig)
	}
	authentication, ok := mihomoConfig["authentication"].([]any)
	if !ok || len(authentication) != 1 || authentication[0] != username+":"+password {
		t.Fatalf("managed Mihomo authentication = %#v", mihomoConfig["authentication"])
	}

	singBoxCandidate, err := (singbox.Renderer{}).Render(gateway)
	if err != nil {
		t.Fatal(err)
	}
	var singBoxConfig struct {
		Inbounds []struct {
			Listen string `json:"listen"`
			Users  []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(singBoxCandidate.Reveal(), &singBoxConfig); err != nil {
		t.Fatal(err)
	}
	if len(singBoxConfig.Inbounds) != 1 || singBoxConfig.Inbounds[0].Listen != "0.0.0.0" || len(singBoxConfig.Inbounds[0].Users) != 1 || singBoxConfig.Inbounds[0].Users[0].Username != username || singBoxConfig.Inbounds[0].Users[0].Password != password {
		t.Fatalf("managed sing-box inbound does not match authenticated contract")
	}
}

func testGateway(t testing.TB, reverse bool) policy.Gateway {
	return testGatewayWithListener(t, reverse, func(t testing.TB) policy.Listener {
		listener, err := policy.NewSOCKSListener("127.0.0.1", 1080)
		if err != nil {
			t.Fatal(err)
		}
		return listener
	})
}

func testGatewayWithListener(t testing.TB, reverse bool, makeListener func(testing.TB) policy.Listener) policy.Gateway {
	t.Helper()
	lines := []string{
		"vless://7ae477a8-3884-4dad-a5a8-a5106778cbbb@edge.example.com:443?security=tls&type=ws&path=%2Fproxy&sni=tls.example.com#VLESS",
		"trojan://synthetic-password@[2001:db8::1]:8443?allowInsecure=1#Trojan",
	}
	if reverse {
		lines[0], lines[1] = lines[1], lines[0]
	}
	sourceID, err := endpoint.NewSourceID("golden")
	if err != nil {
		t.Fatal(err)
	}
	inline, err := source.NewInline(sourceID, []byte(lines[0]+"\n"+lines[1]), source.Limits{})
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
	listener := makeListener(t)
	gateway, err := policy.NewGateway(inventory, listener)
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}
