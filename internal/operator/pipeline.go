package operator

import (
	"context"
	"errors"
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

var ErrPipeline = errors.New("operator pipeline failed")

type PipelineError struct{ code string }

func (e *PipelineError) Error() string  { return "operator pipeline failed code=" + e.code }
func (e *PipelineError) Unwrap() error  { return ErrPipeline }
func (e *PipelineError) Code() string   { return e.code }
func pipelineFailure(code string) error { return &PipelineError{code: code} }

type PipelineConfig struct {
	Client        client.Client
	Reader        client.Reader
	Scheme        *runtime.Scheme
	Store         *state.Store
	MihomoBinary  string
	SingBoxBinary string
	Now           func() time.Time
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
	now           func() time.Time
	mu            sync.Mutex
	cursors       map[types.UID]int
}

type GatewayOutcome struct {
	Eligible    int
	Selected    int
	Published   bool
	Changed     bool
	RetainedLKG bool
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
	return &Pipeline{client: config.Client, reader: reader, scheme: config.Scheme, store: config.Store, mihomoBinary: config.MihomoBinary, singBoxBinary: config.SingBoxBinary, now: now, cursors: map[types.UID]int{}}, nil
}

func (p *Pipeline) Run(ctx context.Context, gateway *egressv1alpha1.EgressGateway, pool *egressv1alpha1.ProxyPool) (GatewayOutcome, error) {
	if gateway == nil || pool == nil || gateway.Namespace != pool.Namespace || gateway.Spec.PoolRef.Name != pool.Name {
		return GatewayOutcome{}, pipelineFailure("reference")
	}
	poolResult, err := BuildPool(ctx, p.reader, pool)
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
	target, targetName, targetRevision, err := p.target(ctx, pool)
	if err != nil {
		return GatewayOutcome{}, err
	}
	vantage, _ := observation.NewVantageID("k8s/operator")
	executor, err := probe.NewExecutor(probe.Config{Renderer: renderer, Checker: checker, Binary: binary, Vantage: vantage, StartupTimeout: 10 * time.Second, AllowPrivateEndpoints: pool.Spec.Probe.AllowPrivateEndpoints})
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("probe_executor")
	}
	scheduler, err := probe.NewScheduler(executor, probe.DefaultScheduleConfig())
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("probe_scheduler")
	}
	jobs := p.nextJobs(gateway.UID, poolResult.Inventory.Records(), target)
	results, _, err := scheduler.Run(ctx, jobs)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("probe_schedule")
	}
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
	selectionPolicy := selection.DefaultPolicy(selectionStrategy(pool.Spec.Selection.Strategy))
	if pool.Spec.Selection.TopN > 0 {
		selectionPolicy.TopN = int(pool.Spec.Selection.TopN)
	}
	listenerAddress := gateway.Spec.Listener.Address
	if listenerAddress == "" {
		listenerAddress = "127.0.0.1"
	}
	listenerPort := int(gateway.Spec.Listener.Port)
	if listenerPort == 0 {
		listenerPort = 1080
	}
	listener, err := policy.NewSOCKSListener(listenerAddress, listenerPort)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("listener")
	}
	inputRevisions := poolResult.SourceRevisions
	inputRevisions[targetName] = targetRevision
	guard, err := NewSnapshotGuard(p.reader, gateway, pool, inputRevisions)
	if err != nil {
		return GatewayOutcome{}, pipelineFailure("snapshot_guard")
	}
	publisher, err := NewSecretPublisher(p.client, p.scheme, gateway, gateway.Spec.OutputSecretName, guard)
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
	return GatewayOutcome{Eligible: eligible, Selected: len(result.Decision.Selected), Published: result.Published, Changed: result.Changed, RetainedLKG: result.RetainedLKG}, nil
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

func (p *Pipeline) target(ctx context.Context, pool *egressv1alpha1.ProxyPool) (observation.HTTPTarget, types.NamespacedName, ResourceRevision, error) {
	secret := &corev1.Secret{}
	ref := pool.Spec.Probe.TargetSecretRef
	name := types.NamespacedName{Namespace: pool.Namespace, Name: ref.Name}
	if err := p.reader.Get(ctx, name, secret); err != nil {
		return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("target_unavailable")
	}
	revision := ResourceRevision{UID: secret.UID, ResourceVersion: secret.ResourceVersion}
	raw, ok := secret.Data[ref.Key]
	if !ok {
		return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("target_key_missing")
	}
	id, err := observation.NewTargetID("k8s-" + string(pool.UID) + "-probe")
	if err != nil {
		return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("target_id")
	}
	timeout := time.Duration(0)
	if pool.Spec.Probe.Timeout != nil {
		timeout = pool.Spec.Probe.Timeout.Duration
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	expected := int(pool.Spec.Probe.ExpectedStatus)
	if expected == 0 {
		expected = 204
	}
	target, err := observation.NewHTTPTarget(id, string(raw), expected, timeout, observation.HTTPOptions{AllowHTTP: pool.Spec.Probe.AllowHTTP, AllowPrivate: pool.Spec.Probe.AllowPrivateTargets})
	if err != nil {
		return observation.HTTPTarget{}, types.NamespacedName{}, ResourceRevision{}, pipelineFailure("target_invalid")
	}
	return target, name, revision, nil
}

func (p *Pipeline) nextJobs(uid types.UID, records []endpoint.Record, target observation.HTTPTarget) []probe.Job {
	p.mu.Lock()
	defer p.mu.Unlock()
	start := p.cursors[uid]
	if start >= len(records) {
		start = 0
	}
	count := len(records)
	if count > maxProbeBatch {
		count = maxProbeBatch
	}
	jobs := make([]probe.Job, 0, count)
	for offset := range count {
		jobs = append(jobs, probe.Job{Record: records[(start+offset)%len(records)], Target: target})
	}
	p.cursors[uid] = (start + count) % len(records)
	return jobs
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
