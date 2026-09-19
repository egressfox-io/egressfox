package observation_test

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
)

func TestTargetCanonicalizationRevisionAndRedaction(t *testing.T) {
	t.Parallel()
	id := mustTargetID(t, "github-api")
	first := mustTarget(t, id, "HTTPS://Example.COM:443", 204, 5*time.Second, observation.HTTPOptions{})
	equivalent := mustTarget(t, id, "https://example.com/", 204, 5*time.Second, observation.HTTPOptions{})
	changed := mustTarget(t, id, "https://example.com/?token=target-secret-canary", 204, 5*time.Second, observation.HTTPOptions{})
	if !first.Revision().Equal(equivalent.Revision()) {
		t.Fatal("equivalent target descriptions have different revisions")
	}
	if first.Revision().Equal(changed.Revision()) {
		t.Fatal("meaningfully changed target shared a revision")
	}
	formatted := fmt.Sprintf("%v %+v %#v", changed, changed, changed)
	encoded, marshalErr := json.Marshal(changed)
	if strings.Contains(formatted+string(encoded)+fmt.Sprint(marshalErr), "target-secret-canary") {
		t.Fatal("target formatting leaked URL content")
	}
}

func TestTargetRequiresExplicitUnsafeAuthorization(t *testing.T) {
	t.Parallel()
	id := mustTargetID(t, "local")
	if _, err := observation.NewHTTPTarget(id, "http://127.0.0.1/", 200, time.Second, observation.HTTPOptions{}); err == nil {
		t.Fatal("plain HTTP target was accepted without authorization")
	}
	if _, err := observation.NewHTTPTarget(id, "http://127.0.0.1/", 200, time.Second, observation.HTTPOptions{AllowHTTP: true, AllowPrivate: true}); err != nil {
		t.Fatal(err)
	}
}

func TestSummarySeparatesRevisionTargetAndVantage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	oldConnection := mustConnection(t, "old-password")
	newConnection := mustConnection(t, "new-password")
	if oldConnection.ID() != newConnection.ID() || oldConnection.Revision().Equal(newConnection.Revision()) {
		t.Fatal("test connection does not represent credential rotation")
	}
	target := mustTarget(t, mustTargetID(t, "service-a"), "https://example.com/health", 204, 5*time.Second, observation.HTTPOptions{})
	otherTarget := mustTarget(t, mustTargetID(t, "service-b"), "https://example.net/health", 204, 5*time.Second, observation.HTTPOptions{})
	vantage := mustVantage(t, "standalone-a")
	key := mustKey(t, oldConnection, target, vantage, artifact.SingBox1141)
	values := []observation.Observation{
		mustObservation(t, key, now.Add(-4*time.Minute), 40*time.Millisecond, observation.OutcomeSuccess, 204),
		mustObservation(t, key, now.Add(-2*time.Minute), 60*time.Millisecond, observation.OutcomeSuccess, 204),
		mustObservation(t, key, now.Add(-time.Minute), 90*time.Millisecond, observation.OutcomeTimeout, 0),
	}
	random := rand.New(rand.NewPCG(7, 11))
	random.Shuffle(len(values), func(left, right int) { values[left], values[right] = values[right], values[left] })
	summary, err := observation.Summarize(key, values, now, 10*time.Minute, 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Samples != 3 || summary.Successes != 2 || summary.SuccessRatio != 2.0/3.0 || summary.MeanSuccessDuration != 50*time.Millisecond || !summary.Fresh {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.Latest == nil || summary.Latest.Outcome() != observation.OutcomeTimeout || summary.LatestSuccess == nil || !summary.LatestSuccess.CompletedAt().Equal(now.Add(-2*time.Minute)) {
		t.Fatal("latest observation semantics are incorrect")
	}

	for _, different := range []observation.Key{
		mustKey(t, newConnection, target, vantage, artifact.SingBox1141),
		mustKey(t, oldConnection, otherTarget, vantage, artifact.SingBox1141),
		mustKey(t, oldConnection, target, mustVantage(t, "standalone-b"), artifact.SingBox1141),
	} {
		if _, err := observation.Summarize(different, values, now, 10*time.Minute, time.Minute); err == nil {
			t.Fatal("summary accepted evidence from a different revision, target, or vantage")
		}
	}
}

func TestSummaryWindowFreshnessAndReplayAreExplicit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	connection := mustConnection(t, "password")
	target := mustTarget(t, mustTargetID(t, "service"), "https://example.com/health", 200, time.Second, observation.HTTPOptions{})
	key := mustKey(t, connection, target, mustVantage(t, "host-a"), artifact.Mihomo11931)
	boundary := mustObservation(t, key, now.Add(-5*time.Minute), 25*time.Millisecond, observation.OutcomeSuccess, 200)
	future := mustObservation(t, key, now.Add(time.Second), 10*time.Millisecond, observation.OutcomeSuccess, 200)
	first, err := observation.Summarize(key, []observation.Observation{future, boundary}, now, 5*time.Minute, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := observation.Summarize(key, []observation.Observation{boundary, future}, now, 5*time.Minute, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.Samples != 1 || !first.Fresh {
		t.Fatalf("explicit replay differs: first=%+v second=%+v", first, second)
	}
	stale, err := observation.Summarize(key, []observation.Observation{boundary}, now.Add(time.Nanosecond), 6*time.Minute, 5*time.Minute)
	if err != nil || stale.Fresh {
		t.Fatalf("freshness boundary = %+v, %v", stale, err)
	}
}

func TestObservationFormattingDoesNotExposeRevisions(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	connection := mustConnection(t, "observation-secret-canary")
	target := mustTarget(t, mustTargetID(t, "private-target"), "https://example.com/?token=observation-target-canary", 200, time.Second, observation.HTTPOptions{})
	value := mustObservation(t, mustKey(t, connection, target, mustVantage(t, "host"), artifact.SingBox1141), now, time.Millisecond, observation.OutcomeSuccess, 200)
	formatted := fmt.Sprintf("%v %+v %#v", value, value, value)
	encoded, marshalErr := json.Marshal(value)
	if strings.Contains(formatted+string(encoded)+fmt.Sprint(marshalErr), "observation-secret-canary") || strings.Contains(formatted+string(encoded)+fmt.Sprint(marshalErr), "observation-target-canary") {
		t.Fatal("observation diagnostics leaked secret-bearing input")
	}
}

func mustConnection(t testing.TB, password string) observation.ConnectionRef {
	t.Helper()
	address, err := endpoint.NewAddress("edge.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := endpoint.NewTrojanCredential(password)
	if err != nil {
		t.Fatal(err)
	}
	tls, err := endpoint.NewTLS("edge.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := endpoint.NewConfiguration(endpoint.ProtocolTrojan, address, credential, endpoint.NewTCPTransport(), tls)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := observation.NewConnectionRef(configuration.Identity())
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func mustTargetID(t testing.TB, value string) observation.TargetID {
	t.Helper()
	id, err := observation.NewTargetID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustVantage(t testing.TB, value string) observation.VantageID {
	t.Helper()
	id, err := observation.NewVantageID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustTarget(t testing.TB, id observation.TargetID, rawURL string, status int, timeout time.Duration, options observation.HTTPOptions) observation.HTTPTarget {
	t.Helper()
	target, err := observation.NewHTTPTarget(id, rawURL, status, timeout, options)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func mustKey(t testing.TB, connection observation.ConnectionRef, target observation.HTTPTarget, vantage observation.VantageID, profile artifact.Profile) observation.Key {
	t.Helper()
	key, err := observation.NewKey(connection, target.Ref(), vantage, observation.KindHTTPGet, profile)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustObservation(t testing.TB, key observation.Key, completed time.Time, duration time.Duration, outcome observation.Outcome, status int) observation.Observation {
	t.Helper()
	value, err := observation.New(observation.Params{
		Key: key, StartedAt: completed.Add(-duration), CompletedAt: completed,
		Duration: duration, Outcome: outcome, StatusCode: status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
