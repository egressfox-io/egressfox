package operator_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/artifact"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
)

type acceptingChecker struct{}

func (acceptingChecker) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	return artifact.Evidence{ValidatorID: "test/kubernetes"}, nil
}

func TestSecretPublisherCreateNoOpAndReplace(t *testing.T) {
	scheme := testScheme(t)
	owner := testGateway()
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()
	publisher, err := operatoradapter.NewSecretPublisher(kubeClient, scheme, owner, "rendered")
	if err != nil {
		t.Fatal(err)
	}
	first := validated(t, "first-artifact-secret-canary")
	result, err := publisher.Publish(context.Background(), first)
	if err != nil || !result.Changed {
		t.Fatalf("first publish = %v, %v", result, err)
	}
	result, err = publisher.Publish(context.Background(), first)
	if err != nil || result.Changed {
		t.Fatalf("no-op publish = %v, %v", result, err)
	}
	receipt, exists, err := publisher.CurrentReceipt(context.Background())
	if err != nil || !exists {
		t.Fatalf("CurrentReceipt() = %v, %v", exists, err)
	}
	want, _ := first.Receipt()
	if !receipt.Equal(want) {
		t.Fatal("published receipt does not match candidate")
	}
	second := validated(t, "second-artifact-secret-canary")
	current := &corev1.Secret{}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "rendered"}, current); err != nil {
		t.Fatal(err)
	}
	current.Annotations = map[string]string{"example.test/user-metadata": "preserved"}
	current.Labels["example.test/user-label"] = "preserved"
	if err := kubeClient.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	result, err = publisher.Publish(context.Background(), second)
	if err != nil || !result.Changed {
		t.Fatalf("replacement = %v, %v", result, err)
	}
	secret := &corev1.Secret{}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "rendered"}, secret); err != nil {
		t.Fatal(err)
	}
	if secret.Type != operatoradapter.EngineSecretType || len(secret.Data) != 2 || string(secret.Data["config.yaml"]) != "second-artifact-secret-canary" {
		t.Fatal("unexpected managed Secret content contract")
	}
	if secret.Annotations["example.test/user-metadata"] != "preserved" || secret.Labels["example.test/user-label"] != "preserved" {
		t.Fatal("publisher removed user-owned Secret metadata")
	}
	controller := metav1.GetControllerOf(secret)
	if controller == nil || controller.UID != owner.UID {
		t.Fatal("output Secret lacks exact controller ownership")
	}
}

func TestSecretPublisherRefusesCollisionWithoutLeakingData(t *testing.T) {
	scheme := testScheme(t)
	owner := testGateway()
	canary := "foreign-secret-canary"
	foreign := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rendered", Namespace: "egress"}, Data: map[string][]byte{"value": []byte(canary)}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, foreign).Build()
	publisher, err := operatoradapter.NewSecretPublisher(kubeClient, scheme, owner, "rendered")
	if err != nil {
		t.Fatal(err)
	}
	_, err = publisher.Publish(context.Background(), validated(t, "candidate-secret-canary"))
	if err == nil || strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), "candidate-secret-canary") {
		t.Fatalf("unsafe or missing collision error: %v", err)
	}
	unchanged := &corev1.Secret{}
	if getErr := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "rendered"}, unchanged); getErr != nil || string(unchanged.Data["value"]) != canary {
		t.Fatal("foreign Secret was altered")
	}
}

func TestSecretPublisherRejectsOversizedArtifactBeforeMutation(t *testing.T) {
	scheme := testScheme(t)
	owner := testGateway()
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()
	publisher, err := operatoradapter.NewSecretPublisher(kubeClient, scheme, owner, "rendered")
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("x", operatoradapter.MaxSecretDataBytes)
	_, err = publisher.Publish(context.Background(), validated(t, content))
	if err == nil || strings.Contains(err.Error(), content[:32]) {
		t.Fatalf("unsafe or missing size error: %v", err)
	}
	secret := &corev1.Secret{}
	if getErr := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "rendered"}, secret); getErr == nil {
		t.Fatal("oversized artifact created an output Secret")
	}
}

func TestSecretPublisherFailurePreservesKnownGood(t *testing.T) {
	scheme := testScheme(t)
	owner := testGateway()
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()
	publisher, err := operatoradapter.NewSecretPublisher(base, scheme, owner, "rendered")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), validated(t, "known-good")); err != nil {
		t.Fatal(err)
	}
	failing, err := operatoradapter.NewSecretPublisher(updateFailingClient{Client: base}, scheme, owner, "rendered")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.Publish(context.Background(), validated(t, "candidate-secret-canary")); err == nil || strings.Contains(err.Error(), "candidate-secret-canary") {
		t.Fatalf("unsafe or missing update failure: %v", err)
	}
	secret := &corev1.Secret{}
	if err := base.Get(context.Background(), types.NamespacedName{Namespace: "egress", Name: "rendered"}, secret); err != nil || string(secret.Data["config.yaml"]) != "known-good" {
		t.Fatal("failed update replaced known-good output")
	}
}

func TestSnapshotGuardRejectsChangedInput(t *testing.T) {
	scheme := testScheme(t)
	owner := testGateway()
	owner.ResourceVersion = "1"
	pool := testPool()
	pool.ResourceVersion = "1"
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "subscription", Namespace: "egress", UID: types.UID("source-uid"), ResourceVersion: "1"}, Data: map[string][]byte{"nodes": []byte("first")}}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, pool, source).Build()
	name := types.NamespacedName{Namespace: "egress", Name: "subscription"}
	guard, err := operatoradapter.NewSnapshotGuard(base, owner, pool, map[types.NamespacedName]operatoradapter.ResourceRevision{name: {UID: source.UID, ResourceVersion: source.ResourceVersion}})
	if err != nil {
		t.Fatal(err)
	}
	current := &corev1.Secret{}
	if err := base.Get(context.Background(), name, current); err != nil {
		t.Fatal(err)
	}
	current.Data["nodes"] = []byte("changed-secret-canary")
	if err := base.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	publisher, err := operatoradapter.NewSecretPublisher(base, scheme, owner, "rendered", guard)
	if err != nil {
		t.Fatal(err)
	}
	_, err = publisher.Publish(context.Background(), validated(t, "candidate-secret-canary"))
	if err == nil || strings.Contains(err.Error(), "changed-secret-canary") || strings.Contains(err.Error(), "candidate-secret-canary") {
		t.Fatalf("unsafe or missing obsolete-snapshot failure: %v", err)
	}
}

type updateFailingClient struct{ client.Client }

func (updateFailingClient) Update(context.Context, client.Object, ...client.UpdateOption) error {
	return errors.New("injected update failure")
}

func testGateway() *egressv1alpha1.EgressGateway {
	return &egressv1alpha1.EgressGateway{
		TypeMeta:   metav1.TypeMeta{APIVersion: egressv1alpha1.GroupVersion.String(), Kind: "EgressGateway"},
		ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "egress", UID: types.UID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")},
		Spec:       egressv1alpha1.EgressGatewaySpec{Engine: egressv1alpha1.EngineMihomo},
	}
}

func validated(t *testing.T, content string) artifact.Validated {
	t.Helper()
	candidate, err := artifact.NewCandidate(artifact.Mihomo11931, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	value, err := artifact.Validate(context.Background(), candidate, acceptingChecker{})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
