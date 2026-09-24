package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/policy"
)

type fixedResolver struct {
	addresses []netip.Addr
	err       error
	calls     atomic.Int64
}

func (resolver *fixedResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	resolver.calls.Add(1)
	return append([]netip.Addr(nil), resolver.addresses...), resolver.err
}

func TestAuthorizeTargetValidatesEveryResolution(t *testing.T) {
	t.Parallel()
	public := netip.MustParseAddr("8.8.8.8")
	private := netip.MustParseAddr("10.0.0.7")
	cases := []struct {
		name         string
		addresses    []netip.Addr
		allowPrivate bool
		want         netip.Addr
		wantCode     string
	}{
		{name: "public", addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111"), public}, want: public},
		{name: "deterministic order", addresses: []netip.Addr{netip.MustParseAddr("8.8.8.9"), public}, want: public},
		{name: "mixed resolution denied", addresses: []netip.Addr{public, private}, wantCode: "target_address_denied"},
		{name: "private explicitly allowed", addresses: []netip.Addr{private}, allowPrivate: true, want: private},
		{name: "shared space denied", addresses: []netip.Addr{netip.MustParseAddr("100.64.0.1")}, wantCode: "target_address_denied"},
		{name: "shared space explicitly allowed", addresses: []netip.Addr{netip.MustParseAddr("100.64.0.1")}, allowPrivate: true, want: netip.MustParseAddr("100.64.0.1")},
		{name: "link local always denied", addresses: []netip.Addr{netip.MustParseAddr("169.254.1.1")}, allowPrivate: true, wantCode: "target_address_denied"},
		{name: "empty", wantCode: "target_resolution_empty"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fixedResolver{addresses: test.addresses}
			target := testTarget(t, "target", "https://health.example.test/status", test.allowPrivate)
			got, err := authorizeTarget(context.Background(), resolver, target)
			if test.wantCode != "" {
				var failure *ExecutionError
				if !errors.As(err, &failure) || failure.Code() != test.wantCode {
					t.Fatalf("authorize error = %v", err)
				}
				return
			}
			if err != nil || got.address != test.want {
				t.Fatalf("authorize = %v, %v; want %v", got.address, err, test.want)
			}
		})
	}
}

func TestAuthorizeLiteralDoesNotResolveAndRejectsUnspecified(t *testing.T) {
	t.Parallel()
	resolver := &fixedResolver{err: errors.New("must not be called")}
	target := testTarget(t, "literal", "https://8.8.4.4/", false)
	got, err := authorizeTarget(context.Background(), resolver, target)
	if err != nil || got.address.String() != "8.8.4.4" || resolver.calls.Load() != 0 {
		t.Fatalf("literal authorization = %v, %v, calls=%d", got.address, err, resolver.calls.Load())
	}
	denied := testTarget(t, "unspecified", "http://0.0.0.0/", true)
	if _, err := authorizeTarget(context.Background(), resolver, denied); !errors.Is(err, ErrExecution) {
		t.Fatalf("unspecified address error = %v", err)
	}
}

func TestAuthorizeEndpointPinsAddressWithoutChangingEvidenceIdentity(t *testing.T) {
	t.Parallel()
	record := testRecord(t, 7, "endpoint-credential")
	vantage, _ := observation.NewVantageID("test-host")
	executor := &Executor{
		vantage:  vantage,
		resolver: &fixedResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}},
	}
	executionRecord, err := executor.authorizeEndpoint(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if executionRecord.Configuration().Address().Host() != "8.8.8.8" ||
		executionRecord.Configuration().TLS().ServerName() != record.Configuration().TLS().ServerName() ||
		executionRecord.Identity().Equal(record.Identity()) {
		t.Fatalf("execution record did not pin only the network address")
	}
	privateRecord := recordWithAddress(t, "127.0.0.1", "private-endpoint")
	if _, err := executor.authorizeEndpoint(context.Background(), privateRecord); !errors.Is(err, ErrExecution) {
		t.Fatalf("private endpoint error = %v", err)
	}
	executor.allowPrivateEndpoints = true
	if _, err := executor.authorizeEndpoint(context.Background(), privateRecord); err != nil {
		t.Fatalf("explicitly authorized private endpoint = %v", err)
	}
}

func TestAuthorizeEndpointPreservesVMessAndShadowsocksSemantics(t *testing.T) {
	address, err := endpoint.NewAddress("edge.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	vmessCredential, err := endpoint.NewVMessCredential("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	transport, err := endpoint.NewWebSocketTransportWithHost("/ws", "front.example.com")
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := endpoint.NewTLS("sni.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	vmess, err := endpoint.NewVMessConfiguration(address, vmessCredential, transport, tlsConfig, "aes-128-gcm")
	if err != nil {
		t.Fatal(err)
	}
	ssCredential, err := endpoint.NewShadowsocksCredential("synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	shadowsocks, err := endpoint.NewShadowsocksConfiguration(address, ssCredential, "aes-128-gcm")
	if err != nil {
		t.Fatal(err)
	}
	sourceID, _ := endpoint.NewSourceID("controlled")
	recordID, _ := endpoint.NewRecordID("controlled-record")
	provenance, _ := endpoint.NewProvenance(sourceID, recordID)
	executor := &Executor{resolver: &fixedResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}}
	for _, original := range []endpoint.Configuration{vmess, shadowsocks} {
		record, err := endpoint.NewRecord(original, provenance)
		if err != nil {
			t.Fatal(err)
		}
		pinned, err := executor.authorizeEndpoint(context.Background(), record)
		if err != nil {
			t.Fatal(err)
		}
		configuration := pinned.Configuration()
		if configuration.Address().Host() != "8.8.8.8" || configuration.Protocol() != original.Protocol() || configuration.Method() != original.Method() || configuration.Transport().WebSocketPath() != original.Transport().WebSocketPath() || configuration.Transport().WebSocketHost() != original.Transport().WebSocketHost() || configuration.TLS() != original.TLS() || configuration.Credential().Reveal() != original.Credential().Reveal() {
			t.Fatal("probe address pinning changed protocol or connection parameters")
		}
	}
}

func TestExecutorReportsSafeEngineExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses a POSIX shell script")
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "fake-engine")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	vantage, _ := observation.NewVantageID("test-host")
	executor, err := NewExecutor(Config{
		Renderer: fakeRenderer{}, Checker: acceptingChecker{}, Binary: binary, Vantage: vantage,
		Resolver:       &fixedResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}},
		StartupTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	executor.reserve = func() (net.Listener, int, error) { return fakeListener{}, 31987, nil }
	record := testRecord(t, 1, "engine-secret-canary")
	target := testTarget(t, "safe-target", "https://health.example.test/?token=target-secret-canary", false)
	_, err = executor.Execute(context.Background(), record, target)
	var failure *ExecutionError
	if !errors.As(err, &failure) || failure.Code() != "engine_exit" && failure.Code() != "engine_readiness_timeout" {
		t.Fatalf("execution error = %v", err)
	}
	for _, rendered := range []string{fmt.Sprint(err), fmt.Sprintf("%+v", executor)} {
		if strings.Contains(rendered, "engine-secret-canary") || strings.Contains(rendered, "target-secret-canary") || strings.Contains(rendered, binary) {
			t.Fatalf("diagnostic leaked confidential input: %q", rendered)
		}
	}
}

func TestConsumeResponseOutcomesAndBounds(t *testing.T) {
	t.Parallel()
	target := testTarget(t, "response", "https://example.test/", false)
	tests := []struct {
		name       string
		status     int
		body       string
		want       observation.Outcome
		wantStatus int
	}{
		{name: "success", status: 204, want: observation.OutcomeSuccess, wantStatus: 204},
		{name: "unexpected", status: 503, want: observation.OutcomeUnexpectedResponse, wantStatus: 503},
		{name: "bounded", status: 204, body: strings.Repeat("x", int(observation.DefaultMaxResponseBytes)+1), want: observation.OutcomeResponseTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}
			outcome, status, err := consumeResponse(context.Background(), response, target)
			if err != nil || outcome != test.want || status != test.wantStatus {
				t.Fatalf("consume = %s, %d, %v", outcome, status, err)
			}
		})
	}
}

func TestTransportErrorClassification(t *testing.T) {
	t.Parallel()
	if !isTimeout(&net.DNSError{IsTimeout: true}) {
		t.Fatal("network timeout was not classified")
	}
	for _, err := range []error{
		tls.RecordHeaderError{},
		tls.AlertError(40),
		x509.UnknownAuthorityError{},
		x509.HostnameError{},
		x509.CertificateInvalidError{},
	} {
		if !isTLSFailure(err) {
			t.Fatalf("TLS error %T was not classified", err)
		}
	}
	if isTLSFailure(&net.DNSError{IsTimeout: true}) {
		t.Fatal("non-TLS error was classified as TLS")
	}
}

func TestSchedulerBoundsConcurrencyAndPreservesResultOrder(t *testing.T) {
	t.Parallel()
	runner := &delayedRunner{delay: 4 * time.Millisecond}
	scheduler, err := NewScheduler(runner, ScheduleConfig{Concurrency: 6, Queue: 3, PerEndpoint: 1, PerTarget: 2, MaxJobs: 100})
	if err != nil {
		t.Fatal(err)
	}
	var jobs []Job
	sharedRecord := testRecord(t, 1, "shared-record")
	for index := 0; index < 8; index++ {
		jobs = append(jobs, Job{Record: sharedRecord, Target: testTarget(t, fmt.Sprintf("endpoint-target-%d", index), "https://example.test/", false)})
	}
	sharedTarget := testTarget(t, "shared-target", "https://example.test/", false)
	for index := 2; index < 10; index++ {
		jobs = append(jobs, Job{Record: testRecord(t, index, fmt.Sprintf("record-%d", index)), Target: sharedTarget})
	}
	results, stats, err := scheduler.Run(context.Background(), jobs)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PeakRunning > 6 || stats.PeakEndpointRunning > 1 || stats.PeakTargetRunning > 2 || stats.PeakRunning < 2 {
		t.Fatalf("scheduler stats = %+v", stats)
	}
	for index, result := range results {
		if result.Err != nil || result.Observation.Key().Connection().ID().String() != jobs[index].Record.ID().String() {
			t.Fatalf("result %d = %+v", index, result)
		}
	}
}

func TestSchedulerCancellationAndJobLimit(t *testing.T) {
	t.Parallel()
	scheduler, err := NewScheduler(&delayedRunner{delay: time.Second}, ScheduleConfig{Concurrency: 2, Queue: 1, PerEndpoint: 1, PerTarget: 1, MaxJobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	job := Job{Record: testRecord(t, 1, "credential"), Target: testTarget(t, "target", "https://example.test/", false)}
	if _, _, err := scheduler.Run(context.Background(), []Job{job, job, job}); !errors.Is(err, ErrSchedule) {
		t.Fatalf("job limit error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	results, _, err := scheduler.Run(ctx, []Job{job, job})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Err == nil {
			t.Fatal("cancelled schedule returned a successful result")
		}
	}
}

func TestSchedulerConfiguration(t *testing.T) {
	t.Parallel()
	valid := DefaultScheduleConfig()
	if _, err := NewScheduler(&delayedRunner{}, valid); err != nil {
		t.Fatal(err)
	}
	invalid := []ScheduleConfig{
		{},
		{Concurrency: 65, Queue: 1, PerEndpoint: 1, PerTarget: 1, MaxJobs: 1},
		{Concurrency: 1, Queue: 4097, PerEndpoint: 1, PerTarget: 1, MaxJobs: 1},
		{Concurrency: 1, Queue: 1, PerEndpoint: 2, PerTarget: 1, MaxJobs: 1},
		{Concurrency: 1, Queue: 1, PerEndpoint: 1, PerTarget: 1, MaxJobs: 10_001},
	}
	for _, config := range invalid {
		if _, err := NewScheduler(&delayedRunner{}, config); !errors.Is(err, ErrSchedule) {
			t.Fatalf("configuration %+v error = %v", config, err)
		}
	}
	if _, err := NewScheduler(nil, valid); !errors.Is(err, ErrSchedule) {
		t.Fatalf("nil runner error = %v", err)
	}
}

func BenchmarkSchedulerThousandJobs(b *testing.B) {
	runner := &delayedRunner{}
	scheduler, err := NewScheduler(runner, DefaultScheduleConfig())
	if err != nil {
		b.Fatal(err)
	}
	records := make([]endpoint.Record, 32)
	for index := range records {
		records[index] = testRecord(b, index+1, fmt.Sprintf("benchmark-%d", index))
	}
	targets := make([]observation.HTTPTarget, 16)
	for index := range targets {
		targets[index] = testTarget(b, fmt.Sprintf("target-%d", index), "https://example.test/", false)
	}
	jobs := make([]Job, 1000)
	for index := range jobs {
		jobs[index] = Job{Record: records[index%len(records)], Target: targets[index%len(targets)]}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := scheduler.Run(context.Background(), jobs); err != nil {
			b.Fatal(err)
		}
	}
}

type fakeRenderer struct{}

func (fakeRenderer) Profile() artifact.Profile { return artifact.SingBox1141 }
func (fakeRenderer) Render(policy.Gateway) (artifact.Candidate, error) {
	return artifact.NewCandidate(artifact.SingBox1141, []byte("{}"))
}

type acceptingChecker struct{}

func (acceptingChecker) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	return artifact.Evidence{ValidatorID: "native/sing-box/1.14.1"}, nil
}

type fakeListener struct{}

func (fakeListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (fakeListener) Close() error              { return nil }
func (fakeListener) Addr() net.Addr            { return fakeAddress("127.0.0.1:31987") }

type fakeAddress string

func (address fakeAddress) Network() string { return "tcp" }
func (address fakeAddress) String() string  { return string(address) }

type delayedRunner struct {
	delay    time.Duration
	sequence atomic.Int64
}

func (runner *delayedRunner) Execute(ctx context.Context, record endpoint.Record, target observation.HTTPTarget) (observation.Observation, error) {
	if runner.delay > 0 {
		timer := time.NewTimer(runner.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return observation.Observation{}, ctx.Err()
		case <-timer.C:
		}
	}
	connection, err := observation.NewConnectionRef(record.Identity())
	if err != nil {
		return observation.Observation{}, err
	}
	vantage, _ := observation.NewVantageID("scheduler")
	key, err := observation.NewKey(connection, target.Ref(), vantage, observation.KindHTTPGet, artifact.SingBox1141)
	if err != nil {
		return observation.Observation{}, err
	}
	started := time.Date(2026, 9, 19, 0, 0, 0, int(runner.sequence.Add(1)), time.UTC)
	return observation.New(observation.Params{
		Key: key, StartedAt: started, CompletedAt: started.Add(time.Millisecond), Duration: time.Millisecond,
		Outcome: observation.OutcomeSuccess, StatusCode: 204,
	})
}

func testRecord(t testing.TB, index int, credential string) endpoint.Record {
	t.Helper()
	return recordWithAddress(t, fmt.Sprintf("edge-%d.example.test", index), credential)
}

func recordWithAddress(t testing.TB, host, credential string) endpoint.Record {
	t.Helper()
	address, err := endpoint.NewAddress(host, 443)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := endpoint.NewTrojanCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := endpoint.NewTLS("edge.example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := endpoint.NewConfiguration(endpoint.ProtocolTrojan, address, secret, endpoint.NewTCPTransport(), tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := endpoint.NewSourceID("test")
	recordID, _ := endpoint.NewRecordID(fmt.Sprintf("record-%x", []byte(host)))
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

func testTarget(t testing.TB, id, rawURL string, allowPrivate bool) observation.HTTPTarget {
	t.Helper()
	identifier, err := observation.NewTargetID(id)
	if err != nil {
		t.Fatal(err)
	}
	target, err := observation.NewHTTPTarget(identifier, rawURL, 204, 2*time.Second, observation.HTTPOptions{
		AllowHTTP: strings.HasPrefix(rawURL, "http://"), AllowPrivate: allowPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestDefaultScheduleConfigStable(t *testing.T) {
	want := ScheduleConfig{Concurrency: 4, Queue: 256, PerEndpoint: 1, PerTarget: 2, MaxJobs: 10_000}
	if got := DefaultScheduleConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("default config = %+v; want %+v", got, want)
	}
}
