package operator

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/egressfox-io/egressfox/internal/state"
)

type reviewChecker struct{}

func (reviewChecker) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	return artifact.Evidence{ValidatorID: "test/review"}, nil
}

func reviewArtifact(t *testing.T, body string) artifact.Validated {
	t.Helper()
	candidate, err := artifact.NewCandidate(artifact.Mihomo11931, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	validated, err := artifact.Validate(context.Background(), candidate, reviewChecker{})
	if err != nil {
		t.Fatal(err)
	}
	return validated
}

func TestSnapshotGuardRejectsContributingCacheExpiry(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := egressv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "test", UID: types.UID("pool")}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "test", UID: types.UID("gateway")}, Spec: egressv1alpha1.EgressGatewaySpec{Engine: egressv1alpha1.EngineMihomo}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, gateway).Build()
	guard, err := NewSnapshotGuard(reader, gateway, pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(dir, "cache.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	versions := map[string]CacheVersion{}
	for i, key := range []string{"pool/early", "pool/late"} {
		entry := state.SourceCache{Key: key, Fingerprint: sha256.Sum256([]byte(key)), Body: []byte(key), AcceptedAt: base, ValidatedAt: base}
		if err := store.SaveSourceCache(ctx, entry); err != nil {
			t.Fatal(err)
		}
		versions[key] = CacheVersion{Digest: sha256.Sum256(entry.Body), ValidatedAt: base, Present: true, SourceID: key[5:], ExpiresAt: base.Add(time.Duration(i+1) * time.Hour)}
	}
	// An unrelated cache key has no expiry bearing on this snapshot.
	guard.BindCache(store, versions)
	current := base.Add(30 * time.Minute)
	guard.now = func() time.Time { return current }
	if err := guard.Check(ctx); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewSecretPublisher(reader, scheme, gateway, "rendered", guard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(ctx, reviewArtifact(t, "known-good")); err != nil {
		t.Fatal(err)
	}
	lkg, exists, err := publisher.CurrentReceipt(ctx)
	if err != nil || !exists {
		t.Fatalf("LKG receipt: %v", err)
	}
	current = base.Add(time.Hour)
	if err := guard.Check(ctx); !errors.Is(err, ErrSnapshotObsolete) {
		t.Fatalf("expired snapshot = %v", err)
	}
	if _, err := publisher.Publish(ctx, reviewArtifact(t, "stale-candidate")); err == nil {
		t.Fatal("expired candidate published")
	}
	retained, exists, err := publisher.CurrentReceipt(ctx)
	if err != nil || !exists || !retained.Equal(lkg) {
		t.Fatal("expired candidate displaced LKG")
	}
	current = base.Add(30 * time.Minute)
	if err := store.ValidateSourceCache(ctx, "pool/early", sha256.Sum256([]byte("pool/early")), current, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(ctx); !errors.Is(err, ErrSnapshotObsolete) {
		t.Fatalf("refreshed snapshot = %v", err)
	}
	newGuard, err := NewSnapshotGuard(reader, gateway, pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	newVersions := map[string]CacheVersion{}
	for key, version := range versions {
		newVersions[key] = version
	}
	renewed := newVersions["pool/early"]
	renewed.ValidatedAt = current
	renewed.ExpiresAt = current.Add(time.Hour)
	newVersions["pool/early"] = renewed
	newGuard.BindCache(store, newVersions)
	newGuard.now = func() time.Time { return current }
	newPublisher, err := NewSecretPublisher(reader, scheme, gateway, "rendered", newGuard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newPublisher.Publish(ctx, reviewArtifact(t, "refreshed-candidate")); err != nil {
		t.Fatal(err)
	}
	address, _ := endpoint.NewAddress("edge.example.com", 443)
	credential, _ := endpoint.NewVLESSCredential("11111111-1111-4111-8111-111111111111")
	tls, _ := endpoint.NewTLS("", false)
	configuration, _ := endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, credential, endpoint.NewTCPTransport(), tls)
	sourceID, _ := endpoint.NewSourceID("late")
	recordID, _ := endpoint.NewRecordID("node")
	provenance, _ := endpoint.NewProvenance(sourceID, recordID)
	selected, _ := endpoint.NewRecord(configuration, provenance)
	newGuard.BindSelected([]endpoint.Record{selected})
	current = base.Add(90 * time.Minute)
	if err := newGuard.Check(ctx); err != nil {
		t.Fatalf("unselected early expiry: %v", err)
	}
}

func TestNextJobsSkipsIncompatibleWithoutUsingBatch(t *testing.T) {
	address, _ := endpoint.NewAddress("edge.example.com", 443)
	credential, _ := endpoint.NewVLESSCredential("11111111-1111-4111-8111-111111111111")
	tls, _ := endpoint.NewTLS("front.example.com", false)
	xhttp, _ := endpoint.NewXHTTPTransport("/xhttp", "front.example.com", "stream-one")
	compatible, err := endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, credential, endpoint.NewTCPTransport(), tls)
	if err != nil {
		t.Fatal(err)
	}
	sourceID, _ := endpoint.NewSourceID("batch")
	makeRecord := func(config endpoint.Configuration, id string) endpoint.Record {
		recordID, _ := endpoint.NewRecordID(id)
		provenance, _ := endpoint.NewProvenance(sourceID, recordID)
		record, err := endpoint.NewRecord(config, provenance)
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	records := make([]endpoint.Record, 0, 102)
	for i := 0; i < 100; i++ {
		uniqueAddress, _ := endpoint.NewAddress("edge.example.com", 1000+i)
		unique, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolVLESS, uniqueAddress, credential, xhttp, tls, endpoint.SecurityOptions{}, endpoint.FlowNone, nil)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, makeRecord(unique, fmt.Sprintf("xhttp-%d", i)))
	}
	records = append(records, makeRecord(compatible, "tcp"))
	targetID, _ := observation.NewTargetID("target")
	target, _ := observation.NewHTTPTarget(targetID, "https://example.com/", 204, time.Second, observation.HTTPOptions{})
	pipeline := &Pipeline{cursors: map[types.UID]int{}}
	jobs := pipeline.nextJobs("sing", records, target, artifact.SingBox1141)
	if len(jobs) != 1 || jobs[0].Record.Configuration().Transport().Kind() != endpoint.TransportTCP || pipeline.cursors["sing"] != 0 {
		t.Fatalf("sing-box jobs=%d cursor=%d", len(jobs), pipeline.cursors["sing"])
	}
	jobs = pipeline.nextJobs("mihomo", records, target, artifact.Mihomo11931)
	if len(jobs) != 64 || pipeline.cursors["mihomo"] != 64 {
		t.Fatalf("mihomo jobs=%d cursor=%d", len(jobs), pipeline.cursors["mihomo"])
	}
	jobs = pipeline.nextJobs("mihomo", records, target, artifact.Mihomo11931)
	if len(jobs) != 64 || pipeline.cursors["mihomo"] != 27 {
		t.Fatalf("mihomo next jobs=%d cursor=%d", len(jobs), pipeline.cursors["mihomo"])
	}
	if jobs := pipeline.nextJobs("incompatible", records[:100], target, artifact.SingBox1141); len(jobs) != 0 || pipeline.cursors["incompatible"] != 0 {
		t.Fatalf("incompatible-only jobs=%d cursor=%d", len(jobs), pipeline.cursors["incompatible"])
	}
	if jobs := pipeline.nextJobs("compatible", records[100:], target, artifact.SingBox1141); len(jobs) != 1 {
		t.Fatalf("compatible-only jobs=%d", len(jobs))
	}
	hy2Auth, _ := endpoint.NewHysteria2Credential("synthetic-auth")
	hy2Options, _ := endpoint.NewHysteria2OptionsWithPorts(20, 40, "", []string{"443:444"})
	hy2, err := endpoint.NewExtendedConfiguration(endpoint.ProtocolHysteria2, address, hy2Auth, endpoint.NewQUICTransport(), tls, endpoint.SecurityOptions{}, endpoint.FlowNone, &hy2Options)
	if err != nil {
		t.Fatal(err)
	}
	hy2Record := makeRecord(hy2, "hy2")
	if jobs := pipeline.nextJobs("quic", []endpoint.Record{hy2Record}, target, artifact.SingBox1141); len(jobs) != 1 {
		t.Fatalf("QUIC profile jobs=%d", len(jobs))
	}
	withoutQUIC := artifact.SingBox1141
	withoutQUIC.RendererSchema = "egressfox.sing-box/v2"
	if jobs := pipeline.nextJobs("no-quic", []endpoint.Record{hy2Record}, target, withoutQUIC); len(jobs) != 0 {
		t.Fatalf("profile without QUIC jobs=%d", len(jobs))
	}
}
