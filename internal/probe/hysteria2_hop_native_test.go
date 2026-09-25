package probe_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/probe"
	"github.com/egressfox-io/egressfox/internal/source"
)

// hopForwarder observes the actual destination port before forwarding each UDP
// datagram to one controlled Hysteria2 server. It exists only in this native test.
type hopForwarder struct {
	listener *net.UDPConn
	backend  *net.UDPAddr
	count    atomic.Int64
	mu       sync.Mutex
	sessions map[string]*net.UDPConn
}

func newHopForwarder(t *testing.T, backend *net.UDPAddr) *hopForwarder {
	t.Helper()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	forwarder := &hopForwarder{listener: listener, backend: backend, sessions: map[string]*net.UDPConn{}}
	t.Cleanup(func() {
		listener.Close()
		forwarder.mu.Lock()
		defer forwarder.mu.Unlock()
		for _, session := range forwarder.sessions {
			session.Close()
		}
	})
	go forwarder.receive()
	return forwarder
}

func (forwarder *hopForwarder) port() int { return forwarder.listener.LocalAddr().(*net.UDPAddr).Port }

func (forwarder *hopForwarder) receive() {
	buffer := make([]byte, 65535)
	for {
		n, client, err := forwarder.listener.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		forwarder.count.Add(1)
		forwarder.mu.Lock()
		session := forwarder.sessions[client.String()]
		if session == nil && len(forwarder.sessions) < 64 {
			session, err = net.DialUDP("udp4", nil, forwarder.backend)
			if err == nil {
				forwarder.sessions[client.String()] = session
				go forwarder.returnTraffic(session, client)
			}
		}
		forwarder.mu.Unlock()
		if session != nil {
			_, _ = session.Write(buffer[:n])
		}
	}
}

func (forwarder *hopForwarder) returnTraffic(session *net.UDPConn, client *net.UDPAddr) {
	buffer := make([]byte, 65535)
	for {
		n, err := session.Read(buffer)
		if err != nil {
			return
		}
		_, _ = forwarder.listener.WriteToUDP(buffer[:n], client)
	}
}

func TestHysteria2PortHopping(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		if os.Getenv("EGRESSFOX_NATIVE_REQUIRED") == "1" {
			t.Fatal("C4 sing-box binary is required; run make engines-native")
		}
		t.Skip("set EGRESSFOX_SINGBOX_BINARY to the C4 with_quic,with_utls build")
	}
	requireHysteria2ServerProfile(t, serverBinary)
	for _, test := range []struct {
		name, env string
		profile   artifact.Profile
		renderer  engine.Renderer
	}{
		{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}},
		{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			clientBinary := os.Getenv(test.env)
			if clientBinary == "" {
				if os.Getenv("EGRESSFOX_NATIVE_REQUIRED") == "1" {
					t.Fatalf("%s binary is required; run make engines-native", test.name)
				}
				t.Skipf("set %s", test.env)
			}
			work := t.TempDir()
			certificate, key := writeCertificate(t, work)
			reserved, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			backend := reserved.LocalAddr().(*net.UDPAddr)
			reserved.Close()
			forwarders := make([]*hopForwarder, 8)
			ports := make([]string, len(forwarders))
			for index := range forwarders {
				forwarders[index] = newHopForwarder(t, backend)
				ports[index] = strconv.Itoa(forwarders[index].port())
			}
			usedPorts := func() int {
				count := 0
				for _, forwarder := range forwarders {
					if forwarder.count.Load() > 0 {
						count++
					}
				}
				return count
			}
			server := map[string]any{
				"log": map[string]any{"disabled": true},
				"inbounds": []any{map[string]any{
					"type": "hysteria2", "tag": "server", "listen": "127.0.0.1", "listen_port": backend.Port,
					"users": []any{map[string]any{"password": "synthetic-hy2-auth"}},
					"obfs":  map[string]any{"type": "salamander", "password": "synthetic-hy2-obfs"},
					"tls":   map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key},
				}},
				"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
				"route":     map[string]any{"final": "direct"},
			}
			content, _ := json.Marshal(server)
			path := filepath.Join(work, "hy2-hop-server.json")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(serverBinary, "run", "-c", path, "-D", work)
			command.Stdout, command.Stderr = io.Discard, io.Discard
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
			time.Sleep(400 * time.Millisecond)
			uri := "hy2://synthetic-hy2-auth@127.0.0.1:" + ports[0] + "?ports=" + url.QueryEscape(strings.Join(ports, ",")) + "&sni=localhost&insecure=1&obfs=salamander&obfs-password=synthetic-hy2-obfs&upmbps=20&downmbps=40"
			sourceID, _ := endpoint.NewSourceID("c4-hop")
			inline, err := source.NewInline(sourceID, []byte(uri), source.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			payload, err := inline.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			snapshot, _, err := source.Parse(payload, source.ParseOptions{Format: source.FormatURIList})
			if err != nil || snapshot.Len() != 1 {
				t.Fatalf("port-set source admission: %v", err)
			}
			// Streaming application bytes keep one client process and QUIC session
			// active across the engine's default 30-second hop interval.
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.WriteHeader(http.StatusOK)
				flusher, _ := w.(http.Flusher)
				deadline := time.NewTimer(95 * time.Second)
				defer deadline.Stop()
				ticker := time.NewTicker(500 * time.Millisecond)
				defer ticker.Stop()
				for {
					_, _ = w.Write([]byte("x"))
					if flusher != nil {
						flusher.Flush()
					}
					if usedPorts() >= 2 {
						return
					}
					select {
					case <-request.Context().Done():
						return
					case <-deadline.C:
						return
					case <-ticker.C:
					}
				}
			}))
			defer destination.Close()
			targetID, _ := observation.NewTargetID("c4-hop")
			target, err := observation.NewHTTPTarget(targetID, destination.URL, http.StatusOK, 105*time.Second, observation.HTTPOptions{AllowHTTP: true, AllowPrivate: true, MaxResponseBytes: 1024})
			if err != nil {
				t.Fatal(err)
			}
			vantage, _ := observation.NewVantageID("c4-hop-native")
			checker, err := artifact.NewNativeChecker(test.profile, clientBinary, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			executor, err := probe.NewExecutor(probe.Config{Renderer: test.renderer, Checker: checker, Binary: clientBinary, Vantage: vantage, StartupTimeout: 5 * time.Second, AllowPrivateEndpoints: true})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
			defer cancel()
			result, err := executor.Execute(ctx, snapshot.Records()[0], target)
			for _, forwarder := range forwarders {
				if count := forwarder.count.Load(); count > 0 {
					t.Logf("observed UDP destination port %d: %d datagrams", forwarder.port(), count)
				}
			}
			if err != nil || result.Outcome() != observation.OutcomeSuccess || usedPorts() < 2 {
				t.Fatalf("Hysteria2 hopping traffic outcome=%v error=%v", result.Outcome(), err)
			}
		})
	}
}
