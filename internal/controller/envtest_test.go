package controller_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/controller"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
)

type envtestChecker struct{}

func (envtestChecker) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	return artifact.Evidence{ValidatorID: "test/envtest"}, nil
}

func TestEnvtestAPIDefaultsStatusAndOwnedSecret(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is not set; run make test-envtest")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	environment := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join(root, "config", "crd", "bases")}, ErrorIfCRDPathMissing: true, BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS")}
	restConfig, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := egressv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kubeClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := kubeClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "egress"}}); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "input", Namespace: "egress"}, Data: map[string][]byte{"nodes": []byte("vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?encryption=none&security=tls")}}
	if err := kubeClient.Create(ctx, secret); err != nil {
		t.Fatal(err)
	}
	invalidTimeout := metav1.Duration{Duration: time.Millisecond}
	invalidRefresh := metav1.Duration{Duration: time.Second}
	invalid := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "egress"}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", SecretRef: egressv1alpha1.SecretKeyReference{Name: "input", Key: "nodes"}, Format: egressv1alpha1.SourceFormatURIList}}, Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "target", Key: "url"}, Timeout: &invalidTimeout}, RefreshInterval: &invalidRefresh}}
	if err := kubeClient.Create(ctx, invalid); err == nil {
		t.Fatal("API server admitted durations outside the CRD bounds")
	}
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress"}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", SecretRef: egressv1alpha1.SecretKeyReference{Name: "input", Key: "nodes"}, Format: egressv1alpha1.SourceFormatURIList}}, Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "target", Key: "url"}}}}
	if err := kubeClient.Create(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "pool"}, pool); err != nil {
		t.Fatal(err)
	}
	if pool.Spec.Selection.TopN != 1 || pool.Spec.Selection.Strategy != egressv1alpha1.SelectionAdaptive || pool.Spec.Probe.ExpectedStatus != 204 ||
		pool.Spec.Probe.Timeout == nil || pool.Spec.Probe.Timeout.Duration != 5*time.Second || pool.Spec.RefreshInterval == nil || pool.Spec.RefreshInterval.Duration != 5*time.Minute {
		t.Fatalf("API defaults not applied: %#v", pool.Spec)
	}
	reconciler := &controller.ProxyPoolReconciler{Client: kubeClient, Scheme: scheme, Now: func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) }}
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "pool"}}); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "pool"}, pool); err != nil || pool.Status.AcceptedEndpoints != 1 {
		t.Fatalf("status subresource not updated: %#v, %v", pool.Status, err)
	}

	gateway := &egressv1alpha1.EgressGateway{TypeMeta: metav1.TypeMeta{APIVersion: egressv1alpha1.GroupVersion.String(), Kind: "EgressGateway"}, ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, Engine: egressv1alpha1.EngineMihomo, OutputSecretName: "output"}}
	if err := kubeClient.Create(ctx, gateway); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "gateway"}, gateway); err != nil {
		t.Fatal(err)
	}
	publisher, err := operatoradapter.NewSecretPublisher(kubeClient, scheme, gateway, "output")
	if err != nil {
		t.Fatal(err)
	}
	candidate, _ := artifact.NewCandidate(artifact.Mihomo11931, []byte("synthetic-envtest-artifact"))
	validated, _ := artifact.Validate(ctx, candidate, envtestChecker{})
	if _, err := publisher.Publish(ctx, validated); err != nil {
		t.Fatal(err)
	}
	output := &corev1.Secret{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: "egress", Name: "output"}, output); err != nil {
		t.Fatal(err)
	}
	if owner := metav1.GetControllerOf(output); owner == nil || owner.UID != gateway.UID {
		t.Fatal("published Secret does not have the exact Gateway controller owner")
	}

	managed := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "managed", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, Engine: egressv1alpha1.EngineSingBox, Runtime: &egressv1alpha1.GatewayRuntimeSpec{Managed: &egressv1alpha1.ManagedRuntimeSpec{}}}}
	if err := kubeClient.Create(ctx, managed); err != nil {
		t.Fatalf("managed Gateway rejected: %v", err)
	}
	contradictory := managed.DeepCopy()
	contradictory.ResourceVersion = ""
	contradictory.UID = ""
	contradictory.Name = "contradictory"
	contradictory.Spec.OutputSecretName = "must-not-coexist"
	if err := kubeClient.Create(ctx, contradictory); err == nil {
		t.Fatal("API server admitted managed and BYO output fields together")
	}
	emptyRuntime := managed.DeepCopy()
	emptyRuntime.ResourceVersion = ""
	emptyRuntime.UID = ""
	emptyRuntime.Name = "empty-runtime"
	emptyRuntime.Spec.Runtime = &egressv1alpha1.GatewayRuntimeSpec{}
	if err := kubeClient.Create(ctx, emptyRuntime); err == nil {
		t.Fatal("API server admitted an undiscriminated runtime union")
	}
}
