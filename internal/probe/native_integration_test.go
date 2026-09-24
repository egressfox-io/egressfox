package probe_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
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

func TestRealityVisionControlledObservations(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY to a with_utls build")
	}
	const privateKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"
	const publicKey = "hlF7wMOf6Ha3ZGy1407gPa9xzLrWdFDBapcK2MvAPn4"
	work := t.TempDir()
	deception := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer deception.Close()
	deceptionPort := deception.Listener.Addr().(*net.TCPAddr).Port
	port := availablePort(t)
	server := map[string]any{
		"log": map[string]any{"disabled": true},
		"inbounds": []any{map[string]any{
			"type": "vless", "tag": "server", "listen": "127.0.0.1", "listen_port": port,
			"users": []any{map[string]any{"name": "synthetic", "uuid": "11111111-1111-4111-8111-111111111111", "flow": "xtls-rprx-vision"}},
			"tls": map[string]any{"enabled": true, "server_name": "front.example.com", "reality": map[string]any{
				"enabled": true, "private_key": privateKey, "short_id": []string{"a1b2"},
				"handshake": map[string]any{"server": "127.0.0.1", "server_port": deceptionPort},
			}},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}
	content, _ := json.Marshal(server)
	path := filepath.Join(work, "reality-server.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(serverBinary, "run", "-c", path, "-D", work)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	waitForPort(t, port)
	time.Sleep(100 * time.Millisecond)
	uri := "vless://11111111-1111-4111-8111-111111111111@127.0.0.1:" + strconv.Itoa(port) + "?security=reality&type=tcp&sni=front.example.com&pbk=" + publicKey + "&sid=a1b2&fp=chrome&flow=xtls-rprx-vision"
	sourceID, _ := endpoint.NewSourceID("c3-reality-probe")
	inline, err := source.NewInline(sourceID, []byte(uri), source.DefaultLimits())
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
	record := snapshot.Records()[0]
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer destination.Close()
	targetID, _ := observation.NewTargetID("c3-reality")
	target, err := observation.NewHTTPTarget(targetID, destination.URL, http.StatusNoContent, 10*time.Second, observation.HTTPOptions{AllowHTTP: true, AllowPrivate: true, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	vantage, _ := observation.NewVantageID("c3-native")
	for _, test := range []struct {
		name, env string
		profile   artifact.Profile
		renderer  engine.Renderer
	}{
		{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}},
		{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			binary := os.Getenv(test.env)
			if binary == "" {
				t.Skipf("set %s", test.env)
			}
			checker, err := artifact.NewNativeChecker(test.profile, binary, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			executor, err := probe.NewExecutor(probe.Config{Renderer: test.renderer, Checker: checker, Binary: binary, Vantage: vantage, StartupTimeout: 5 * time.Second, AllowPrivateEndpoints: true})
			if err != nil {
				t.Fatal(err)
			}
			result, err := executor.Execute(context.Background(), record, target)
			if err != nil || result.Outcome() != observation.OutcomeSuccess || result.Key().Profile() != test.profile {
				t.Fatalf("Reality probe did not succeed: %v", err)
			}
		})
	}
}

func TestHysteria2ControlledQUICObservations(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY to the with_quic,with_utls build")
	}
	work := t.TempDir()
	certificate, key := writeCertificate(t, work)
	port := availablePort(t)
	server := map[string]any{
		"log": map[string]any{"disabled": true},
		"inbounds": []any{map[string]any{
			"type": "hysteria2", "tag": "server", "listen": "127.0.0.1", "listen_port": port,
			"users": []any{map[string]any{"password": "synthetic-hy2-auth"}},
			"obfs":  map[string]any{"type": "salamander", "password": "synthetic-hy2-obfs"},
			"tls":   map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}
	content, _ := json.Marshal(server)
	path := filepath.Join(work, "hy2-server.json")
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
	uri := "hy2://synthetic-hy2-auth@127.0.0.1:" + strconv.Itoa(port) + "?sni=localhost&insecure=1&obfs=salamander&obfs-password=synthetic-hy2-obfs&upmbps=20&downmbps=40"
	sourceID, _ := endpoint.NewSourceID("c4-hy2-probe")
	inline, err := source.NewInline(sourceID, []byte(uri), source.DefaultLimits())
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
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer destination.Close()
	targetID, _ := observation.NewTargetID("c4-hy2")
	target, err := observation.NewHTTPTarget(targetID, destination.URL, http.StatusNoContent, 10*time.Second, observation.HTTPOptions{AllowHTTP: true, AllowPrivate: true, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	vantage, _ := observation.NewVantageID("c4-native")
	for _, test := range []struct {
		name, env string
		profile   artifact.Profile
		renderer  engine.Renderer
	}{
		{"mihomo", "EGRESSFOX_MIHOMO_BINARY", artifact.Mihomo11931, mihomo.Renderer{}},
		{"sing-box", "EGRESSFOX_SINGBOX_BINARY", artifact.SingBox1141, singbox.Renderer{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			binary := os.Getenv(test.env)
			if binary == "" {
				t.Skipf("set %s", test.env)
			}
			checker, err := artifact.NewNativeChecker(test.profile, binary, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			executor, err := probe.NewExecutor(probe.Config{Renderer: test.renderer, Checker: checker, Binary: binary, Vantage: vantage, StartupTimeout: 5 * time.Second, AllowPrivateEndpoints: true})
			if err != nil {
				t.Fatal(err)
			}
			result, err := executor.Execute(context.Background(), snapshot.Records()[0], target)
			if err != nil || result.Outcome() != observation.OutcomeSuccess {
				t.Fatalf("Hysteria2 UDP probe: %v", err)
			}
		})
	}
}

func TestUnsupportedXHTTPDoesNotCreateHealthObservation(t *testing.T) {
	binary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY")
	}
	address, _ := endpoint.NewAddress("edge.example.com", 443)
	credential, _ := endpoint.NewVLESSCredential("11111111-1111-4111-8111-111111111111")
	transport, err := endpoint.NewXHTTPTransport("/xhttp", "front.example.com", "stream-one")
	if err != nil {
		t.Fatal(err)
	}
	tls, _ := endpoint.NewTLS("edge.example.com", false)
	configuration, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, address, credential, transport, tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	sourceID, _ := endpoint.NewSourceID("xhttp")
	recordID, _ := endpoint.NewRecordID("node")
	provenance, _ := endpoint.NewProvenance(sourceID, recordID)
	record, err := endpoint.NewRecord(configuration, provenance)
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := observation.NewTargetID("unsupported-xhttp")
	target, err := observation.NewHTTPTarget(targetID, "https://example.com/", http.StatusOK, time.Second, observation.HTTPOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vantage, _ := observation.NewVantageID("c3-native")
	checker, err := artifact.NewNativeChecker(artifact.SingBox1141, binary, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := probe.NewExecutor(probe.Config{Renderer: singbox.Renderer{}, Checker: checker, Binary: binary, Vantage: vantage, StartupTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), record, target)
	if !errors.Is(err, engine.ErrUnsupported) || !reflect.DeepEqual(result, observation.Observation{}) {
		t.Fatalf("unsupported XHTTP became an observation: %v", err)
	}
}

func TestPinnedEnginesProduceControlledObservations(t *testing.T) {
	serverBinary := os.Getenv("EGRESSFOX_SINGBOX_BINARY")
	if serverBinary == "" {
		t.Skip("set EGRESSFOX_SINGBOX_BINARY to run controlled through-engine observations")
	}
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	serverPort := availablePort(t)
	startTrojanServer(t, serverBinary, workspace, serverPort)
	destination := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer destination.Close()
	targetID, _ := observation.NewTargetID("controlled-http")
	target, err := observation.NewHTTPTarget(targetID, destination.URL, http.StatusNoContent, 20*time.Second, observation.HTTPOptions{
		AllowHTTP: true, AllowPrivate: true, MaxResponseBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	record := controlledRecord(t, serverPort)
	vantage, _ := observation.NewVantageID("native-fixture")
	tests := []struct {
		name     string
		env      string
		profile  artifact.Profile
		renderer engine.Renderer
	}{
		{name: "mihomo", env: "EGRESSFOX_MIHOMO_BINARY", profile: artifact.Mihomo11931, renderer: mihomo.Renderer{}},
		{name: "sing-box", env: "EGRESSFOX_SINGBOX_BINARY", profile: artifact.SingBox1141, renderer: singbox.Renderer{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binary := os.Getenv(test.env)
			if binary == "" {
				t.Skipf("set %s to run this controlled observation", test.env)
			}
			checker, err := artifact.NewNativeChecker(test.profile, binary, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			executor, err := probe.NewExecutor(probe.Config{
				Renderer: test.renderer, Checker: checker, Binary: binary, Vantage: vantage,
				StartupTimeout: 5 * time.Second, AllowPrivateEndpoints: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			value, err := executor.Execute(context.Background(), record, target)
			if err != nil {
				t.Fatal(err)
			}
			if value.Outcome() != observation.OutcomeSuccess || value.StatusCode() != http.StatusNoContent || value.Key().Profile() != test.profile {
				t.Fatalf("observation = %v", value)
			}
		})
	}
}

func startTrojanServer(t testing.TB, binary, workspace string, port int) {
	t.Helper()
	certificate, key := writeCertificate(t, workspace)
	configuration := map[string]any{
		"log": map[string]any{"disabled": true},
		"inbounds": []any{map[string]any{
			"type": "trojan", "tag": "server", "listen": "127.0.0.1", "listen_port": port,
			"users": []any{map[string]any{"password": "controlled-probe-secret"}},
			"tls":   map[string]any{"enabled": true, "certificate_path": certificate, "key_path": key},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}
	content, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "server.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, binary, "run", "-c", path, "-D", workspace)
	command.Dir = workspace
	command.Env = []string{"HOME=" + workspace, "TMPDIR=" + workspace, "LANG=C", "LC_ALL=C"}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	})
	waitForPort(t, port)
	// A listening socket precedes completion of sing-box route initialization on
	// some builds. Keep this stabilization in the opt-in fixture, never in the
	// production executor where a failed first request is a real observation.
	time.Sleep(100 * time.Millisecond)
}

func controlledRecord(t testing.TB, port int) endpoint.Record {
	t.Helper()
	address, err := endpoint.NewAddress("127.0.0.1", port)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := endpoint.NewTrojanCredential("controlled-probe-secret")
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := endpoint.NewTLS("127.0.0.1", true)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := endpoint.NewConfiguration(endpoint.ProtocolTrojan, address, credential, endpoint.NewTCPTransport(), tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := endpoint.NewSourceID("native-fixture")
	recordID, _ := endpoint.NewRecordID("trojan-server")
	provenance, err := endpoint.NewProvenance(source, recordID)
	if err != nil {
		t.Fatal(err)
	}
	record, err := endpoint.NewRecord(configuration, provenance)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func writeCertificate(t testing.TB, directory string) (string, string) {
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
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForPort(t testing.TB, port int) {
	t.Helper()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp4", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("controlled engine did not become ready")
}
