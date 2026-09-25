package controller

import (
	"testing"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProfileStatusesRetainIncarnationUntilRemoval(t *testing.T) {
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Generation: 4}, Spec: egressv1alpha1.ProxyPoolSpec{Profiles: []egressv1alpha1.TargetProfile{{Name: "beta"}, {Name: "alpha"}}}}
	first := profileStatuses(pool)
	if len(first) != 2 || first[0].Name != "alpha" || first[0].FirstObservedGeneration != 4 || first[1].Name != "beta" {
		t.Fatalf("initial status: %#v", first)
	}
	pool.Status.Profiles = first
	pool.Generation = 5
	if next := profileStatuses(pool); next[0].FirstObservedGeneration != 4 {
		t.Fatalf("unrelated update reset incarnation: %#v", next)
	}
	pool.Spec.Profiles = nil
	pool.Status.Profiles = profileStatuses(pool)
	pool.Generation = 6
	pool.Spec.Profiles = []egressv1alpha1.TargetProfile{{Name: "alpha"}}
	if next := profileStatuses(pool); len(next) != 1 || next[0].FirstObservedGeneration != 6 {
		t.Fatalf("recreated profile retained old incarnation: %#v", next)
	}
}
