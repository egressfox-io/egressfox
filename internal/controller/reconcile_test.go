package controller_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/controller"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
)

func TestProxyPoolReconcileReportsSafeAggregateStatus(t *testing.T) {
	scheme := controllerScheme(t)
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("11111111-2222-3333-4444-555555555555")}, Spec: egressv1alpha1.ProxyPoolSpec{
		Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", SecretRef: &egressv1alpha1.SecretKeyReference{Name: "input", Key: "nodes"}, Format: egressv1alpha1.SourceFormatURIList}},
	}}
	canary := "controller-source-secret-canary"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "input", Namespace: "egress"}, Data: map[string][]byte{"nodes": []byte("trojan://" + canary + "@edge.example.com:443?security=tls")}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(pool).WithObjects(pool, secret).Build()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	reconciler := &controller.ProxyPoolReconciler{Client: kubeClient, Scheme: scheme, Now: func() time.Time { return now }}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "pool"}}); err != nil {
		t.Fatal(err)
	}
	current := &egressv1alpha1.ProxyPool{}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "pool"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.AcceptedEndpoints != 1 || len(current.Status.Conditions) == 0 {
		t.Fatalf("unexpected status: %#v", current.Status)
	}
	if strings.Contains(strings.ToLower(strings.Join(conditionMessages(current.Status.Conditions), " ")), canary) {
		t.Fatal("status leaked source credential")
	}
}

type fakePipeline struct {
	outcome operatoradapter.GatewayOutcome
	err     error
}

type fakeRuntime struct {
	outcome operatoradapter.RuntimeOutcome
	err     error
}

func (r fakeRuntime) Reconcile(context.Context, *egressv1alpha1.EgressGateway, string) (operatoradapter.RuntimeOutcome, error) {
	return r.outcome, r.err
}

func (fakeRuntime) Cleanup(context.Context, *egressv1alpha1.EgressGateway) error { return nil }

func (p fakePipeline) Run(context.Context, *egressv1alpha1.EgressGateway, *egressv1alpha1.ProxyPool) (operatoradapter.GatewayOutcome, error) {
	return p.outcome, p.err
}

type resolvingProfilePipeline struct{}

func (resolvingProfilePipeline) Run(_ context.Context, gateway *egressv1alpha1.EgressGateway, pool *egressv1alpha1.ProxyPool) (operatoradapter.GatewayOutcome, error) {
	if _, _, err := operatoradapter.ResolveProfile(pool, gateway.Spec.ProfileRef); err != nil {
		return operatoradapter.GatewayOutcome{}, err
	}
	return operatoradapter.GatewayOutcome{Eligible: 1, Selected: 1, Published: true}, nil
}

func TestGatewayProfileReferenceAndRemovedProfileStatus(t *testing.T) {
	scheme := controllerScheme(t)
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress"}, Spec: egressv1alpha1.ProxyPoolSpec{Profiles: []egressv1alpha1.TargetProfile{{Name: "alpha", Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "target", Key: "url"}}}}}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, ProfileRef: "alpha", Runtime: &egressv1alpha1.GatewayRuntimeSpec{Managed: &egressv1alpha1.ManagedRuntimeSpec{}}}, Status: egressv1alpha1.EgressGatewayStatus{ActiveGeneration: "healthy-old"}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(gateway).WithObjects(pool, gateway).Build()
	reconciler := &controller.EgressGatewayReconciler{Client: kubeClient, Scheme: scheme, Pipeline: resolvingProfilePipeline{}, Runtime: fakeRuntime{outcome: operatoradapter.RuntimeOutcome{ActiveGeneration: "healthy-old", RuntimeReady: true}}}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "gateway"}}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	current := &egressv1alpha1.EgressGateway{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Profile != "alpha" || !conditionTrue(current.Status.Conditions, controller.ConditionSelectionReady) {
		t.Fatalf("named profile status: %#v", current.Status)
	}
	p := &egressv1alpha1.ProxyPool{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(pool), p); err != nil {
		t.Fatal(err)
	}
	p.Spec.Profiles = nil
	if err := kubeClient.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), current); err != nil {
		t.Fatal(err)
	}
	if conditionTrue(current.Status.Conditions, controller.ConditionSelectionReady) || current.Status.Profile != "alpha" || current.Status.ActiveGeneration != "healthy-old" {
		t.Fatalf("removed profile replaced LKG: %#v", current.Status)
	}
}

func TestGatewayReconcileSeparatesDesiredActivationFromLKGReadiness(t *testing.T) {
	scheme := controllerScheme(t)
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress"}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, Runtime: &egressv1alpha1.GatewayRuntimeSpec{Managed: &egressv1alpha1.ManagedRuntimeSpec{}}}, Status: egressv1alpha1.EgressGatewayStatus{ActiveGeneration: "old-generation"}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(gateway).WithObjects(pool, gateway).Build()
	reconciler := &controller.EgressGatewayReconciler{
		Client: kubeClient, Scheme: scheme,
		Pipeline: fakePipeline{outcome: operatoradapter.GatewayOutcome{Eligible: 2, Selected: 1, Published: true, Changed: true, PublishedGeneration: "new-generation"}},
		Runtime:  fakeRuntime{outcome: operatoradapter.RuntimeOutcome{PublishedGeneration: "new-generation", ActiveGeneration: "old-generation", ServiceName: "gateway-proxy", ClientAuthSecretName: "gateway-auth", RuntimeReady: true, Degraded: true, Reason: "ProgressDeadlineExceeded"}},
	}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "gateway"}}); err != nil {
		t.Fatal(err)
	}
	current := &egressv1alpha1.EgressGateway{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), current); err != nil {
		t.Fatal(err)
	}
	if !conditionTrue(current.Status.Conditions, controller.ConditionPublished) || conditionTrue(current.Status.Conditions, controller.ConditionActivated) || !conditionTrue(current.Status.Conditions, controller.ConditionRuntimeReady) || !conditionTrue(current.Status.Conditions, controller.ConditionDegraded) || conditionTrue(current.Status.Conditions, controller.ConditionReady) {
		t.Fatalf("misleading managed conditions: %#v", current.Status.Conditions)
	}
	if current.Status.PublishedGeneration != "new-generation" || current.Status.ActiveGeneration != "old-generation" {
		t.Fatalf("generation attribution = %#v", current.Status)
	}
}

func TestGatewayReconcilePublishesTruthfulConditions(t *testing.T) {
	scheme := controllerScheme(t)
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress"}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(gateway).WithObjects(pool, gateway).Build()
	reconciler := &controller.EgressGatewayReconciler{Client: kubeClient, Scheme: scheme, Pipeline: fakePipeline{outcome: operatoradapter.GatewayOutcome{Eligible: 2, Selected: 1, Published: true, Changed: true}}, Now: func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) }}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "gateway"}}); err != nil {
		t.Fatal(err)
	}
	current := &egressv1alpha1.EgressGateway{}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "gateway"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.SelectedEndpoints != 1 || !conditionTrue(current.Status.Conditions, controller.ConditionPublished) {
		t.Fatalf("unexpected status: %#v", current.Status)
	}
	for _, message := range conditionMessages(current.Status.Conditions) {
		if strings.Contains(strings.ToLower(message), "activated") {
			t.Fatal("status claimed runtime activation")
		}
	}
}

func TestGatewayFailureUsesBoundedSafeStatus(t *testing.T) {
	scheme := controllerScheme(t)
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress"}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(gateway).WithObjects(pool, gateway).Build()
	canary := "pipeline-error-secret-canary"
	reconciler := &controller.EgressGatewayReconciler{Client: kubeClient, Scheme: scheme, Pipeline: fakePipeline{err: errors.New(canary)}}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "gateway"}}); err != nil {
		t.Fatal(err)
	}
	current := &egressv1alpha1.EgressGateway{}
	_ = kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "gateway"}, current)
	if strings.Contains(strings.Join(conditionMessages(current.Status.Conditions), " "), canary) {
		t.Fatal("status leaked pipeline error")
	}
}

func controllerScheme(t *testing.T) *runtime.Scheme {
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

func conditionMessages(conditions []metav1.Condition) []string {
	values := make([]string, 0, len(conditions))
	for _, item := range conditions {
		values = append(values, item.Message)
	}
	return values
}
func conditionTrue(conditions []metav1.Condition, kind string) bool {
	for _, item := range conditions {
		if item.Type == kind {
			return item.Status == metav1.ConditionTrue
		}
	}
	return false
}

func TestGatewayFailedProfileDoesNotKeepPreviousCounts(t *testing.T) {
	scheme := controllerScheme(t)
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress"}, Spec: egressv1alpha1.ProxyPoolSpec{Profiles: []egressv1alpha1.TargetProfile{{Name: "alpha", Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "target", Key: "url"}}}}}}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, ProfileRef: "alpha", Runtime: &egressv1alpha1.GatewayRuntimeSpec{Managed: &egressv1alpha1.ManagedRuntimeSpec{}}}, Status: egressv1alpha1.EgressGatewayStatus{}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(gateway).WithObjects(pool, gateway).Build()
	reconciler := &controller.EgressGatewayReconciler{Client: kubeClient, Scheme: scheme, Pipeline: resolvingProfilePipeline{}, Runtime: fakeRuntime{outcome: operatoradapter.RuntimeOutcome{ActiveGeneration: "healthy-old", RuntimeReady: true}}}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "gateway"}}
	current := &egressv1alpha1.EgressGateway{}
	reconcile := func() {
		t.Helper()
		if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), current); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	if current.Status.Profile != "alpha" || current.Status.EligibleEndpoints != 1 || current.Status.SelectedEndpoints != 1 {
		t.Fatalf("healthy profile status: %#v", current.Status)
	}
	// Switch to a profile that does not exist: the desired profile is reported
	// with no counts, while the last-known-good generations are preserved.
	current.Status.PublishedGeneration = "published-old"
	if err := kubeClient.Status().Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	current.Spec.ProfileRef = "missing"
	if err := kubeClient.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	reconcile()
	status := current.Status
	if status.Profile != "missing" || status.EligibleEndpoints != 0 || status.SelectedEndpoints != 0 {
		t.Fatalf("failed profile shows stale counts: %#v", status)
	}
	if conditionTrue(status.Conditions, controller.ConditionSelectionReady) || conditionTrue(status.Conditions, controller.ConditionReady) {
		t.Fatalf("failed profile reported ready: %#v", status.Conditions)
	}
	if status.ActiveGeneration != "healthy-old" || status.PublishedGeneration != "published-old" {
		t.Fatalf("LKG generations were not preserved: %#v", status)
	}
}

type codedError string

func (e codedError) Error() string { return "pipeline failed" }
func (e codedError) Code() string  { return string(e) }

// TopN is an upper bound: the status distinguishes a full selection, a
// shortfall caused by the maintainable probe cohort, a shortfall caused by
// evidence, probe overload and a refused shared-capacity reservation.
func TestGatewaySelectionShortfallReasons(t *testing.T) {
	for _, test := range []struct {
		name    string
		outcome operatoradapter.GatewayOutcome
		err     error
		status  metav1.ConditionStatus
		reason  string
	}{
		{"below capacity, full", operatoradapter.GatewayOutcome{Eligible: 9, Selected: 5, RequestedTopN: 5, EffectiveTopN: 5, Published: true}, nil, metav1.ConditionTrue, "SelectionReady"},
		{"exactly at capacity", operatoradapter.GatewayOutcome{Eligible: 26, Selected: 24, RequestedTopN: 24, EffectiveTopN: 24, Published: true}, nil, metav1.ConditionTrue, "SelectionReady"},
		{"above capacity", operatoradapter.GatewayOutcome{Eligible: 26, Selected: 24, RequestedTopN: 100, EffectiveTopN: 24, Published: true}, nil, metav1.ConditionTrue, "ProbeCapacityLimited"},
		{"above capacity, evidence-limited", operatoradapter.GatewayOutcome{Eligible: 10, Selected: 10, RequestedTopN: 100, EffectiveTopN: 24, Published: true}, nil, metav1.ConditionTrue, "InsufficientEligibleEndpoints"},
		{"below capacity, evidence-limited", operatoradapter.GatewayOutcome{Eligible: 3, Selected: 3, RequestedTopN: 5, EffectiveTopN: 5, Published: true}, nil, metav1.ConditionTrue, "InsufficientEligibleEndpoints"},
		{"overloaded with selection", operatoradapter.GatewayOutcome{Eligible: 1, Selected: 1, RequestedTopN: 1, EffectiveTopN: 1, Published: true, ProbeOverloaded: true}, nil, metav1.ConditionTrue, "ProbeRoundOverloaded"},
		{"overloaded without selection", operatoradapter.GatewayOutcome{RequestedTopN: 1, EffectiveTopN: 1, ProbeOverloaded: true}, nil, metav1.ConditionFalse, "ProbeRoundOverloaded"},
		{"no evidence", operatoradapter.GatewayOutcome{RequestedTopN: 1, EffectiveTopN: 1}, nil, metav1.ConditionFalse, "NoEligibleEndpoints"},
		{"limited by shared demand", operatoradapter.GatewayOutcome{Eligible: 26, Selected: 24, RequestedTopN: 24, EffectiveTopN: 24, Published: true, SharedCapacityLimited: true}, nil, metav1.ConditionTrue, "ProbeCapacityShared"},
		{"shared demand leaves no maintainable selection", operatoradapter.GatewayOutcome{}, codedError("probe_capacity"), metav1.ConditionFalse, "ProbeCapacityExceeded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := controllerScheme(t)
			pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress"}}
			gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress"}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}}}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(gateway).WithObjects(pool, gateway).Build()
			reconciler := &controller.EgressGatewayReconciler{Client: kubeClient, Scheme: scheme, Pipeline: fakePipeline{outcome: test.outcome, err: test.err}, Now: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }}
			if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "egress", Name: "gateway"}}); err != nil {
				t.Fatal(err)
			}
			current := &egressv1alpha1.EgressGateway{}
			if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), current); err != nil {
				t.Fatal(err)
			}
			ready := apimeta.FindStatusCondition(current.Status.Conditions, controller.ConditionSelectionReady)
			if ready == nil || ready.Status != test.status || ready.Reason != test.reason {
				t.Fatalf("SelectionReady = %#v, want %s/%s", ready, test.status, test.reason)
			}
			if test.err != nil && (current.Status.SelectedEndpoints != 0 || conditionTrue(current.Status.Conditions, controller.ConditionPublished)) {
				t.Fatalf("refused reservation reported a published selection: %#v", current.Status)
			}
			if test.err == nil && current.Status.SelectedEndpoints != int32(test.outcome.Selected) {
				t.Fatalf("selected=%d want %d", current.Status.SelectedEndpoints, test.outcome.Selected)
			}
		})
	}
}
