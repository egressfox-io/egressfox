package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/policy"
	"github.com/egressfox-io/egressfox/internal/probe"
	"github.com/egressfox-io/egressfox/internal/reconcile"
	"github.com/egressfox-io/egressfox/internal/selection"
	"github.com/egressfox-io/egressfox/internal/state"
)

const maxProbeBatch = 64

// Named profiles keep a bounded working set of endpoints that answered the
// profile target and re-probe it every window so M5 evidence accumulates on
// the same endpoints, while a small deterministic cursor explores the rest.
const (
	namedWorkingSet = 8
	namedExploreMin = 2
	probeStateTTL   = 24 * time.Hour
)

var ErrPipeline = errors.New("operator pipeline failed")

type PipelineError struct{ code string }

func (e *PipelineError) Error() string  { return "operator pipeline failed code=" + e.code }
func (e *PipelineError) Unwrap() error  { return ErrPipeline }
func (e *PipelineError) Code() string   { return e.code }
func pipelineFailure(code string) error { return &PipelineError{code: code} }

type PipelineConfig struct {
	Client         client.Client
	Reader         client.Reader
	Scheme         *runtime.Scheme
	Store          *state.Store
	MihomoBinary   string
	SingBoxBinary  string
	ManagedImage   string
	ManagedRuntime *ManagedRuntime
	Now            func() time.Time
}

// Pipeline composes existing M1-M5 services. Its only Kubernetes concerns are
// bounded Secret reads and the owned Secret publisher.
type Pipeline struct {
	client        client.Client
	reader        client.Reader
	scheme        *runtime.Scheme
	store         *state.Store
	mihomoBinary  string
	singBoxBinary string
	managed       *ManagedRuntime
	now           func() time.Time
	mu            sync.Mutex
	cursors       map[types.UID]int
	probes        map[probeContextKey]*probeState
	probeSlots    chan struct{}
}

// probeContextKey identifies shared probe/evidence state. It follows the
// observation dimensions: pool, exact engine profile, target ID and target
// revision. For named profiles the target ID already encodes the profile name
// and its incarnation (FirstObservedGeneration), so a removed and recreated
// profile never inherits old budget, cursor or working set. Gateways are
// deliberately not part of the key; selection state stays Gateway-scoped.
type probeContextKey struct {
	pool     types.UID
	name     string
	engine   artifact.Profile
	target   string
	revision [sha256.Size]byte
}

func newProbeContextKey(pool types.UID, name string, profile artifact.Profile, target observation.HTTPTarget) probeContextKey {
	if name == "" {
		name = "default"
	}
	revisionBytes, _ := target.Revision().RevealForPersistence()
	key := probeContextKey{pool: pool, name: name, engine: profile, target: target.ID().String()}
	copy(key.revision[:], revisionBytes)
	return key
}

type probeState struct {
	until   time.Time
	used    int
	cursor  int
	working []string
	seen    time.Time
}

type limitedRunner struct {
	inner probe.Runner
	slots chan struct{}
}

func (r limitedRunner) Execute(ctx context.Context, record endpoint.Record, target observation.HTTPTarget) (observation.Observation, error) {
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
		return r.inner.Execute(ctx, record, target)
	case <-ctx.Done():
		return observation.Observation{}, ctx.Err()
	}
}

type GatewayOutcome struct {
	Eligible            int
	Selected            int
	Published           bool
	Changed             bool
	RetainedLKG         bool
	PublishedGeneration string
}

func NewPipeline(config PipelineConfig) (*Pipeline, error) {
	if config.Client == nil || config.Scheme == nil || config.Store == nil || config.MihomoBinary == "" || config.SingBoxBinary == "" {
		return nil, pipelineFailure("configuration")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	reader := config.Reader
	if reader == nil {
		reader = config.Client
	}
	managed := config.ManagedRuntime
	if managed == nil {
		var err error
		managed, err = NewManagedRuntime(ManagedRuntimeConfig{Client: config.Client, Scheme: config.Scheme, Image: config.ManagedImage})
		if err != nil {
			return nil, pipelineFailure("managed_runtime")
		}
	}
	return &Pipeline{client: config.Client, reader: reader, scheme: config.Scheme, store: config.Store, mihomoBinary: config.MihomoBinary, singBoxBinary: config.SingBoxBinary, managed: managed, now: now, cursors: map[types.UID]int{}, probes: map[probeContextKey]*probeState{}, probeSlots: make(chan struct{}, 4)}, nil
}

func (p *Pipeline) ManagedRuntime() *ManagedRuntime { return p.managed }

func (p *Pipeline) Run(ctx context.Context, gateway *egressv1alpha1.EgressGateway, pool *egressv1alpha1.ProxyPool) (GatewayOutcome, error) {
	if gateway == nil || pool == nil || gateway.Namespace != pool.Namespace || gateway.Spec.PoolRef.Name != pool.Name {
		return GatewayOutcome{}, pipelineFailure("reference")
	}
	profileName := gateway.Spec.ProfileRef
	probeSpec, selectionSpec, err := ResolveProfile(pool, profileName)
	if err != nil {
		return GatewayOutcome{}, err
	}
	poolResult, err := BuildPoolWithCache(ctx, p.reader, pool, p.store, p.now())
	if err != nil {
		var coded interface{ Code() string }
		if errors.As(err, &coded) {
			return GatewayOutcome{}, pipelineFailure(coded.Code())
		}
		return GatewayOutcome{}, pipelineFailure("source")
	}
	if poolResult.Inventory.Len() == 0 {
		return GatewayOutcome{}, pipelineFailure("inventory_empty")
	}
	profile, renderer, binary, ok := p.engine(gateway.Spec.Engine)
	if !ok {
		return GatewayOutcome{}, pipelineFailure("engine")
	}
	checker, err := artifact.NewNativeChecker(profile, binary, 15*time.Second)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("checker")
	}
	target, targetName, targetRevision, err := p.target(ctx, pool, profileName, probeSpec)
	if err != nil {
		return GatewayOutcome{}, err
	}
	vantage, _ := observation.NewVantageID("k8s/operator")
	executor, err := probe.NewExecutor(probe.Config{Renderer: renderer, Checker: checker, Binary: binary, Vantage: vantage, StartupTimeout: 10 * time.Second, AllowPrivateEndpoints: probeSpec.AllowPrivateEndpoints})
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("probe_executor")
	}
	scheduler, err := probe.NewScheduler(limitedRunner{inner: executor, slots: p.probeSlots}, probe.DefaultScheduleConfig())
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("probe_scheduler")
	}
	probeKey := newProbeContextKey(pool.UID, profileName, profile, target)
	jobs := p.budgetedJobs(probeKey, gateway.UID, poolResult.Inventory.Records(), target, profile, refreshDuration(pool.Spec.RefreshInterval), p.now())
	results, _, err := scheduler.Run(ctx, jobs)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("probe_schedule")
	}
	p.recordProbeResults(probeKey, jobs, results)
	now := p.now().UTC()
	for _, result := range results {
		if result.Err == nil {
			if err := p.store.Append(ctx, result.Observation, now); err != nil {
				return GatewayOutcome{}, pipelineFailure("evidence_store")
			}
		}
	}
	selectionContext, err := selection.NewContext(target.Ref(), vantage, observation.KindHTTPGet, profile)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("selection_context")
	}
	selectionPolicy := selection.DefaultPolicy(selectionStrategy(selectionSpec.Strategy))
	if selectionSpec.TopN > 0 {
		selectionPolicy.TopN = int(selectionSpec.TopN)
	}
	inputRevisions := poolResult.SourceRevisions
	inputRevisions[targetName] = targetRevision
	var listener policy.Listener
	if IsManaged(gateway) {
		var authName types.NamespacedName
		var authRevision ResourceRevision
		listener, authName, authRevision, err = p.managed.Prepare(ctx, gateway)
		if err != nil {
			return GatewayOutcome{}, pipelineFailure(runtimeErrorCode(err))
		}
		inputRevisions[authName] = authRevision
	} else {
		listenerAddress := ""
		listenerPort := 0
		if gateway.Spec.Listener != nil {
			listenerAddress = gateway.Spec.Listener.Address
			listenerPort = int(gateway.Spec.Listener.Port)
		}
		if listenerAddress == "" {
			listenerAddress = "127.0.0.1"
		}
		if listenerPort == 0 {
			listenerPort = 1080
		}
		listener, err = policy.NewSOCKSListener(listenerAddress, listenerPort)
		if err != nil {
			return GatewayOutcome{}, pipelineFailure("listener")
		}
	}
	guard, err := NewSnapshotGuard(p.reader, gateway, pool, inputRevisions)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("snapshot_guard")
	}
	guard.BindCache(p.store, poolResult.CacheVersions)
	guard.now = p.now
	var publisher reconcile.Publisher
	var generationPublisher *GenerationPublisher
	if IsManaged(gateway) {
		generationPublisher, err = p.managed.Publisher(gateway, guard)
		publisher = generationPublisher
	} else {
		publisher, err = NewSecretPublisher(p.client, p.scheme, gateway, gateway.Spec.OutputSecretName, guard)
	}
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("publisher")
	}
	useCase, err := reconcile.New(p.store, p.store, publisher)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("reconciler")
	}
	result, err := useCase.Reconcile(ctx, reconcile.Request{
		Scope: "k8s_" + string(gateway.UID), Inventory: poolResult.Inventory, Context: selectionContext,
		Selection: selectionPolicy, Listener: listener, Renderer: renderer, Checker: checker, EvaluatedAt: now,
		BeforeApply: guard.BindSelected,
	})
	if err != nil {
		var staged interface{ Stage() string }
		if errors.As(err, &staged) {
			if staged.Stage() == "publication" {
				var coded interface{ Code() string }
				if errors.As(err, &coded) {
					switch coded.Code() {
					case "snapshot_obsolete":
						return GatewayOutcome{}, pipelineFailure("snapshot_obsolete")
					case "ownership":
						return GatewayOutcome{}, pipelineFailure("publication_conflict")
					case "payload_too_large":
						return GatewayOutcome{}, pipelineFailure("publication_too_large")
					}
				}
			}
			return GatewayOutcome{}, pipelineFailure(reconcileStageCode(staged.Stage()))
		}
		return GatewayOutcome{}, pipelineFailure("reconcile")
	}
	eligible := 0
	for _, explanation := range result.Decision.Explanations {
		if explanation.Reason == selection.ReasonEligible {
			eligible++
		}
	}
	outcome := GatewayOutcome{Eligible: eligible, Selected: len(result.Decision.Selected), Published: result.Published, Changed: result.Changed, RetainedLKG: result.RetainedLKG}
	if generationPublisher != nil {
		outcome.PublishedGeneration = generationPublisher.GenerationName()
	}
	return outcome, nil
}

func runtimeErrorCode(err error) string {
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return "managed_runtime"
}

func reconcileStageCode(stage string) string {
	switch stage {
	case "validation":
		return "native_validation"
	case "render", "policy", "inventory", "profile", "receipt":
		return "render"
	case "publication", "publication_readback", "publisher_read":
		return "publication"
	case "state_recover", "state_stage", "state_commit", "evidence_load":
		return "persistence"
	case "selection", "candidate", "evidence_key":
		return "selection"
	default:
		return "reconcile"
	}
}

func (p *Pipeline) engine(value egressv1alpha1.Engine) (artifact.Profile, engine.Renderer, string, bool) {
	switch value {
	case egressv1alpha1.EngineMihomo:
		return artifact.Mihomo11931, mihomo.Renderer{}, p.mihomoBinary, true
	case egressv1alpha1.EngineSingBox:
		return artifact.SingBox1141, singbox.Renderer{}, p.singBoxBinary, true
	default:
		return artifact.Profile{}, nil, "", false
	}
}

func (p *Pipeline) target(ctx context.Context, pool *egressv1alpha1.ProxyPool, profileName string, spec egressv1alpha1.ProbeSpec) (observation.HTTPTarget, types.NamespacedName, ResourceRevision, error) {
	secret := &corev1.Secret{}
	ref := spec.TargetSecretRef
	name := types.NamespacedName{Namespace: pool.Namespace, Name: ref.Name}
	if err := p.reader.Get(ctx, name, secret); err != nil {
		return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("target_unavailable")
	}
	revision := ResourceRevision{UID: secret.UID, ResourceVersion: secret.ResourceVersion}
	raw, ok := secret.Data[ref.Key]
	if !ok {
		return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("target_key_missing")
	}
	idSuffix := "probe"
	if profileName != "" && profileName != "default" {
		if pool.Status.ObservedGeneration != pool.Generation {
			return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("profile_pending")
		}
		epoch := int64(0)
		for _, status := range pool.Status.Profiles {
			if status.Name == profileName {
				epoch = status.FirstObservedGeneration
				break
			}
		}
		if epoch <= 0 {
			return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("profile_pending")
		}
		sum := sha256.Sum256([]byte(profileName + "/" + strconv.FormatInt(epoch, 10)))
		idSuffix = "profile-" + hex.EncodeToString(sum[:12])
	}
	id, err := observation.NewTargetID("k8s-" + string(pool.UID) + "-" + idSuffix)
	if err != nil {
		return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("target_id")
	}
	timeout := time.Duration(0)
	if spec.Timeout != nil {
		timeout = spec.Timeout.Duration
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	expected := int(spec.ExpectedStatus)
	if expected == 0 {
		expected = 204
	}
	options := observation.HTTPOptions{AllowHTTP: spec.AllowHTTP, AllowPrivate: spec.AllowPrivateTargets}
	if profileName != "" && profileName != "default" {
		options.RevisionSalt = fmt.Sprintf("m9-probe-permissions/http=%t/private-target=%t/private-endpoint=%t", spec.AllowHTTP, spec.AllowPrivateTargets, spec.AllowPrivateEndpoints)
	}
	target, err := observation.NewHTTPTarget(id, string(raw), expected, timeout, options)
	if err != nil {
		return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("target_invalid")
	}
	return target, name, revision, nil
}

func (p *Pipeline) nextJobs(uid types.UID, records []endpoint.Record, target observation.HTTPTarget, profile artifact.Profile) []probe.Job {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nextJobsLocked(uid, records, target, profile, maxProbeBatch)
}

// budgetedJobs returns the probe jobs for one probe context. Budget, cursor
// and working set belong to the context, so equivalent Gateways share them and
// a changed target or recreated profile starts fresh. The legacy default
// profile keeps its 64-job round robin with the per-Gateway cursor.
func (p *Pipeline) budgetedJobs(key probeContextKey, gatewayUID types.UID, records []endpoint.Record, target observation.HTTPTarget, profile artifact.Profile, interval time.Duration, now time.Time) []probe.Job {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.probes == nil {
		p.probes = map[probeContextKey]*probeState{}
	}
	if p.cursors == nil {
		p.cursors = map[types.UID]int{}
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	evidence := selection.DefaultPolicy(selection.StrategyAdaptive)
	isDefault := key.name == "default"
	window := interval
	if !isDefault {
		// A long source refresh must not stretch the probe budget window.
		window = min(interval, evidence.Freshness)
	}
	state := p.probes[key]
	if state == nil {
		state = &probeState{}
		p.probes[key] = state
	}
	if !now.Before(state.until) {
		state.until = now.Add(window)
		state.used = 0
	}
	state.seen = now
	defer p.expireProbeStatesLocked(now)
	if isDefault {
		available := maxProbeBatch - state.used
		if available <= 0 {
			return nil
		}
		jobs := p.nextJobsLocked(gatewayUID, records, target, profile, available)
		state.used += len(jobs)
		return jobs
	}
	if state.used > 0 {
		return nil
	}
	compatible := make(map[string]endpoint.Record, len(records))
	for _, record := range records {
		if engine.CheckEndpoint(profile, record.Configuration()) == nil {
			compatible[record.ID().String()] = record
		}
	}
	working := make([]endpoint.Record, 0, len(state.working))
	state.working = slices.DeleteFunc(state.working, func(id string) bool {
		record, ok := compatible[id]
		if ok {
			working = append(working, record)
		}
		return !ok
	})
	skip := make(map[string]bool, len(working))
	for _, id := range state.working {
		skip[id] = true
	}
	explore, next := nextProbeBatch(records, target, profile, state.cursor, max(namedExploreMin, namedWorkingSet-len(working)), skip)
	state.cursor = next
	// One pass per window is enough when successive windows fit MinSamples into
	// the evidence window; otherwise burst so a long refresh interval still
	// qualifies endpoints. Newly explored endpoints always burst.
	workingPasses := 1
	if time.Duration(evidence.MinSamples-1)*interval >= evidence.EvidenceWindow {
		workingPasses = evidence.MinSamples
	}
	var jobs []probe.Job
	for pass := range evidence.MinSamples {
		if pass < workingPasses {
			for _, record := range working {
				jobs = append(jobs, probe.Job{Record: record, Target: target})
			}
		}
		jobs = append(jobs, explore...)
	}
	state.used = len(jobs)
	return jobs
}

// recordProbeResults promotes endpoints that answered to the context's bounded
// working set and drops those that failed. Infrastructure errors carry no
// endpoint verdict and are ignored.
func (p *Pipeline) recordProbeResults(key probeContextKey, jobs []probe.Job, results []probe.Result) {
	if key.name == "default" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.probes[key]
	if state == nil {
		return
	}
	for index, result := range results {
		if index >= len(jobs) || result.Err != nil {
			continue
		}
		id := jobs[index].Record.ID().String()
		position := slices.Index(state.working, id)
		switch {
		case !result.Observation.Successful():
			if position >= 0 {
				state.working = slices.Delete(state.working, position, position+1)
			}
		case position < 0 && len(state.working) < namedWorkingSet:
			state.working = append(state.working, id)
		}
	}
}

func (p *Pipeline) expireProbeStatesLocked(now time.Time) {
	for key, state := range p.probes {
		if now.Sub(state.seen) > probeStateTTL {
			delete(p.probes, key)
		}
	}
}

func (p *Pipeline) nextJobsLocked(uid types.UID, records []endpoint.Record, target observation.HTTPTarget, profile artifact.Profile, limit int) []probe.Job {
	jobs, next := nextProbeBatch(records, target, profile, p.cursors[uid], limit, nil)
	p.cursors[uid] = next
	return jobs
}

func nextProbeBatch(records []endpoint.Record, target observation.HTTPTarget, profile artifact.Profile, start, limit int, skip map[string]bool) ([]probe.Job, int) {
	if start >= len(records) {
		start = 0
	}
	jobs := make([]probe.Job, 0, min(len(records), limit))
	scanned := 0
	for scanned < len(records) && len(jobs) < limit {
		record := records[(start+scanned)%len(records)]
		if !skip[record.ID().String()] && engine.CheckEndpoint(profile, record.Configuration()) == nil {
			jobs = append(jobs, probe.Job{Record: record, Target: target})
		}
		scanned++
	}
	if len(records) > 0 {
		return jobs, (start + scanned) % len(records)
	}
	return jobs, 0
}

func selectionStrategy(value egressv1alpha1.SelectionStrategy) selection.Strategy {
	switch value {
	case egressv1alpha1.SelectionStatic:
		return selection.StrategyStatic
	case egressv1alpha1.SelectionLowestLatency:
		return selection.StrategyLowestLatency
	default:
		return selection.StrategyAdaptive
	}
}
