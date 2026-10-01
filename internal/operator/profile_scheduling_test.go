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
	failCost    func(int) time.Duration
	history     map[string][]observation.Observation
	states      map[types.UID]*selection.State
	selected    map[types.UID][]string
	failPublish map[types.UID]bool
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
		failPublish: map[types.UID]bool{},
		refused:     map[types.UID]bool{},
		constrained: map[types.UID]bool{},
	}
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

var sequentialProbes = probe.ScheduleConfig{Concurrency: 1, Queue: 256, PerEndpoint: 1, PerTarget: 1, MaxJobs: probe.MaxSchedulerJobs}

// reconcile follows Run for one Gateway: the shared round (if due), evidence
// recording, demand touch, the M5 decision, reservation (with constrained
// re-planning on refusal), simulated publication, then commit or release. It
// returns the probe jobs that actually executed.
func (h *probeHarness) reconcile(gateway types.UID) []probe.Job {
	h.t.Helper()
	key := h.key()
	var executed []probe.Job
	if round, due := h.pipeline.budgetedJobs(key, h.records, h.target, h.profile, h.now); due {
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
	if h.failPublish[gateway] {
		h.pipeline.releaseSelection(reservation)
		return executed
	}
	h.pipeline.commitSelection(reservation)
	next := decision.Next
	h.states[gateway] = &next
	ids := make([]string, 0, len(decision.Selected))
	for _, record := range decision.Selected {
		ids = append(ids, record.ID().String())
	}
	h.selected[gateway] = ids
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
	if !state.demanded(first[0]) || !state.demanded(h.id(1)) {
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
	if roundJobs(pipeline.budgetedJobs(oldKey, records, old, artifact.Mihomo11931, now)) == 0 {
		t.Fatal("old context empty")
	}
	if roundJobs(pipeline.budgetedJobs(newKey, records, recreated, artifact.Mihomo11931, now)) == 0 {
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
			if roundJobs(pipeline.budgetedJobs(mihomoKey, records, target, artifact.Mihomo11931, now)) == 0 {
				t.Fatal("mihomo empty")
			}
			if roundJobs(pipeline.budgetedJobs(mihomoKey, records, target, artifact.Mihomo11931, now)) != 0 {
				t.Fatal("mihomo round repeated")
			}
			if roundJobs(pipeline.budgetedJobs(singKey, records, target, artifact.SingBox1141, now)) == 0 {
				t.Fatal("sing-box starved by mihomo context")
			}
			otherPool := newProbeContextKey("other-pool", name, artifact.Mihomo11931, target)
			if roundJobs(pipeline.budgetedJobs(otherPool, records, target, artifact.Mihomo11931, now)) == 0 {
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

// checkDemandInvariant asserts that committed demand fits the cohort and is
// maintained: every demanded endpoint is a cohort member.
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
		for _, id := range demand.selected {
			union[id] = true
			if !members[id] {
				h.t.Fatalf("published selection of %s is not maintained", gateway)
			}
		}
	}
	if len(union) > cohortCapacity(h.key(), evidencePolicy()) || len(state.cohort) > cohortCapacity(h.key(), evidencePolicy()) {
		h.t.Fatalf("demand %d or cohort %d exceeds capacity", len(union), len(state.cohort))
	}
}

// Two Gateways share a full cohort: Gateway b's selection pins every member,
// and Gateway a independently prefers an explored endpoint after its own M5
// failure streak and cooldown. The union no longer fits, so a chooses among
// maintained endpoints instead of publishing an unmaintainable endpoint, and
// regains its preference once b's demand expires.
func TestSharedCohortConstrainsSelectionThatDoesNotFit(t *testing.T) {
	for _, name := range []string{"alpha", "default"} {
		t.Run(name, func(t *testing.T) {
			probeKey := newProbeContextKey("pool", name, artifact.Mihomo11931, testTarget(t, "target-"+name, "https://example.com/"+name))
			capacity := cohortCapacity(probeKey, evidencePolicy())
			h := newHarness(t, name, artifact.Mihomo11931, testRecords(t, capacity+12, 0))
			h.topN = capacity
			h.latency = func(int) time.Duration { return 400 * time.Millisecond }
			step := func(gateways ...types.UID) {
				for _, gateway := range gateways {
					h.reconcile(gateway)
				}
				h.checkDemandInvariant()
				h.now = h.now.Add(maxRequeueSpacing())
			}
			for range 20 {
				step("gateway-a")
			}
			if len(h.selected["gateway-a"]) != capacity {
				t.Fatalf("warm-up selected %d of %d", len(h.selected["gateway-a"]), capacity)
			}
			// Overlapping demands whose union fits are not constrained.
			for range 3 {
				step("gateway-a", "gateway-b")
				if h.constrained["gateway-a"] || h.constrained["gateway-b"] {
					t.Fatal("overlapping demands that fit were constrained")
				}
			}
			if len(h.selected["gateway-b"]) != capacity {
				t.Fatalf("gateway-b selected %d", len(h.selected["gateway-b"]))
			}
			bLast := h.now.Add(-maxRequeueSpacing())
			// Gateway a alone observes a failure streak on one of its members.
			victim := h.index[h.selected["gateway-a"][0]]
			h.healthy = func(i int) bool { return i != victim }
			streak := evidencePolicy().FailureStreak
			for range streak {
				step("gateway-a")
			}
			if !h.constrained["gateway-a"] || len(h.selected["gateway-a"]) != capacity-1 || slices.Contains(h.selected["gateway-a"], h.id(victim)) {
				t.Fatalf("gateway-a constrained=%t selected=%d", h.constrained["gateway-a"], len(h.selected["gateway-a"]))
			}
			constrainedSet := slices.Sorted(slices.Values(h.selected["gateway-a"]))
			// While b's demand lives, a stays constrained and its published
			// selection does not churn; once it expires, a fills topN again.
			released := false
			for range 12 {
				step("gateway-a")
				alive := h.now.Add(-maxRequeueSpacing()).Sub(bLast) <= evidencePolicy().EvidenceWindow
				switch {
				case alive && !slices.Equal(constrainedSet, slices.Sorted(slices.Values(h.selected["gateway-a"]))):
					t.Fatal("constrained selection churned while shared demand persisted")
				case !alive && !h.constrained["gateway-a"] && len(h.selected["gateway-a"]) == capacity:
					released = true
				}
			}
			if !released {
				t.Fatal("expired demand did not release shared capacity")
			}
			if _, ok := h.state().demand["gateway-b"]; ok {
				t.Fatal("expired Gateway demand still held")
			}
		})
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
	h.failPublish["gateway"] = true
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
	h.failPublish["gateway"] = false
	h.reconcile("gateway")
	h.checkDemandInvariant()
	if !slices.Equal(h.selected["gateway"], []string{h.id(5)}) || !h.state().demanded(h.id(5)) {
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
	round, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now)
	if !due || round.maintain[len(round.maintain)-1].Record.ID().String() != selected[0] {
		t.Fatal("selected endpoint is not probed last")
	}
}

// A round abandoned before its evidence was stored is retried by the next
// reconciliation instead of suppressing probes for a whole cadence, and its
// exploration position is not lost.
func TestAbandonedRoundIsRetried(t *testing.T) {
	h := newHarness(t, "alpha", artifact.Mihomo11931, testRecords(t, 100, 0))
	first, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now)
	if !due {
		t.Fatal("first round not due")
	}
	h.pipeline.abandonRound(first)
	retry, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now)
	if !due || retry.id == first.id || !slices.Equal(slices.Sorted(maps.Keys(jobIDs(retry.explore))), slices.Sorted(maps.Keys(jobIDs(first.explore)))) {
		t.Fatal("abandoned round was not retried from the same position")
	}
	h.pipeline.completeRound(retry, false)
	if _, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now); due {
		t.Fatal("completed round repeated within its window")
	}
	// Abandoning a superseded round does not reopen the current one.
	h.pipeline.abandonRound(first)
	if _, due := h.pipeline.budgetedJobs(h.key(), h.records, h.target, h.profile, h.now); due {
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
