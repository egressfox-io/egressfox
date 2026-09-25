package operator

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
)

func TestProfileSnapshotRejectsMutationAndRemoval(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := egressv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "test", UID: types.UID("pool-uid"), Generation: 1}, Spec: egressv1alpha1.ProxyPoolSpec{Profiles: []egressv1alpha1.TargetProfile{{Name: "alpha", Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "targets", Key: "alpha"}}}}}, Status: egressv1alpha1.ProxyPoolStatus{Profiles: []egressv1alpha1.ProfileStatus{{Name: "alpha", FirstObservedGeneration: 1}}}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "test", UID: types.UID("gateway-uid"), Generation: 1}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, ProfileRef: "alpha"}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "targets", Namespace: "test", UID: types.UID("secret-uid")}, Data: map[string][]byte{"alpha": []byte("https://example.com/alpha")}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, gateway, secret).Build()
	read := func() (*egressv1alpha1.ProxyPool, *egressv1alpha1.EgressGateway, *corev1.Secret) {
		p, g, s := &egressv1alpha1.ProxyPool{}, &egressv1alpha1.EgressGateway{}, &corev1.Secret{}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(pool), p); err != nil {
			t.Fatal(err)
		}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(gateway), g); err != nil {
			t.Fatal(err)
		}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(secret), s); err != nil {
			t.Fatal(err)
		}
		return p, g, s
	}
	p, g, s := read()
	guard, err := NewSnapshotGuard(reader, g, p, map[types.NamespacedName]ResourceRevision{{Namespace: "test", Name: "targets"}: {UID: s.UID, ResourceVersion: s.ResourceVersion}})
	if err != nil || guard.Check(ctx) != nil {
		t.Fatalf("initial guard: %v", err)
	}
	s.Data["alpha"] = []byte("https://example.com/changed")
	if err := reader.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(guard.Check(ctx), ErrSnapshotObsolete) {
		t.Fatal("target mutation did not obsolete selection")
	}
	p, g, s = read()
	guard, err = NewSnapshotGuard(reader, g, p, map[types.NamespacedName]ResourceRevision{{Namespace: "test", Name: "targets"}: {UID: s.UID, ResourceVersion: s.ResourceVersion}})
	if err != nil {
		t.Fatal(err)
	}
	p.Spec.Profiles = nil
	if err := reader.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(guard.Check(ctx), ErrSnapshotObsolete) {
		t.Fatal("profile removal did not obsolete selection")
	}
}
