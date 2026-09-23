package controller

import (
	"context"
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
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
	"github.com/egressfox-io/egressfox/internal/state"
)

func TestHTTPRefreshDispatchHasFixedBudget(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	r := &HTTPRefresher{slots: map[string]*refreshSlot{}, Now: func() time.Time { return now }}
	for i := range 100 {
		key := fmt.Sprintf("pool/%03d", i)
		r.slots[key] = &refreshSlot{next: now.Add(-time.Second), pool: &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{UID: types.UID("pool")}}}
	}
	work := make(chan refreshJob, maxHTTPRefreshWorkers)
	r.dispatch(work)
	if len(work) != maxHTTPRefreshWorkers {
		t.Fatalf("queued = %d", len(work))
	}
	active := 0
	for _, slot := range r.slots {
		if slot.active {
			active++
		}
	}
	if active != maxHTTPRefreshWorkers {
		t.Fatalf("active = %d", active)
	}
	if !r.NeedLeaderElection() {
		t.Fatal("refresher must stop on leadership loss")
	}
}

func TestHTTPRefresherStopsOnCancellation(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(dir, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scheme := runtime.NewScheme()
	if err := egressv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewHTTPRefresher(reader, store, "egress")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresher ignored cancellation")
	}
}

func TestHTTPRefresherPrunesRemovedSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(dir, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scheme := runtime.NewScheme()
	if err := egressv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("pool-uid")}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "url"}}}}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"url": []byte("https://example.test/subscription")}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, secret).Build()
	config, err := operatoradapter.ResolveHTTP(ctx, reader, pool, pool.Spec.Sources[0])
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSourceCache(ctx, state.SourceCache{Key: config.Key, Fingerprint: config.Fingerprint, Body: []byte("# empty"), AcceptedAt: now, ValidatedAt: now}); err != nil {
		t.Fatal(err)
	}
	r := NewHTTPRefresher(reader, store, "egress")
	r.inspect(ctx, types.NamespacedName{Namespace: "egress", Name: "pool"})
	if len(r.slots) != 1 {
		t.Fatalf("slots = %d", len(r.slots))
	}
	current := &egressv1alpha1.ProxyPool{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "pool"}, current); err != nil {
		t.Fatal(err)
	}
	current.Spec.Sources = nil
	if err := reader.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	r.inspect(ctx, types.NamespacedName{Namespace: "egress", Name: "pool"})
	if len(r.slots) != 0 {
		t.Fatal("removed source remains scheduled")
	}
	if _, found, err := store.LoadSourceCache(ctx, config.Key, config.Fingerprint); err != nil || found {
		t.Fatalf("removed cache = %v, %v", found, err)
	}
}
