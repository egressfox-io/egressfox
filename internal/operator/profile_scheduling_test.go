package operator

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/probe"
	"github.com/egressfox-io/egressfox/internal/selection"
)

// testRecords returns xhttp records that sing-box rejects followed by tcp
// records compatible with both engines.
func testRecords(t *testing.T, tcp, xhttp int) []endpoint.Record {
	t.Helper()
	credential, _ := endpoint.NewVLESSCredential("11111111-1111-4111-8111-111111111111")
	tls, _ := endpoint.NewTLS("front.example.com", false)
	xhttpTransport, _ := endpoint.NewXHTTPTransport("/xhttp", "front.example.com", "stream-one")
	sourceID, _ := endpoint.NewSourceID("shared")
	records := make([]endpoint.Record, 0, tcp+xhttp)
	add := func(configuration endpoint.Configuration, name string) {
		id, _ := endpoint.NewRecordID(name)
		provenance, _ := endpoint.NewProvenance(sourceID, id)
		record, err := endpoint.NewRecord(configuration, provenance)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	for i := 0; i < xhttp; i++ {
		address, _ := endpoint.NewAddress("xhttp.example.com", 20000+i)
		configuration, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, address, credential, xhttpTransport, tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
		if err != nil {
			t.Fatal(err)
		}
		add(configuration, fmt.Sprintf("xhttp-%d", i))
	}
	for i := 0; i < tcp; i++ {
		address, _ := endpoint.NewAddress("edge.example.com", 1000+i)
		configuration, err := endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, credential, endpoint.NewTCPTransport(), tls)
		if err != nil {
			t.Fatal(err)
		}
		add(configuration, fmt.Sprintf("tcp-%d", i))
	}
	return records
}

func testTarget(t *testing.T, id, url string) observation.HTTPTarget {
	t.Helper()
	targetID, err := observation.NewTargetID(id)
	if err != nil {
		t.Fatal(err)
	}
	target, err := observation.NewHTTPTarget(targetID, url, 204, time.Second, observation.HTTPOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// maxRequeueSpacing is the longest interval between two reconciliations of
// one Gateway: the evidence cadence plus the controller's maximum jitter.
func maxRequeueSpacing() time.Duration {
	cadence := EvidenceCadence()
	return cadence + cadence*RequeueJitterPermille/1000
}

// probeHarness replays the operator's probe rounds and the real M5 selector
// with a fake clock that advances while each probe runs, a controlled runner
// and simulated publication, without a store, native engine or Kubernetes.
// Like Run, a Gateway's M5 state and published selection change only when its
// reserved decision is published.
type probeHarness struct {
	t        *testing.T
	pipeline *Pipeline
	pool     types.UID
	name     string
	profile  artifact.Profile
	target   observation.HTTPTarget
	records  []endpoint.Record
	index    map[string]int
	topN     int
	now      time.Time
	healthy  func(int) bool
	latency  func(int) time.Duration
	// failCost is how long an unsuccessful probe occupies its slot.
	failCost func(int) time.Duration
	history  map[string][]observation.Observation
	states   map[types.UID]*selection.State
	// selected is each Gateway's externally published selection (its output).
	selected map[types.UID][]string
	// publish injects the outcome of the next publication per Gateway.
	publish map[types.UID]publishMode
	// staged is a Gateway's pending M5 checkpoint and the output it belongs to;
	// like the store, it is promoted only when that output is what is published.
	staged       map[types.UID]*selection.State
	stagedOutput map[types.UID][]string
	// attempted is the output an uncertain write would have published.
	attempted map[types.UID][]string
	// receiptUnreadable makes a Gateway's receipt read fail, so its
	// reconciliation stops before planning, as Reconcile does.
	receiptUnreadable map[types.UID]bool
	// managed models generation publication: the current output is the
	// generation the status records, which a failed operation does not move.
	managed bool
	// refresh is the pool refresh interval passed to the scheduler; zero
	// leaves the round window at the evidence cadence.
	refresh time.Duration
	// writes records every publisher write with a changed artifact.
	writes      map[types.UID][][]string
	refused     map[types.UID]bool
	constrained map[types.UID]bool
	overloaded  bool
}

func newHarness(t *testing.T, name string, profile artifact.Profile, records []endpoint.Record) *probeHarness {
	h := &probeHarness{
		t: t, pool: "pool", name: name, profile: profile, records: records, topN: 1,
		target:      testTarget(t, "target-"+name, "https://example.com/"+name),
		now:         time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC),
		healthy:     func(int) bool { return true },
		latency:     func(int) time.Duration { return 100 * time.Millisecond },
		failCost:    func(int) time.Duration { return 5 * time.Second },
		history:     map[string][]observation.Observation{},
		states:      map[types.UID]*selection.State{},
		selected:    map[types.UID][]string{},
		publish:     map[types.UID]publishMode{},
		refused:     map[types.UID]bool{},
		constrained: map[types.UID]bool{},
	}
	h.staged, h.stagedOutput, h.attempted = map[types.UID]*selection.State{}, map[types.UID][]string{}, map[types.UID][]string{}
	h.receiptUnreadable, h.writes = map[types.UID]bool{}, map[types.UID][][]string{}
	h.pipeline = &Pipeline{now: func() time.Time { return h.now }}
	h.index = make(map[string]int, len(records))
	for i, record := range records {
		h.index[record.ID().String()] = i
	}
	return h
}

func (h *probeHarness) key() probeContextKey {
	return newProbeContextKey(h.pool, h.name, h.profile, h.target)
}

func (h *probeHarness) id(index int) string { return h.records[index].ID().String() }

func (h *probeHarness) evidenceKey(record endpoint.Record) observation.Key {
	h.t.Helper()
	vantage, _ := observation.NewVantageID("test")
	connection, err := observation.NewConnectionRef(record.Identity())
	if err != nil {
		h.t.Fatal(err)
	}
	key, err := observation.NewKey(connection, h.target.Ref(), vantage, observation.KindHTTPGet, h.profile)
	if err != nil {
		h.t.Fatal(err)
	}
	return key
}

// Execute is the controlled runner: each probe advances the fake clock by its
// cost and completes at the new time. The round runs one probe at a time, a
// conservative model of the operator's per-target concurrency.
func (h *probeHarness) Execute(_ context.Context, record endpoint.Record, _ observation.HTTPTarget) (observation.Observation, error) {
	position := h.index[record.ID().String()]
	params := observation.Params{Key: h.evidenceKey(record), Outcome: observation.OutcomeTimeout}
	cost := h.failCost(position)
	if h.healthy(position) {
		cost = h.latency(position)
		params.Outcome, params.StatusCode = observation.OutcomeSuccess, 204
	}
	params.StartedAt = h.now
	h.now = h.now.Add(cost)
	params.CompletedAt, params.Duration = h.now, cost
	return observation.New(params)
}

// publishMode injects one publication outcome at the boundaries Apply reports.
type publishMode uint8

const (
	publishOK publishMode = iota
	// publishBefore fails before any external write (render, validation, a
	// definite publisher refusal).
	publishBefore
	// publishReadbackFail writes the output, then the receipt read fails.
	publishReadbackFail
	// publishCommitFail writes and confirms the output, then the durable
	// checkpoint commit fails.
	publishCommitFail
	// publishUncertainWritten fails ambiguously after the write took effect.
	publishUncertainWritten
	// publishUncertainLost fails ambiguously without the write taking effect.
	publishUncertainLost
)

var sequentialProbes = probe.ScheduleConfig{Concurrency: 1, Queue: 256, PerEndpoint: 1, PerTarget: 1, MaxJobs: probe.MaxSchedulerJobs}

// reconcile follows Run for one Gateway: the shared round (if due), evidence
// recording, demand touch, the M5 decision, reservation (with constrained
// re-planning on refusal), simulated publication, then commit or release. It
// returns the probe jobs that actually executed.
func (h *probeHarness) reconcile(gateway types.UID) []probe.Job {
	h.t.Helper()
	key := h.key()
	// Like Run, settle an unresolved write from the actual current output
	// before the round, so the round maintains the output that is current.
	resolved := false
	h.pipeline.resolveUncertain(key, gateway, func(artifact.Receipt) (bool, error) {
		if h.receiptUnreadable[gateway] {
			return false, errors.New("synthetic receipt read failure")
		}
		resolved = true
		return h.attempted[gateway] != nil && slices.Equal(h.selected[gateway], h.attempted[gateway]), nil
	})
	if resolved {
		delete(h.attempted, gateway)
	}
	var executed []probe.Job
	if round, due := h.pipeline.budgetedJobs(key, h.records, h.target, h.profile, h.now, h.refresh); due {
		if jobs := len(round.explore) + len(round.maintain); jobs > roundBudget(key) {
			h.t.Fatalf("round of %d jobs exceeds budget %d", jobs, roundBudget(key))
		}
		result, err := h.pipeline.runRound(context.Background(), h, sequentialProbes, round)
		if err != nil {
			h.t.Fatal(err)
		}
		h.pipeline.recordProbeResults(key, result.jobs, result.results)
		for index, value := range result.results {
			if value.Err == nil {
				id := result.jobs[index].Record.ID().String()
				h.history[id] = append(h.history[id], value.Observation)
				executed = append(executed, result.jobs[index])
			} else if !errors.Is(value.Err, errExplorationDeferred) {
				h.t.Fatalf("unexpected probe infrastructure error: %v", value.Err)
			}
		}
		h.overloaded = h.now.Sub(result.maintenanceStart) > evidencePolicy().Freshness
		h.pipeline.completeRound(round, h.overloaded)
	}
	h.pipeline.touchDemand(key, gateway, h.now)
	h.refused[gateway], h.constrained[gateway] = false, false
	if h.receiptUnreadable[gateway] {
		// Reconcile reads the current receipt first and stops here.
		return executed
	}
	// The store promotes a staged checkpoint only when its output is current.
	if staged := h.staged[gateway]; staged != nil && slices.Equal(h.selected[gateway], h.stagedOutput[gateway]) {
		h.states[gateway] = staged
	}
	delete(h.staged, gateway)
	decision := h.decide(gateway, nil)
	if len(decision.Selected) == 0 {
		h.pipeline.observeVerdicts(key, decision)
		return executed
	}
	reservation, err := h.pipeline.reserveSelection(key, gateway, decision, h.now)
	if err != nil {
		h.constrained[gateway] = true
		decision = h.decide(gateway, h.pipeline.maintained(key))
		if len(decision.Selected) == 0 {
			h.pipeline.observeVerdicts(key, decision)
			return executed
		}
		if reservation, err = h.pipeline.reserveSelection(key, gateway, decision, h.now); err != nil {
			h.refused[gateway] = true
			return executed
		}
	}
	ids := make([]string, 0, len(decision.Selected))
	for _, record := range decision.Selected {
		ids = append(ids, record.ID().String())
	}
	next := decision.Next
	mode := h.publish[gateway]
	changed := !slices.Equal(slices.Sorted(slices.Values(ids)), slices.Sorted(slices.Values(h.selected[gateway])))
	if mode != publishBefore && !changed {
		// Equal validated bytes: the publisher writes nothing and succeeds.
		mode = publishOK
	}
	if mode != publishBefore && changed {
		h.writes[gateway] = append(h.writes[gateway], ids)
	}
	switch mode {
	case publishBefore:
		h.pipeline.releaseSelection(reservation)
	case publishOK:
		h.selected[gateway] = ids
		h.pipeline.commitSelection(reservation)
		h.states[gateway] = &next
	default:
		// The checkpoint is staged before the write. A BYO write that took
		// effect replaces the output Secret. A managed write only creates a
		// generation Secret: on this error path the Gateway's status does not
		// record it, so the current generation does not change.
		h.staged[gateway], h.stagedOutput[gateway] = &next, ids
		if mode != publishUncertainLost && !h.managed {
			h.selected[gateway] = ids
		}
		h.attempted[gateway] = ids
		h.pipeline.holdUncertain(reservation, artifact.Receipt{}, h.now)
	}
	return executed
}

// decide plans like reconcile.Plan: evidence of records maintained rejects is
// withheld. It does not commit the Gateway's M5 state.
func (h *probeHarness) decide(gateway types.UID, maintained func(endpoint.Record) bool) selection.Decision {
	h.t.Helper()
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	policy.TopN = maintainableTopN(h.key(), h.topN)
	vantage, _ := observation.NewVantageID("test")
	context, err := selection.NewContext(h.target.Ref(), vantage, observation.KindHTTPGet, h.profile)
	if err != nil {
		h.t.Fatal(err)
	}
	candidates := make([]selection.Candidate, 0, len(h.records))
	for _, record := range h.records {
		candidate := selection.Candidate{Record: record}
		id := record.ID().String()
		h.history[id] = slices.DeleteFunc(h.history[id], func(value observation.Observation) bool {
			return h.now.Sub(value.CompletedAt()) > policy.EvidenceWindow
		})
		if values := h.history[id]; len(values) > 0 && (maintained == nil || maintained(record)) {
			summary, err := observation.Summarize(h.evidenceKey(record), values, h.now, policy.EvidenceWindow, policy.Freshness)
			if err != nil {
				h.t.Fatal(err)
			}
			candidate.Evidence = &summary
		}
		candidates = append(candidates, candidate)
	}
	decision, err := selection.Select("k8s_"+string(gateway), context, policy, candidates, h.states[gateway], h.now)
	if err != nil {
		h.t.Fatal(err)
	}
	return decision
}

func (h *probeHarness) state() *probeState { return h.pipeline.probes[h.key()] }

// fastest makes one endpoint clearly better than the rest, so adaptive
// re-ranking cannot explain a selection change.
func fastest(index int) func(int) time.Duration {
	return func(i int) time.Duration {
		if i == index {
			return 20 * time.Millisecond
		}
		return 400 * time.Millisecond
	}
}

func roundJobs(round probeRound, due bool) int {
	if !due {
		return 0
	}
	return len(round.explore) + len(round.maintain)
}

func jobIDs(jobs []probe.Job) map[string]int {
	ids := map[string]int{}
	for _, job := range jobs {
		ids[job.Record.ID().String()]++
	}
	return ids
}

func TestProbeCadenceIsDerivedFromEvidencePolicy(t *testing.T) {
	jitter := func(d time.Duration) time.Duration { return d + d*RequeueJitterPermille/1000 }
	// Keeps MinSamples samples inside the evidence window when every round is
	// as late as the controller allows.
	sustains := func(policy selection.Policy, spacing time.Duration) bool {
		return time.Duration(policy.MinSamples-1)*spacing <= policy.EvidenceWindow
	}
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	policy.MinSamples, policy.EvidenceWindow, policy.Freshness = 3, 30*time.Minute, 30*time.Minute
	cadence := probeCadence(policy)
	if !sustains(policy, jitter(cadence)) || time.Duration(policy.MinSamples)*jitter(cadence) > policy.EvidenceWindow {
		t.Fatalf("cadence %s cannot keep %d samples in %s under jitter", cadence, policy.MinSamples, policy.EvidenceWindow)
	}
	// The coupling this replaces: a 14m refresh fits nominally but not with
	// the controller's real positive jitter.
	if !sustains(policy, 14*time.Minute) || sustains(policy, jitter(14*time.Minute)) {
		t.Fatal("14m boundary case no longer demonstrates the jitter hazard")
	}
	if cadence >= 14*time.Minute {
		t.Fatalf("cadence %s would reproduce the jitter hazard", cadence)
	}
	// Freshness bounds the round window, so an evaluation never reads a
	// round older than Freshness.
	defaults := evidencePolicy()
	if got := probeCadence(defaults); got > defaults.Freshness || !sustains(defaults, jitter(got)) {
		t.Fatalf("default cadence %s", got)
	}
	// A degenerate policy cannot create a hot loop.
	policy.EvidenceWindow, policy.Freshness = time.Second, time.Second
	if got := probeCadence(policy); got < minProbeCadence {
		t.Fatalf("cadence %s below the floor", got)
	}
}

// Rounds at the worst-case requeue spacing keep the selection fed by fresh
// M5 evidence for a whole day, for default and named profiles alike, without
// any source refresh (the inventory never changes).
func TestEvidenceMaintainedAtWorstCaseRequeueWithoutSourceRefresh(t *testing.T) {
	for _, name := range []string{"default", "alpha"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name, artifact.Mihomo11931, testRecords(t, 300, 0))
			spacing := maxRequeueSpacing()
			for elapsed := time.Duration(0); elapsed < 24*time.Hour; elapsed += spacing {
				h.reconcile("gateway")
				if len(h.selected["gateway"]) == 0 {
					t.Fatalf("after %s: no endpoint has sufficient M5 evidence", elapsed)
				}
				h.now = h.now.Add(spacing)
			}
		})
	}
}

// One failed observation is M5 evidence, not a scheduler verdict: the
// endpoint stays maintained and selected, and recovers in place.
func TestSingleFailureKeepsEvidenceMaintenance(t *testing.T) {
	for _, name := range []string{"default", "alpha"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name, artifact.Mihomo11931, testRecords(t, 120, 0))
			h.latency = fastest(0)
			for range 4 {
				h.reconcile("gateway")
				h.now = h.now.Add(maxRequeueSpacing())
			}
			incumbent := h.selected["gateway"]
			if len(incumbent) != 1 {
				t.Fatalf("selected=%d", len(incumbent))
			}
			victim := h.index[incumbent[0]]
			h.healthy = func(i int) bool { return i != victim }
			if jobIDs(h.reconcile("gateway"))[incumbent[0]] == 0 {
				t.Fatal("incumbent not probed in the failing round")
			}
			h.healthy = func(int) bool { return true }
			for round := range 10 {
				h.now = h.now.Add(maxRequeueSpacing())
				if jobIDs(h.reconcile("gateway"))[incumbent[0]] == 0 {
					t.Fatalf("round %d after one failure: incumbent no longer maintained", round)
				}
				if !slices.Equal(h.selected["gateway"], incumbent) {
					t.Fatalf("round %d: one failure changed the selection", round)
				}
			}
		})
	}
}

// Reaching M5's configured failure streak, not the first failure, moves the
// selection; maintained standbys make the replacement immediate.
func TestFailureStreakHandsSelectionToM5(t *testing.T) {
	h := newHarness(t, "alpha", artifact.SingBox1141, testRecords(t, 60, 0))
	h.latency = fastest(0)
	for range 4 {
		h.reconcile("gateway")
		h.now = h.now.Add(maxRequeueSpacing())
	}
	incumbent := h.selected["gateway"][0]
	victim := h.index[incumbent]
	h.healthy = func(i int) bool { return i != victim }
	streak := evidencePolicy().FailureStreak
	for round := 1; round <= streak+3; round++ {
		jobs := h.reconcile("gateway")
		if round <= streak && jobIDs(jobs)[incumbent] == 0 {
			t.Fatalf("failure %d: scheduler dropped the incumbent before M5 decided", round)
		}
		switch {
		case round < streak && !slices.Equal(h.selected["gateway"], []string{incumbent}):
			t.Fatalf("failure %d below the streak changed the selection", round)
		case round >= streak && (len(h.selected["gateway"]) != 1 || h.selected["gateway"][0] == incumbent):
			t.Fatalf("failure %d: M5 failure streak did not replace the incumbent", round)
		}
		h.now = h.now.Add(maxRequeueSpacing())
	}
}

// A challenger found by exploration after the cohort is full becomes selected
// and stays maintained; the selection does not roll back and churn.
func TestSelectedChallengerStaysMaintained(t *testing.T) {
	for _, name := range []string{"default", "alpha"} {
		t.Run(name, func(t *testing.T) {
			records := testRecords(t, 400, 0)
			h := newHarness(t, name, artifact.Mihomo11931, records)
			fast := 350
			h.latency = func(i int) time.Duration {
				if i == fast {
					return 5 * time.Millisecond
				}
				return 400 * time.Millisecond
			}
			challenger := h.id(fast)
			found := -1
			for round := 0; round < 2*len(records) && found < 0; round++ {
				h.reconcile("gateway")
				if slices.Contains(h.selected["gateway"], challenger) {
					found = round
				}
				h.now = h.now.Add(maxRequeueSpacing())
			}
			if found < 0 {
				t.Fatal("exploration never promoted the challenger into the selection")
			}
			if len(h.state().cohort) < cohortCapacity(h.key(), evidencePolicy()) {
				t.Fatal("cohort was not full when the challenger was found; scenario is too weak")
			}
			// Several full exploration laps afterwards.
			for round := 0; round < len(records); round++ {
				if jobIDs(h.reconcile("gateway"))[challenger] == 0 {
					t.Fatalf("round %d: selected challenger fell out of evidence maintenance", round)
				}
				if !slices.Equal(h.selected["gateway"], []string{challenger}) {
					t.Fatalf("round %d: selection churned away from the challenger", round)
				}
				h.now = h.now.Add(maxRequeueSpacing())
			}
		})
	}
}

// TopN is an upper bound (M5 reports a shortfall rather than filling it). A
// TopN above the historical eight-endpoint working set is maintained in full;
// a TopN above the context's maintainable cohort is bounded to it and stable.
func TestTopNSelectionIsMaintained(t *testing.T) {
	for _, tc := range []struct {
		name string
		topN int
	}{{"default", 20}, {"alpha", 20}, {"default", 10_000}, {"alpha", 10_000}} {
		t.Run(fmt.Sprintf("%s-%d", tc.name, tc.topN), func(t *testing.T) {
			h := newHarness(t, tc.name, artifact.Mihomo11931, testRecords(t, 300, 0))
			h.topN = tc.topN
			want := min(tc.topN, cohortCapacity(h.key(), evidencePolicy()))
			if tc.topN == 20 && want != 20 {
				t.Fatalf("maintainable bound %d below TopN 20", want)
			}
			var stable []string
			for round := 0; round < 120; round++ {
				jobs := jobIDs(h.reconcile("gateway"))
				selected := h.selected["gateway"]
				if len(selected) > want {
					t.Fatalf("round %d: selected %d beyond the maintainable bound %d", round, len(selected), want)
				}
				if round >= 20 {
					if len(selected) != want {
						t.Fatalf("round %d: selected %d want %d", round, len(selected), want)
					}
					for _, id := range selected {
						if jobs[id] == 0 {
							t.Fatalf("round %d: selected endpoint not maintained", round)
						}
					}
					sorted := slices.Sorted(slices.Values(selected))
					if stable == nil {
						stable = sorted
					} else if !slices.Equal(stable, sorted) {
						t.Fatalf("round %d: selection churned without a health change", round)
					}
				}
				h.now = h.now.Add(maxRequeueSpacing())
			}
		})
	}
}

// Equivalent default-profile Gateways share one round and one exploration
// cursor; their M5 selection states stay independent.
func TestDefaultProfileGatewaysShareExploration(t *testing.T) {
	records := testRecords(t, 300, 0)
	h := newHarness(t, "default", artifact.Mihomo11931, records)
	gateways := []types.UID{"gateway-a", "gateway-b", "gateway-c"}
	explored := map[string]int{}
	for round := 0; round < 40; round++ {
		spent := 0
		cohort := map[string]bool{}
		if state := h.state(); state != nil {
			for _, id := range state.cohort {
				cohort[id] = true
			}
		}
		for _, gateway := range gateways {
			jobs := jobIDs(h.reconcile(gateway))
			if len(jobs) > 0 {
				spent++
			}
			for id := range jobs {
				if !cohort[id] {
					explored[id]++
				}
			}
		}
		if spent != 1 {
			t.Fatalf("round %d: %d Gateways spent the shared round", round, spent)
		}
		h.now = h.now.Add(maxRequeueSpacing())
	}
	for id, count := range explored {
		if count > 1 {
			t.Fatalf("endpoint %s explored %d times: exploration restarted its prefix", id, count)
		}
	}
	if len(explored) <= roundBudget(h.key()) {
		t.Fatalf("exploration did not advance beyond one batch: %d", len(explored))
	}
	for _, gateway := range gateways {
		if h.states[gateway] == nil || h.states[gateway].Scope != "k8s_"+string(gateway) {
			t.Fatal("Gateway selection state is not Gateway-scoped")
		}
	}
}

// Gateways in one context keep their own anti-flap decisions, and the shared
// cohort maintains each of them.
func TestGatewaySelectionsStayIndependentInSharedContext(t *testing.T) {
	h := newHarness(t, "default", artifact.Mihomo11931, testRecords(t, 10, 0))
	h.latency = func(i int) time.Duration {
		if i == 0 {
			return 100 * time.Millisecond
		}
		return 300 * time.Millisecond
	}
	for range 4 {
		h.reconcile("gateway-a")
		h.now = h.now.Add(maxRequeueSpacing())
	}
	first := h.selected["gateway-a"]
	// Endpoint 1 becomes marginally faster: within M5's required improvement,
	// so gateway-a keeps its incumbent while a new Gateway picks endpoint 1.
	h.latency = func(i int) time.Duration {
		switch i {
		case 0:
			return 100 * time.Millisecond
		case 1:
			return 98 * time.Millisecond
		}
		return 300 * time.Millisecond
	}
	// Let the new latency fill gateway-a's evidence window before
	// gateway-b makes its first decision.
	for range 10 {
		h.reconcile("gateway-a")
		h.now = h.now.Add(maxRequeueSpacing())
	}
	for range 30 {
		h.reconcile("gateway-a")
		h.reconcile("gateway-b")
		h.now = h.now.Add(maxRequeueSpacing())
	}
	if !slices.Equal(h.selected["gateway-a"], first) || !slices.Equal(h.selected["gateway-b"], []string{h.id(1)}) {
		t.Fatalf("selections a=%v b=%v", h.selected["gateway-a"], h.selected["gateway-b"])
	}
	state := h.state()
	if !state.protected(first[0]) || !state.protected(h.id(1)) {
		t.Fatal("shared cohort does not maintain both Gateway selections")
	}
}

func TestProbeContextFollowsTargetRevision(t *testing.T) {
	for _, name := range []string{"default", "alpha"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name, artifact.Mihomo11931, testRecords(t, 50, 0))
			if len(h.reconcile("gateway")) == 0 {
				t.Fatal("first round empty")
			}
			if len(h.reconcile("gateway")) != 0 {
				t.Fatal("round repeated within the window")
			}
			old := h.key()
			// Same target ID, new revision (changed URL): a fresh context
			// and, as in the store, separate evidence keys.
			h.target = testTarget(t, "target-"+name, "https://example.com/changed")
			h.history = map[string][]observation.Observation{}
			if h.key() == old {
				t.Fatal("target revision is not part of the probe context")
			}
			if len(h.reconcile("gateway")) == 0 {
				t.Fatal("new target revision inherited the old round")
			}
			if _, ok := h.pipeline.probes[old].demand["gateway"]; ok {
				t.Fatal("Gateway demand stayed in the replaced context")
			}
			// A different target is a different context too.
			h.target = testTarget(t, "target-other", "https://example.com/other")
			h.history = map[string][]observation.Observation{}
			if len(h.reconcile("gateway")) == 0 {
				t.Fatal("different target shares the old round")
			}
		})
	}
}

func TestProbeContextFollowsProfileIncarnation(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "targets", Namespace: "test"}, Data: map[string][]byte{"alpha": []byte("https://example.com/alpha")}}
	pipeline := &Pipeline{reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()}
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "test", UID: "pool-uid"}}
	spec := egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "targets", Key: "alpha"}}
	targetFor := func(epoch int64) observation.HTTPTarget {
		pool.Status.Profiles = []egressv1alpha1.ProfileStatus{{Name: "alpha", FirstObservedGeneration: epoch}}
		target, _, _, err := pipeline.target(context.Background(), pool, "alpha", spec)
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	records := testRecords(t, 20, 0)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	old, recreated := targetFor(1), targetFor(2)
	oldKey := newProbeContextKey(pool.UID, "alpha", artifact.Mihomo11931, old)
	newKey := newProbeContextKey(pool.UID, "alpha", artifact.Mihomo11931, recreated)
	if oldKey == newKey {
		t.Fatal("recreated profile shares the old probe context")
	}
	if roundJobs(pipeline.budgetedJobs(oldKey, records, old, artifact.Mihomo11931, now, 0)) == 0 {
		t.Fatal("old context empty")
	}
	if roundJobs(pipeline.budgetedJobs(newKey, records, recreated, artifact.Mihomo11931, now, 0)) == 0 {
		t.Fatal("recreated profile inherited the old round")
	}
	if len(pipeline.probes[newKey].cohort) != 0 || pipeline.probes[newKey].cursor == 0 {
		t.Fatal("recreated profile did not start its own cohort and cursor")
	}
}

func TestEngineContextsAreIndependent(t *testing.T) {
	for _, name := range []string{"default", "alpha"} {
		t.Run(name, func(t *testing.T) {
			records := testRecords(t, 30, 0)
			pipeline := &Pipeline{}
			target := testTarget(t, "target-"+name, "https://example.com/"+name)
			now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
			mihomoKey := newProbeContextKey("pool", name, artifact.Mihomo11931, target)
			singKey := newProbeContextKey("pool", name, artifact.SingBox1141, target)
			if roundJobs(pipeline.budgetedJobs(mihomoKey, records, target, artifact.Mihomo11931, now, 0)) == 0 {
				t.Fatal("mihomo empty")
			}
			if roundJobs(pipeline.budgetedJobs(mihomoKey, records, target, artifact.Mihomo11931, now, 0)) != 0 {
				t.Fatal("mihomo round repeated")
			}
			if roundJobs(pipeline.budgetedJobs(singKey, records, target, artifact.SingBox1141, now, 0)) == 0 {
				t.Fatal("sing-box starved by mihomo context")
			}
			otherPool := newProbeContextKey("other-pool", name, artifact.Mihomo11931, target)
			if roundJobs(pipeline.budgetedJobs(otherPool, records, target, artifact.Mihomo11931, now, 0)) == 0 {
				t.Fatal("another pool shares the context")
			}
		})
	}
}

// More than one batch of exact-engine-incompatible endpoints ahead of the
// compatible ones never consumes the round, and exploration is deterministic.
func TestIncompatiblePrefixDoesNotConsumeProbeCapacity(t *testing.T) {
	for _, name := range []string{"default", "alpha"} {
		t.Run(name, func(t *testing.T) {
			records := testRecords(t, 40, 150)
			run := func() [][]string {
				h := newHarness(t, name, artifact.SingBox1141, records)
				var rounds [][]string
				for round := 0; round < 12; round++ {
					jobs := h.reconcile("gateway")
					ids := make([]string, 0, len(jobs))
					for _, job := range jobs {
						if job.Record.Configuration().Transport().Kind() != endpoint.TransportTCP {
							t.Fatal("incompatible endpoint consumed probe capacity")
						}
						ids = append(ids, job.Record.ID().String())
					}
					if round == 0 && len(h.selected["gateway"]) == 0 {
						t.Fatal("compatible endpoints starved behind the incompatible prefix")
					}
					if len(h.state().cohort) > cohortCapacity(h.key(), evidencePolicy()) {
						t.Fatal("cohort exceeded its bound")
					}
					rounds = append(rounds, ids)
					h.now = h.now.Add(maxRequeueSpacing())
				}
				return rounds
			}
			first, second := run(), run()
			for round := range first {
				if !slices.Equal(first[round], second[round]) {
					t.Fatalf("round %d is not deterministic", round)
				}
			}
		})
	}
}

func TestProbeStateIsBoundedAndExpires(t *testing.T) {
	h := newHarness(t, "alpha", artifact.Mihomo11931, testRecords(t, 10, 0))
	h.reconcile("gateway")
	h.reconcile("gone")
	if len(h.pipeline.probes) != 1 || len(h.state().demand) != 2 {
		t.Fatalf("states=%d demand=%d", len(h.pipeline.probes), len(h.state().demand))
	}
	// A deleted Gateway's demand expires with the evidence window.
	h.now = h.now.Add(evidencePolicy().EvidenceWindow + time.Minute)
	h.reconcile("gateway")
	if _, ok := h.state().demand["gone"]; ok || len(h.state().demand) != 1 {
		t.Fatal("deleted Gateway demand was not released")
	}
	// An unused context is released after the TTL.
	h.now = h.now.Add(probeStateTTL + time.Hour)
	other := newHarness(t, "beta", artifact.Mihomo11931, h.records)
	other.pipeline, other.now = h.pipeline, h.now
	other.reconcile("gateway")
	if _, ok := h.pipeline.probes[h.key()]; ok || len(h.pipeline.probes) != 1 {
		t.Fatalf("stale probe context was not released: %d", len(h.pipeline.probes))
	}
}

// checkDemandInvariant asserts that protected demand fits the cohort and is
// maintained: every selected or uncertain endpoint M5 has not rejected is a
// cohort member.
func (h *probeHarness) checkDemandInvariant() {
	h.t.Helper()
	state := h.state()
	members := map[string]bool{}
	for _, id := range state.cohort {
		members[id] = true
	}
	union := map[string]bool{}
	for gateway, demand := range state.demand {
		if demand.pending {
			h.t.Fatalf("pending reservation of %s survived its reconciliation", gateway)
		}
		for _, id := range append(slices.Clone(demand.selected), demand.uncertain...) {
			if state.rejected[id] {
				continue
			}
			union[id] = true
			if !members[id] {
				h.t.Fatalf("protected demand of %s is not maintained", gateway)
			}
		}
	}
	if len(union) > cohortCapacity(h.key(), evidencePolicy()) || len(state.cohort) > cohortCapacity(h.key(), evidencePolicy()) {
		h.t.Fatalf("demand %d or cohort %d exceeds capacity", len(union), len(state.cohort))
	}
}

// checkOutputMaintained asserts that the round just executed actually
// completed a probe of every endpoint the Gateway's output may rely on: its
// published output after the reconciliation and every further plausible
// output the caller passes, such as the output current before the round or an
// attempted selection whose write is unresolved. executed must be the jobs
// that completed in that round, as returned by reconcile. Endpoints whose
// latest M5 explanation is a failure streak or unreliability are exempt,
// because their demand no longer reserves capacity.
func (h *probeHarness) checkOutputMaintained(gateway types.UID, executed []probe.Job, outputs ...[]string) {
	h.t.Helper()
	probed := jobIDs(executed)
	rejected := map[string]bool{}
	if state := h.state(); state != nil {
		rejected = state.rejected
	}
	checked := 0
	for index, output := range append([][]string{h.selected[gateway]}, outputs...) {
		label := "published output"
		if index > 0 {
			label = fmt.Sprintf("plausible output %d", index)
		}
		for _, id := range output {
			if rejected[id] {
				continue
			}
			checked++
			if probed[id] == 0 {
				h.t.Fatalf("%s: endpoint %d of its %s was not probed in the round", gateway, h.index[id], label)
			}
		}
	}
	if checked == 0 {
		h.t.Fatalf("%s: no output to check; the scenario is vacuous", gateway)
	}
}

// sharedHarness warms up two Gateways that both publish the whole cohort of a
// context (topN equals the cohort capacity) over capacity+extra endpoints.
func sharedHarness(t *testing.T, name string, extra int) (*probeHarness, int, func(...types.UID)) {
	t.Helper()
	probeKey := newProbeContextKey("pool", name, artifact.Mihomo11931, testTarget(t, "target-"+name, "https://example.com/"+name))
	capacity := cohortCapacity(probeKey, evidencePolicy())
	h := newHarness(t, name, artifact.Mihomo11931, testRecords(t, capacity+extra, 0))
	h.topN = capacity
	h.latency = func(int) time.Duration { return 400 * time.Millisecond }
	h.healthy = func(i int) bool { return i < capacity }
	step := func(gateways ...types.UID) {
		t.Helper()
		for _, gateway := range gateways {
			h.reconcile(gateway)
		}
		h.checkDemandInvariant()
		h.now = h.now.Add(maxRequeueSpacing())
	}
	for range 20 {
		step("gateway-a", "gateway-b")
	}
	for _, gateway := range []types.UID{"gateway-a", "gateway-b"} {
		if len(h.selected[gateway]) != capacity || h.constrained[gateway] {
			t.Fatalf("%s warm-up selected %d of %d", gateway, len(h.selected[gateway]), capacity)
		}
	}
	return h, capacity, step
}

// Both Gateways publish the full cohort and an outside endpoint becomes
// clearly faster while every member stays healthy. Maintaining both the
// current output and the attempted one would exceed the cohort, so the
// transition is refused before any write: no publisher write happens even
// though every write would fail ambiguously, the outputs stay unchanged and
// maintained, and each Gateway chooses among maintained endpoints. Expired
// demand of a stopped Gateway is released, but a Gateway's own full output
// still leaves no transition headroom.
func TestInfeasibleTransitionIsRefusedBeforeWrite(t *testing.T) {
	for _, name := range []string{"alpha", "default"} {
		t.Run(name, func(t *testing.T) {
			h, capacity, _ := sharedHarness(t, name, 12)
			// Warm-up publications are history; the scenario measures writes
			// from here on.
			baseline := map[types.UID]int{"gateway-a": len(h.writes["gateway-a"]), "gateway-b": len(h.writes["gateway-b"])}
			challenger := capacity + 6
			h.healthy = func(i int) bool { return i < capacity || i == challenger }
			h.latency = func(i int) time.Duration {
				if i == challenger {
					return 20 * time.Millisecond
				}
				return 400 * time.Millisecond
			}
			gateways := []types.UID{"gateway-a", "gateway-b"}
			before := map[types.UID][]string{}
			for _, gateway := range gateways {
				before[gateway] = slices.Sorted(slices.Values(h.selected[gateway]))
				h.publish[gateway] = publishUncertainLost
			}
			round := func(active []types.UID) bool {
				constrained := true
				for _, gateway := range active {
					executed := h.reconcile(gateway)
					if gateway == active[0] {
						h.checkOutputMaintained(gateway, executed)
					}
					constrained = constrained && h.constrained[gateway]
				}
				h.checkDemandInvariant()
				for _, gateway := range gateways {
					if got := len(h.writes[gateway]); got != baseline[gateway] {
						t.Fatalf("%s wrote %d artifacts after warm-up; want none", gateway, got-baseline[gateway])
					}
					if h.state().demand[gateway].uncertain != nil {
						t.Fatalf("%s holds an uncertain write the cohort could not maintain", gateway)
					}
					if !slices.Equal(before[gateway], slices.Sorted(slices.Values(h.selected[gateway]))) {
						t.Fatalf("%s output changed", gateway)
					}
				}
				h.now = h.now.Add(maxRequeueSpacing())
				return constrained
			}
			refused := false
			for range 8 {
				refused = round(gateways) || refused
			}
			if !refused {
				t.Fatal("the challenger never needed a refusal; scenario is too weak")
			}
			bLast := h.now.Add(-maxRequeueSpacing())
			for range 16 {
				round(gateways[:1])
			}
			if h.now.Sub(bLast) <= evidencePolicy().EvidenceWindow {
				t.Fatal("scenario did not outlast the evidence window")
			}
			if _, ok := h.state().demand["gateway-b"]; ok {
				t.Fatal("expired Gateway demand still held")
			}
		})
	}
}

// Both Gateways keep reconciling while their whole shared cohort fails. M5
// rejects the failed members, so their retained last-known-good no longer
// reserves capacity: both Gateways first retain the LKG with an empty
// decision, then publish maintained healthy replacements once exploration
// finds them, without deletion, restart or demand expiry.
func TestFailedSharedCohortDoesNotBlockFailover(t *testing.T) {
	for _, name := range []string{"alpha", "default"} {
		t.Run(name, func(t *testing.T) {
			h, capacity, step := sharedHarness(t, name, contextCapacity(name))
			lkg := map[types.UID][]string{"gateway-a": h.selected["gateway-a"], "gateway-b": h.selected["gateway-b"]}
			h.healthy = func(int) bool { return false }
			sawEmpty := false
			for range evidencePolicy().FailureStreak + 3 {
				step("gateway-a", "gateway-b")
				for gateway, published := range lkg {
					if !slices.Equal(slices.Sorted(slices.Values(h.selected[gateway])), slices.Sorted(slices.Values(published))) {
						t.Fatalf("%s changed its output without a healthy replacement", gateway)
					}
				}
				if len(h.decide("gateway-a", nil).Selected) == 0 {
					sawEmpty = true
				}
			}
			if !sawEmpty {
				t.Fatal("failed cohort still produced a selection")
			}
			// Healthy endpoints outside the failed cohort appear.
			h.healthy = func(i int) bool { return i >= capacity }
			for range 40 {
				step("gateway-a", "gateway-b")
			}
			h.reconcile("gateway-a")
			h.reconcile("gateway-b")
			h.checkDemandInvariant()
			for _, gateway := range []types.UID{"gateway-a", "gateway-b"} {
				if len(h.selected[gateway]) == 0 {
					t.Fatalf("%s never replaced its failed cohort", gateway)
				}
				for _, id := range h.selected[gateway] {
					if h.index[id] < capacity {
						t.Fatalf("%s still publishes a failed endpoint", gateway)
					}
					if !slices.Contains(h.state().cohort, id) {
						t.Fatalf("%s publishes an unmaintained replacement", gateway)
					}
				}
			}
			h.now = h.now.Add(maxRequeueSpacing())
			executed := jobIDs(h.reconcile("gateway-a"))
			for _, id := range h.selected["gateway-a"] {
				if executed[id] == 0 {
					t.Fatal("published replacement is not probed")
				}
			}
		})
	}
}

func contextCapacity(name string) int {
	return cohortCapacity(probeContextKey{name: name}, evidencePolicy())
}

// When only part of the shared cohort fails, the healthy shared members stay
// protected, selected and maintained while the failed ones are replaced.
func TestPartialSharedCohortFailureKeepsHealthyMembers(t *testing.T) {
	h, capacity, step := sharedHarness(t, "alpha", 24)
	failed := func(i int) bool { return i < capacity/2 }
	h.healthy = func(i int) bool { return !failed(i) }
	for range 30 {
		step("gateway-a", "gateway-b")
		for i := capacity / 2; i < capacity; i++ {
			if !slices.Contains(h.state().cohort, h.id(i)) {
				t.Fatal("a healthy shared member lost maintenance")
			}
			for _, gateway := range []types.UID{"gateway-a", "gateway-b"} {
				if !slices.Contains(h.selected[gateway], h.id(i)) {
					t.Fatalf("%s dropped a healthy shared member", gateway)
				}
			}
		}
	}
	for _, gateway := range []types.UID{"gateway-a", "gateway-b"} {
		for _, id := range h.selected[gateway] {
			if failed(h.index[id]) {
				t.Fatalf("%s still publishes a failed member", gateway)
			}
		}
	}
}

// One failed observation on every shared member does not reach M5 rejection,
// so no protection is released and nothing changes.
func TestTransientSharedFailureKeepsProtection(t *testing.T) {
	h, capacity, step := sharedHarness(t, "alpha", 12)
	cohort := slices.Sorted(slices.Values(h.state().cohort))
	before := slices.Sorted(slices.Values(h.selected["gateway-a"]))
	h.healthy = func(int) bool { return false }
	step("gateway-a", "gateway-b")
	if len(h.state().rejected) != 0 {
		t.Fatal("one failed observation released protection ahead of M5")
	}
	h.healthy = func(i int) bool { return i < capacity }
	for range 6 {
		step("gateway-a", "gateway-b")
	}
	if !slices.Equal(cohort, slices.Sorted(slices.Values(h.state().cohort))) || !slices.Equal(before, slices.Sorted(slices.Values(h.selected["gateway-a"]))) {
		t.Fatal("a transient failure changed the shared cohort or selection")
	}
}

// transitionHarness warms up Gateways with topN 8 on a named context of 40
// healthy endpoints, so the cohort is full of 8 selected members and 16
// unprotected standbys. It returns the challenger index, outside the cohort,
// which becomes clearly faster once makeFast is called.
func transitionHarness(t *testing.T, gateways ...types.UID) (*probeHarness, int, func()) {
	t.Helper()
	h := newHarness(t, "alpha", artifact.Mihomo11931, testRecords(t, 40, 0))
	h.topN = 8
	h.latency = func(int) time.Duration { return 400 * time.Millisecond }
	for range 20 {
		for _, gateway := range gateways {
			h.reconcile(gateway)
		}
		h.now = h.now.Add(maxRequeueSpacing())
	}
	capacity := cohortCapacity(h.key(), evidencePolicy())
	challenger := -1
	for i := len(h.records) - 1; i >= 0; i-- {
		if !slices.Contains(h.state().cohort, h.id(i)) {
			challenger = i
			break
		}
	}
	if len(h.state().cohort) != capacity || challenger < 0 || len(h.selected[gateways[0]]) != h.topN {
		t.Fatalf("warm-up cohort=%d selected=%d", len(h.state().cohort), len(h.selected[gateways[0]]))
	}
	return h, challenger, func() {
		h.latency = func(i int) time.Duration {
			if i == challenger {
				return 20 * time.Millisecond
			}
			return 400 * time.Millisecond
		}
	}
}

// untilHeld reconciles until the Gateway holds an uncertain write and returns
// the output that was current before it.
func (h *probeHarness) untilHeld(gateway types.UID, others ...types.UID) []string {
	h.t.Helper()
	for range 16 {
		previous := slices.Clone(h.selected[gateway])
		executed := h.reconcile(gateway)
		h.checkOutputMaintained(gateway, executed, previous)
		for _, other := range others {
			h.reconcile(other)
		}
		h.checkDemandInvariant()
		h.now = h.now.Add(maxRequeueSpacing())
		if h.state().demand[gateway].uncertain != nil {
			return previous
		}
	}
	h.t.Fatal("no transition was attempted")
	return nil
}

// A feasible transition into a full cohort uses unprotected standby slots, so
// a lost BYO write never costs the current output its maintenance: the
// receipt stays unreadable for longer than Freshness and every round still
// probes the healthy current output and the attempted one. Resolving to the
// current output keeps it fresh, so no stale-driven replacement follows.
func TestLostUncertainWriteKeepsCurrentOutputMaintained(t *testing.T) {
	h, challenger, makeFast := transitionHarness(t, "gateway")
	baseline := len(h.writes["gateway"])
	makeFast()
	h.publish["gateway"] = publishUncertainLost
	previous := h.untilHeld("gateway")
	if got := len(h.writes["gateway"]) - baseline; got != 1 {
		t.Fatalf("writes while establishing the hold = %d, want 1", got)
	}
	attempted := h.state().demand["gateway"].uncertain
	if !slices.Contains(attempted, h.id(challenger)) || !slices.Equal(h.selected["gateway"], previous) {
		t.Fatal("lost write changed the output or attempted another selection")
	}
	if len(h.state().cohort) != cohortCapacity(h.key(), evidencePolicy()) {
		t.Fatal("attempted endpoints were not admitted into standby capacity")
	}
	h.receiptUnreadable["gateway"] = true
	start := h.now
	for h.now.Sub(start) <= evidencePolicy().Freshness+maxRequeueSpacing() {
		executed := h.reconcile("gateway")
		h.checkOutputMaintained("gateway", executed, attempted)
		h.checkDemandInvariant()
		if got := len(h.writes["gateway"]) - baseline; got != 1 {
			t.Fatalf("writes while unresolved = %d, want 1: a further attempt stacked on the unresolved write", got)
		}
		h.now = h.now.Add(maxRequeueSpacing())
	}
	h.receiptUnreadable["gateway"] = false
	h.publish["gateway"] = publishOK
	// Resolution runs before the round, which therefore maintains the actual
	// output; M5 then moves to the challenger by optimisation, not because
	// the incumbents went stale.
	executed := h.reconcile("gateway")
	h.checkOutputMaintained("gateway", executed, previous)
	h.checkDemandInvariant()
	demand := h.state().demand["gateway"]
	if demand.uncertain != nil || !slices.Equal(demand.selected, h.selected["gateway"]) {
		t.Fatal("resolution did not settle demand on the actual output")
	}
	kept := 0
	for _, id := range h.selected["gateway"] {
		if slices.Contains(previous, id) {
			kept++
		}
	}
	if kept < len(previous)-1 {
		t.Fatalf("resolution replaced %d incumbents; only the challenger swap is expected", len(previous)-kept)
	}
}

// An uncertain write that took effect is serviced together with the previous
// output until the receipt is readable; the confirmed new output then stays
// maintained.
func TestAppliedUncertainWriteServicesBothOutputs(t *testing.T) {
	h, challenger, makeFast := transitionHarness(t, "gateway")
	makeFast()
	h.publish["gateway"] = publishUncertainWritten
	previous := h.untilHeld("gateway")
	if !slices.Contains(h.selected["gateway"], h.id(challenger)) {
		t.Fatal("applied write is not the output")
	}
	h.receiptUnreadable["gateway"] = true
	for range 2 {
		executed := h.reconcile("gateway")
		h.checkOutputMaintained("gateway", executed, previous)
		h.checkDemandInvariant()
		h.now = h.now.Add(maxRequeueSpacing())
	}
	h.receiptUnreadable["gateway"] = false
	h.publish["gateway"] = publishOK
	executed := h.reconcile("gateway")
	h.checkOutputMaintained("gateway", executed)
	h.checkDemandInvariant()
	if demand := h.state().demand["gateway"]; demand.uncertain != nil || !slices.Equal(demand.selected, h.selected["gateway"]) || !slices.Contains(demand.selected, h.id(challenger)) {
		t.Fatal("confirmed output is not the Gateway's demand")
	}
}

// A managed write creates a generation Secret, but after a failed read-back
// or checkpoint commit the status keeps the previous generation, which stays
// current and maintained. Resolution reads the current generation, so the
// attempt is dropped rather than treated as activated.
func TestManagedGenerationWriteKeepsCurrentGenerationMaintained(t *testing.T) {
	for _, mode := range []struct {
		name string
		mode publishMode
	}{{"readback fails", publishReadbackFail}, {"commit fails", publishCommitFail}, {"create is ambiguous", publishUncertainWritten}} {
		t.Run(mode.name, func(t *testing.T) {
			h, challenger, makeFast := transitionHarness(t, "gateway")
			h.managed = true
			baseline := len(h.writes["gateway"])
			makeFast()
			h.publish["gateway"] = mode.mode
			previous := h.untilHeld("gateway")
			if got := len(h.writes["gateway"]) - baseline; got != 1 {
				t.Fatalf("generation writes while establishing the hold = %d, want 1", got)
			}
			if !slices.Equal(h.selected["gateway"], previous) {
				t.Fatal("generation write changed the current generation")
			}
			h.publish["gateway"] = publishOK
			executed := h.reconcile("gateway")
			h.checkOutputMaintained("gateway", executed, previous)
			h.checkDemandInvariant()
			if !slices.Contains(h.selected["gateway"], h.id(challenger)) || !slices.Equal(h.state().demand["gateway"].selected, h.selected["gateway"]) {
				t.Fatal("the next confirmed publication did not become current")
			}
		})
	}
}

// Another Gateway reconciling during an unresolved write cannot take the
// capacity reserved for either plausible output; budgets and bounds hold.
func TestOtherGatewayRespectsUnresolvedWrite(t *testing.T) {
	h, _, makeFast := transitionHarness(t, "gateway-a", "gateway-b")
	makeFast()
	h.publish["gateway-a"] = publishUncertainLost
	previous := h.untilHeld("gateway-a", "gateway-b")
	attempted := h.state().demand["gateway-a"].uncertain
	h.receiptUnreadable["gateway-a"] = true
	for range 4 {
		executed := h.reconcile("gateway-a")
		h.checkOutputMaintained("gateway-a", executed, previous, attempted)
		h.reconcile("gateway-b")
		h.checkDemandInvariant()
		if h.state().demand["gateway-a"].uncertain == nil {
			t.Fatal("unreadable receipt resolved the write")
		}
		h.now = h.now.Add(maxRequeueSpacing())
	}
	h.receiptUnreadable["gateway-a"] = false
	h.publish["gateway-a"] = publishOK
	h.reconcile("gateway-a")
	if h.state().demand["gateway-a"].uncertain != nil {
		t.Fatal("readable receipt did not resolve the write")
	}
}

// The managed current output is the generation the status records, not the
// newest generation Secret: a generation written by a failed operation is not
// treated as activated.
func TestManagedCurrentGenerationFollowsStatus(t *testing.T) {
	ctx := context.Background()
	gateway, runtimeAdapter, _ := managedFixture(t)
	if _, _, _, err := runtimeAdapter.Prepare(ctx, gateway); err != nil {
		t.Fatal(err)
	}
	publish := func(content string) (string, artifact.Receipt) {
		publisher, err := runtimeAdapter.Publisher(gateway, allowPublication{})
		if err != nil {
			t.Fatal(err)
		}
		validated := validatedFixture(t, artifact.SingBox1141, []byte(content))
		if _, err := publisher.Publish(ctx, validated); err != nil {
			t.Fatal(err)
		}
		receipt, err := validated.Receipt()
		if err != nil {
			t.Fatal(err)
		}
		return publisher.GenerationName(), receipt
	}
	if _, exists, err := runtimeAdapter.CurrentGenerationReceipt(ctx, gateway); err != nil || exists {
		t.Fatalf("no recorded generation reported current=%t err=%v", exists, err)
	}
	current, currentReceipt := publish(`{"current":true}`)
	gateway.Status.PublishedGeneration = current
	_, newerReceipt := publish(`{"newer":true}`)
	receipt, exists, err := runtimeAdapter.CurrentGenerationReceipt(ctx, gateway)
	if err != nil || !exists || !receipt.Equal(currentReceipt) || receipt.Equal(newerReceipt) {
		t.Fatalf("current generation receipt exists=%t err=%v", exists, err)
	}
}

// A reservation whose publication fails is released: no speculative pin
// remains, M5 state and the published selection stay, and the next successful
// publication commits normally.
func TestFailedPublicationReleasesReservation(t *testing.T) {
	h := newHarness(t, "alpha", artifact.Mihomo11931, testRecords(t, 10, 0))
	h.latency = fastest(0)
	for range 4 {
		h.reconcile("gateway")
		h.now = h.now.Add(maxRequeueSpacing())
	}
	incumbent := h.selected["gateway"]
	before := *h.states["gateway"]
	// Endpoint 5 becomes clearly faster; M5 prefers it after residence.
	h.latency = fastest(5)
	h.publish["gateway"] = publishBefore
	for range 6 {
		h.reconcile("gateway")
		h.checkDemandInvariant()
		if !slices.Equal(h.selected["gateway"], incumbent) || !slices.Equal(h.state().demand["gateway"].selected, incumbent) {
			t.Fatal("failed publication replaced the committed selection or demand")
		}
		h.now = h.now.Add(maxRequeueSpacing())
	}
	if !h.states["gateway"].EvaluatedAt.Equal(before.EvaluatedAt) {
		t.Fatal("failed publication committed M5 state")
	}
	h.publish["gateway"] = publishOK
	h.reconcile("gateway")
	h.checkDemandInvariant()
	if !slices.Equal(h.selected["gateway"], []string{h.id(5)}) || !h.state().protected(h.id(5)) {
		t.Fatalf("successful publication did not commit: %v", h.selected["gateway"])
	}
}

// Unreachable exploration endpoints cost a full probe timeout each. They must
// not delay maintenance until the maintained selection's evidence is stale.
func TestSlowExplorationDoesNotStaleMaintainedEvidence(t *testing.T) {
	for _, name := range []string{"alpha", "default"} {
		t.Run(name, func(t *testing.T) {
			probeKey := newProbeContextKey("pool", name, artifact.Mihomo11931, testTarget(t, "target-"+name, "https://example.com/"+name))
			healthy := cohortCapacity(probeKey, evidencePolicy())
			h := newHarness(t, name, artifact.Mihomo11931, testRecords(t, healthy+40, 0))
			h.healthy = func(i int) bool { return i < healthy }
			h.failCost = func(int) time.Duration { return 2 * time.Minute }
			var selected []string
			for round := range 30 {
				start := h.now
				executed := h.reconcile("gateway")
				if h.overloaded {
					t.Fatalf("round %d: fast maintenance reported overload", round)
				}
				// Exploration starts no probe after its budget; one already
				// running finishes within its own timeout.
				if elapsed := h.now.Sub(start); round > 0 && elapsed > explorationBudget(evidencePolicy())+2*time.Minute+time.Minute {
					t.Fatalf("round %d took %s", round, elapsed)
				}
				if round >= 8 {
					if selected == nil {
						selected = h.selected["gateway"]
					}
					if len(h.selected["gateway"]) == 0 || !slices.Equal(selected, h.selected["gateway"]) {
						t.Fatalf("round %d: slow exploration staled the maintained selection", round)
					}
				}
				// Deferred exploration leaves no observation behind.
				for id, count := range jobIDs(executed) {
					if h.index[id] >= healthy && count > evidencePolicy().MinSamples {
						t.Fatal("deferred exploration recorded observations")
					}
				}
				h.now = h.now.Add(maxRequeueSpacing())
			}
		})
	}
}

// Maintenance slower than Freshness cannot keep every observation fresh. The
// round reports overload, and demanded members run last so the selection
// they back is still observed immediately before evaluation.
func TestSlowMaintenanceIsReportedAndSelectionProbedLast(t *testing.T) {
	h := newHarness(t, "alpha", artifact.Mihomo11931, testRecords(t, 40, 0))
	// 24 maintained probes of 13s take longer than Freshness, while the round
	// stays short enough to keep MinSamples in the evidence window.
	h.latency = func(int) time.Duration { return 13 * time.Second }
	overloaded := false
	var selected []string
	for round := range 16 {
		h.reconcile("gateway")
		overloaded = overloaded || h.overloaded
		if round >= 8 {
			if selected == nil {
				selected = h.selected["gateway"]
			}
			if !slices.Equal(selected, h.selected["gateway"]) {
				t.Fatalf("round %d: selection lost under slow maintenance", round)
			}
		}
		h.now = h.now.Add(maxRequeueSpacing())
	}
	if !overloaded || len(selected) != 1 {
		t.Fatalf("overloaded=%t selected=%d", overloaded, len(selected))
	}
	round, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now, h.refresh)
	if !due || round.maintain[len(round.maintain)-1].Record.ID().String() != selected[0] {
		t.Fatal("selected endpoint is not probed last")
	}
}

// A round abandoned before its evidence was stored is retried by the next
// reconciliation instead of suppressing probes for a whole cadence, and its
// exploration position is not lost.
func TestAbandonedRoundIsRetried(t *testing.T) {
	h := newHarness(t, "alpha", artifact.Mihomo11931, testRecords(t, 100, 0))
	first, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now, h.refresh)
	if !due {
		t.Fatal("first round not due")
	}
	h.pipeline.abandonRound(first)
	retry, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now, h.refresh)
	if !due || retry.id == first.id || !slices.Equal(slices.Sorted(maps.Keys(jobIDs(retry.explore))), slices.Sorted(maps.Keys(jobIDs(first.explore)))) {
		t.Fatal("abandoned round was not retried from the same position")
	}
	h.pipeline.completeRound(retry, false)
	if _, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now, h.refresh); due {
		t.Fatal("completed round repeated within its window")
	}
	// Abandoning a superseded round does not reopen the current one.
	h.pipeline.abandonRound(first)
	if _, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now, h.refresh); due {
		t.Fatal("stale abandonment reopened the current round")
	}
}

func TestMaintainableTopNKeepsRequestBelowCapacity(t *testing.T) {
	for _, name := range []string{"alpha", "default"} {
		key := newProbeContextKey("pool", name, artifact.Mihomo11931, testTarget(t, "target", "https://example.com/"))
		capacity := cohortCapacity(key, evidencePolicy())
		for requested, want := range map[int]int{1: 1, capacity - 1: capacity - 1, capacity: capacity, capacity + 1: capacity, 10_000: capacity} {
			if got := maintainableTopN(key, requested); got != want {
				t.Fatalf("%s: maintainableTopN(%d)=%d want %d", name, requested, got, want)
			}
		}
	}
}

// A pool whose refresh interval is shorter than the evidence cadence retries
// a round that produced no usable evidence within that interval, for example
// while its proxy or probe target is still starting, instead of waiting a full
// cadence; a long refresh interval keeps the cadence. Shared contexts still run
// one round per window.
func TestShortRefreshIntervalBoundsRoundWindow(t *testing.T) {
	for _, name := range []string{"default", "alpha"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name, artifact.Mihomo11931, testRecords(t, 10, 0))
			h.refresh = 30 * time.Second
			// Probes are refused quickly, as by a proxy that is not yet listening.
			h.healthy = func(int) bool { return false }
			h.failCost = func(int) time.Duration { return 100 * time.Millisecond }
			start := h.now
			if len(h.reconcile("gateway")) == 0 {
				t.Fatal("first round empty")
			}
			if len(h.selected["gateway"]) != 0 {
				t.Fatal("failed probes produced a selection")
			}
			// The fixture becomes ready. The next reconciliation after one
			// refresh interval runs a new round; M5 still counts the earlier
			// failures, so publication follows once successes outweigh them,
			// well inside one evidence cadence.
			h.healthy = func(int) bool { return true }
			if len(h.reconcile("gateway")) != 0 {
				t.Fatal("round repeated inside its window")
			}
			h.now = start.Add(h.refresh + time.Second)
			if len(h.reconcile("gateway")) == 0 {
				t.Fatal("short refresh interval did not retry the round")
			}
			for len(h.selected["gateway"]) == 0 && h.now.Sub(start) < EvidenceCadence() {
				h.now = h.now.Add(h.refresh + time.Second)
				if len(h.reconcile("gateway")) == 0 {
					t.Fatal("a due round did not run")
				}
			}
			if len(h.selected["gateway"]) == 0 {
				t.Fatal("no publication within one evidence cadence of the fixture becoming ready")
			}
			// A refresh interval longer than the cadence keeps the cadence.
			long := newHarness(t, name, artifact.Mihomo11931, testRecords(t, 10, 0))
			long.refresh = 24 * time.Hour
			longStart := long.now
			long.reconcile("gateway")
			long.now = longStart.Add(EvidenceCadence() - time.Second)
			if len(long.reconcile("gateway")) != 0 {
				t.Fatal("round repeated inside the evidence cadence")
			}
			long.now = longStart.Add(EvidenceCadence() + time.Second)
			if len(long.reconcile("gateway")) == 0 {
				t.Fatal("long refresh interval delayed the round beyond the cadence")
			}
		})
	}
}
