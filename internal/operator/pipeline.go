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

// Probe scheduling is one mechanism for the default and every named profile.
// Each probe context runs at most one round per evidence cadence. A round
// re-probes the context's maintained cohort once and explores a few further
// compatible endpoints from a shared cursor, probing each MinSamples times so
// a challenger can reach M5 evidence in one round. The per-round job budget is
// the documented bound: 64 for the default profile, 30 for a named profile.
const (
	maxProbeBatch   = 64
	namedProbeBatch = 30
	exploreMin      = 2
	probeStateTTL   = 24 * time.Hour
	minProbeCadence = 30 * time.Second
	// RequeueJitterPermille is the controller's maximum stable positive requeue
	// spread. The evidence cadence assumes every requeue may be this late.
	RequeueJitterPermille = 100
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
	probes        map[probeContextKey]*probeState
	probeSlots    chan struct{}
}

// probeContextKey identifies shared probe/evidence state. It follows the
// observation dimensions: pool, exact engine profile, target ID and target
// revision. For named profiles the target ID already encodes the profile name
// and its incarnation (FirstObservedGeneration), so a removed and recreated
// profile never inherits old round, cursor or cohort. Gateways are deliberately
// not part of the key; M5 selection state stays Gateway-scoped.
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

// probeState is the shared scheduling state of one probe context. It holds
// which endpoints to keep observing, never health verdicts: eligibility,
// failure streaks, cooldown and anti-flap remain M5 decisions.
type probeState struct {
	until  time.Time
	round  uint64
	cursor int
	// cohort lists maintained endpoint IDs in admission order.
	cohort []string
	// rejected holds cohort members whose latest M5 explanation was a failure
	// streak or unreliability; only they may yield a slot to a newcomer.
	rejected map[string]bool
	// demand records each Gateway's scheduling demand in this context. Demand
	// for an endpoint M5 rejected is not protected, so the union of protected
	// demand stays within the cohort capacity and is always maintained.
	demand map[types.UID]probeDemand
	// overloaded reports that the latest maintenance phase outlasted Freshness.
	overloaded bool
	attempts   uint64
	seen       time.Time
}

// probeDemand is one Gateway's demand. selected is the selection it last
// published here, or while pending, a reservation for the decision it is
// about to publish. uncertain is a selection whose external write may have
// taken effect (intended is its receipt); it stays protected next to selected
// until the Gateway's next reconciliation reads the actual receipt.
type probeDemand struct {
	selected  []string
	seen      time.Time
	pending   bool
	uncertain []string
	intended  artifact.Receipt
	attempt   uint64
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
	Eligible int
	Selected int
	// RequestedTopN is the profile's topN; EffectiveTopN is what M5 was asked
	// for after the context's maintainable cohort bound.
	RequestedTopN int
	EffectiveTopN int
	// ProbeOverloaded reports that the context's latest maintenance phase
	// outlasted Freshness, so early maintenance evidence was stale at evaluation.
	ProbeOverloaded bool
	// SharedCapacityLimited reports that the preferred selection did not fit
	// the cohort shared with other Gateways and M5 chose among maintained
	// endpoints instead.
	SharedCapacityLimited bool
	Published             bool
	Changed               bool
	RetainedLKG           bool
	PublishedGeneration   string
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
	return &Pipeline{client: config.Client, reader: reader, scheme: config.Scheme, store: config.Store, mihomoBinary: config.MihomoBinary, singBoxBinary: config.SingBoxBinary, managed: managed, now: now, probes: map[probeContextKey]*probeState{}, probeSlots: make(chan struct{}, 4)}, nil
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
	probeKey := newProbeContextKey(pool.UID, profileName, profile, target)
	// Settle an earlier uncertain write from the actual current output before
	// the round, so the round maintains the output that is really current.
	p.resolveUncertain(probeKey, gateway.UID, p.currentOutput(ctx, gateway))
	if round, due := p.budgetedJobs(probeKey, poolResult.Inventory.Records(), target, profile, p.now()); due {
		executed, err := p.runRound(ctx, limitedRunner{inner: executor, slots: p.probeSlots}, probe.DefaultScheduleConfig(), round)
		if err != nil {
			p.abandonRound(round)
			return GatewayOutcome{}, pipelineFailure("probe_schedule")
		}
		p.recordProbeResults(probeKey, executed.jobs, executed.results)
		received := p.now().UTC()
		for _, result := range executed.results {
			if result.Err == nil {
				if err := p.store.Append(ctx, result.Observation, received); err != nil {
					p.abandonRound(round)
					return GatewayOutcome{}, pipelineFailure("evidence_store")
				}
			}
		}
		if ctx.Err() != nil {
			p.abandonRound(round)
			return GatewayOutcome{}, pipelineFailure("probe_schedule")
		}
		p.completeRound(round, received.Sub(executed.maintenanceStart) > evidencePolicy().Freshness)
	}
	p.touchDemand(probeKey, gateway.UID, p.now())
	now := p.now().UTC()
	selectionContext, err := selection.NewContext(target.Ref(), vantage, observation.KindHTTPGet, profile)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("selection_context")
	}
	selectionPolicy := selection.DefaultPolicy(selectionStrategy(selectionSpec.Strategy))
	requestedTopN := selectionPolicy.TopN
	if selectionSpec.TopN > 0 {
		requestedTopN = int(selectionSpec.TopN)
	}
	selectionPolicy.TopN = maintainableTopN(probeKey, requestedTopN)
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
	// A non-empty decision reserves its maintenance in the shared cohort before
	// anything is rendered or published. The reservation becomes the Gateway's
	// demand once that selection is published and durably committed; after an
	// external write without that confirmation it is held as uncertain demand;
	// only a failure before any external write releases it.
	var reservation *selectionReservation
	defer func() {
		if reservation != nil {
			p.releaseSelection(reservation)
		}
	}()
	result, err := useCase.Reconcile(ctx, reconcile.Request{
		Scope: "k8s_" + string(gateway.UID), Inventory: poolResult.Inventory, Context: selectionContext,
		Selection: selectionPolicy, Listener: listener, Renderer: renderer, Checker: checker, EvaluatedAt: now,
		Reserve: func(decision selection.Decision) error {
			var err error
			reservation, err = p.reserveSelection(probeKey, gateway.UID, decision, now)
			return err
		},
		Maintained:  p.maintained(probeKey),
		BeforeApply: guard.BindSelected,
	})
	switch {
	case reservation == nil:
		if err == nil {
			p.observeVerdicts(probeKey, result.Decision)
		}
	case err == nil && result.Published:
		p.commitSelection(reservation)
		reservation = nil
	case result.Publication != reconcile.PublicationNone:
		// The output may now carry the new selection while the receipt or the
		// durable checkpoint is unconfirmed; a managed Gateway also activates
		// only the generation its status records. Keep both selections
		// protected until the next reconciliation reads the actual receipt.
		p.holdUncertain(reservation, result.Intended, now)
		reservation = nil
		if err != nil {
			// The status must not claim the previous output was retained.
			return GatewayOutcome{}, pipelineFailure("publication_unconfirmed")
		}
	}
	if errors.Is(err, errPublicationUnresolved) {
		return GatewayOutcome{}, pipelineFailure("publication_unconfirmed")
	}
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
	outcome := GatewayOutcome{Eligible: eligible, Selected: len(result.Decision.Selected), Published: result.Published, Changed: result.Changed, RetainedLKG: result.RetainedLKG,
		RequestedTopN: requestedTopN, EffectiveTopN: selectionPolicy.TopN, ProbeOverloaded: p.roundOverloaded(probeKey), SharedCapacityLimited: result.Constrained}
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
	case "scheduling":
		return "probe_capacity"
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

// evidencePolicy is the M5 evidence policy every profile currently uses; the
// API exposes only strategy and TopN.
func evidencePolicy() selection.Policy {
	return selection.DefaultPolicy(selection.StrategyAdaptive)
}

// probeCadence derives the evidence-maintenance round interval from the M5
// evidence policy, independently of the source refresh interval. Rounds run
// when a Gateway reconciles, and the controller requeues after the cadence
// plus at most RequeueJitterPermille of it. The cadence therefore keeps:
//   - every evaluation within Freshness of the round it reads, because a
//     round's window never exceeds Freshness; and
//   - MinSamples maintenance rounds inside EvidenceWindow even when every
//     requeue is maximally late, with one extra round of headroom for probe
//     and queue latency: MinSamples * cadence * (1 + jitter) <= EvidenceWindow.
func probeCadence(policy selection.Policy) time.Duration {
	cadence := policy.Freshness
	if policy.MinSamples > 1 {
		spread := time.Duration(policy.MinSamples) * (1000 + RequeueJitterPermille)
		cadence = min(cadence, policy.EvidenceWindow*1000/spread)
	}
	return max(cadence, minProbeCadence)
}

// explorationBudget bounds how long after a round starts a new exploration
// probe may begin. Probes already running finish within their own timeout;
// later exploration jobs are deferred, not observed.
func explorationBudget(policy selection.Policy) time.Duration {
	return policy.Freshness / 2
}

// EvidenceCadence is the Gateway requeue base needed to maintain M5 evidence.
// The controller requeues at the shorter of it and the source refresh interval.
func EvidenceCadence() time.Duration { return probeCadence(evidencePolicy()) }

func roundBudget(key probeContextKey) int {
	if key.name == "default" {
		return maxProbeBatch
	}
	return namedProbeBatch
}

// cohortCapacity is the number of endpoints a context can maintain every
// round while still reserving exploration for exploreMin newcomers.
func cohortCapacity(key probeContextKey, policy selection.Policy) int {
	return roundBudget(key) - exploreMin*policy.MinSamples
}

// maintainableTopN bounds a requested TopN by what the context can keep fresh.
// M5 receives the bounded value; the operator reports the requested one.
func maintainableTopN(key probeContextKey, requested int) int {
	return max(1, min(requested, cohortCapacity(key, evidencePolicy())))
}

var (
	// errProbeCapacity refuses a selection whose maintenance does not fit the
	// context's cohort next to the other Gateways' demand.
	errProbeCapacity = errors.New("probe cohort capacity exceeded")
	// errPublicationUnresolved refuses a new reservation while the Gateway's
	// previous write is still unresolved, so attempts never stack.
	errPublicationUnresolved = errors.New("previous publication unresolved")
	// errExplorationDeferred marks an exploration job not started because the
	// round's exploration budget had passed. It is not an endpoint verdict.
	errExplorationDeferred = errors.New("probe exploration deferred")
)

// probeRound is one context's work for a round. id identifies the claim, and
// cursor is the exploration position to restore if the round is abandoned.
type probeRound struct {
	key      probeContextKey
	id       uint64
	explore  []probe.Job
	maintain []probe.Job
	cursor   int
}

type roundResult struct {
	jobs             []probe.Job
	results          []probe.Result
	maintenanceStart time.Time
}

// budgetedJobs claims the next round for one probe context. Round window,
// cursor and cohort belong to the context, so equivalent Gateways share one
// round and one exploration position, and a changed target, engine profile or
// recreated profile starts fresh. A Gateway reconciling inside the current
// window gets no round and evaluates the evidence the last round produced.
// Maintenance lists demanded (selected) members last so they are observed
// closest to evaluation.
func (p *Pipeline) budgetedJobs(key probeContextKey, records []endpoint.Record, target observation.HTTPTarget, profile artifact.Profile, now time.Time) (probeRound, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.probes == nil {
		p.probes = map[probeContextKey]*probeState{}
	}
	policy := evidencePolicy()
	state := p.probes[key]
	if state == nil {
		state = &probeState{}
		p.probes[key] = state
	}
	state.seen = now
	defer p.expireProbeStatesLocked(now)
	if now.Before(state.until) {
		return probeRound{}, false
	}
	state.until = now.Add(probeCadence(policy))
	state.round++
	round := probeRound{key: key, id: state.round, cursor: state.cursor}
	// Members leave the cohort only when they disappear from the inventory or
	// stop being exact-engine compatible; health is M5's decision.
	compatible := make(map[string]endpoint.Record, len(records))
	for _, record := range records {
		if engine.CheckEndpoint(profile, record.Configuration()) == nil {
			compatible[record.ID().String()] = record
		}
	}
	var demanded []probe.Job
	skip := make(map[string]bool, len(state.cohort))
	state.cohort = slices.DeleteFunc(state.cohort, func(id string) bool {
		record, ok := compatible[id]
		if ok {
			job := probe.Job{Record: record, Target: target}
			if state.protected(id) {
				demanded = append(demanded, job)
			} else {
				round.maintain = append(round.maintain, job)
			}
			skip[id] = true
		}
		return !ok
	})
	round.maintain = append(round.maintain, demanded...)
	explore, next := nextProbeBatch(records, target, profile, state.cursor, max(exploreMin, (roundBudget(key)-len(round.maintain))/policy.MinSamples), skip)
	state.cursor = next
	for range policy.MinSamples {
		round.explore = append(round.explore, explore...)
	}
	return round, true
}

// runRound executes exploration first and maintenance last, so maintenance
// evidence is the newest evidence at evaluation however slow exploration is.
// New exploration probes start only within explorationBudget of the round
// start; deferred jobs return errExplorationDeferred and record nothing.
// Maintenance is not cut short: a phase longer than Freshness is reported as
// overload by the caller rather than hidden by skipping observations.
func (p *Pipeline) runRound(ctx context.Context, runner probe.Runner, config probe.ScheduleConfig, round probeRound) (roundResult, error) {
	start := p.now()
	explorer, err := probe.NewScheduler(deadlineRunner{inner: runner, now: p.now, notAfter: start.Add(explorationBudget(evidencePolicy()))}, config)
	if err != nil {
		return roundResult{}, err
	}
	maintainer, err := probe.NewScheduler(runner, config)
	if err != nil {
		return roundResult{}, err
	}
	explored, _, err := explorer.Run(ctx, round.explore)
	if err != nil {
		return roundResult{}, err
	}
	result := roundResult{maintenanceStart: p.now()}
	maintained, _, err := maintainer.Run(ctx, round.maintain)
	if err != nil {
		return roundResult{}, err
	}
	result.jobs = append(append(result.jobs, round.explore...), round.maintain...)
	result.results = append(append(result.results, explored...), maintained...)
	return result, nil
}

type deadlineRunner struct {
	inner    probe.Runner
	now      func() time.Time
	notAfter time.Time
}

func (r deadlineRunner) Execute(ctx context.Context, record endpoint.Record, target observation.HTTPTarget) (observation.Observation, error) {
	if r.now().After(r.notAfter) {
		return observation.Observation{}, errExplorationDeferred
	}
	return r.inner.Execute(ctx, record, target)
}

// abandonRound releases a round that failed before its evidence was stored,
// so the next reconciliation retries it instead of waiting a whole cadence.
func (p *Pipeline) abandonRound(round probeRound) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if state := p.probes[round.key]; state != nil && state.round == round.id {
		state.until, state.cursor = time.Time{}, round.cursor
	}
}

func (p *Pipeline) completeRound(round probeRound, overloaded bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if state := p.probes[round.key]; state != nil && state.round == round.id {
		state.overloaded = overloaded
	}
}

func (p *Pipeline) roundOverloaded(key probeContextKey) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.probes[key]
	return state != nil && state.overloaded
}

// recordProbeResults admits endpoints that answered the target to the cohort.
// A failed observation changes nothing here: it is evidence for M5, and a
// maintained member keeps being probed until M5 rejects it and a newcomer
// needs its slot. Infrastructure errors and deferred jobs carry no verdict.
func (p *Pipeline) recordProbeResults(key probeContextKey, jobs []probe.Job, results []probe.Result) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.probes[key]
	if state == nil {
		return
	}
	capacity := cohortCapacity(key, evidencePolicy())
	for index, result := range results {
		if index < len(jobs) && result.Err == nil && result.Observation.Successful() {
			state.admit(jobs[index].Record.ID().String(), capacity, false)
		}
	}
}

// touchDemand marks the Gateway as evaluating in key: its committed demand
// (the selection it last published here) stays alive, its demand in any other
// context is released, and demand of Gateways that stopped reconciling for an
// evidence window expires.
func (p *Pipeline) touchDemand(key probeContextKey, gateway types.UID, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for other, state := range p.probes {
		if other != key {
			delete(state.demand, gateway)
		}
	}
	state := p.probes[key]
	if state == nil {
		return
	}
	if demand, ok := state.demand[gateway]; ok && !demand.pending {
		demand.seen = now
		state.demand[gateway] = demand
	}
	window := evidencePolicy().EvidenceWindow
	for id, demand := range state.demand {
		if !demand.pending && now.Sub(demand.seen) > window {
			delete(state.demand, id)
		}
	}
}

// observeVerdicts records which cohort members M5 rejected for failure streak
// or unreliability; only they may yield a slot to an answering newcomer.
func (p *Pipeline) observeVerdicts(key probeContextKey, decision selection.Decision) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if state := p.probes[key]; state != nil {
		state.observeVerdicts(decision)
	}
}

func (s *probeState) observeVerdicts(decision selection.Decision) {
	members := make(map[string]bool, len(s.cohort))
	for _, id := range s.cohort {
		members[id] = true
	}
	s.rejected = map[string]bool{}
	for _, explanation := range decision.Explanations {
		id := explanation.EndpointID.String()
		if members[id] && (explanation.Reason == selection.ReasonFailureStreak || explanation.Reason == selection.ReasonUnreliable) {
			s.rejected[id] = true
		}
	}
}

// selectionReservation is a pending demand for one Gateway's decision. It
// holds the Gateway's previous demand so a failure before any external write
// leaves the context exactly as it was.
type selectionReservation struct {
	key         probeContextKey
	gateway     types.UID
	previous    probeDemand
	hadPrevious bool
}

// reserveSelection accepts a decision only when the union of every protected
// demand in the context, with this decision replacing the Gateway's own
// selection, fits the cohort. The check runs before rendering or publication.
// Demand counts only for maintained endpoints M5 has not rejected: a Gateway
// keeps publishing its last-known-good, but an endpoint M5 rejected for its
// failure streak or unreliability no longer reserves capacity. Missing
// evidence, withheld evidence, deferral and cooldown are not rejections. A
// refusal makes the reconciler plan again among maintained endpoints; if that
// is refused as well, the last-known-good output and its demand remain.
func (p *Pipeline) reserveSelection(key probeContextKey, gateway types.UID, decision selection.Decision, now time.Time) (*selectionReservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.probes[key]
	if state == nil {
		return nil, errProbeCapacity
	}
	state.observeVerdicts(decision)
	selected := make([]string, 0, len(decision.Selected))
	for _, record := range decision.Selected {
		selected = append(selected, record.ID().String())
	}
	members := make(map[string]bool, len(state.cohort))
	for _, id := range state.cohort {
		members[id] = true
	}
	if own, ok := state.demand[gateway]; ok && own.uncertain != nil {
		return nil, errPublicationUnresolved
	}
	// The Gateway's own current output counts too: if this write's outcome
	// becomes uncertain, both its current and its attempted selection must be
	// maintained, so a transition is admitted only when both fit.
	needed := make(map[string]bool, len(selected))
	for other, demand := range state.demand {
		for _, id := range demand.selected {
			if (other != gateway && demand.pending) || (members[id] && !state.rejected[id]) {
				needed[id] = true
			}
		}
		for _, id := range demand.uncertain {
			if members[id] && !state.rejected[id] {
				needed[id] = true
			}
		}
	}
	for _, id := range selected {
		needed[id] = true
	}
	if len(needed) > cohortCapacity(key, evidencePolicy()) {
		return nil, errProbeCapacity
	}
	reservation := &selectionReservation{key: key, gateway: gateway}
	reservation.previous, reservation.hadPrevious = state.demand[gateway]
	if state.demand == nil {
		state.demand = map[types.UID]probeDemand{}
	}
	pending := reservation.previous
	pending.selected, pending.seen, pending.pending = selected, now, true
	state.demand[gateway] = pending
	return reservation, nil
}

// currentOutput reads the receipt of the output that is actually current. For
// BYO that is the owned output Secret. For a managed Gateway it is the
// generation its status records as published, which is what the runtime
// activates; a newer generation Secret written by a failed operation is not.
func (p *Pipeline) currentOutput(ctx context.Context, gateway *egressv1alpha1.EgressGateway) func(artifact.Receipt) (bool, error) {
	return func(intended artifact.Receipt) (bool, error) {
		var current artifact.Receipt
		var exists bool
		var err error
		if IsManaged(gateway) {
			current, exists, err = p.managed.CurrentGenerationReceipt(ctx, gateway)
		} else {
			reader, readerErr := NewSecretPublisher(p.client, p.scheme, gateway, gateway.Spec.OutputSecretName)
			if readerErr != nil {
				return false, readerErr
			}
			current, exists, err = reader.CurrentReceipt(ctx)
		}
		if err != nil {
			return false, err
		}
		return exists && current.Equal(intended), nil
	}
}

// maintained snapshots the context's cohort for constrained planning: when a
// preferred decision does not fit, M5 chooses again among these endpoints.
// Every protected demand is a cohort member, so such a decision always fits.
func (p *Pipeline) maintained(key probeContextKey) func(endpoint.Record) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	members := map[string]bool{}
	if state := p.probes[key]; state != nil {
		for _, id := range state.cohort {
			members[id] = true
		}
	}
	return func(record endpoint.Record) bool { return members[record.ID().String()] }
}

// commitSelection turns a published reservation into the Gateway's demand and
// admits its endpoints. Reservation keeps protected demand within capacity, so
// an unprotected member can always yield its slot. A confirmed publication
// also settles any earlier uncertain write.
func (p *Pipeline) commitSelection(reservation *selectionReservation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.probes[reservation.key]
	if state == nil {
		return
	}
	demand, ok := state.demand[reservation.gateway]
	if !ok || !demand.pending {
		return
	}
	demand.pending, demand.uncertain, demand.intended = false, nil, artifact.Receipt{}
	state.demand[reservation.gateway] = demand
	capacity := cohortCapacity(reservation.key, evidencePolicy())
	for _, id := range demand.selected {
		state.admit(id, capacity, true)
	}
}

// holdUncertain handles a reservation whose external write happened or may
// have happened without a confirmed, durably committed result. The current
// selection stays the Gateway's demand, the attempted selection is kept as
// uncertain demand and admitted, and both are maintained until
// resolveUncertain reads the actual current output. Nothing is rolled back
// externally.
func (p *Pipeline) holdUncertain(reservation *selectionReservation, intended artifact.Receipt, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.probes[reservation.key]
	if state == nil {
		return
	}
	demand, ok := state.demand[reservation.gateway]
	if !ok || !demand.pending {
		return
	}
	state.attempts++
	held := reservation.previous
	held.uncertain, held.intended, held.attempt, held.seen, held.pending = demand.selected, intended, state.attempts, now, false
	state.demand[reservation.gateway] = held
	// The reservation counted the current and attempted selections together,
	// so the attempted endpoints are admitted only into rejected or
	// unprotected slots; the current output stays maintained.
	capacity := cohortCapacity(reservation.key, evidencePolicy())
	for _, id := range held.uncertain {
		state.admit(id, capacity, true)
	}
}

// resolveUncertain settles a Gateway's uncertain write before its next round:
// if the actual current output carries the intended receipt, the attempted
// selection becomes its demand; otherwise the attempt is dropped and the
// current selection, which stayed in the cohort throughout, remains. When the
// output cannot be read, both stay maintained and no new attempt is reserved.
func (p *Pipeline) resolveUncertain(key probeContextKey, gateway types.UID, published func(artifact.Receipt) (bool, error)) {
	p.mu.Lock()
	state := p.probes[key]
	if state == nil {
		p.mu.Unlock()
		return
	}
	demand, ok := state.demand[gateway]
	if !ok || demand.pending || demand.uncertain == nil {
		p.mu.Unlock()
		return
	}
	intended, attempt := demand.intended, demand.attempt
	p.mu.Unlock()
	written, err := published(intended)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if state = p.probes[key]; state == nil {
		return
	}
	if demand, ok = state.demand[gateway]; !ok || demand.pending || demand.attempt != attempt || demand.uncertain == nil {
		return
	}
	if written {
		demand.selected = demand.uncertain
	}
	demand.uncertain, demand.intended = nil, artifact.Receipt{}
	state.demand[gateway] = demand
}

// releaseSelection restores the Gateway's previous demand after a refusal or a
// failure before any external write, leaving no speculative pin behind.
func (p *Pipeline) releaseSelection(reservation *selectionReservation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.probes[reservation.key]
	if state == nil {
		return
	}
	if demand, ok := state.demand[reservation.gateway]; !ok || !demand.pending {
		return
	}
	if reservation.hadPrevious {
		state.demand[reservation.gateway] = reservation.previous
	} else {
		delete(state.demand, reservation.gateway)
	}
}

// protected reports whether some Gateway's demand still requires id to be
// maintained: it is selected, reserved or uncertainly written, and M5 has not
// rejected it. Rejected demand keeps its published last-known-good but no
// longer blocks admission of replacements.
func (s *probeState) protected(id string) bool {
	if s.rejected[id] {
		return false
	}
	for _, demand := range s.demand {
		if slices.Contains(demand.selected, id) || slices.Contains(demand.uncertain, id) {
			return true
		}
	}
	return false
}

// admit adds id to a bounded cohort. When the cohort is full, the oldest
// member M5 rejected that no demand protects yields its slot; a selected
// endpoint may displace the oldest unprotected member. Protected members are
// never displaced.
func (s *probeState) admit(id string, capacity int, selected bool) {
	if slices.Contains(s.cohort, id) {
		return
	}
	if len(s.cohort) >= capacity {
		victim := slices.IndexFunc(s.cohort, func(member string) bool { return s.rejected[member] })
		if victim < 0 && selected {
			victim = slices.IndexFunc(s.cohort, func(member string) bool { return !s.protected(member) })
		}
		if victim < 0 {
			return
		}
		delete(s.rejected, s.cohort[victim])
		s.cohort = slices.Delete(s.cohort, victim, victim+1)
	}
	s.cohort = append(s.cohort, id)
}

func (p *Pipeline) expireProbeStatesLocked(now time.Time) {
	for key, state := range p.probes {
		if now.Sub(state.seen) > probeStateTTL {
			delete(p.probes, key)
		}
	}
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
