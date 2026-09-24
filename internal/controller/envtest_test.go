package controller_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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
	"github.com/egressfox-io/egressfox/internal/state"
)

type envtestChecker struct{}

func (envtestChecker) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	return artifact.Evidence{ValidatorID: "test/envtest"}, nil
}

type envtestPublicationGate struct{}

func (envtestPublicationGate) Check(context.Context) error { return nil }

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
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
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
	invalid := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "egress"}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", SecretRef: &egressv1alpha1.SecretKeyReference{Name: "input", Key: "nodes"}, Format: egressv1alpha1.SourceFormatURIList}}, Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "target", Key: "url"}, Timeout: &invalidTimeout}, RefreshInterval: &invalidRefresh}}
	if err := kubeClient.Create(ctx, invalid); err == nil {
		t.Fatal("API server admitted durations outside the CRD bounds")
	}
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress"}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "main", SecretRef: &egressv1alpha1.SecretKeyReference{Name: "input", Key: "nodes"}, Format: egressv1alpha1.SourceFormatURIList}}, Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "target", Key: "url"}}}}
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte("trojan://synthetic@edge.example.com:443?security=tls"))
	}))
	defer server.Close()
	urlSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "subscription-url", Namespace: "egress"}, Data: map[string][]byte{"url": []byte(server.URL)}}
	if err := kubeClient.Create(ctx, urlSecret); err != nil {
		t.Fatal(err)
	}
	httpPool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "http-pool", Namespace: "egress"}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "managed", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "subscription-url", Key: "url"}, AllowHTTP: true, AllowPrivateNetworks: true, AllowLoopback: true}}}, Probe: egressv1alpha1.ProbeSpec{TargetSecretRef: egressv1alpha1.SecretKeyReference{Name: "target", Key: "url"}}}}
	if err := kubeClient.Create(ctx, httpPool); err != nil {
		t.Fatalf("HTTP union rejected: %v", err)
	}
	if httpPool.Spec.Sources[0].HTTP.MaxStale == nil || httpPool.Spec.Sources[0].HTTP.MaxStale.Duration != 24*time.Hour {
		t.Fatal("maxStale default missing")
	}
	compatPool := httpPool.DeepCopy()
	compatPool.Name = "compat-pool"
	compatPool.ResourceVersion = ""
	compatPool.UID = ""
	compatPool.Spec.Sources[0].Format = egressv1alpha1.SourceFormatAuto
	compatPool.Spec.Sources[0].HTTP.ClientIdentity = "stable:provider-a"
	compatPool.Spec.Sources[0].HTTP.AllowLoopback = true
	compatPool.Spec.Sources[0].HTTP.Profile = &egressv1alpha1.HTTPClientProfile{UserAgent: "ProviderClient/2", HWID: "fixed-hwid", DeviceOS: "Android", OSVersion: "15", DeviceModel: "Pixel", Headers: map[string]string{"X-Variant": "mobile"}}
	compatPool.Spec.Sources[0].HTTP.SecretHeaders = []egressv1alpha1.HTTPSecretHeader{{Name: "X-Api-Key", SecretRef: egressv1alpha1.SecretKeyReference{Name: "provider-key", Key: "token"}}}
	if err := kubeClient.Create(ctx, compatPool); err != nil {
		t.Fatalf("compatibility profile rejected: %v", err)
	}
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(compatPool), compatPool); err != nil || compatPool.Spec.Sources[0].HTTP.Profile.HWID != "fixed-hwid" || len(compatPool.Spec.Sources[0].HTTP.SecretHeaders) != 1 || compatPool.Spec.Sources[0].HTTP.ClientIdentity != "stable:provider-a" || !compatPool.Spec.Sources[0].HTTP.AllowLoopback {
		t.Fatalf("compatibility fields did not round-trip: %v", err)
	}
	invalidProfile := compatPool.DeepCopy()
	invalidProfile.Name = "invalid-profile"
	invalidProfile.ResourceVersion = ""
	invalidProfile.UID = ""
	invalidProfile.Spec.Sources[0].HTTP.Profile.Mode = "Undocumented"
	if err := kubeClient.Create(ctx, invalidProfile); err == nil {
		t.Fatal("API admitted unknown request-profile mode")
	}
	badUnion := httpPool.DeepCopy()
	badUnion.Name = "bad-union"
	badUnion.ResourceVersion = ""
	badUnion.UID = ""
	badUnion.Spec.Sources[0].SecretRef = &egressv1alpha1.SecretKeyReference{Name: "input", Key: "nodes"}
	if err := kubeClient.Create(ctx, badUnion); err == nil {
		t.Fatal("API admitted contradictory source union")
	}
	missingUnion := httpPool.DeepCopy()
	missingUnion.Name = "missing-union"
	missingUnion.ResourceVersion = ""
	missingUnion.UID = ""
	missingUnion.Spec.Sources[0].HTTP = nil
	if err := kubeClient.Create(ctx, missingUnion); err == nil {
		t.Fatal("API admitted missing source variant")
	}
	cacheDir := t.TempDir()
	if err := os.Chmod(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cacheStore, err := state.Open(filepath.Join(cacheDir, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer cacheStore.Close()
	cacheNow := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	if changed, err := operatoradapter.RefreshHTTP(ctx, kubeClient, cacheStore, httpPool, httpPool.Spec.Sources[0], cacheNow); err != nil || !changed {
		t.Fatalf("HTTP refresh = %v, %v", changed, err)
	}
	cacheReconciler := &controller.ProxyPoolReconciler{Client: kubeClient, Scheme: scheme, Store: cacheStore, Now: func() time.Time { return cacheNow }}
	if _, err := cacheReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(httpPool)}); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(httpPool), httpPool); err != nil || httpPool.Status.AcceptedEndpoints != 1 || len(httpPool.Status.Sources) != 1 {
		t.Fatalf("HTTP status = %+v, %v", httpPool.Status, err)
	}
	if changed, err := operatoradapter.RefreshHTTP(ctx, kubeClient, cacheStore, httpPool, httpPool.Spec.Sources[0], cacheNow.Add(time.Minute)); err != nil || changed {
		t.Fatalf("HTTP 304 = %v, %v", changed, err)
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

	// Exercise the managed adapter against a real API server. This verifies
	// immutable Secret and owner-reference admission, create/no-op behavior and
	// explicit managed-to-BYO cleanup without relying on fake-client semantics.
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(managed), managed); err != nil {
		t.Fatal(err)
	}
	managedRuntime, err := operatoradapter.NewManagedRuntime(operatoradapter.ManagedRuntimeConfig{
		Client: kubeClient,
		Scheme: scheme,
		Image:  "registry.example/egressfox@sha256:synthetic",
		Random: bytes.NewReader(bytes.Repeat([]byte{0x51}, 128)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := managedRuntime.Prepare(ctx, managed); err != nil {
		t.Fatal(err)
	}
	managedPublisher, err := managedRuntime.Publisher(managed, envtestPublicationGate{})
	if err != nil {
		t.Fatal(err)
	}
	managedCandidate, err := artifact.NewCandidate(artifact.SingBox1141, []byte(`{"inbounds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	managedValidated, err := artifact.Validate(ctx, managedCandidate, envtestChecker{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := managedPublisher.Publish(ctx, managedValidated); err != nil {
		t.Fatal(err)
	}
	generation := managedPublisher.GenerationName()
	if generation == "" {
		t.Fatal("managed publication did not assign a generation")
	}
	if _, err := managedRuntime.Reconcile(ctx, managed, generation); err != nil {
		t.Fatal(err)
	}
	deployments := &appsv1.DeploymentList{}
	if err := kubeClient.List(ctx, deployments, client.InNamespace(managed.Namespace), client.MatchingLabels{operatoradapter.GatewayUIDLabel: string(managed.UID)}); err != nil {
		t.Fatal(err)
	}
	if len(deployments.Items) != 1 {
		t.Fatalf("managed Deployments = %d, want 1", len(deployments.Items))
	}
	deployment := &deployments.Items[0]
	deploymentKey := client.ObjectKeyFromObject(deployment)
	if owner := metav1.GetControllerOf(deployment); owner == nil || owner.UID != managed.UID {
		t.Fatal("managed Deployment does not have the exact Gateway controller owner")
	}
	beforeRuntimeVersion := deployment.ResourceVersion
	if _, err := managedRuntime.Reconcile(ctx, managed, generation); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, deploymentKey, deployment); err != nil {
		t.Fatal(err)
	}
	if deployment.ResourceVersion != beforeRuntimeVersion {
		t.Fatalf("no-op managed reconcile wrote Deployment: %s -> %s", beforeRuntimeVersion, deployment.ResourceVersion)
	}
	if err := managedRuntime.Cleanup(ctx, managed); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(ctx, deploymentKey, &appsv1.Deployment{}); err == nil {
		t.Fatal("managed Deployment survived cleanup")
	} else if client.IgnoreNotFound(err) != nil {
		t.Fatalf("read managed Deployment after cleanup: %v", err)
	}
}
