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
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
)

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
				StartupTimeout: 5 * time.Second,
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
