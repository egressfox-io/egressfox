package operator_test

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
)

func TestBuildPoolSecretSourcesAndSafeFailure(t *testing.T) {
	scheme := testScheme(t)
	secretCanary := "pool-secret-canary"
	sourceSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "subscription", Namespace: "egress"}, Data: map[string][]byte{
		"nodes": []byte("vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?encryption=none&security=tls#first\n" +
			"trojan://" + secretCanary + "@other.example.com:443?security=tls#second\n"),
	}}
	pool := testPool()
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sourceSecret).Build()
	result, err := operatoradapter.BuildPool(context.Background(), reader, pool)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inventory.Len() != 2 || result.Accepted != 2 || result.Rejected != 0 {
		t.Fatalf("unexpected safe counts: inventory=%d accepted=%d rejected=%d", result.Inventory.Len(), result.Accepted, result.Rejected)
	}
	if len(result.SourceRevisions) != 1 {
		t.Fatalf("source revisions = %d, want 1", len(result.SourceRevisions))
	}

	pool.Spec.Sources[0].SecretRef.Key = "missing"
	_, err = operatoradapter.BuildPool(context.Background(), reader, pool)
	if err == nil || strings.Contains(err.Error(), secretCanary) {
		t.Fatalf("unsafe or missing error: %v", err)
	}
}

func testPool() *egressv1alpha1.ProxyPool {
	return &egressv1alpha1.ProxyPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("11111111-2222-3333-4444-555555555555")},
		Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{
			ID: "primary", SecretRef: egressv1alpha1.SecretKeyReference{Name: "subscription", Key: "nodes"}, Format: egressv1alpha1.SourceFormatURIList,
		}}},
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := egressv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}
