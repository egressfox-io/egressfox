package engine_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
)

func TestRealityVisionControlledTraffic(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY to a with_utls build for controlled Reality traffic")
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	deception := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer deception.Close()
	deceptionPort, err := strconv.Atoi(strings.TrimPrefix(deception.Listener.Addr().String(), "127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	serverPort := availablePort(t)
	serverDir := t.TempDir()
	serverConfig := map[string]any{
		"log": map[string]any{"disabled": true},
		"inbounds": []any{map[string]any{
			"type": "vless", "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort,
			"users": []any{map[string]any{"name": "synthetic", "uuid": "11111111-1111-4111-8111-111111111111", "flow": "xtls-rprx-vision"}},
			"tls": map[string]any{"enabled": true, "server_name": "front.example.com", "reality": map[string]any{
				"enabled": true, "private_key": base64.RawURLEncoding.EncodeToString(key.Bytes()), "short_id": []string{"a1b2"},
				"handshake": map[string]any{"server": "127.0.0.1", "server_port": deceptionPort},
			}},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}
	serverBytes, _ := json.Marshal(serverConfig)
	serverPath := filepath.Join(serverDir, "reality-server.json")
	if err := os.WriteFile(serverPath, serverBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	process := exec.Command(serverBinary, "run", "-c", serverPath, "-D", serverDir)
	process.Stdout, process.Stderr = io.Discard, io.Discard
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitProcess(process) })
	waitForPort(t, serverPort)
	time.Sleep(300 * time.Millisecond) // listener opens before the Reality handshake service is ready
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "reality-vision-ok") }))
	defer destination.Close()
	uri := fmt.Sprintf("vless://11111111-1111-4111-8111-111111111111@127.0.0.1:%d?security=reality&type=tcp&sni=front.example.com&pbk=%s&sid=a1b2&fp=chrome&flow=xtls-rprx-vision", serverPort, base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()))
	for _, client := range []struct {
		name, env string
		profile   artifact.Profile
		renderer  engine.Renderer
		args      func(string, string) []string
	}{
		{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}, func(path, dir string) []string { return []string{"-f", path, "-d", dir} }},
		{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}, func(path, dir string) []string { return []string{"run", "-c", path, "-D", dir} }},
	} {
		t.Run(client.name, func(t *testing.T) {
			binary := os.Getenv(client.env)
			if binary == "" {
				t.Skipf("set %s", client.env)
			}
			port := availablePort(t)
			candidate, err := client.renderer.Render(gatewayFromShareURI(t, uri, port))
			if err != nil {
				t.Fatal(err)
			}
			checker, err := artifact.NewNativeChecker(client.profile, binary, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			validated, err := artifact.Validate(context.Background(), candidate, checker)
			if err != nil {
				t.Fatal(err)
			}
			work := t.TempDir()
			path := filepath.Join(work, "client.config")
			if err := os.WriteFile(path, validated.Reveal(), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(binary, client.args(path, work)...)
			command.Stdout, command.Stderr = io.Discard, io.Discard
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { waitProcess(command) })
			waitForPort(t, port)
			time.Sleep(100 * time.Millisecond)
			if body := requestThroughSOCKS(t, port, strings.TrimPrefix(destination.URL, "http://")); !strings.Contains(body, "reality-vision-ok") {
				t.Fatal("Reality/Vision proxy did not carry application traffic")
			}
		})
	}
}

func TestAdvancedTransportControlledTraffic(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY for controlled transport traffic")
	}
	for _, variant := range []struct {
		name, transportType, shareType, query string
		options                               map[string]any
	}{
		{"tls-utls-alpn", "", "tcp", "fp=qq&alpn=http%2F1.1", nil},
		{"http2", "http", "h2", "path=%2Fh2&host=127.0.0.1", map[string]any{"path": "/h2", "host": []string{"127.0.0.1"}}},
		{"httpupgrade", "httpupgrade", "httpupgrade", "path=%2Fupgrade", map[string]any{"path": "/upgrade"}},
		{"grpc", "grpc", "grpc", "serviceName=EgressFox", map[string]any{"service_name": "EgressFox"}},
	} {
		t.Run(variant.name, func(t *testing.T) {
			serverPort := availablePort(t)
			serverDir := t.TempDir()
			certificate, key := writeTestCertificate(t, serverDir)
			if variant.options != nil {
				variant.options["type"] = variant.transportType
			}
			tlsOptions := map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key}
			if variant.name == "tls-utls-alpn" {
				tlsOptions["alpn"] = []string{"http/1.1"}
			}
			serverModel := map[string]any{
				"log": map[string]any{"disabled": true},
				"inbounds": []any{map[string]any{
					"type": "vless", "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort,
					"users":     []any{map[string]any{"name": "synthetic", "uuid": "11111111-1111-4111-8111-111111111111"}},
					"transport": variant.options,
					"tls":       tlsOptions,
				}},
				"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
				"route":     map[string]any{"final": "direct"},
			}
			serverBytes, _ := json.Marshal(serverModel)
			serverPath := filepath.Join(serverDir, "server.json")
			if err := os.WriteFile(serverPath, serverBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			server := exec.Command(serverBinary, "run", "-c", serverPath, "-D", serverDir)
			server.Stdout, server.Stderr = io.Discard, io.Discard
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { waitProcess(server) })
			waitForPort(t, serverPort)
			time.Sleep(300 * time.Millisecond)
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "advanced-transport-ok") }))
			defer destination.Close()
			uri := fmt.Sprintf("vless://11111111-1111-4111-8111-111111111111@127.0.0.1:%d?type=%s&%s&security=tls&sni=127.0.0.1&allowInsecure=1", serverPort, variant.shareType, variant.query)
			for _, client := range []struct {
				name, env string
				profile   artifact.Profile
				renderer  engine.Renderer
				args      func(string, string) []string
			}{
				{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}, func(path, dir string) []string { return []string{"-f", path, "-d", dir} }},
				{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}, func(path, dir string) []string { return []string{"run", "-c", path, "-D", dir} }},
			} {
				t.Run(client.name, func(t *testing.T) {
					binary := os.Getenv(client.env)
					if binary == "" {
						t.Skipf("set %s", client.env)
					}
					port := availablePort(t)
					candidate, err := client.renderer.Render(gatewayFromShareURI(t, uri, port))
					if err != nil {
						t.Fatal(err)
					}
					checker, err := artifact.NewNativeChecker(client.profile, binary, 10*time.Second)
					if err != nil {
						t.Fatal(err)
					}
					validated, err := artifact.Validate(context.Background(), candidate, checker)
					if err != nil {
						t.Fatal(err)
					}
					work := t.TempDir()
					path := filepath.Join(work, "client.config")
					if err := os.WriteFile(path, validated.Reveal(), 0o600); err != nil {
						t.Fatal(err)
					}
					command := exec.Command(binary, client.args(path, work)...)
					command.Stdout, command.Stderr = io.Discard, io.Discard
					if err := command.Start(); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { waitProcess(command) })
					waitForPort(t, port)
					if body := requestThroughSOCKS(t, port, strings.TrimPrefix(destination.URL, "http://")); !strings.Contains(body, "advanced-transport-ok") {
						t.Fatal("transport did not carry application traffic")
					}
				})
			}
		})
	}
}

func TestXHTTPMihomoControlledTraffic(t *testing.T) {
	binary := os.Getenv("EGRESSFOX_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("set EGRESSFOX_MIHOMO_BINARY for XHTTP traffic")
	}
	for _, mode := range []string{"stream-one", "stream-up", "packet-up"} {
		t.Run(mode, func(t *testing.T) {
			serverPort := availablePort(t)
			work := t.TempDir()
			certificate, key := writeTestCertificate(t, work)
			serverModel := map[string]any{
				"mode": "rule", "log-level": "silent", "allow-lan": false,
				"listeners": []any{map[string]any{
					"name": "synthetic-vless", "type": "vless", "listen": "127.0.0.1", "port": serverPort,
					"users":       []any{map[string]any{"username": "synthetic", "uuid": "11111111-1111-4111-8111-111111111111"}},
					"certificate": certificate, "private-key": key,
					"xhttp-config": map[string]any{"path": "/xhttp", "host": "127.0.0.1", "mode": mode},
				}},
				"rules": []string{"MATCH,DIRECT"},
			}
			serverBytes, _ := yaml.Marshal(serverModel)
			serverPath := filepath.Join(work, "server.yaml")
			if err := os.WriteFile(serverPath, serverBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			server := exec.Command(binary, "-f", serverPath, "-d", work)
			server.Stdout, server.Stderr = io.Discard, io.Discard
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { waitProcess(server) })
			waitForPort(t, serverPort)
			time.Sleep(300 * time.Millisecond)
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "xhttp-ok") }))
			defer destination.Close()
			uri := fmt.Sprintf("vless://11111111-1111-4111-8111-111111111111@127.0.0.1:%d?type=xhttp&path=%%2Fxhttp&host=127.0.0.1&mode=%s&security=tls&sni=127.0.0.1&allowInsecure=1", serverPort, mode)
			port := availablePort(t)
			gateway := gatewayFromShareURI(t, uri, port)
			if err := engine.CheckEndpoint(artifact.SingBox1141, gateway.Inventory().Records()[0].Configuration()); err == nil {
				t.Fatal("sing-box accepted XHTTP without a transport implementation")
			}
			candidate, err := (mihomo.Renderer{}).Render(gateway)
			if err != nil {
				t.Fatal(err)
			}
			checker, err := artifact.NewNativeChecker(artifact.Mihomo11931, binary, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			validated, err := artifact.Validate(context.Background(), candidate, checker)
			if err != nil {
				t.Fatal(err)
			}
			clientDir := t.TempDir()
			clientPath := filepath.Join(clientDir, "client.yaml")
			if err := os.WriteFile(clientPath, validated.Reveal(), 0o600); err != nil {
				t.Fatal(err)
			}
			client := exec.Command(binary, "-f", clientPath, "-d", clientDir)
			client.Stdout, client.Stderr = io.Discard, io.Discard
			if err := client.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { waitProcess(client) })
			waitForPort(t, port)
			if body := requestThroughSOCKS(t, port, strings.TrimPrefix(destination.URL, "http://")); !strings.Contains(body, "xhttp-ok") {
				t.Fatal("XHTTP did not carry application traffic")
			}
		})
	}
}
