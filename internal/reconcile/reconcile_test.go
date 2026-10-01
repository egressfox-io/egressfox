package reconcile_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/engine/mihomo"
	"github.com/egressfox-io/egressfox/internal/engine/singbox"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/policy"
	"github.com/egressfox-io/egressfox/internal/publish"
	"github.com/egressfox-io/egressfox/internal/reconcile"
	"github.com/egressfox-io/egressfox/internal/selection"
	"github.com/egressfox-io/egressfox/internal/state"
)

type checker struct{ reject bool }

func (value checker) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	if value.reject {
		return artifact.Evidence{}, errors.New("synthetic validation rejection")
	}
	return artifact.Evidence{ValidatorID: "test/reconcile"}, nil
}

type trackingDecisions struct{ staged, committed bool }

func (value *trackingDecisions) RecoverDecision(context.Context, string, artifact.Receipt, bool) (*selection.State, error) {
	return nil, nil
}
func (value *trackingDecisions) StageDecision(context.Context, selection.State, artifact.Receipt, time.Time) error {
	value.staged = true
	return nil
}
func (value *trackingDecisions) CommitDecision(context.Context, string, artifact.Receipt) error {
	value.committed = true
	return nil
}

type failingPublisher struct{}

func (failingPublisher) CurrentReceipt(context.Context) (artifact.Receipt, bool, error) {
	return artifact.Receipt{}, false, nil
}
func (failingPublisher) Publish(context.Context, artifact.Validated) (artifact.Publication, error) {
	return artifact.Publication{}, errors.New("synthetic publication failure")
}

func TestStandaloneLoopBothRenderersRestartAndNoOp(t *testing.T) {
	for _, test := range []struct {
		name     string
		renderer engine.Renderer
	}{
		{"mihomo", mihomo.Renderer{}}, {"sing-box", singbox.Renderer{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := privateDirectory(t)
			databasePath := filepath.Join(directory, "state.db")
			targetPath := filepath.Join(directory, "gateway.conf")
			store, err := state.Open(databasePath, state.DefaultRetention())
			if err != nil {
				t.Fatal(err)
			}
			inventory := testInventory(t, "first-secret-canary", "second-secret-canary")
			now := time.Date(2026, 9, 19, 17, 0, 0, 0, time.UTC)
			selectionContext := testSelectionContext(t, test.renderer.Profile())
			appendEvidence(t, store, inventory, selectionContext, now)
			publisher, err := publish.NewFilePublisher(targetPath)
			if err != nil {
				t.Fatal(err)
			}
			reconciler, err := reconcile.New(store, store, publisher)
			if err != nil {
				t.Fatal(err)
			}
			request := testRequest(t, inventory, selectionContext, test.renderer, now, checker{})
			first, err := reconciler.Reconcile(context.Background(), request)
			if err != nil || !first.Published || !first.Changed || len(first.Decision.Selected) != 1 {
				t.Fatalf("first reconcile = %s, %v", first, err)
			}
			published, err := os.ReadFile(targetPath)
			if err != nil || len(published) == 0 {
				t.Fatal("artifact was not published")
			}
			second, err := reconciler.Reconcile(context.Background(), request)
			if err != nil || !second.Published || second.Changed || second.Decision.Changed {
				t.Fatalf("no-op reconcile = %s, %v", second, err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = state.Open(databasePath, state.DefaultRetention())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			restarted, err := reconcile.New(store, store, publisher)
			if err != nil {
				t.Fatal(err)
			}
			afterRestart, err := restarted.Reconcile(context.Background(), request)
			if err != nil || afterRestart.Changed || afterRestart.Decision.Changed || !sameSelected(first.Decision.Selected, afterRestart.Decision.Selected) {
				t.Fatalf("restart reconcile = %s, %v", afterRestart, err)
			}
		})
	}
}

func TestPlanSeparatesNamedTargetEvidenceOverSharedInventory(t *testing.T) {
	dir := privateDirectory(t)
	store, err := state.Open(filepath.Join(dir, "profile-history.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	inventory := testInventory(t, "first-secret", "second-secret")
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	vantage, _ := observation.NewVantageID("standalone")
	contextFor := func(name, url string, profile artifact.Profile) selection.Context {
		id, _ := observation.NewTargetID(name)
		target, err := observation.NewHTTPTarget(id, url, 200, time.Second, observation.HTTPOptions{})
		if err != nil {
			t.Fatal(err)
		}
		result, err := selection.NewContext(target.Ref(), vantage, observation.KindHTTPGet, profile)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	alpha := contextFor("alpha", "https://example.com/a", artifact.Mihomo11931)
	beta := contextFor("beta", "https://example.com/b", artifact.Mihomo11931)
	records := inventory.Records()
	for _, entry := range []struct {
		record  endpoint.Record
		context selection.Context
	}{{records[0], alpha}, {records[1], beta}} {
		connection, _ := observation.NewConnectionRef(entry.record.Identity())
		key, _ := observation.NewKey(connection, entry.context.Target, entry.context.Vantage, entry.context.Kind, entry.context.Profile)
		for i := 0; i < 3; i++ {
			completed := now.Add(time.Duration(i-2) * time.Second)
			value, err := observation.New(observation.Params{Key: key, StartedAt: completed.Add(-20 * time.Millisecond), CompletedAt: completed, Duration: 20 * time.Millisecond, Outcome: observation.OutcomeSuccess, StatusCode: 200})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Append(context.Background(), value, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, entry := range []struct {
		name     string
		context  selection.Context
		expected endpoint.Record
	}{{"alpha", alpha, records[0]}, {"beta", beta, records[1]}} {
		request := testRequest(t, inventory, entry.context, mihomo.Renderer{}, now, checker{})
		request.Scope = entry.name
		request.Selection = selection.DefaultPolicy(selection.StrategyStatic)
		decision, err := reconcile.Plan(context.Background(), store, request, nil)
		if err != nil || len(decision.Selected) != 1 || !decision.Selected[0].Identity().Equal(entry.expected.Identity()) {
			t.Fatalf("%s decision=%s err=%v", entry.name, decision, err)
		}
	}
	mutated := contextFor("alpha", "https://example.com/changed", artifact.Mihomo11931)
	request := testRequest(t, inventory, mutated, mihomo.Renderer{}, now, checker{})
	request.Selection = selection.DefaultPolicy(selection.StrategyStatic)
	decision, err := reconcile.Plan(context.Background(), store, request, nil)
	if err != nil || len(decision.Selected) != 0 {
		t.Fatalf("target mutation reused evidence: %s %v", decision, err)
	}
}

func TestValidationAndNoEligiblePreserveLastKnownGood(t *testing.T) {
	directory := privateDirectory(t)
	store, err := state.Open(filepath.Join(directory, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	publisher, err := publish.NewFilePublisher(filepath.Join(directory, "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	inventory := testInventory(t, "known-secret", "alternate-secret")
	now := time.Date(2026, 9, 19, 17, 0, 0, 0, time.UTC)
	selectionContext := testSelectionContext(t, artifact.SingBox1141)
	appendEvidence(t, store, inventory, selectionContext, now)
	reconciler, _ := reconcile.New(store, store, publisher)
	request := testRequest(t, inventory, selectionContext, singbox.Renderer{}, now, checker{})
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	knownGood, err := os.ReadFile(filepath.Join(directory, "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}

	request.Checker = checker{reject: true}
	request.EvaluatedAt = now.Add(time.Second)
	if _, err := reconciler.Reconcile(context.Background(), request); failureStage(err) != "validation" {
		t.Fatalf("validation error = %v", err)
	}
	afterFailure, _ := os.ReadFile(filepath.Join(directory, "gateway.json"))
	if !bytes.Equal(knownGood, afterFailure) {
		t.Fatal("validation failure replaced LKG")
	}

	rotated := testInventory(t, "rotated-known-secret", "rotated-alternate-secret")
	request.Inventory = rotated
	request.Checker = checker{}
	request.EvaluatedAt = now.Add(2 * time.Second)
	result, err := reconciler.Reconcile(context.Background(), request)
	if err != nil || len(result.Decision.Selected) != 0 || !result.RetainedLKG || result.Published {
		t.Fatalf("zero eligible result = %s, %v", result, err)
	}
	afterUnknown, _ := os.ReadFile(filepath.Join(directory, "gateway.json"))
	if !bytes.Equal(knownGood, afterUnknown) {
		t.Fatal("unknown revisions replaced LKG")
	}
}

func TestOlderReconciliationCannotOverwriteCommittedState(t *testing.T) {
	directory := privateDirectory(t)
	store, err := state.Open(filepath.Join(directory, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	publisher, _ := publish.NewFilePublisher(filepath.Join(directory, "gateway.json"))
	inventory := testInventory(t, "one-secret", "two-secret")
	now := time.Date(2026, 9, 19, 17, 0, 0, 0, time.UTC)
	selectionContext := testSelectionContext(t, artifact.SingBox1141)
	appendEvidence(t, store, inventory, selectionContext, now)
	reconciler, _ := reconcile.New(store, store, publisher)
	request := testRequest(t, inventory, selectionContext, singbox.Renderer{}, now, checker{})
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.EvaluatedAt = now.Add(-time.Second)
	if _, err := reconciler.Reconcile(context.Background(), request); failureStage(err) != "selection" {
		t.Fatalf("obsolete result = %v", err)
	}
}

func TestPublicationFailureLeavesDecisionPending(t *testing.T) {
	directory := privateDirectory(t)
	store, err := state.Open(filepath.Join(directory, "history.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	inventory := testInventory(t, "publication-secret")
	now := time.Date(2026, 9, 19, 18, 0, 0, 0, time.UTC)
	selectionContext := testSelectionContext(t, artifact.Mihomo11931)
	appendEvidence(t, store, inventory, selectionContext, now)
	request := testRequest(t, inventory, selectionContext, mihomo.Renderer{}, now, checker{})
	decision, err := reconcile.Plan(context.Background(), store, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	tracker := &trackingDecisions{}
	if _, err := reconcile.Apply(context.Background(), tracker, failingPublisher{}, request, decision); failureStage(err) != "publication" {
		t.Fatalf("publication error = %v", err)
	}
	if !tracker.staged || tracker.committed {
		t.Fatalf("checkpoint flags staged=%t committed=%t", tracker.staged, tracker.committed)
	}
}

// A refused reservation stops the decision before rendering, staging or
// publication, so the last-known-good output and committed state stay put.
func TestRefusedReservationPreservesLastKnownGood(t *testing.T) {
	directory := privateDirectory(t)
	store, err := state.Open(filepath.Join(directory, "history.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	targetPath := filepath.Join(directory, "gateway.conf")
	publisher, err := publish.NewFilePublisher(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	inventory := testInventory(t, "reservation-secret")
	now := time.Date(2026, 9, 19, 19, 0, 0, 0, time.UTC)
	selectionContext := testSelectionContext(t, artifact.Mihomo11931)
	appendEvidence(t, store, inventory, selectionContext, now)
	reconciler, err := reconcile.New(store, store, publisher)
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest(t, inventory, selectionContext, mihomo.Renderer{}, now, checker{})
	reserved := 0
	request.Reserve = func(decision selection.Decision) error {
		reserved++
		if len(decision.Selected) == 0 {
			t.Fatal("empty decision reached Reserve")
		}
		return errors.New("synthetic capacity refusal")
	}
	request.BeforeApply = func([]endpoint.Record) { t.Fatal("refused decision reached BeforeApply") }
	if _, err := reconciler.Reconcile(context.Background(), request); failureStage(err) != "scheduling" || reserved != 1 {
		t.Fatalf("reservation error = %v reserved=%d", err, reserved)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatal("refused decision was published")
	}
	// With nothing maintained the constrained plan selects nothing, which
	// retains the last-known-good without another reservation.
	reserved = 0
	request.Maintained = func(endpoint.Record) bool { return false }
	result, err := reconciler.Reconcile(context.Background(), request)
	if err != nil || reserved != 1 || !result.Constrained || len(result.Decision.Selected) != 0 || result.Published {
		t.Fatalf("constrained result = %s constrained=%t reserved=%d err=%v", result, result.Constrained, reserved, err)
	}
}

// A refused decision is planned again among maintained endpoints only; that
// constrained decision is reserved and published.
func TestRefusedReservationPlansAmongMaintainedEndpoints(t *testing.T) {
	directory := privateDirectory(t)
	store, err := state.Open(filepath.Join(directory, "history.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	publisher, err := publish.NewFilePublisher(filepath.Join(directory, "gateway.conf"))
	if err != nil {
		t.Fatal(err)
	}
	inventory := testInventory(t, "preferred-secret", "maintained-secret")
	now := time.Date(2026, 9, 19, 20, 0, 0, 0, time.UTC)
	selectionContext := testSelectionContext(t, artifact.Mihomo11931)
	appendEvidence(t, store, inventory, selectionContext, now)
	reconciler, err := reconcile.New(store, store, publisher)
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest(t, inventory, selectionContext, mihomo.Renderer{}, now, checker{})
	unconstrained, err := reconcile.Plan(context.Background(), store, request, nil)
	if err != nil || len(unconstrained.Selected) != 1 {
		t.Fatalf("plan = %v, %v", unconstrained, err)
	}
	preferred := unconstrained.Selected[0].ID()
	var reservations []endpoint.ID
	request.Reserve = func(decision selection.Decision) error {
		reservations = append(reservations, decision.Selected[0].ID())
		if decision.Selected[0].ID() == preferred {
			return errors.New("synthetic capacity refusal")
		}
		return nil
	}
	request.Maintained = func(record endpoint.Record) bool { return record.ID() != preferred }
	result, err := reconciler.Reconcile(context.Background(), request)
	if err != nil || !result.Published || !result.Constrained || len(result.Decision.Selected) != 1 || result.Decision.Selected[0].ID() == preferred {
		t.Fatalf("constrained publication = %s constrained=%t err=%v", result, result.Constrained, err)
	}
	if len(reservations) != 2 {
		t.Fatalf("reservations=%d want 2", len(reservations))
	}
}

// memoryPublisher holds one published receipt and injects faults around the
// external write.
type memoryPublisher struct {
	receipt        artifact.Receipt
	exists         bool
	publishErr     error
	writeOnFailure bool
	// readbackErr fails receipt reads once something has been written.
	readbackErr error
}

func (p *memoryPublisher) CurrentReceipt(context.Context) (artifact.Receipt, bool, error) {
	if p.readbackErr != nil && p.exists {
		return artifact.Receipt{}, false, p.readbackErr
	}
	return p.receipt, p.exists, nil
}

func (p *memoryPublisher) Publish(_ context.Context, validated artifact.Validated) (artifact.Publication, error) {
	receipt, err := validated.Receipt()
	if err != nil {
		return artifact.Publication{}, err
	}
	if p.publishErr != nil {
		if p.writeOnFailure {
			p.receipt, p.exists = receipt, true
		}
		return artifact.Publication{}, p.publishErr
	}
	changed := !p.exists || !p.receipt.Equal(receipt)
	p.receipt, p.exists = receipt, true
	return artifact.Publication{Changed: changed}, nil
}

type uncertainError struct{}

func (uncertainError) Error() string              { return "synthetic write timeout" }
func (uncertainError) PublicationUncertain() bool { return true }

// failingCommit wraps the real store and fails CommitDecision while set.
type failingCommit struct {
	*state.Store
	fail bool
}

func (d *failingCommit) CommitDecision(ctx context.Context, scope string, receipt artifact.Receipt) error {
	if d.fail {
		return errors.New("synthetic commit failure")
	}
	return d.Store.CommitDecision(ctx, scope, receipt)
}

// Apply reports the external publication state independently of its error:
// failures before the write report none, an ambiguous write is uncertain, a
// write whose receipt cannot be read back is written, and a matching receipt
// is confirmed even when the durable checkpoint commit fails.
func TestApplyReportsPublicationStateSeparately(t *testing.T) {
	for _, test := range []struct {
		name        string
		checker     artifact.Checker
		publisher   *memoryPublisher
		failCommit  bool
		stage       string
		publication reconcile.Publication
		written     bool
	}{
		{"validation fails before publication", checker{reject: true}, &memoryPublisher{}, false, "validation", reconcile.PublicationNone, false},
		{"publication fails before writing", checker{}, &memoryPublisher{publishErr: errors.New("synthetic ownership refusal")}, false, "publication", reconcile.PublicationNone, false},
		{"uncertain write that happened", checker{}, &memoryPublisher{publishErr: uncertainError{}, writeOnFailure: true}, false, "publication", reconcile.PublicationUncertain, true},
		{"uncertain write that did not happen", checker{}, &memoryPublisher{publishErr: uncertainError{}}, false, "publication", reconcile.PublicationUncertain, false},
		{"receipt read-back fails after the write", checker{}, &memoryPublisher{readbackErr: errors.New("synthetic read failure")}, false, "publication_readback", reconcile.PublicationWritten, true},
		{"commit fails after a confirmed write", checker{}, &memoryPublisher{}, true, "state_commit", reconcile.PublicationConfirmed, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := state.Open(filepath.Join(privateDirectory(t), "history.db"), state.DefaultRetention())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			inventory := testInventory(t, "fault-secret")
			now := time.Date(2026, 9, 19, 21, 0, 0, 0, time.UTC)
			selectionContext := testSelectionContext(t, artifact.Mihomo11931)
			appendEvidence(t, store, inventory, selectionContext, now)
			decisions := &failingCommit{Store: store, fail: test.failCommit}
			reconciler, err := reconcile.New(store, decisions, test.publisher)
			if err != nil {
				t.Fatal(err)
			}
			result, err := reconciler.Reconcile(context.Background(), testRequest(t, inventory, selectionContext, mihomo.Renderer{}, now, test.checker))
			if failureStage(err) != test.stage || result.Publication != test.publication || result.Published {
				t.Fatalf("stage=%q publication=%d published=%t", failureStage(err), result.Publication, result.Published)
			}
			if test.publisher.exists != test.written {
				t.Fatalf("external write=%t want %t", test.publisher.exists, test.written)
			}
			if test.written && !test.publisher.receipt.Equal(result.Intended) {
				t.Fatal("intended receipt does not identify the written artifact")
			}
		})
	}
}

// After a confirmed write whose durable commit failed, the next reconciliation
// recovers the staged checkpoint from the actual published receipt instead of
// treating the decision as new.
func TestPendingCheckpointRecoversFromPublishedReceipt(t *testing.T) {
	store, err := state.Open(filepath.Join(privateDirectory(t), "history.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	inventory := testInventory(t, "recovery-secret")
	now := time.Date(2026, 9, 19, 22, 0, 0, 0, time.UTC)
	selectionContext := testSelectionContext(t, artifact.Mihomo11931)
	appendEvidence(t, store, inventory, selectionContext, now)
	publisher := &memoryPublisher{}
	decisions := &failingCommit{Store: store, fail: true}
	reconciler, err := reconcile.New(store, decisions, publisher)
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest(t, inventory, selectionContext, mihomo.Renderer{}, now, checker{})
	first, err := reconciler.Reconcile(context.Background(), request)
	if failureStage(err) != "state_commit" || first.Publication != reconcile.PublicationConfirmed {
		t.Fatalf("first reconcile stage=%q publication=%d", failureStage(err), first.Publication)
	}
	decisions.fail = false
	second, err := reconciler.Reconcile(context.Background(), request)
	if err != nil || !second.Published || second.Changed || second.Decision.Changed || !sameSelected(first.Decision.Selected, second.Decision.Selected) {
		t.Fatalf("recovery reconcile = %s, %v", second, err)
	}
}

func testRequest(t testing.TB, inventory endpoint.Inventory, context selection.Context, renderer engine.Renderer, now time.Time, checker artifact.Checker) reconcile.Request {
	t.Helper()
	listener, err := policy.NewSOCKSListener("127.0.0.1", 1080)
	if err != nil {
		t.Fatal(err)
	}
	return reconcile.Request{Scope: "gateway", Inventory: inventory, Context: context, Selection: selection.DefaultPolicy(selection.StrategyAdaptive), Listener: listener, Renderer: renderer, Checker: checker, EvaluatedAt: now}
}

func testInventory(t testing.TB, secrets ...string) endpoint.Inventory {
	t.Helper()
	sourceID, _ := endpoint.NewSourceID("test")
	records := make([]endpoint.Record, len(secrets))
	for index, secret := range secrets {
		address, err := endpoint.NewAddress(fmt.Sprintf("edge-%d.example.com", index+1), 443)
		if err != nil {
			t.Fatal(err)
		}
		credential, err := endpoint.NewTrojanCredential(secret)
		if err != nil {
			t.Fatal(err)
		}
		tls, err := endpoint.NewTLS("", false)
		if err != nil {
			t.Fatal(err)
		}
		configuration, err := endpoint.NewConfiguration(endpoint.ProtocolTrojan, address, credential, endpoint.NewTCPTransport(), tls)
		if err != nil {
			t.Fatal(err)
		}
		recordID, _ := endpoint.NewRecordID(fmt.Sprintf("record-%d", index+1))
		provenance, _ := endpoint.NewProvenance(sourceID, recordID)
		records[index], err = endpoint.NewRecord(configuration, provenance)
		if err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := endpoint.Deduplicate(records)
	if err != nil {
		t.Fatal(err)
	}
	return inventory
}

func testSelectionContext(t testing.TB, profile artifact.Profile) selection.Context {
	t.Helper()
	targetID, _ := observation.NewTargetID("service")
	target, err := observation.NewHTTPTarget(targetID, "https://example.com/health", 200, time.Second, observation.HTTPOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vantage, _ := observation.NewVantageID("standalone")
	result, err := selection.NewContext(target.Ref(), vantage, observation.KindHTTPGet, profile)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func appendEvidence(t testing.TB, store *state.Store, inventory endpoint.Inventory, context selection.Context, now time.Time) {
	t.Helper()
	for recordIndex, record := range inventory.Records() {
		connection, _ := observation.NewConnectionRef(record.Identity())
		key, err := observation.NewKey(connection, context.Target, context.Vantage, context.Kind, context.Profile)
		if err != nil {
			t.Fatal(err)
		}
		for sample := 0; sample < 5; sample++ {
			completed := now.Add(time.Duration(sample-4) * time.Second)
			duration := time.Duration(20+recordIndex*20) * time.Millisecond
			value, err := observation.New(observation.Params{Key: key, StartedAt: completed.Add(-duration), CompletedAt: completed, Duration: duration, Outcome: observation.OutcomeSuccess, StatusCode: 200})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Append(contextBackground(), value, now); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func contextBackground() context.Context { return context.Background() }
func sameSelected(left, right []endpoint.Record) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].Identity().Equal(right[index].Identity()) {
			return false
		}
	}
	return true
}
func failureStage(err error) string {
	var failure *reconcile.Failure
	if errors.As(err, &failure) {
		return failure.Stage()
	}
	return ""
}

func privateDirectory(t testing.TB) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}
