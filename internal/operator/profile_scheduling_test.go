package operator

import (
	"context"
	"fmt"
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

// probeHarness replays the operator's probe scheduling and the real M5
// selector with a fake clock and fake probe outcomes, without a store or
// native engine. Each Gateway keeps its own M5 state, as the pipeline does.
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
	history  map[string][]observation.Observation
	states   map[types.UID]*selection.State
	selected map[types.UID][]string
}

func newHarness(t *testing.T, name string, profile artifact.Profile, records []endpoint.Record) *probeHarness {
	h := &probeHarness{
		t: t, pipeline: &Pipeline{}, pool: "pool", name: name, profile: profile, records: records, topN: 1,
		target:   testTarget(t, "target-"+name, "https://example.com/"+name),
		now:      time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC),
		healthy:  func(int) bool { return true },
		latency:  func(int) time.Duration { return 100 * time.Millisecond },
		history:  map[string][]observation.Observation{},
		states:   map[types.UID]*selection.State{},
		selected: map[types.UID][]string{},
	}
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

// reconcile runs one Gateway reconciliation: the shared probe round (if due),
// then that Gateway's M5 decision, fed back to the scheduler like Run does.
func (h *probeHarness) reconcile(gateway types.UID) []probe.Job {
	h.t.Helper()
	key := h.key()
	jobs := h.pipeline.budgetedJobs(key, h.records, h.target, h.profile, h.now)
	if len(jobs) > roundBudget(key) {
		h.t.Fatalf("round of %d jobs exceeds budget %d", len(jobs), roundBudget(key))
	}
	results := make([]probe.Result, len(jobs))
	for index, job := range jobs {
		id := job.Record.ID().String()
		position := h.index[id]
		completed := h.now.Add(time.Duration(index-len(jobs)) * time.Millisecond)
		params := observation.Params{Key: h.evidenceKey(job.Record), StartedAt: completed, CompletedAt: completed, Duration: time.Millisecond, Outcome: observation.OutcomeTimeout}
		if h.healthy(position) {
			params.StartedAt = completed.Add(-h.latency(position))
			params.Duration, params.Outcome, params.StatusCode = h.latency(position), observation.OutcomeSuccess, 204
		}
		value, err := observation.New(params)
		if err != nil {
			h.t.Fatal(err)
		}
		results[index] = probe.Result{Observation: value}
		h.history[id] = append(h.history[id], value)
	}
	h.pipeline.recordProbeResults(key, jobs, results)
	decision := h.decide(gateway)
	h.pipeline.recordSelection(key, gateway, decision, h.now)
	return jobs
}

func (h *probeHarness) decide(gateway types.UID) selection.Decision {
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
		if values := h.history[id]; len(values) > 0 {
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
	next := decision.Next
	h.states[gateway] = &next
	ids := make([]string, 0, len(decision.Selected))
	for _, record := range decision.Selected {
		ids = append(ids, record.ID().String())
	}
	h.selected[gateway] = ids
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
	if !state.pinned(first[0]) || !state.pinned(h.id(1)) {
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
	if len(pipeline.budgetedJobs(oldKey, records, old, artifact.Mihomo11931, now)) == 0 {
		t.Fatal("old context empty")
	}
	if len(pipeline.budgetedJobs(newKey, records, recreated, artifact.Mihomo11931, now)) == 0 {
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
			if len(pipeline.budgetedJobs(mihomoKey, records, target, artifact.Mihomo11931, now)) == 0 {
				t.Fatal("mihomo empty")
			}
			if len(pipeline.budgetedJobs(mihomoKey, records, target, artifact.Mihomo11931, now)) != 0 {
				t.Fatal("mihomo round repeated")
			}
			if len(pipeline.budgetedJobs(singKey, records, target, artifact.SingBox1141, now)) == 0 {
				t.Fatal("sing-box starved by mihomo context")
			}
			otherPool := newProbeContextKey("other-pool", name, artifact.Mihomo11931, target)
			if len(pipeline.budgetedJobs(otherPool, records, target, artifact.Mihomo11931, now)) == 0 {
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
