package probe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/probe"
)

func TestC2PinnedEnginesProbeProxyEndpoints(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY")
	}
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer destination.Close()
	targetID, _ := observation.NewTargetID("c2-controlled")
	target, err := observation.NewHTTPTarget(targetID, destination.URL, http.StatusNoContent, 15*time.Second, observation.HTTPOptions{AllowHTTP: true, AllowPrivate: true, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	vantage, _ := observation.NewVantageID("c2-fixture")
	for _, variant := range []struct {
		name, kind string
		protocol   endpoint.Protocol
		tls        bool
	}{
		{"socks5", "socks", endpoint.ProtocolSOCKS5, false},
		{"http", "http", endpoint.ProtocolHTTPProxy, false},
		{"https", "http", endpoint.ProtocolHTTPProxy, true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			workspace := t.TempDir()
			if err := os.Chmod(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			serverPort := availablePort(t)
			inbound := map[string]any{"type": variant.kind, "tag": "proxy", "listen": "127.0.0.1", "listen_port": serverPort, "users": []any{map[string]any{"username": "c2-user", "password": "c2-password"}}}
			if variant.tls {
				certificate, key := writeCertificate(t, workspace)
				inbound["tls"] = map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key}
			}
			content, err := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "inbounds": []any{inbound}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "direct"}})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, "server.json")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			command := exec.CommandContext(ctx, serverBinary, "run", "-c", path, "-D", workspace)
			command.Stdout, command.Stderr = io.Discard, io.Discard
			if err := command.Start(); err != nil {
				cancel()
				t.Fatal(err)
			}
			t.Cleanup(func() { cancel(); _ = command.Process.Kill(); _ = command.Wait() })
			waitForPort(t, serverPort)
			time.Sleep(100 * time.Millisecond)
			address, _ := endpoint.NewAddress("127.0.0.1", serverPort)
			credential, _ := endpoint.NewProxyCredential(variant.protocol, "c2-user", "c2-password")
			tlsConfig := endpoint.DisabledTLS()
			if variant.tls {
				tlsConfig, _ = endpoint.NewTLS("127.0.0.1", true)
			}
			configuration, err := endpoint.NewExtendedConfiguration(variant.protocol, address, credential, endpoint.NewTCPTransport(), tlsConfig, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
			if err != nil {
				t.Fatal(err)
			}
			sourceID, _ := endpoint.NewSourceID("c2-fixture")
			recordID, _ := endpoint.NewRecordID("proxy")
			provenance, _ := endpoint.NewProvenance(sourceID, recordID)
			record, err := endpoint.NewRecord(configuration, provenance)
			if err != nil {
				t.Fatal(err)
			}
			for _, profile := range []struct {
				name, env string
				value     artifact.Profile
				renderer  engine.Renderer
			}{
				{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}},
				{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}},
			} {
				t.Run(profile.name, func(t *testing.T) {
					binary := os.Getenv(profile.env)
					if binary == "" {
						t.Skipf("set %s", profile.env)
					}
					checker, err := artifact.NewNativeChecker(profile.value, binary, 10*time.Second)
					if err != nil {
						t.Fatal(err)
					}
					executor, err := probe.NewExecutor(probe.Config{Renderer: profile.renderer, Checker: checker, Binary: binary, Vantage: vantage, StartupTimeout: 5 * time.Second, AllowPrivateEndpoints: true})
					if err != nil {
						t.Fatal(err)
					}
					value, err := executor.Execute(context.Background(), record, target)
					if err != nil {
						t.Fatal(err)
					}
					if value.Outcome() != observation.OutcomeSuccess || value.StatusCode() != http.StatusNoContent {
						t.Fatalf("probe outcome=%v status=%d", value.Outcome(), value.StatusCode())
					}
				})
			}
		})
	}
}
