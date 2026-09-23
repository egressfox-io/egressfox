package operator_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
	"github.com/egressfox-io/egressfox/internal/state"
)

func TestManagedHTTPRefreshFallbackRotationAndExpiry(t *testing.T) {
	var mode atomic.Int32
	var requests atomic.Int32
	body := "trojan://synthetic-secret@edge.example.com:443?security=tls#edge\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic" && r.Header.Get("Authorization") != "Bearer rotated" {
			t.Error("authorization missing")
		}
		switch mode.Load() {
		case 0:
			if r.Header.Get("If-None-Match") != "" {
				t.Error("initial request conditional")
			}
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte(body))
		case 1:
			if r.Header.Get("If-None-Match") != `"v1"` {
				t.Error("missing conditional validator")
			}
			w.WriteHeader(http.StatusNotModified)
		case 2:
			w.WriteHeader(http.StatusUnauthorized)
		case 3:
			_, _ = w.Write([]byte("malformed subscription"))
		case 4:
			if r.Header.Get("If-None-Match") != "" {
				t.Error("rotated credentials inherited validator")
			}
			_, _ = w.Write([]byte(body))
		}
	}))
	defer server.Close()
	ctx := context.Background()
	urlSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"url": []byte(server.URL)}}
	authSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "egress"}, Data: map[string][]byte{"value": []byte("Bearer synthetic")}}
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("11111111-2222-3333-4444-555555555555"), Generation: 1}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "url"}, AuthorizationSecretRef: &egressv1alpha1.SecretKeyReference{Name: "auth", Key: "value"}, AllowHTTP: true, AllowPrivateNetworks: true}}}}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(urlSecret, authSecret, pool).Build()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cache.db")
	store, err := state.Open(path, state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if changed, err := operatoradapter.RefreshHTTP(ctx, reader, store, pool, pool.Spec.Sources[0], now); err != nil || !changed {
		t.Fatalf("initial refresh = %v, %v", changed, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = state.Open(path, state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result, err := operatoradapter.BuildPoolWithCache(ctx, reader, pool, store, now)
	if err != nil || result.Inventory.Len() != 1 || len(result.SourceRevisions) != 2 {
		t.Fatalf("restart = %+v, %v", result, err)
	}
	mode.Store(1)
	if changed, err := operatoradapter.RefreshHTTP(ctx, reader, store, pool, pool.Spec.Sources[0], now.Add(time.Hour)); err != nil || changed {
		t.Fatalf("304 = %v, %v", changed, err)
	}
	mode.Store(2)
	if _, err := operatoradapter.RefreshHTTP(ctx, reader, store, pool, pool.Spec.Sources[0], now.Add(2*time.Hour)); err == nil || strings.Contains(err.Error(), "synthetic") {
		t.Fatalf("outage = %v", err)
	}
	result, err = operatoradapter.BuildPoolWithCache(ctx, reader, pool, store, now.Add(2*time.Hour))
	if err != nil || result.Inventory.Len() != 1 {
		t.Fatalf("fallback = %d, %v", result.Inventory.Len(), err)
	}
	result, err = operatoradapter.BuildPoolWithCache(ctx, reader, pool, store, now.Add(25*time.Hour))
	if err != nil || result.Inventory.Len() != 0 || result.Sources[0].State != "Expired" {
		t.Fatalf("exact expiry = %+v, %v", result.Sources, err)
	}
	mode.Store(3)
	if _, err := operatoradapter.RefreshHTTP(ctx, reader, store, pool, pool.Spec.Sources[0], now.Add(3*time.Hour)); err == nil {
		t.Fatal("malformed response accepted")
	}
	result, _ = operatoradapter.BuildPoolWithCache(ctx, reader, pool, store, now.Add(3*time.Hour))
	if result.Inventory.Len() != 1 {
		t.Fatal("malformed response poisoned cache")
	}
	rotated := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "auth"}, rotated); err != nil {
		t.Fatal(err)
	}
	rotated.Data["value"] = []byte("Bearer rotated")
	if err := reader.Update(ctx, rotated); err != nil {
		t.Fatal(err)
	}
	result, _ = operatoradapter.BuildPoolWithCache(ctx, reader, pool, store, now.Add(3*time.Hour))
	if result.Inventory.Len() != 0 {
		t.Fatal("rotated auth reused cache")
	}
	mode.Store(4)
	if changed, err := operatoradapter.RefreshHTTP(ctx, reader, store, pool, pool.Spec.Sources[0], now.Add(3*time.Hour)); err != nil || !changed {
		t.Fatalf("rotated refresh = %v, %v", changed, err)
	}
	if requests.Load() < 5 {
		t.Fatalf("requests = %d", requests.Load())
	}
	stable, err := operatoradapter.ResolveHTTP(ctx, reader, pool, pool.Spec.Sources[0])
	if err != nil {
		t.Fatal(err)
	}
	metadataOnly := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "url"}, metadataOnly); err != nil {
		t.Fatal(err)
	}
	metadataOnly.Labels = map[string]string{"irrelevant": "updated"}
	if err := reader.Update(ctx, metadataOnly); err != nil {
		t.Fatal(err)
	}
	stableAfter, err := operatoradapter.ResolveHTTP(ctx, reader, pool, pool.Spec.Sources[0])
	if err != nil || stableAfter.Fingerprint != stable.Fingerprint {
		t.Fatal("metadata-only update invalidated cache")
	}
	changedFormat := pool.DeepCopy()
	changedFormat.Spec.Sources[0].Format = egressv1alpha1.SourceFormatBase64URIList
	formatConfig, err := operatoradapter.ResolveHTTP(ctx, reader, changedFormat, changedFormat.Spec.Sources[0])
	if err != nil || formatConfig.Fingerprint == stable.Fingerprint {
		t.Fatal("format change reused cache")
	}
	changedAdmission := pool.DeepCopy()
	changedAdmission.Spec.Sources[0].AllowPartial = true
	admissionConfig, err := operatoradapter.ResolveHTTP(ctx, reader, changedAdmission, changedAdmission.Spec.Sources[0])
	if err != nil || admissionConfig.Fingerprint == stable.Fingerprint {
		t.Fatal("admission change reused cache")
	}
	changedTLS := pool.DeepCopy()
	changedTLS.Spec.Sources[0].HTTP.AllowInsecureTLS = true
	tlsConfig, err := operatoradapter.ResolveHTTP(ctx, reader, changedTLS, changedTLS.Spec.Sources[0])
	if err != nil || tlsConfig.Fingerprint == stable.Fingerprint {
		t.Fatal("HTTP TLS policy change reused cache")
	}
	urlRotated := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "url"}, urlRotated); err != nil {
		t.Fatal(err)
	}
	urlRotated.Data["url"] = []byte(server.URL + "?rotated=1")
	if err := reader.Update(ctx, urlRotated); err != nil {
		t.Fatal(err)
	}
	result, _ = operatoradapter.BuildPoolWithCache(ctx, reader, pool, store, now.Add(3*time.Hour))
	if result.Inventory.Len() != 0 {
		t.Fatal("URL rotation reused cache")
	}
	currentURL := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "url"}, currentURL); err != nil {
		t.Fatal(err)
	}
	currentURL.Data["url"] = []byte(server.URL)
	if err := reader.Update(ctx, currentURL); err != nil {
		t.Fatal(err)
	}
	secretSource := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "inline", Namespace: "egress"}, Data: map[string][]byte{"nodes": []byte(body)}}
	if err := reader.Create(ctx, secretSource); err != nil {
		t.Fatal(err)
	}
	mixed := pool.DeepCopy()
	mixed.Spec.Sources = append(mixed.Spec.Sources, egressv1alpha1.SubscriptionSource{ID: "backup", SecretRef: &egressv1alpha1.SecretKeyReference{Name: "inline", Key: "nodes"}, Format: egressv1alpha1.SourceFormatURIList})
	result, err = operatoradapter.BuildPoolWithCache(ctx, reader, mixed, store, now.Add(3*time.Hour))
	if err != nil || result.Inventory.Len() != 1 || len(result.Inventory.Records()[0].Provenance()) != 2 {
		t.Fatalf("mixed provenance = %d, %v", result.Inventory.Len(), err)
	}
	mixed.Spec.Sources = mixed.Spec.Sources[1:]
	result, err = operatoradapter.BuildPoolWithCache(ctx, reader, mixed, store, now.Add(3*time.Hour))
	if err != nil || result.Inventory.Len() != 1 {
		t.Fatal("removing HTTP source erased Secret contribution")
	}
}

func TestHTTP304WithoutCacheFailsAfterOneUnconditionalRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" {
			t.Error("unexpected conditional request")
		}
		calls.Add(1)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	ctx := context.Background()
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("pool-uid"), Generation: 1}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "url"}, AllowHTTP: true, AllowPrivateNetworks: true}}}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"url": []byte(server.URL)}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(pool, secret).Build()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(dir, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := operatoradapter.RefreshHTTP(ctx, reader, store, pool, pool.Spec.Sources[0], time.Now()); err == nil || calls.Load() != 2 {
		t.Fatalf("304 without cache = %v, calls=%d", err, calls.Load())
	}
	result, err := operatoradapter.BuildPoolWithCache(ctx, reader, pool, store, time.Now())
	if err != nil || result.Inventory.Len() != 0 {
		t.Fatalf("unexpected source state = %+v, %v", result.Sources, err)
	}
}

func TestInFlightHTTPRefreshRejectsRotatedInput(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("trojan://synthetic@edge.example.com:443?security=tls"))
	}))
	defer server.Close()
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("pool-uid"), Generation: 1}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "url"}, AllowHTTP: true, AllowPrivateNetworks: true}}}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"url": []byte(server.URL)}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(pool, secret).Build()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(dir, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	done := make(chan error, 1)
	go func() {
		_, err := operatoradapter.RefreshHTTP(context.Background(), reader, store, pool, pool.Spec.Sources[0], time.Now())
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP request did not start")
	}
	current := &corev1.Secret{}
	if err := reader.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "url"}, current); err != nil {
		t.Fatal(err)
	}
	current.Data["url"] = []byte(server.URL + "?new=1")
	if err := reader.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("obsolete response committed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not stop")
	}
	config, err := operatoradapter.ResolveHTTP(context.Background(), reader, pool, pool.Spec.Sources[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.LoadSourceCache(context.Background(), config.Key, config.Fingerprint); err != nil || found {
		t.Fatalf("obsolete cache = %v, %v", found, err)
	}
}
