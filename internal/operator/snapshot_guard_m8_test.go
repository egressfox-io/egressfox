package operator_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
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

func TestSnapshotGuardRejectsNewSecretAndCacheValidation(t *testing.T) {
	ctx := context.Background()
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("pool-uid")}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress", UID: types.UID("gateway-uid")}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(pool, gateway).Build()
	if err := reader.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "pool"}, pool); err != nil {
		t.Fatal(err)
	}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "gateway"}, gateway); err != nil {
		t.Fatal(err)
	}
	missing := types.NamespacedName{Namespace: "egress", Name: "new-url"}
	guard, err := operatoradapter.NewSnapshotGuard(reader, gateway, pool, map[types.NamespacedName]operatoradapter.ResourceRevision{missing: {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reader.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: missing.Name, Namespace: missing.Namespace}}); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(ctx); !errors.Is(err, operatoradapter.ErrSnapshotObsolete) {
		t.Fatalf("new Secret = %v", err)
	}
	guard, err = operatoradapter.NewSnapshotGuard(reader, gateway, pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(dir, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fingerprint := sha256.Sum256([]byte("context"))
	now := time.Now().UTC()
	guard.BindCache(store, map[string]operatoradapter.CacheVersion{"pool/main": {}})
	if err := guard.Check(ctx); err != nil {
		t.Fatal(err)
	}
	entry := state.SourceCache{Key: "pool/main", Fingerprint: fingerprint, Body: []byte("body"), AcceptedAt: now, ValidatedAt: now}
	if err := store.SaveSourceCache(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(ctx); !errors.Is(err, operatoradapter.ErrSnapshotObsolete) {
		t.Fatalf("new cache = %v", err)
	}
	digest := sha256.Sum256(entry.Body)
	guard.BindCache(store, map[string]operatoradapter.CacheVersion{"pool/main": {Digest: digest, ValidatedAt: now, Present: true}})
	if err := guard.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateSourceCache(ctx, entry.Key, fingerprint, now.Add(time.Minute), "", ""); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(ctx); !errors.Is(err, operatoradapter.ErrSnapshotObsolete) {
		t.Fatalf("new validation = %v", err)
	}
}
