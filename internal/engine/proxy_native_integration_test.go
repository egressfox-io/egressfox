package engine_test

import (
	"bufio"
	"context"
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
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/policy"
	"github.com/egressfox-io/egressfox/internal/publish"
	"github.com/egressfox-io/egressfox/internal/source"
)

func TestC2ControlledProxyTraffic(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY to run controlled proxy traffic")
	}
	for _, variant := range []struct {
		name, kind string
		auth, tls  bool
	}{
		{"socks-anonymous", "socks", false, false},
		{"socks-authenticated", "socks", true, false},
		{"http-anonymous", "http", false, false},
		{"http-authenticated", "http", true, false},
		{"https-anonymous", "http", false, true},
		{"https-authenticated", "http", true, true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			serverDir := t.TempDir()
			if err := os.Chmod(serverDir, 0o700); err != nil {
				t.Fatal(err)
			}
			serverPort := availablePort(t)
			inbound := map[string]any{"type": variant.kind, "tag": "proxy", "listen": "127.0.0.1", "listen_port": serverPort}
			if variant.auth {
				inbound["users"] = []any{map[string]any{"username": "synthetic-user", "password": "synthetic-password"}}
			}
			if variant.tls {
				cert, key := writeTestCertificate(t, serverDir)
				inbound["tls"] = map[string]any{"enabled": true, "certificate_path": cert, "key_path": key}
			}
			serverModel := map[string]any{"log": map[string]any{"disabled": true}, "inbounds": []any{inbound}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "direct"}}
			serverBytes, err := json.Marshal(serverModel)
			if err != nil {
				t.Fatal(err)
			}
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
			var requests atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = io.WriteString(w, "c2-controlled-traffic")
			}))
			defer destination.Close()
			scheme := "http"
			if variant.kind == "socks" {
				scheme = "socks5"
			} else if variant.tls {
				scheme = "https"
			}
			authority := ""
			if variant.auth {
				authority = "synthetic-user:synthetic-password@"
			}
			uri := fmt.Sprintf("%s://%s127.0.0.1:%d", scheme, authority, serverPort)
			for _, profile := range []struct {
				name, env string
				value     artifact.Profile
				renderer  engine.Renderer
				arguments func(string, string) []string
			}{
				{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}, func(path, dir string) []string { return []string{"-f", path, "-d", dir} }},
				{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}, func(path, dir string) []string { return []string{"run", "-c", path, "-D", dir} }},
			} {
				t.Run(profile.name, func(t *testing.T) {
					binary := os.Getenv(profile.env)
					if binary == "" {
						t.Skipf("set %s", profile.env)
					}
					clientPort := availablePort(t)
					gateway := c2GatewayFromSource(t, uri, clientPort, variant.tls, variant.auth)
					candidate, err := profile.renderer.Render(gateway)
					if err != nil {
						t.Fatal(err)
					}
					checker, err := artifact.NewNativeChecker(profile.value, binary, 10*time.Second)
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
					process := exec.Command(binary, profile.arguments(path, clientDir)...)
					process.Stdout, process.Stderr = io.Discard, io.Discard
					if err := process.Start(); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { waitProcess(process) })
					waitForPort(t, clientPort)
					before := requests.Load()
					body := requestThroughSOCKS(t, clientPort, strings.TrimPrefix(destination.URL, "http://"))
					if body != "c2-controlled-traffic" || requests.Load() != before+1 {
						t.Fatal("traffic did not traverse controlled proxy")
					}
					if variant.auth {
						wrongPort := availablePort(t)
						wrongURI := fmt.Sprintf("%s://synthetic-user:wrong-password@127.0.0.1:%d", scheme, serverPort)
						wrongGateway := c2GatewayFromSource(t, wrongURI, wrongPort, variant.tls, true)
						wrongCandidate, err := profile.renderer.Render(wrongGateway)
						if err != nil {
							t.Fatal(err)
						}
						wrongValidated, err := artifact.Validate(context.Background(), wrongCandidate, checker)
						if err != nil {
							t.Fatal(err)
						}
						wrongPath := filepath.Join(clientDir, "wrong-config")
						wrongPublisher, err := publish.NewFilePublisher(wrongPath)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := wrongPublisher.Publish(context.Background(), wrongValidated); err != nil {
							t.Fatal(err)
						}
						wrongProcess := exec.Command(binary, profile.arguments(wrongPath, clientDir)...)
						wrongProcess.Stdout, wrongProcess.Stderr = io.Discard, io.Discard
						if err := wrongProcess.Start(); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { waitProcess(wrongProcess) })
						waitForPort(t, wrongPort)
						dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:"+strconv.Itoa(wrongPort), nil, proxy.Direct)
						if err != nil {
							t.Fatal(err)
						}
						connection, err := dialer.Dial("tcp", strings.TrimPrefix(destination.URL, "http://"))
						if err == nil {
							_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
							_, err = fmt.Fprintf(connection, "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")
							if err == nil {
								response, readErr := http.ReadResponse(bufio.NewReader(connection), nil)
								err = readErr
								if response != nil {
									_ = response.Body.Close()
									if response.StatusCode != http.StatusOK {
										err = fmt.Errorf("upstream denied request")
									}
								}
							}
						}
						if connection != nil {
							_ = connection.Close()
						}
						if err == nil || requests.Load() != before+1 {
							t.Fatalf("invalid proxy credentials carried traffic: dial err=%v requests before=%d after=%d", err, before, requests.Load())
						}
					}
				})
			}
		})
	}
}

func c2GatewayFromSource(t *testing.T, uri string, listenerPort int, insecureProxyTLS, authenticated bool) policy.Gateway {
	t.Helper()
	if !insecureProxyTLS {
		return gatewayFromShareURI(t, uri, listenerPort)
	}
	// An explicit JSON proxy record carries the test-only insecure verification
	// choice; HTTPS proxy URIs intentionally do not have a weakening query flag.
	id, _ := endpoint.NewSourceID("c2-native")
	portText := uri[strings.LastIndex(uri, ":")+1:]
	serverPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	outbound := map[string]any{"type": "http", "server": "127.0.0.1", "server_port": serverPort, "tls": map[string]any{"enabled": true, "server_name": "127.0.0.1", "insecure": true}}
	if authenticated {
		password := "synthetic-password"
		if strings.Contains(uri, "wrong-password") {
			password = "wrong-password"
		}
		outbound["username"] = "synthetic-user"
		outbound["password"] = password
	}
	model := []any{outbound}
	content, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	inline, err := source.NewInline(id, content, source.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := inline.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := source.Parse(payload, source.ParseOptions{Format: source.FormatJSON})
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
