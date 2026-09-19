// Package reconcile composes the standalone evidence-to-publication use case.
// Its interfaces are defined at the side-effect consumers so the selection
// domain remains deterministic and independent of SQL, engines and filesystems.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/policy"
	"github.com/egressfox-io/egressfox/internal/publish"
	"github.com/egressfox-io/egressfox/internal/selection"
)

type History interface {
	Summarize(context.Context, observation.Key, time.Time, time.Duration, time.Duration) (observation.Summary, error)
}

type Decisions interface {
	RecoverDecision(context.Context, string, artifact.Receipt, bool) (*selection.State, error)
	StageDecision(context.Context, selection.State, artifact.Receipt, time.Time) error
	CommitDecision(context.Context, string, artifact.Receipt) error
}

type Publisher interface {
	CurrentReceipt() (artifact.Receipt, bool, error)
	Publish(context.Context, artifact.Validated) (publish.Result, error)
}

type Request struct {
	Scope       string
	Inventory   endpoint.Inventory
	Context     selection.Context
	Selection   selection.Policy
	Listener    policy.Listener
	Renderer    engine.Renderer
	Checker     artifact.Checker
	EvaluatedAt time.Time
}

type Result struct {
	Decision    selection.Decision
	Published   bool
	Changed     bool
	RetainedLKG bool
}

func (result Result) String() string {
	return fmt.Sprintf("reconciliation result selected=%d published=%t changed=%t retained_lkg=%t transition=%s", len(result.Decision.Selected), result.Published, result.Changed, result.RetainedLKG, result.Decision.Transition)
}

type Failure struct {
	stage string
	cause error
}

func (failureValue *Failure) Error() string {
	return "standalone reconciliation failed stage=" + failureValue.stage
}
func (failureValue *Failure) Unwrap() error { return failureValue.cause }
func (failureValue *Failure) Stage() string { return failureValue.stage }

type Reconciler struct {
	mu        sync.Mutex
	history   History
	decisions Decisions
	publisher Publisher
}

func New(history History, decisions Decisions, publisher Publisher) (*Reconciler, error) {
	if history == nil || decisions == nil || publisher == nil {
		return nil, errors.New("reconciler requires history, decision store and publisher")
	}
	return &Reconciler{history: history, decisions: decisions, publisher: publisher}, nil
}

// Reconcile serializes one standalone target's planning and apply sequence.
func (reconciler *Reconciler) Reconcile(ctx context.Context, request Request) (Result, error) {
	reconciler.mu.Lock()
	defer reconciler.mu.Unlock()
	if request.Renderer == nil || request.Checker == nil || request.Inventory.Len() == 0 || request.EvaluatedAt.IsZero() {
		return Result{}, fail("request", errors.New("invalid reconciliation request"))
	}
	if request.Renderer.Profile() != request.Context.Profile {
		return Result{}, fail("profile", errors.New("renderer and evidence profiles differ"))
	}
	current, exists, err := reconciler.publisher.CurrentReceipt()
	if err != nil {
		return Result{}, fail("publisher_read", err)
	}
	previous, err := reconciler.decisions.RecoverDecision(ctx, request.Scope, current, exists)
	if err != nil {
		return Result{}, fail("state_recover", err)
	}
	decision, err := Plan(ctx, reconciler.history, request, previous)
	if err != nil {
		return Result{}, err
	}
	if len(decision.Selected) == 0 {
		return Result{Decision: decision, RetainedLKG: exists}, nil
	}
	return Apply(ctx, reconciler.decisions, reconciler.publisher, request, decision)
}

// Plan loads one bounded summary for every current connection revision and then
// delegates the side-effect-free decision to internal/selection.
func Plan(ctx context.Context, history History, request Request, previous *selection.State) (selection.Decision, error) {
	candidates := make([]selection.Candidate, 0, request.Inventory.Len())
	for _, record := range request.Inventory.Records() {
		connection, err := observation.NewConnectionRef(record.Identity())
		if err != nil {
			return selection.Decision{}, fail("candidate", err)
		}
		key, err := observation.NewKey(connection, request.Context.Target, request.Context.Vantage, request.Context.Kind, request.Context.Profile)
		if err != nil {
			return selection.Decision{}, fail("evidence_key", err)
		}
		summary, err := history.Summarize(ctx, key, request.EvaluatedAt, request.Selection.EvidenceWindow, request.Selection.Freshness)
		if err != nil {
			return selection.Decision{}, fail("evidence_load", err)
		}
		candidates = append(candidates, selection.Candidate{Record: record, Evidence: &summary})
	}
	decision, err := selection.Select(request.Scope, request.Context, request.Selection, candidates, previous, request.EvaluatedAt)
	if err != nil {
		return selection.Decision{}, fail("selection", err)
	}
	return decision, nil
}

// Apply renders and validates before staging state, then publishes and promotes
// only the checkpoint whose receipt matches the resulting LKG.
func Apply(ctx context.Context, decisions Decisions, publisher Publisher, request Request, decision selection.Decision) (Result, error) {
	inventory, err := endpoint.Deduplicate(decision.Selected)
	if err != nil {
		return Result{}, fail("inventory", err)
	}
	gateway, err := policy.NewGateway(inventory, request.Listener)
	if err != nil {
		return Result{}, fail("policy", err)
	}
	candidate, err := request.Renderer.Render(gateway)
	if err != nil {
		return Result{}, fail("render", err)
	}
	validated, err := artifact.Validate(ctx, candidate, request.Checker)
	if err != nil {
		return Result{}, fail("validation", err)
	}
	receipt, err := validated.Receipt()
	if err != nil {
		return Result{}, fail("receipt", err)
	}
	if err := decisions.StageDecision(ctx, decision.Next, receipt, request.EvaluatedAt); err != nil {
		return Result{}, fail("state_stage", err)
	}
	published, err := publisher.Publish(ctx, validated)
	if err != nil {
		return Result{}, fail("publication", err)
	}
	current, exists, err := publisher.CurrentReceipt()
	if err != nil || !exists || !current.Equal(receipt) {
		if err == nil {
			err = errors.New("published receipt mismatch")
		}
		return Result{}, fail("publication_readback", err)
	}
	if err := decisions.CommitDecision(ctx, request.Scope, current); err != nil {
		return Result{}, fail("state_commit", err)
	}
	return Result{Decision: decision, Published: true, Changed: published.Changed}, nil
}

func fail(stage string, cause error) error { return &Failure{stage: stage, cause: cause} }
