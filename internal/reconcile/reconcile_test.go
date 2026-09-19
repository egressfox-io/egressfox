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

func (failingPublisher) CurrentReceipt() (artifact.Receipt, bool, error) {
	return artifact.Receipt{}, false, nil
}
func (failingPublisher) Publish(context.Context, artifact.Validated) (publish.Result, error) {
	return publish.Result{}, errors.New("synthetic publication failure")
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
