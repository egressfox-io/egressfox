package operator

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
)

func TestResolveProfilePreservesDefaultAndIsolatesNamedTargets(t *testing.T) {
	pool := &egressv1alpha1.ProxyPool{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "test", UID: types.UID("pool-1")},
		Spec: egressv1alpha1.ProxyPoolSpec{
			Probe:     egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "targets", Key: "legacy"}},
			Selection: egressv1alpha1.SelectionSpec{Strategy: egressv1alpha1.SelectionAdaptive, TopN: 1},
			Profiles: []egressv1alpha1.TargetProfile{
				{Name: "alpha", Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "targets", Key: "alpha"}}, Selection: egressv1alpha1.SelectionSpec{Strategy: egressv1alpha1.SelectionStatic, TopN: 2}},
				{Name: "beta", Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "targets", Key: "beta"}}, Selection: egressv1alpha1.SelectionSpec{Strategy: egressv1alpha1.SelectionLowestLatency, TopN: 3}},
			},
		},
		Status: egressv1alpha1.ProxyPoolStatus{Profiles: []egressv1alpha1.ProfileStatus{{Name: "alpha", FirstObservedGeneration: 1}, {Name: "beta", FirstObservedGeneration: 1}}},
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "targets", Namespace: "test"}, Data: map[string][]byte{"legacy": []byte("https://legacy.example.com"), "alpha": []byte("https://alpha.example.com"), "beta": []byte("https://beta.example.com")}}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	pipeline := &Pipeline{reader: reader}
	seen := map[string]string{}
	for _, name := range []string{"", "default", "alpha", "beta"} {
		probe, selection, err := ResolveProfile(pool, name)
		if err != nil {
			t.Fatal(err)
		}
		target, _, _, err := pipeline.target(context.Background(), pool, name, probe)
		if err != nil {
			t.Fatal(err)
		}
		seen[name] = target.ID().String()
		if name == "" && (selection.Strategy != egressv1alpha1.SelectionAdaptive || selection.TopN != 1) {
			t.Fatal("legacy selection changed")
		}
		if name == "alpha" && (selection.Strategy != egressv1alpha1.SelectionStatic || selection.TopN != 2) {
			t.Fatal("alpha selection merged with default")
		}
	}
	if seen[""] != seen["default"] || seen[""] != "k8s-pool-1-probe" {
		t.Fatalf("legacy identity changed: %v", seen)
	}
	if seen["alpha"] == seen["beta"] || seen["alpha"] == seen[""] {
		t.Fatalf("profiles share evidence identity: %v", seen)
	}
	if _, _, err := ResolveProfile(pool, "missing"); err == nil {
		t.Fatal("missing profile silently resolved")
	}
	pool.Spec.Profiles = append(pool.Spec.Profiles, pool.Spec.Profiles[0])
	if _, _, err := ResolveProfile(pool, "alpha"); err == nil {
		t.Fatal("duplicate profile accepted")
	}
	pool.Spec.Profiles = make([]egressv1alpha1.TargetProfile, 9)
	if _, _, err := ResolveProfile(pool, "default"); err == nil {
		t.Fatal("unbounded profile count accepted")
	}
}

func TestPipelineRejectsMissingProfileBeforeSourceAcquisition(t *testing.T) {
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "test"}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "test"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, ProfileRef: "missing"}}
	_, err := (&Pipeline{}).Run(context.Background(), gateway, pool)
	var coded interface{ Code() string }
	if !errors.As(err, &coded) || coded.Code() != "profile_missing" {
		t.Fatalf("missing profile error = %v", err)
	}
}
