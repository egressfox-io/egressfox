package operator

import (
	"context"
	"fmt"
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

// probeHarness replays budgetedJobs with a fake clock and fake probe outcomes
// and keeps the samples M5 would see, without a store or native engine.
type probeHarness struct {
	t        *testing.T
	pipeline *Pipeline
	name     string
	profile  artifact.Profile
	target   observation.HTTPTarget
	records  []endpoint.Record
	interval time.Duration
	now      time.Time
	healthy  func(endpoint.Record) bool
	samples  map[string][]time.Time
}

func newHarness(t *testing.T, name string, profile artifact.Profile, records []endpoint.Record, interval time.Duration) *probeHarness {
	return &probeHarness{
		t: t, pipeline: &Pipeline{}, name: name, profile: profile, records: records, interval: interval,
		target:  testTarget(t, "target-"+name, "https://example.com/"+name),
		now:     time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC),
		healthy: func(endpoint.Record) bool { return true },
		samples: map[string][]time.Time{},
	}
}

func (h *probeHarness) key() probeContextKey {
	return newProbeContextKey("pool", h.name, h.profile, h.target)
}

func (h *probeHarness) window(gateway types.UID) []probe.Job {
	h.t.Helper()
	key := h.key()
	jobs := h.pipeline.budgetedJobs(key, gateway, h.records, h.target, h.profile, h.interval, h.now)
	vantage, _ := observation.NewVantageID("test")
	results := make([]probe.Result, len(jobs))
	for index, job := range jobs {
		ok := h.healthy(job.Record)
		connection, err := observation.NewConnectionRef(job.Record.Identity())
		if err != nil {
			h.t.Fatal(err)
		}
		observationKey, err := observation.NewKey(connection, job.Target.Ref(), vantage, observation.KindHTTPGet, h.profile)
		if err != nil {
			h.t.Fatal(err)
		}
		params := observation.Params{Key: observationKey, StartedAt: h.now, CompletedAt: h.now, Duration: time.Millisecond, Outcome: observation.OutcomeTimeout}
		if ok {
			params.Outcome, params.StatusCode = observation.OutcomeSuccess, 204
			id := job.Record.ID().String()
			h.samples[id] = append(h.samples[id], h.now)
		}
		value, err := observation.New(params)
		if err != nil {
			h.t.Fatal(err)
		}
		results[index] = probe.Result{Observation: value}
	}
	h.pipeline.recordProbeResults(key, jobs, results)
	return jobs
}

// eligible applies the M5 default evidence rules to the collected samples.
func (h *probeHarness) eligible() int {
	policy := selection.DefaultPolicy(selection.StrategyAdaptive)
	count := 0
	for _, times := range h.samples {
		inWindow := 0
		last := time.Time{}
		for _, at := range times {
			if h.now.Sub(at) <= policy.EvidenceWindow {
				inWindow++
			}
			if at.After(last) {
				last = at
			}
		}
		if inWindow >= policy.MinSamples && h.now.Sub(last) <= policy.Freshness {
			count++
		}
	}
	return count
}

func TestNamedProfileQualifiesWorkingSetOnLargeInventory(t *testing.T) {
	for _, interval := range []time.Duration{time.Minute, 5 * time.Minute, time.Hour, 24 * time.Hour} {
		t.Run(interval.String(), func(t *testing.T) {
			records := testRecords(t, 300, 0)
			h := newHarness(t, "alpha", artifact.Mihomo11931, records, interval)
			probed := map[string]bool{}
			for window := 0; window < 4; window++ {
				jobs := h.window("gateway")
				if len(jobs) == 0 || len(jobs) > maxProbeBatch {
					t.Fatalf("window %d jobs=%d", window, len(jobs))
				}
				for _, job := range jobs {
					probed[job.Record.ID().String()] = true
				}
				if h.eligible() == 0 {
					t.Fatalf("window %d: no endpoint has sufficient M5 evidence", window)
				}
				h.now = h.now.Add(interval)
			}
			// Bounded deterministic exploration: the cursor still covers the
			// whole inventory over time.
			for window := 0; window < len(records) && len(probed) < len(records); window++ {
				for _, job := range h.window("gateway") {
					probed[job.Record.ID().String()] = true
				}
				h.now = h.now.Add(interval)
			}
			if len(probed) != len(records) {
				t.Fatalf("explored %d of %d endpoints", len(probed), len(records))
			}
			if h.eligible() == 0 {
				t.Fatal("eligibility lost while exploring")
			}
		})
	}
}

func TestNamedProfileIgnoresIncompatibleEndpoints(t *testing.T) {
	records := testRecords(t, 3, 150) // >64 incompatible endpoints ahead of the compatible ones
	h := newHarness(t, "alpha", artifact.SingBox1141, records, 5*time.Minute)
	jobs := h.window("gateway")
	if len(jobs) == 0 {
		t.Fatal("compatible endpoints starved behind incompatible ones")
	}
	for _, job := range jobs {
		if job.Record.Configuration().Transport().Kind() != endpoint.TransportTCP {
			t.Fatal("incompatible endpoint consumed probe capacity")
		}
	}
	if got := h.eligible(); got != 3 {
		t.Fatalf("eligible=%d want 3", got)
	}
}

func TestNamedProfileFailedEndpointIsReplaced(t *testing.T) {
	records := testRecords(t, 40, 0)
	h := newHarness(t, "alpha", artifact.Mihomo11931, records, time.Minute)
	h.window("gateway")
	initial := len(h.pipeline.probes[h.key()].working)
	if initial == 0 {
		t.Fatal("no working set")
	}
	dead := h.pipeline.probes[h.key()].working[0]
	h.healthy = func(record endpoint.Record) bool { return record.ID().String() != dead }
	for window := 0; window < 10; window++ {
		h.now = h.now.Add(time.Minute)
		h.window("gateway")
	}
	working := h.pipeline.probes[h.key()].working
	if len(working) != initial {
		t.Fatalf("working set %d want %d", len(working), initial)
	}
	for _, id := range working {
		if id == dead {
			t.Fatal("failed endpoint stayed in the working set")
		}
	}
}

func TestProbeBudgetFollowsTargetRevision(t *testing.T) {
	records := testRecords(t, 50, 0)
	h := newHarness(t, "alpha", artifact.Mihomo11931, records, 24*time.Hour)
	if len(h.window("gateway")) == 0 {
		t.Fatal("first window empty")
	}
	if len(h.window("gateway")) != 0 {
		t.Fatal("budget was not exhausted within the window")
	}
	// Same target ID, new revision (changed URL): a fresh context.
	h.target = testTarget(t, "target-alpha", "https://example.com/changed")
	if len(h.window("gateway")) == 0 {
		t.Fatal("new target revision inherited the exhausted budget")
	}
	// Default profile follows the same rule.
	d := newHarness(t, "default", artifact.Mihomo11931, records, 24*time.Hour)
	first := d.window("gateway")
	if len(first) != len(records) {
		t.Fatalf("legacy batch=%d", len(first))
	}
	d.target = testTarget(t, "target-default", "https://example.com/changed")
	if len(d.window("gateway")) == 0 {
		t.Fatal("default target change inherited the exhausted budget")
	}
}

func TestProbeBudgetFollowsProfileIncarnation(t *testing.T) {
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
	if len(pipeline.budgetedJobs(oldKey, "g", records, old, artifact.Mihomo11931, time.Hour, now)) == 0 {
		t.Fatal("old context empty")
	}
	if len(pipeline.budgetedJobs(newKey, "g", records, recreated, artifact.Mihomo11931, time.Hour, now)) == 0 {
		t.Fatal("recreated profile inherited the exhausted budget")
	}
}

func TestEquivalentGatewaysShareProbeExploration(t *testing.T) {
	records := testRecords(t, 200, 0)
	h := newHarness(t, "alpha", artifact.Mihomo11931, records, time.Minute)
	seen := map[string]int{}
	gateways := []types.UID{"gateway-a", "gateway-b", "gateway-c"}
	for window := 0; window < 30; window++ {
		// Every Gateway reconciles in every window; only one may spend budget.
		spent := 0
		for _, gateway := range gateways {
			jobs := h.window(gateway)
			if len(jobs) > 0 {
				spent++
			}
			first := map[string]bool{}
			for _, job := range jobs {
				id := job.Record.ID().String()
				if !first[id] {
					first[id] = true
					seen[id]++
				}
			}
		}
		if spent != 1 {
			t.Fatalf("window %d: %d Gateways spent the shared budget", window, spent)
		}
		h.now = h.now.Add(time.Minute)
	}
	// Exploration never restarted from a Gateway-specific cursor: endpoints
	// outside the working set were each explored at most once in this lap.
	working := map[string]bool{}
	for _, id := range h.pipeline.probes[h.key()].working {
		working[id] = true
	}
	for id, count := range seen {
		if !working[id] && count > 1 {
			t.Fatalf("endpoint explored %d times before the cursor completed a lap", count)
		}
	}
	if len(h.pipeline.cursors) != 0 {
		t.Fatal("named profile touched the Gateway-scoped legacy cursor")
	}
}

func TestEngineContextsAreIndependent(t *testing.T) {
	records := testRecords(t, 30, 0)
	pipeline := &Pipeline{}
	target := testTarget(t, "target-alpha", "https://example.com/alpha")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	mihomoKey := newProbeContextKey("pool", "alpha", artifact.Mihomo11931, target)
	singKey := newProbeContextKey("pool", "alpha", artifact.SingBox1141, target)
	if len(pipeline.budgetedJobs(mihomoKey, "g1", records, target, artifact.Mihomo11931, time.Minute, now)) == 0 {
		t.Fatal("mihomo empty")
	}
	if len(pipeline.budgetedJobs(mihomoKey, "g1", records, target, artifact.Mihomo11931, time.Minute, now)) != 0 {
		t.Fatal("mihomo budget not exhausted")
	}
	if len(pipeline.budgetedJobs(singKey, "g2", records, target, artifact.SingBox1141, time.Minute, now)) == 0 {
		t.Fatal("sing-box starved by mihomo context")
	}
	otherPool := newProbeContextKey("other-pool", "alpha", artifact.Mihomo11931, target)
	if len(pipeline.budgetedJobs(otherPool, "g3", records, target, artifact.Mihomo11931, time.Minute, now)) == 0 {
		t.Fatal("another pool shares the context")
	}
}

func TestProbeStateExpires(t *testing.T) {
	records := testRecords(t, 10, 0)
	h := newHarness(t, "alpha", artifact.Mihomo11931, records, time.Minute)
	h.window("gateway")
	if len(h.pipeline.probes) != 1 {
		t.Fatalf("states=%d", len(h.pipeline.probes))
	}
	h.now = h.now.Add(probeStateTTL + time.Hour)
	other := newHarness(t, "beta", artifact.Mihomo11931, records, time.Minute)
	other.pipeline, other.now = h.pipeline, h.now
	other.window("gateway")
	if _, ok := h.pipeline.probes[h.key()]; ok || len(h.pipeline.probes) != 1 {
		t.Fatalf("stale probe context was not released: %d", len(h.pipeline.probes))
	}
}
