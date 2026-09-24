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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/policy"
)

var ErrExecution = errors.New("probe execution failed")

type ExecutionError struct{ code string }

func executionFailure(code string) error { return &ExecutionError{code: code} }
func (failure *ExecutionError) Error() string {
	return "probe execution failed code=" + failure.code
}
func (failure *ExecutionError) Unwrap() error { return ErrExecution }
func (failure *ExecutionError) Code() string  { return failure.code }

type Config struct {
	Renderer              engine.Renderer
	Checker               artifact.Checker
	Binary                string
	Vantage               observation.VantageID
	Resolver              Resolver
	StartupTimeout        time.Duration
	AllowPrivateEndpoints bool
}

type Executor struct {
	renderer              engine.Renderer
	checker               artifact.Checker
	binary                string
	vantage               observation.VantageID
	resolver              Resolver
	startupTimeout        time.Duration
	allowPrivateEndpoints bool
	reserve               func() (net.Listener, int, error)
}

func NewExecutor(config Config) (*Executor, error) {
	if config.Renderer == nil || config.Checker == nil || !filepath.IsAbs(config.Binary) ||
		config.Vantage.String() == "" || config.StartupTimeout < 100*time.Millisecond || config.StartupTimeout > time.Minute {
		return nil, executionFailure("configuration")
	}
	profile := config.Renderer.Profile()
	if profile != artifact.Mihomo11931 && profile != artifact.SingBox1141 {
		return nil, executionFailure("profile")
	}
	info, err := os.Lstat(config.Binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&0o111 == 0 {
		return nil, executionFailure("binary")
	}
	resolver := config.Resolver
	if resolver == nil {
		resolver = netResolver{resolver: net.DefaultResolver}
	}
	return &Executor{
		renderer: config.Renderer, checker: config.Checker, binary: config.Binary,
		vantage: config.Vantage, resolver: resolver, startupTimeout: config.StartupTimeout,
		allowPrivateEndpoints: config.AllowPrivateEndpoints, reserve: reserveLoopback,
	}, nil
}

func (executor *Executor) Execute(ctx context.Context, record endpoint.Record, target observation.HTTPTarget) (observation.Observation, error) {
	if err := engine.CheckEndpoint(executor.renderer.Profile(), record.Configuration()); err != nil {
		return observation.Observation{}, err
	}
	authorized, err := authorizeTarget(ctx, executor.resolver, target)
	if err != nil {
		return observation.Observation{}, err
	}
	connection, err := observation.NewConnectionRef(record.Identity())
	if err != nil {
		return observation.Observation{}, executionFailure("endpoint")
	}
	key, err := observation.NewKey(connection, target.Ref(), executor.vantage, observation.KindHTTPGet, executor.renderer.Profile())
	if err != nil {
		return observation.Observation{}, executionFailure("evidence_key")
	}
	executionRecord, err := executor.authorizeEndpoint(ctx, record)
	if err != nil {
		return observation.Observation{}, err
	}
	listener, port, err := executor.reserve()
	if err != nil {
		return observation.Observation{}, executionFailure("listener")
	}
	inventory, err := endpoint.Deduplicate([]endpoint.Record{executionRecord})
	if err != nil {
		_ = listener.Close()
		return observation.Observation{}, executionFailure("inventory")
	}
	policyListener, err := policy.NewSOCKSListener("127.0.0.1", port)
	if err != nil {
		_ = listener.Close()
		return observation.Observation{}, executionFailure("listener_policy")
	}
	gateway, err := policy.NewGateway(inventory, policyListener)
	if err != nil {
		_ = listener.Close()
		return observation.Observation{}, executionFailure("gateway_policy")
	}
	candidate, err := executor.renderer.Render(gateway)
	if err != nil {
		_ = listener.Close()
		return observation.Observation{}, executionFailure("render")
	}
	validated, err := artifact.Validate(ctx, candidate, executor.checker)
	if err != nil {
		_ = listener.Close()
		return observation.Observation{}, executionFailure(contextCode(ctx, "validation"))
	}
	wantValidator := "native/" + validated.Profile().Engine.String() + "/" + validated.Profile().Version
	if validated.Evidence().ValidatorID != wantValidator {
		_ = listener.Close()
		return observation.Observation{}, executionFailure("native_validation_required")
	}
	workspace, err := os.MkdirTemp("", "egressfox-probe-")
	if err != nil {
		_ = listener.Close()
		return observation.Observation{}, executionFailure("workspace")
	}
	defer os.RemoveAll(workspace)
	if err := os.Chmod(workspace, 0o700); err != nil {
		_ = listener.Close()
		return observation.Observation{}, executionFailure("workspace")
	}
	extension := ".json"
	if validated.Profile().Engine == artifact.EngineMihomo {
		extension = ".yaml"
	}
	configPath := filepath.Join(workspace, "probe"+extension)
	if err := os.WriteFile(configPath, validated.Reveal(), 0o600); err != nil {
		_ = listener.Close()
		return observation.Observation{}, executionFailure("workspace")
	}
	if err := listener.Close(); err != nil {
		return observation.Observation{}, executionFailure("listener_release")
	}
	engineCtx, stopEngine := context.WithCancel(ctx)
	command := exec.CommandContext(engineCtx, executor.binary, engineArguments(validated.Profile(), configPath, workspace)...)
	command.Dir = workspace
	command.Env = []string{"HOME=" + workspace, "TMPDIR=" + workspace, "LANG=C", "LC_ALL=C"}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		stopEngine()
		return observation.Observation{}, executionFailure("engine_start")
	}
	processDone := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(processDone)
	}()
	defer func() {
		stopEngine()
		select {
		case <-processDone:
		case <-time.After(executor.startupTimeout):
			_ = command.Process.Kill()
			<-processDone
		}
	}()
	proxyAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := waitReady(ctx, proxyAddress, executor.startupTimeout, processDone); err != nil {
		return observation.Observation{}, err
	}
	startedAt := time.Now()
	startedMonotonic := time.Now()
	requestCtx, cancelRequest := context.WithTimeout(ctx, target.Timeout())
	outcome, status, err := measureHTTP(requestCtx, authorized, proxyAddress, processDone)
	cancelRequest()
	duration := time.Since(startedMonotonic)
	if err != nil {
		return observation.Observation{}, err
	}
	return observation.New(observation.Params{
		Key: key, StartedAt: startedAt, CompletedAt: startedAt.Add(duration), Duration: duration,
		Outcome: outcome, StatusCode: status,
	})
}

func (executor *Executor) authorizeEndpoint(ctx context.Context, record endpoint.Record) (endpoint.Record, error) {
	configuration := record.Configuration()
	address, err := authorizeHost(ctx, executor.resolver, configuration.Address().Host(), executor.allowPrivateEndpoints, "endpoint")
	if err != nil {
		return endpoint.Record{}, err
	}
	executionAddress, err := endpoint.NewAddress(address.String(), int(configuration.Address().Port()))
	if err != nil {
		return endpoint.Record{}, executionFailure("endpoint_address")
	}
	executionConfiguration, err := configuration.WithAddress(executionAddress)
	if err != nil {
		return endpoint.Record{}, executionFailure("endpoint_configuration")
	}
	executionRecord, err := endpoint.NewRecord(executionConfiguration, record.Provenance()...)
	if err != nil {
		return endpoint.Record{}, executionFailure("endpoint_record")
	}
	return executionRecord, nil
}

func engineArguments(profile artifact.Profile, configPath, workspace string) []string {
	if profile.Engine == artifact.EngineMihomo {
		return []string{"-f", configPath, "-d", workspace}
	}
	return []string{"run", "-c", configPath, "-D", workspace}
}

func reserveLoopback() (net.Listener, int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	return listener, listener.Addr().(*net.TCPAddr).Port, nil
}

func waitReady(ctx context.Context, address string, timeout time.Duration, processDone <-chan struct{}) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			stabilize := time.NewTimer(25 * time.Millisecond)
			defer stabilize.Stop()
			select {
			case <-ctx.Done():
				return executionFailure(contextCode(ctx, "engine_readiness"))
			case <-processDone:
				return executionFailure("engine_exit")
			case <-deadline.C:
				return executionFailure("engine_readiness_timeout")
			case <-stabilize.C:
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return executionFailure(contextCode(ctx, "engine_readiness"))
		case <-processDone:
			return executionFailure("engine_exit")
		case <-deadline.C:
			return executionFailure("engine_readiness_timeout")
		case <-ticker.C:
		}
	}
}

func measureHTTP(ctx context.Context, authorized authorizedTarget, proxyAddress string, processDone <-chan struct{}) (observation.Outcome, int, error) {
	dialer, err := proxy.SOCKS5("tcp", proxyAddress, nil, proxy.Direct)
	if err != nil {
		return 0, 0, executionFailure("socks_client")
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return 0, 0, executionFailure("socks_context")
	}
	requestURL := authorized.target.ExecutionURL()
	wantedPort := requestURL.Port()
	if wantedPort == "" {
		if requestURL.Scheme == "https" {
			wantedPort = "443"
		} else {
			wantedPort = "80"
		}
	}
	wantedHost := strings.ToLower(requestURL.Hostname())
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false,
		TLSClientConfig: &tls.Config{ServerName: requestURL.Hostname(), MinVersion: tls.VersionTLS12},
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			host, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil || strings.ToLower(strings.TrimSuffix(host, ".")) != wantedHost || port != wantedPort {
				return nil, executionFailure("target_mismatch")
			}
			return contextDialer.DialContext(dialCtx, "tcp", net.JoinHostPort(authorized.address.String(), wantedPort))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") },
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return 0, 0, executionFailure("request")
	}
	response, err := client.Do(request)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
			return observation.OutcomeUnexpectedResponse, response.StatusCode, nil
		}
		select {
		case <-processDone:
			return 0, 0, executionFailure("engine_exit")
		default:
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return 0, 0, executionFailure("cancelled")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || isTimeout(err) {
			return observation.OutcomeTimeout, 0, nil
		}
		if isTLSFailure(err) {
			return observation.OutcomeTLSFailure, 0, nil
		}
		return observation.OutcomeConnectionFailure, 0, nil
	}
	return consumeResponse(ctx, response, authorized.target)
}

func consumeResponse(ctx context.Context, response *http.Response, target observation.HTTPTarget) (observation.Outcome, int, error) {
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, target.MaxResponseBytes()+1)
	read, readErr := io.Copy(io.Discard, limited)
	if readErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return 0, 0, executionFailure("cancelled")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || isTimeout(readErr) {
			return observation.OutcomeTimeout, 0, nil
		}
		return observation.OutcomeConnectionFailure, 0, nil
	}
	if read > target.MaxResponseBytes() {
		return observation.OutcomeResponseTooLarge, 0, nil
	}
	if response.StatusCode != target.ExpectedStatus() {
		return observation.OutcomeUnexpectedResponse, response.StatusCode, nil
	}
	return observation.OutcomeSuccess, response.StatusCode, nil
}

func isTimeout(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func isTLSFailure(err error) bool {
	var certificateError *tls.CertificateVerificationError
	var recordError tls.RecordHeaderError
	var alertError tls.AlertError
	var authorityError x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidError x509.CertificateInvalidError
	return errors.As(err, &certificateError) || errors.As(err, &recordError) || errors.As(err, &alertError) ||
		errors.As(err, &authorityError) || errors.As(err, &hostnameError) || errors.As(err, &invalidError)
}

func contextCode(ctx context.Context, fallback string) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	return fallback
}

func (executor *Executor) String() string {
	return fmt.Sprintf("probe executor profile=%s binary=<redacted> vantage=%s", executor.renderer.Profile(), executor.vantage)
}
