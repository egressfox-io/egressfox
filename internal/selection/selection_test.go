package selection_test

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/selection"
)

var testNow = time.Date(2026, 9, 19, 15, 0, 0, 0, time.UTC)

func TestStrategiesUseCommonEligibilityAndConfidence(t *testing.T) {
	context := testContext(t, artifact.SingBox1141)
	first := testCandidate(t, context, 1, "first-secret", 3, 3, 20*time.Millisecond, 3, 0, true)
	second := testCandidate(t, context, 2, "second-secret", 100, 100, 20*time.Millisecond, 100, 0, true)
	static, err := selection.Select("gateway", context, selection.DefaultPolicy(selection.StrategyStatic), []selection.Candidate{second, first}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	lowest, err := selection.Select("gateway", context, selection.DefaultPolicy(selection.StrategyLowestLatency), []selection.Candidate{second, first}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	adaptive, err := selection.Select("gateway", context, selection.DefaultPolicy(selection.StrategyAdaptive), []selection.Candidate{first, second}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical := first.Record
	if second.Record.Identity().Compare(first.Record.Identity()) < 0 {
		wantCanonical = second.Record
	}
	if !static.Selected[0].Identity().Equal(wantCanonical.Identity()) || !lowest.Selected[0].Identity().Equal(wantCanonical.Identity()) {
		t.Fatal("baseline tie did not use canonical connection order")
	}
	if !adaptive.Selected[0].Identity().Equal(second.Record.Identity()) {
		t.Fatal("adaptive strategy did not prefer stronger evidence at equal latency")
	}
	if adaptive.Explanations[0].ConfidencePPM == adaptive.Explanations[1].ConfidencePPM {
		t.Fatal("confidence failed to distinguish sample counts")
	}
}

func TestEligibilityReasonsAreExplicit(t *testing.T) {
	context := testContext(t, artifact.Mihomo11931)
	missing := testRecord(t, 1, "missing-secret")
	stale := testCandidate(t, context, 2, "stale-secret", 10, 10, 10*time.Millisecond, 10, 0, false)
	insufficient := testCandidate(t, context, 3, "few-secret", 2, 2, 10*time.Millisecond, 2, 0, true)
	unreliable := testCandidate(t, context, 4, "bad-secret", 10, 5, 10*time.Millisecond, 0, 1, true)
	failing := testCandidate(t, context, 5, "failed-secret", 10, 8, 10*time.Millisecond, 0, 2, true)
	decision, err := selection.Select("gateway", context, selection.DefaultPolicy(selection.StrategyAdaptive), []selection.Candidate{{Record: missing}, stale, insufficient, unreliable, failing}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := map[selection.Reason]bool{selection.ReasonMissingEvidence: true, selection.ReasonStale: true, selection.ReasonInsufficientSamples: true, selection.ReasonUnreliable: true, selection.ReasonFailureStreak: true}
	for _, explanation := range decision.Explanations {
		delete(want, explanation.Reason)
	}
	if len(want) != 0 || len(decision.Selected) != 0 || !decision.Degraded || decision.Transition != selection.TransitionNoEligible {
		t.Fatalf("unexpected eligibility result: %v %s", want, decision)
	}
}

func TestAdaptiveResidenceHysteresisAndEmergencyRecovery(t *testing.T) {
	context := testContext(t, artifact.SingBox1141)
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	incumbent := testCandidate(t, context, 1, "incumbent-secret", 20, 20, 100*time.Millisecond, 20, 0, true)
	challenger := testCandidate(t, context, 2, "challenger-secret", 20, 20, 50*time.Millisecond, 20, 0, true)
	initial, err := selection.Select("gateway", context, policy, []selection.Candidate{incumbent}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	before, err := selection.Select("gateway", context, policy, []selection.Candidate{incumbent, challenger}, &initial.Next, testNow.Add(policy.Residence-time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	if before.Changed {
		t.Fatal("residence did not retain incumbent")
	}
	atBoundary, err := selection.Select("gateway", context, policy, []selection.Candidate{incumbent, challenger}, &initial.Next, testNow.Add(policy.Residence))
	if err != nil {
		t.Fatal(err)
	}
	if !atBoundary.Changed || !atBoundary.Selected[0].Identity().Equal(challenger.Record.Identity()) {
		t.Fatal("challenger was not admitted at residence boundary")
	}

	failingIncumbent := testCandidate(t, context, 2, "challenger-secret", 20, 18, 50*time.Millisecond, 0, 2, true)
	fallback := testCandidate(t, context, 1, "incumbent-secret", 20, 20, 100*time.Millisecond, 20, 0, true)
	emergency, err := selection.Select("gateway", context, policy, []selection.Candidate{failingIncumbent, fallback}, &atBoundary.Next, testNow.Add(policy.Residence+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if emergency.Transition != selection.TransitionEmergency || !emergency.Selected[0].Identity().Equal(fallback.Record.Identity()) || len(emergency.Next.Cooldowns) != 1 {
		t.Fatal("failure did not trigger emergency replacement and cooldown")
	}

	recoveredTooSoon := testCandidate(t, context, 2, "challenger-secret", 30, 28, 20*time.Millisecond, 2, 0, true)
	during, err := selection.Select("gateway", context, policy, []selection.Candidate{recoveredTooSoon, fallback}, &emergency.Next, testNow.Add(policy.Residence+policy.Cooldown))
	if err != nil {
		t.Fatal(err)
	}
	if !during.Selected[0].Identity().Equal(fallback.Record.Identity()) {
		t.Fatal("recovery quorum was bypassed")
	}
	recovered := testCandidate(t, context, 2, "challenger-secret", 31, 29, 20*time.Millisecond, 3, 0, true)
	after, err := selection.Select("gateway", context, policy, []selection.Candidate{recovered, fallback}, &during.Next, testNow.Add(2*policy.Residence+policy.Cooldown+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !after.Selected[0].Identity().Equal(recovered.Record.Identity()) || len(after.Next.Cooldowns) != 0 {
		t.Fatal("recovered endpoint did not re-enter competition")
	}
}

func TestTopNPermutationRestartAndRevisionIsolation(t *testing.T) {
	context := testContext(t, artifact.Mihomo11931)
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	policy.TopN = 2
	candidates := []selection.Candidate{
		testCandidate(t, context, 1, "one-secret", 20, 20, 30*time.Millisecond, 20, 0, true),
		testCandidate(t, context, 2, "two-secret", 20, 19, 20*time.Millisecond, 10, 0, true),
		testCandidate(t, context, 3, "three-secret", 20, 20, 40*time.Millisecond, 20, 0, true),
	}
	first, err := selection.Select("gateway", context, policy, candidates, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	permuted := append([]selection.Candidate(nil), candidates...)
	rand.New(rand.NewPCG(7, 11)).Shuffle(len(permuted), func(i, j int) { permuted[i], permuted[j] = permuted[j], permuted[i] })
	replayed, err := selection.Select("gateway", context, policy, permuted, &first.Next, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSelected(first.Selected, replayed.Selected) || replayed.Changed {
		t.Fatal("permutation/restart changed decision")
	}

	rotated := testRecord(t, 1, "rotated-secret")
	decision, err := selection.Select("gateway", context, policy, []selection.Candidate{{Record: rotated}, candidates[1]}, &first.Next, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Selected) != 1 || decision.Selected[0].ID() != candidates[1].Record.ID() || !decision.Degraded {
		t.Fatal("credential rotation reused old evidence")
	}
}

func TestFailureReasonTracksExactConnectionRevision(t *testing.T) {
	context := testContext(t, artifact.SingBox1141)
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	oldRevision := testCandidate(t, context, 1, "old-secret", 20, 20, 20*time.Millisecond, 20, 0, true)
	initial, err := selection.Select("gateway", context, policy, []selection.Candidate{oldRevision}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	failedOld := testCandidate(t, context, 1, "old-secret", 20, 18, 20*time.Millisecond, 0, 2, true)
	newRevision := testCandidate(t, context, 1, "new-secret", 20, 20, 30*time.Millisecond, 20, 0, true)
	if failedOld.Record.ID() != newRevision.Record.ID() || failedOld.Record.Identity().Equal(newRevision.Record.Identity()) {
		t.Fatal("fixture is not a credential rotation")
	}
	decision, err := selection.Select("gateway", context, policy, []selection.Candidate{newRevision, failedOld}, &initial.Next, testNow.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Next.Cooldowns) != 1 || !decision.Next.Cooldowns[0].Connection.Equal(initial.Next.Members[0].Connection) {
		t.Fatal("failure cooldown was attached to the wrong connection revision")
	}
}

func TestAdaptiveHysteresisExactBoundary(t *testing.T) {
	context := testContext(t, artifact.SingBox1141)
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	policy.Residence = 0
	incumbent := testCandidate(t, context, 1, "incumbent-secret", 100, 100, 100*time.Millisecond, 100, 0, true)
	initial, err := selection.Select("gateway", context, policy, []selection.Candidate{incumbent}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		latency time.Duration
		changed bool
	}{
		{91 * time.Millisecond, false}, {90 * time.Millisecond, true},
	} {
		challenger := testCandidate(t, context, 2, "challenger-secret", 100, 100, test.latency, 100, 0, true)
		decision, err := selection.Select("gateway", context, policy, []selection.Candidate{incumbent, challenger}, &initial.Next, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Changed != test.changed {
			t.Fatalf("latency %s changed=%t want %t", test.latency, decision.Changed, test.changed)
		}
	}
}

func TestAdaptiveScenarioResistsJitterAndHandlesOutage(t *testing.T) {
	context := testContext(t, artifact.Mihomo11931)
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	policy.Residence = time.Minute
	policy.Cooldown = 2 * time.Minute
	a := func(latency time.Duration, failures, successes int, fresh bool) selection.Candidate {
		return testCandidate(t, context, 1, "a-secret", 30, 30-failures, latency, successes, failures, fresh)
	}
	b := func(latency time.Duration, failures, successes int, fresh bool) selection.Candidate {
		return testCandidate(t, context, 2, "b-secret", 30, 30-failures, latency, successes, failures, fresh)
	}
	frames := []selection.Frame{
		{At: testNow, Candidates: []selection.Candidate{a(50*time.Millisecond, 0, 30, true), b(60*time.Millisecond, 0, 30, true)}},
		{At: testNow.Add(2 * time.Minute), Candidates: []selection.Candidate{a(50*time.Millisecond, 0, 30, true), b(47*time.Millisecond, 0, 30, true)}},
		{At: testNow.Add(3 * time.Minute), Candidates: []selection.Candidate{a(50*time.Millisecond, 2, 0, true), b(60*time.Millisecond, 0, 30, true)}},
		{At: testNow.Add(4 * time.Minute), Candidates: []selection.Candidate{a(50*time.Millisecond, 2, 0, false), b(60*time.Millisecond, 2, 0, false)}},
		{At: testNow.Add(6 * time.Minute), Candidates: []selection.Candidate{a(40*time.Millisecond, 0, 3, true), b(60*time.Millisecond, 0, 30, true)}},
	}
	var state *selection.State
	selected := make([]string, 0, len(frames))
	for _, frame := range frames {
		decision, err := selection.Select("gateway", context, policy, frame.Candidates, state, frame.At)
		if err != nil {
			t.Fatal(err)
		}
		if len(decision.Selected) == 0 {
			selected = append(selected, "none")
		} else {
			selected = append(selected, decision.Selected[0].ID().String())
		}
		next := decision.Next
		state = &next
	}
	if selected[0] != selected[1] {
		t.Fatal("sub-threshold jitter changed selection")
	}
	if selected[2] == selected[1] {
		t.Fatal("confirmed failure did not replace incumbent")
	}
	if selected[3] != "none" {
		t.Fatal("complete outage fabricated an eligible endpoint")
	}
	if selected[4] == "none" {
		t.Fatal("recovery did not restore a feasible selection")
	}
}

func TestSelectionDiagnosticsDoNotLeak(t *testing.T) {
	context := testContext(t, artifact.SingBox1141)
	candidate := testCandidate(t, context, 1, "selection-secret-canary", 10, 10, time.Millisecond, 10, 0, true)
	decision, err := selection.Select("gateway", context, selection.DefaultPolicy(selection.StrategyAdaptive), []selection.Candidate{candidate}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Sprintf("%v %+v %#v", decision, decision, decision.Explanations)
	encoded, marshalErr := json.Marshal(decision)
	if strings.Contains(formatted+string(encoded)+fmt.Sprint(marshalErr), "selection-secret-canary") {
		t.Fatal("selection diagnostics leaked credentials")
	}
}

func BenchmarkSelectTopN(b *testing.B) {
	for _, size := range []int{1_000, 10_000} {
		b.Run(fmt.Sprintf("candidates_%d", size), func(b *testing.B) {
			context := testContext(b, artifact.SingBox1141)
			policy := selection.DefaultPolicy(selection.StrategyAdaptive)
			policy.TopN = 10
			candidates := make([]selection.Candidate, size)
			for index := range candidates {
				candidates[index] = testCandidate(b, context, index+1, fmt.Sprintf("secret-%d", index), 20, 18+index%3, time.Duration(10+index%100)*time.Millisecond, 5, 0, true)
			}
			b.ResetTimer()
			for range b.N {
				if _, err := selection.Select("gateway", context, policy, candidates, nil, testNow); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzSelectionPermutation(f *testing.F) {
	context := testContext(f, artifact.SingBox1141)
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	policy.TopN = 2
	candidates := []selection.Candidate{testCandidate(f, context, 1, "one", 20, 20, 30*time.Millisecond, 20, 0, true), testCandidate(f, context, 2, "two", 20, 19, 20*time.Millisecond, 10, 0, true), testCandidate(f, context, 3, "three", 20, 20, 40*time.Millisecond, 20, 0, true)}
	baseline, err := selection.Select("gateway", context, policy, candidates, nil, testNow)
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte{2, 1, 0})
	f.Add([]byte{0, 0, 0})
	f.Fuzz(func(t *testing.T, order []byte) {
		permuted := append([]selection.Candidate(nil), candidates...)
		for index := len(permuted) - 1; index > 0; index-- {
			choice := 0
			if len(order) > 0 {
				choice = int(order[index%len(order)]) % (index + 1)
			}
			permuted[index], permuted[choice] = permuted[choice], permuted[index]
		}
		decision, err := selection.Select("gateway", context, policy, permuted, nil, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if !sameSelected(baseline.Selected, decision.Selected) {
			t.Fatal("input permutation changed semantic decision")
		}
	})
}

func testContext(t testing.TB, profile artifact.Profile) selection.Context {
	t.Helper()
	id, _ := observation.NewTargetID("service")
	target, err := observation.NewHTTPTarget(id, "https://example.com/health", 200, time.Second, observation.HTTPOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vantage, _ := observation.NewVantageID("host-a")
	context, err := selection.NewContext(target.Ref(), vantage, observation.KindHTTPGet, profile)
	if err != nil {
		t.Fatal(err)
	}
	return context
}

func testRecord(t testing.TB, number int, secret string) endpoint.Record {
	t.Helper()
	address, err := endpoint.NewAddress(fmt.Sprintf("edge-%d.example.com", number), 443)
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
	sourceID, _ := endpoint.NewSourceID("test")
	recordID, _ := endpoint.NewRecordID(fmt.Sprintf("record-%d", number))
	provenance, _ := endpoint.NewProvenance(sourceID, recordID)
	record, err := endpoint.NewRecord(configuration, provenance)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func testCandidate(t testing.TB, context selection.Context, number int, secret string, samples, successes int, latency time.Duration, successStreak, failureStreak int, fresh bool) selection.Candidate {
	t.Helper()
	record := testRecord(t, number, secret)
	ref, _ := observation.NewConnectionRef(record.Identity())
	key, err := observation.NewKey(ref, context.Target, context.Vantage, context.Kind, context.Profile)
	if err != nil {
		t.Fatal(err)
	}
	summary := observation.Summary{Key: key, Samples: samples, Successes: successes, MeanSuccessDuration: latency, Fresh: fresh, ConsecutiveSuccesses: successStreak, ConsecutiveFailures: failureStreak}
	if samples > 0 {
		summary.SuccessRatio = float64(successes) / float64(samples)
	}
	return selection.Candidate{Record: record, Evidence: &summary}
}

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

func TestReplayComparesStrategiesOnIdenticalFrames(t *testing.T) {
	context := testContext(t, artifact.SingBox1141)
	fastSparse := testCandidate(t, context, 1, "sparse-secret", 3, 3, 5*time.Millisecond, 3, 0, true)
	steady := testCandidate(t, context, 2, "steady-secret", 100, 98, 20*time.Millisecond, 20, 0, true)
	failedFast := testCandidate(t, context, 1, "sparse-secret", 10, 7, 10*time.Millisecond, 0, 2, true)
	frames := []selection.Frame{
		{At: testNow, Candidates: []selection.Candidate{fastSparse, steady}},
		{At: testNow.Add(11 * time.Minute), Candidates: []selection.Candidate{failedFast, steady}},
	}
	reports, err := selection.Compare(selection.Scenario{Scope: "gateway", Context: context, Policy: selection.DefaultPolicy(selection.StrategyAdaptive), Frames: frames}, selection.StrategyStatic, selection.StrategyLowestLatency, selection.StrategyAdaptive)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 3 {
		t.Fatal("missing strategy reports")
	}
	for _, report := range reports {
		if report.Evaluations != 2 || report.NoEligible != 0 {
			t.Fatalf("unexpected report: %+v", report)
		}
	}
	if reports[2].EmergencyChanges != 1 {
		t.Fatalf("adaptive emergency count = %d", reports[2].EmergencyChanges)
	}
}
