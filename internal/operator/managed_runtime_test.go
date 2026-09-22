package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/artifact"
)

type allowPublication struct{}

func (allowPublication) Check(context.Context) error { return nil }

type acceptArtifact struct{}

func (acceptArtifact) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	return artifact.Evidence{ValidatorID: "managed-runtime-test"}, nil
}

func TestManagedRuntimeCreatesSecureOwnedResourcesAndActivatesExactGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gateway, runtimeAdapter, kubeClient := managedFixture(t)
	listener, _, _, err := runtimeAdapter.Prepare(ctx, gateway)
	if err != nil {
		t.Fatal(err)
	}
	if !listener.Managed() || listener.Address() != "0.0.0.0" {
		t.Fatalf("listener managed=%t address=%q", listener.Managed(), listener.Address())
	}
	passwordCanary := listener.Password()

	publisher, err := runtimeAdapter.Publisher(gateway, allowPublication{})
	if err != nil {
		t.Fatal(err)
	}
	validated := validatedFixture(t, artifact.SingBox1141, []byte(`{"inbounds":[]}`))
	publication, err := publisher.Publish(ctx, validated)
	if err != nil || !publication.Changed {
		t.Fatalf("first publication = %#v, %v", publication, err)
	}
	second, err := publisher.Publish(ctx, validated)
	if err != nil || second.Changed {
		t.Fatalf("identical publication = %#v, %v", second, err)
	}
	generation := publisher.GenerationName()
	if generation == "" || strings.Contains(generation, passwordCanary) {
		t.Fatalf("unsafe generation name %q", generation)
	}

	outcome, err := runtimeAdapter.Reconcile(ctx, gateway, generation)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Activated || outcome.RuntimeReady || outcome.ServiceName == "" || outcome.ClientAuthSecretName == "" {
		t.Fatalf("initial runtime outcome = %#v", outcome)
	}

	deployments := &appsv1.DeploymentList{}
	if err := kubeClient.List(ctx, deployments, client.InNamespace(gateway.Namespace)); err != nil || len(deployments.Items) != 1 {
		t.Fatalf("deployments=%d err=%v", len(deployments.Items), err)
	}
	deployment := &deployments.Items[0]
	pod := deployment.Spec.Template.Spec
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.IntValue() != 0 || deployment.Spec.Strategy.RollingUpdate.MaxSurge.IntValue() != 1 {
		t.Fatalf("unsafe rollout strategy: %#v", deployment.Spec.Strategy)
	}
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken || pod.HostNetwork || pod.HostPID || pod.HostIPC || len(pod.Containers) != 1 {
		t.Fatalf("unsafe Pod contract: %#v", pod)
	}
	container := pod.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.RunAsNonRoot == nil || !*container.SecurityContext.RunAsNonRoot || container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem || container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation || len(container.SecurityContext.Capabilities.Drop) != 1 || container.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("unsafe container security context: %#v", container.SecurityContext)
	}
	encodedPod, err := json.Marshal(deployment.Spec.Template)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encodedPod, []byte(passwordCanary)) {
		t.Fatal("credential leaked into Pod template")
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.Exec == nil || strings.Contains(strings.Join(container.ReadinessProbe.Exec.Command, " "), passwordCanary) {
		t.Fatal("readiness probe is missing or exposes credentials")
	}

	service := &corev1.Service{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: outcome.ServiceName}, service); err != nil {
		t.Fatal(err)
	}
	if service.Spec.Type != corev1.ServiceTypeClusterIP || len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != ManagedSOCKSPort {
		t.Fatalf("unsafe Service: %#v", service.Spec)
	}
	policies := &networkingv1.NetworkPolicyList{}
	if err := kubeClient.List(ctx, policies, client.InNamespace(gateway.Namespace)); err != nil || len(policies.Items) != 1 {
		t.Fatalf("network policies=%d err=%v", len(policies.Items), err)
	}

	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	if err := kubeClient.Status().Update(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	outcome, err = runtimeAdapter.Reconcile(ctx, gateway, generation)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Activated || !outcome.RuntimeReady || outcome.ActiveGeneration != generation {
		t.Fatalf("activated runtime outcome = %#v", outcome)
	}
	gateway.Status.PublishedGeneration = generation
	gateway.Status.ActiveGeneration = generation
	authentication := &corev1.Secret{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: outcome.ClientAuthSecretName}, authentication); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Delete(ctx, authentication); err != nil {
		t.Fatal(err)
	}
	repaired, _, _, err := runtimeAdapter.Prepare(ctx, gateway)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Password() != passwordCanary {
		t.Fatal("auth Secret repair rotated credentials before a coordinated rollout")
	}
}

func TestManagedRuntimeRejectsUnownedResourceCollision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gateway, runtimeAdapter, kubeClient := managedFixture(t)
	conflict := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: managedResourceName(gateway, "proxy"), Namespace: gateway.Namespace}}
	if err := kubeClient.Create(ctx, conflict); err != nil {
		t.Fatal(err)
	}
	_, err := runtimeAdapter.Reconcile(ctx, gateway, "synthetic-generation")
	if !errors.Is(err, ErrManagedRuntime) || runtimeErrorCode(err) != "service_conflict" {
		t.Fatalf("collision error = %v", err)
	}
	current := &corev1.Service{}
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(conflict), current); err != nil {
		t.Fatal(err)
	}
	if metav1.GetControllerOf(current) != nil {
		t.Fatal("unowned Service was adopted")
	}
}

func TestManagedGenerationRetentionIsBounded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gateway, runtimeAdapter, kubeClient := managedFixture(t)
	if _, _, _, err := runtimeAdapter.Prepare(ctx, gateway); err != nil {
		t.Fatal(err)
	}
	var latest string
	for _, content := range []string{"one", "two", "three"} {
		publisher, err := runtimeAdapter.Publisher(gateway, allowPublication{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := publisher.Publish(ctx, validatedFixture(t, artifact.SingBox1141, []byte(content))); err != nil {
			t.Fatal(err)
		}
		latest = publisher.GenerationName()
	}
	if _, err := runtimeAdapter.Reconcile(ctx, gateway, latest); err != nil {
		t.Fatal(err)
	}
	deployments := &appsv1.DeploymentList{}
	if err := kubeClient.List(ctx, deployments, client.InNamespace(gateway.Namespace)); err != nil || len(deployments.Items) != 1 {
		t.Fatal(err)
	}
	deployment := &deployments.Items[0]
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	if err := kubeClient.Status().Update(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeAdapter.Reconcile(ctx, gateway, latest); err != nil {
		t.Fatal(err)
	}
	secrets := &corev1.SecretList{}
	if err := kubeClient.List(ctx, secrets, client.InNamespace(gateway.Namespace), client.MatchingLabels{ComponentLabel: componentGeneration}); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Items) != 2 {
		t.Fatalf("retained generations = %d, want 2", len(secrets.Items))
	}
}

func managedFixture(t *testing.T) (*egressv1alpha1.EgressGateway, *ManagedRuntime, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := egressv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	gateway := &egressv1alpha1.EgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "example.gateway", Namespace: "egress", UID: types.UID("11111111-1111-4111-8111-111111111111")}, Spec: egressv1alpha1.EgressGatewaySpec{PoolRef: egressv1alpha1.LocalReference{Name: "pool"}, Engine: egressv1alpha1.EngineSingBox, Runtime: &egressv1alpha1.GatewayRuntimeSpec{Managed: &egressv1alpha1.ManagedRuntimeSpec{}}}}
	randomInput := append(bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x42}, 10)...)
	randomInput = append(randomInput, bytes.Repeat([]byte{0x43}, 10)...)
	randomInput = append(randomInput, bytes.Repeat([]byte{0x44}, 10)...)
	randomInput = append(randomInput, bytes.Repeat([]byte{0x45}, 10)...)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&appsv1.Deployment{}).Build()
	runtimeAdapter, err := NewManagedRuntime(ManagedRuntimeConfig{Client: kubeClient, Scheme: scheme, Image: "registry.example/egressfox@sha256:synthetic", Random: bytes.NewReader(randomInput)})
	if err != nil {
		t.Fatal(err)
	}
	return gateway, runtimeAdapter, kubeClient
}

func validatedFixture(t *testing.T, profile artifact.Profile, content []byte) artifact.Validated {
	t.Helper()
	candidate, err := artifact.NewCandidate(profile, content)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := artifact.Validate(context.Background(), candidate, acceptArtifact{})
	if err != nil {
		t.Fatal(err)
	}
	return validated
}
