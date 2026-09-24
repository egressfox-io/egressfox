package controller_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/controller"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
	"github.com/egressfox-io/egressfox/internal/state"
)

func expiryFixture(t *testing.T, sources ...egressv1alpha1.SubscriptionSource) (*egressv1alpha1.ProxyPool, client.Client, *state.Store, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("11111111-2222-3333-4444-555555555555"), Generation: 1}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: sources, RefreshInterval: &metav1.Duration{Duration: time.Hour}}}
	objects := []client.Object{pool}
	for _, desired := range sources {
		if desired.HTTP != nil {
			objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: desired.HTTP.URLSecretRef.Name, Namespace: "egress"}, Data: map[string][]byte{"url": []byte("https://provider.example/" + desired.ID)}})
		}
	}
	reader := fake.NewClientBuilder().WithScheme(controllerScheme(t)).WithStatusSubresource(pool).WithObjects(objects...).Build()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(dir, "cache.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return pool, reader, store, &now
}

func httpExpirySource(id string, ttl time.Duration) egressv1alpha1.SubscriptionSource {
	return egressv1alpha1.SubscriptionSource{ID: id, Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: id + "-url", Key: "url"}, MaxStale: &metav1.Duration{Duration: ttl}}}
}

func saveExpiryCache(t *testing.T, reader client.Reader, store *state.Store, pool *egressv1alpha1.ProxyPool, desired egressv1alpha1.SubscriptionSource, at time.Time, body string) {
	t.Helper()
	config, err := operatoradapter.ResolveHTTP(context.Background(), reader, pool, desired)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSourceCache(context.Background(), state.SourceCache{Key: config.Key, Fingerprint: config.Fingerprint, Body: []byte(body), AcceptedAt: at, ValidatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

func reconcileExpiry(t *testing.T, reader client.Client, store *state.Store, now *time.Time) (ctrl.Result, *egressv1alpha1.ProxyPool) {
	t.Helper()
	r := &controller.ProxyPoolReconciler{Client: reader, Store: store, Now: func() time.Time { return *now }}
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "pool"}})
	if err != nil {
		t.Fatal(err)
	}
	current := &egressv1alpha1.ProxyPool{}
	if err := reader.Get(context.Background(), client.ObjectKey{Namespace: "egress", Name: "pool"}, current); err != nil {
		t.Fatal(err)
	}
	return result, current
}

func TestPoolReconcileSchedulesEarliestCacheExpiryAndRefresh(t *testing.T) {
	first, second := httpExpirySource("first", 10*time.Minute), httpExpirySource("second", 30*time.Minute)
	pool, reader, store, now := expiryFixture(t, first, second)
	saveExpiryCache(t, reader, store, pool, first, *now, "trojan://one@edge-one.example:443?security=tls\n")
	saveExpiryCache(t, reader, store, pool, second, *now, "trojan://two@edge-two.example:443?security=tls\n")
	result, current := reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter != 10*time.Minute || current.Status.AcceptedEndpoints != 2 {
		t.Fatalf("initial expiry scheduling: %s, %d", result.RequeueAfter, current.Status.AcceptedEndpoints)
	}
	*now = now.Add(9 * time.Minute)
	saveExpiryCache(t, reader, store, pool, first, *now, "trojan://one@edge-one.example:443?security=tls\n")
	result, _ = reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter != 10*time.Minute {
		t.Fatalf("refreshed deadline = %s", result.RequeueAfter)
	}
	*now = now.Add(time.Minute) // the original deadline must no longer expire the source
	_, current = reconcileExpiry(t, reader, store, now)
	if current.Status.AcceptedEndpoints != 2 {
		t.Fatal("old cache deadline remained authoritative after refresh")
	}
	*now = now.Add(9 * time.Minute) // refreshed first source expires, second remains usable
	result, current = reconcileExpiry(t, reader, store, now)
	if current.Status.AcceptedEndpoints != 1 || current.Status.Sources[0].State != "Expired" || current.Status.Sources[1].State == "Expired" || result.RequeueAfter != 11*time.Minute {
		t.Fatalf("mixed expiry = %d, %+v, %s", current.Status.AcceptedEndpoints, current.Status.Sources, result.RequeueAfter)
	}
	*now = now.Add(11 * time.Minute)
	result, current = reconcileExpiry(t, reader, store, now)
	if current.Status.AcceptedEndpoints != 0 || current.Status.Sources[1].State != "Expired" || result.RequeueAfter < time.Hour {
		t.Fatalf("expired pool hot loop = %d, %+v, %s", current.Status.AcceptedEndpoints, current.Status.Sources, result.RequeueAfter)
	}
	result, _ = reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter < time.Hour {
		t.Fatal("repeat reconcile immediately requeued an expired cache")
	}
}

func TestPoolCacheExpiryWithSecretFailureAndMissingCache(t *testing.T) {
	httpSource := httpExpirySource("http", 10*time.Minute)
	absentSource := httpExpirySource("absent", 20*time.Minute)
	secretSource := egressv1alpha1.SubscriptionSource{ID: "missing", Format: egressv1alpha1.SourceFormatURIList, SecretRef: &egressv1alpha1.SecretKeyReference{Name: "missing", Key: "nodes"}}
	pool, reader, store, now := expiryFixture(t, secretSource, absentSource, httpSource)
	saveExpiryCache(t, reader, store, pool, httpSource, *now, "trojan://one@edge.example:443?security=tls\n")
	previous := pool.DeepCopy()
	previous.Status.AcceptedEndpoints = 1
	if err := reader.Status().Update(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	result, current := reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter != 10*time.Minute || current.Status.AcceptedEndpoints != 0 || len(current.Status.Sources) != 2 || current.Status.Sources[0].Reason != "CacheMissing" || current.Status.Sources[1].State != "Fresh" {
		t.Fatalf("failed Secret suppressed HTTP deadline: %s, %+v", result.RequeueAfter, current.Status.Sources)
	}
	*now = now.Add(10 * time.Minute)
	result, current = reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter < time.Hour || current.Status.Sources[1].State != "Expired" {
		t.Fatalf("error-path expiry = %s, %+v", result.RequeueAfter, current.Status.Sources)
	}
	config, err := operatoradapter.ResolveHTTP(context.Background(), reader, pool, httpSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSourceCache(context.Background(), config.Key); err != nil {
		t.Fatal(err)
	}
	result, current = reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter < time.Hour || current.Status.Sources[1].Reason != "CacheMissing" {
		t.Fatalf("missing cache = %s, %+v", result.RequeueAfter, current.Status.Sources)
	}
}

func TestPoolCacheExpiryEmptyCorruptRemovedAndRestarted(t *testing.T) {
	desired := httpExpirySource("http", 10*time.Minute)
	desired.AllowEmpty = true
	pool, reader, store, now := expiryFixture(t, desired)
	saveExpiryCache(t, reader, store, pool, desired, *now, "# empty\n")
	result, current := reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter != 10*time.Minute || current.Status.AcceptedEndpoints != 0 {
		t.Fatalf("empty admitted inventory = %s, %d", result.RequeueAfter, current.Status.AcceptedEndpoints)
	}
	// A new reconciler after restart reads the same protected cache deadline.
	result, _ = reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter != 10*time.Minute {
		t.Fatal("restart lost cache deadline")
	}
	saveExpiryCache(t, reader, store, pool, desired, *now, "malformed subscription")
	result, current = reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter < time.Hour || current.Status.Sources[0].Reason != "CacheCorrupt" {
		t.Fatalf("corrupt cache = %s, %+v", result.RequeueAfter, current.Status.Sources)
	}
	urlSecret := &corev1.Secret{}
	if err := reader.Get(context.Background(), client.ObjectKey{Namespace: "egress", Name: "http-url"}, urlSecret); err != nil {
		t.Fatal(err)
	}
	urlSecret.Data["url"] = []byte("https://new-provider.example/subscription")
	if err := reader.Update(context.Background(), urlSecret); err != nil {
		t.Fatal(err)
	}
	result, current = reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter < time.Hour || current.Status.Sources[0].Reason != "CacheMissing" {
		t.Fatalf("changed source configuration reused cache = %s, %+v", result.RequeueAfter, current.Status.Sources)
	}
	updated := current.DeepCopy()
	updated.Spec.Sources = []egressv1alpha1.SubscriptionSource{{ID: "secret", Format: egressv1alpha1.SourceFormatURIList, SecretRef: &egressv1alpha1.SecretKeyReference{Name: "missing", Key: "nodes"}}}
	if err := reader.Update(context.Background(), updated); err != nil {
		t.Fatal(err)
	}
	result, _ = reconcileExpiry(t, reader, store, now)
	if result.RequeueAfter < time.Hour {
		t.Fatal("removed HTTP source retained deadline")
	}
}

func TestExpiredCacheDoesNotDiscardGatewayLKG(t *testing.T) {
	desired := httpExpirySource("http", 10*time.Minute)
	pool, reader, store, now := expiryFixture(t, desired)
	saveExpiryCache(t, reader, store, pool, desired, *now, "trojan://one@edge.example:443?security=tls\n")
	*now = now.Add(10 * time.Minute)
	_, expiredPool := reconcileExpiry(t, reader, store, now)
	if expiredPool.Status.AcceptedEndpoints != 0 {
		t.Fatal("expired cache still contributed to the pool")
	}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, Runtime: &egressv1alpha1.GatewayRuntimeSpec{Managed: &egressv1alpha1.ManagedRuntimeSpec{}}}, Status: egressv1alpha1.EgressGatewayStatus{PublishedGeneration: "old-generation", ActiveGeneration: "old-generation"}}
	gatewayClient := fake.NewClientBuilder().WithScheme(controllerScheme(t)).WithStatusSubresource(gateway).WithObjects(expiredPool, gateway).Build()
	r := &controller.EgressGatewayReconciler{Client: gatewayClient, Pipeline: fakePipeline{err: errors.New("synthetic unavailable inventory")}, Runtime: fakeRuntime{outcome: operatoradapter.RuntimeOutcome{PublishedGeneration: "old-generation", ActiveGeneration: "old-generation", RuntimeReady: true}}, Now: func() time.Time { return *now }}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	current := &egressv1alpha1.EgressGateway{}
	if err := gatewayClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), current); err != nil {
		t.Fatal(err)
	}
	if current.Status.ActiveGeneration != "old-generation" || current.Status.PublishedGeneration != "old-generation" {
		t.Fatalf("cache expiry displaced Gateway LKG: %+v", current.Status)
	}
}
