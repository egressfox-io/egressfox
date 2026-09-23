package engine_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/policy"
	"github.com/egressfox-io/egressfox/internal/publish"
	"github.com/egressfox-io/egressfox/internal/source"
)

func TestPinnedNativeValidation(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		profile  artifact.Profile
		renderer interface {
			Render(policy.Gateway) (artifact.Candidate, error)
		}
	}{
		{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}},
		{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binary := os.Getenv(test.env)
			if binary == "" {
				t.Skipf("set %s to run pinned native validation", test.env)
			}
			checker, err := artifact.NewNativeChecker(test.profile, binary, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			managed := testGatewayWithListener(t, false, func(t testing.TB) policy.Listener {
				listener, err := policy.NewManagedSOCKSListener(1080, "egressfox", "synthetic-password-0123456789")
				if err != nil {
					t.Fatal(err)
				}
				return listener
			})
			for name, gateway := range map[string]policy.Gateway{"byo": testGateway(t, false), "managed-authenticated": managed, "vmess-and-shadowsocks": compatibilityGateway(t)} {
				t.Run(name, func(t *testing.T) {
					candidate, err := test.renderer.Render(gateway)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := artifact.Validate(context.Background(), candidate, checker); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func TestSubscriptionProtocolControlledTraffic(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY for controlled proxy servers")
	}
	for _, variant := range []string{"vmess", "vmess-aes-128-gcm", "vmess-chacha20-poly1305", "vmess-none", "vmess-ws-tls", "vless-ws-tls", "trojan-ws-tls", "shadowsocks-aes-128-gcm", "shadowsocks-aes-256-gcm", "shadowsocks-chacha20-ietf-poly1305"} {
		t.Run(variant, func(t *testing.T) {
			serverPort := availablePort(t)
			serverDir := t.TempDir()
			if err := os.Chmod(serverDir, 0o700); err != nil {
				t.Fatal(err)
			}
			var inbound map[string]any
			var uri string
			switch {
			case variant == "vmess" || strings.HasPrefix(variant, "vmess-") && variant != "vmess-ws-tls":
				inbound = map[string]any{"type": "vmess", "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort, "users": []any{map[string]any{"name": "synthetic", "uuid": "11111111-1111-4111-8111-111111111111", "alterId": 0}}}
				cipher := "auto"
				if variant != "vmess" {
					cipher = strings.TrimPrefix(variant, "vmess-")
				}
				share, _ := json.Marshal(map[string]string{"v": "2", "add": "127.0.0.1", "port": strconv.Itoa(serverPort), "id": "11111111-1111-4111-8111-111111111111", "aid": "0", "net": "tcp", "type": "none", "scy": cipher})
				uri = "vmess://" + base64.StdEncoding.EncodeToString(share)
			case variant == "vmess-ws-tls":
				certificate, key := writeTestCertificate(t, serverDir)
				inbound = map[string]any{"type": "vmess", "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort, "users": []any{map[string]any{"name": "synthetic", "uuid": "11111111-1111-4111-8111-111111111111", "alterId": 0}}, "transport": map[string]any{"type": "ws", "path": "/ws"}, "tls": map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key}}
				share, _ := json.Marshal(map[string]string{"v": "2", "add": "127.0.0.1", "port": strconv.Itoa(serverPort), "id": "11111111-1111-4111-8111-111111111111", "aid": "0", "net": "ws", "type": "none", "path": "/ws", "host": "front.example.com", "tls": "tls", "sni": "127.0.0.1", "allowInsecure": "1", "scy": "auto"})
				uri = "vmess://" + base64.StdEncoding.EncodeToString(share)
			case variant == "vless-ws-tls":
				certificate, key := writeTestCertificate(t, serverDir)
				inbound = map[string]any{"type": "vless", "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort, "users": []any{map[string]any{"name": "synthetic", "uuid": "11111111-1111-4111-8111-111111111111"}}, "transport": map[string]any{"type": "ws", "path": "/ws"}, "tls": map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key}}
				uri = fmt.Sprintf("vless://11111111-1111-4111-8111-111111111111@127.0.0.1:%d?type=ws&path=%%2Fws&host=front.example.com&security=tls&allowInsecure=1", serverPort)
			case variant == "trojan-ws-tls":
				certificate, key := writeTestCertificate(t, serverDir)
				inbound = map[string]any{"type": "trojan", "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort, "users": []any{map[string]any{"password": "synthetic-password"}}, "transport": map[string]any{"type": "ws", "path": "/ws"}, "tls": map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key}}
				uri = fmt.Sprintf("trojan://synthetic-password@127.0.0.1:%d?type=ws&path=%%2Fws&host=front.example.com&security=tls&allowInsecure=1", serverPort)
			case strings.HasPrefix(variant, "shadowsocks-"):
				method := strings.TrimPrefix(variant, "shadowsocks-")
				inbound = map[string]any{"type": "shadowsocks", "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort, "network": "tcp", "method": method, "password": "synthetic-password"}
				uri = "ss://" + base64.RawURLEncoding.EncodeToString([]byte(method+":synthetic-password")) + "@127.0.0.1:" + strconv.Itoa(serverPort)
			}
			serverModel := map[string]any{"log": map[string]any{"disabled": true}, "inbounds": []any{inbound}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "direct"}}
			serverBytes, _ := json.Marshal(serverModel)
			serverPath := filepath.Join(serverDir, "server.json")
			if err := os.WriteFile(serverPath, serverBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			serverProcess := exec.Command(serverBinary, "run", "-c", serverPath, "-D", serverDir)
			serverProcess.Stdout, serverProcess.Stderr = io.Discard, io.Discard
			if err := serverProcess.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { waitProcess(serverProcess) })
			waitForPort(t, serverPort)
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "egressfox-compat-ok") }))
			defer destination.Close()
			for _, engineTest := range []struct {
				name, env string
				profile   artifact.Profile
				renderer  engine.Renderer
				args      func(string, string) []string
			}{
				{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}, func(path, dir string) []string { return []string{"-f", path, "-d", dir} }},
				{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}, func(path, dir string) []string { return []string{"run", "-c", path, "-D", dir} }},
			} {
				t.Run(engineTest.name, func(t *testing.T) {
					binary := os.Getenv(engineTest.env)
					if binary == "" {
						t.Skipf("set %s for traffic test", engineTest.env)
					}
					clientPort := availablePort(t)
					gateway := gatewayFromShareURI(t, uri, clientPort)
					candidate, err := engineTest.renderer.Render(gateway)
					if err != nil {
						t.Fatal(err)
					}
					checker, err := artifact.NewNativeChecker(engineTest.profile, binary, 10*time.Second)
					if err != nil {
						t.Fatal(err)
					}
					validated, err := artifact.Validate(context.Background(), candidate, checker)
					if err != nil {
						t.Fatal(err)
					}
					clientDir := t.TempDir()
					if err := os.Chmod(clientDir, 0o700); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(clientDir, "config")
					publisher, err := publish.NewFilePublisher(path)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := publisher.Publish(context.Background(), validated); err != nil {
						t.Fatal(err)
					}
					process := exec.Command(binary, engineTest.args(path, clientDir)...)
					process.Stdout, process.Stderr = io.Discard, io.Discard
					if err := process.Start(); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { waitProcess(process) })
					waitForPort(t, clientPort)
					if body := requestThroughSOCKS(t, clientPort, strings.TrimPrefix(destination.URL, "http://")); !strings.Contains(body, "egressfox-compat-ok") {
						t.Fatal("controlled traffic did not traverse the selected proxy")
					}
				})
			}
		})
	}
}

func gatewayFromShareURI(t *testing.T, uri string, listenerPort int) policy.Gateway {
	t.Helper()
	id, err := endpoint.NewSourceID("native-compat")
	if err != nil {
		t.Fatal(err)
	}
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
	listener, err := policy.NewSOCKSListener("127.0.0.1", listenerPort)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := policy.NewGateway(inventory, listener)
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func TestSingBoxPublishedArtifactCarriesControlledTraffic(t *testing.T) {
	binary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY to run controlled traffic smoke test")
	}
	work := t.TempDir()
	if err := os.Chmod(work, 0o700); err != nil {
		t.Fatal(err)
	}
	certificate, key := writeTestCertificate(t, work)
	serverPort := availablePort(t)
	clientPort := availablePort(t)
	serverConfig := map[string]any{
		"log": map[string]any{"disabled": true},
		"inbounds": []any{map[string]any{
			"type": "trojan", "tag": "server", "listen": "127.0.0.1", "listen_port": serverPort,
			"users": []any{map[string]any{"password": "controlled-traffic-secret"}},
			"tls":   map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}
	serverBytes, err := json.Marshal(serverConfig)
	if err != nil {
		t.Fatal(err)
	}
	serverPath := filepath.Join(work, "server.json")
	if err := os.WriteFile(serverPath, serverBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverProcess := exec.CommandContext(ctx, binary, "run", "-c", serverPath, "-D", work)
	serverProcess.Stdout, serverProcess.Stderr = io.Discard, io.Discard
	if err := serverProcess.Start(); err != nil {
		t.Fatal(err)
	}
	defer waitProcess(serverProcess)
	waitForPort(t, serverPort)

	gateway := smokeGateway(t, serverPort, clientPort)
	candidate, err := (singbox.Renderer{}).Render(gateway)
	if err != nil {
		t.Fatal(err)
	}
	checker, err := artifact.NewNativeChecker(artifact.SingBox1141, binary, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := artifact.Validate(ctx, candidate, checker)
	if err != nil {
		t.Fatal(err)
	}
	clientDir := filepath.Join(work, "client")
	if err := os.Mkdir(clientDir, 0o700); err != nil {
		t.Fatal(err)
	}
	clientPath := filepath.Join(clientDir, "config.json")
	publisher, err := publish.NewFilePublisher(clientPath)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := publisher.Publish(ctx, validated); err != nil || !result.Changed {
		t.Fatalf("publish changed=%t error=%v", result.Changed, err)
	}
	clientProcess := exec.CommandContext(ctx, binary, "run", "-c", clientPath, "-D", clientDir)
	clientProcess.Stdout, clientProcess.Stderr = io.Discard, io.Discard
	if err := clientProcess.Start(); err != nil {
		t.Fatal(err)
	}
	defer waitProcess(clientProcess)
	waitForPort(t, clientPort)

	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "egressfox-smoke-ok")
	}))
	defer destination.Close()
	destinationAddress := strings.TrimPrefix(destination.URL, "http://")
	if body := requestThroughSOCKS(t, clientPort, destinationAddress); !strings.Contains(body, "egressfox-smoke-ok") {
		t.Fatal("controlled response did not traverse the published artifact")
	}
}

func smokeGateway(t testing.TB, serverPort, clientPort int) policy.Gateway {
	t.Helper()
	sourceID, err := endpoint.NewSourceID("native-smoke")
	if err != nil {
		t.Fatal(err)
	}
	record := fmt.Sprintf("trojan://controlled-traffic-secret@127.0.0.1:%d?security=tls&allowInsecure=1#smoke", serverPort)
	inline, err := source.NewInline(sourceID, []byte(record), source.Limits{})
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
	listener, err := policy.NewSOCKSListener("127.0.0.1", clientPort)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := policy.NewGateway(inventory, listener)
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func writeTestCertificate(t testing.TB, directory string) (string, string) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificatePath := filepath.Join(directory, "certificate.pem")
	keyPath := filepath.Join(directory, "key.pem")
	if err := os.WriteFile(certificatePath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certificatePath, keyPath
}

func availablePort(t testing.TB) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForPort(t testing.TB, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("local engine port %d did not become ready", port)
}

func requestThroughSOCKS(t testing.TB, port int, destination string) string {
	t.Helper()
	host, portText, err := net.SplitHostPort(destination)
	if err != nil {
		t.Fatal(err)
	}
	destinationPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := connection.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[0] != 5 || reply[1] != 0 {
		t.Fatal("SOCKS authentication negotiation failed")
	}
	ip := net.ParseIP(host).To4()
	request := []byte{5, 1, 0, 1, ip[0], ip[1], ip[2], ip[3], byte(destinationPort >> 8), byte(destinationPort)}
	if _, err := connection.Write(request); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(connection, header); err != nil || header[1] != 0 {
		t.Fatal("SOCKS connection failed")
	}
	addressLength := 0
	switch header[3] {
	case 1:
		addressLength = 4
	case 4:
		addressLength = 16
	case 3:
		length := []byte{0}
		if _, err := io.ReadFull(connection, length); err != nil {
			t.Fatal(err)
		}
		addressLength = int(length[0])
	default:
		t.Fatal("SOCKS response used an unknown address type")
	}
	if _, err := io.ReadFull(connection, make([]byte, addressLength+2)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(connection, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", destination); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func waitProcess(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	}
}
