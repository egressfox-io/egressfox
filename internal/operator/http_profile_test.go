package operator_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
)

func TestHTTPProfileWireAndFingerprint(t *testing.T) {
	var received []http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Header.Clone())
		_, _ = w.Write([]byte("vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=tls"))
	}))
	defer server.Close()
	ctx := context.Background()
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("pool-uid")}}
	source := egressv1alpha1.SubscriptionSource{ID: "primary", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{
		URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "value"}, AllowHTTP: true, AllowPrivateNetworks: true,
	}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"value": []byte(server.URL)}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(secret).Build()
	fetch := func() operatoradapter.HTTPConfig {
		t.Helper()
		config, err := operatoradapter.ResolveHTTP(ctx, reader, pool, source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = config.HTTP.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
		return config
	}
	baseline := fetch()
	if received[0].Get("User-Agent") != "Happ/1.0" || received[0].Get("X-Hwid") == "" || received[0].Get("X-Device-Os") != "iOS" || received[0].Get("X-Ver-Os") != "18.3" || received[0].Get("X-Device-Model") != "iPhone 14 Pro Max" {
		t.Fatal("default profile missing required request headers")
	}
	firstHWID := received[0].Get("X-Hwid")
	if same := fetch(); same.Fingerprint != baseline.Fingerprint || received[1].Get("X-Hwid") != firstHWID {
		t.Fatal("profile changed during ordinary refresh")
	}
	source.HTTP.Profile = &egressv1alpha1.HTTPClientProfile{UserAgent: "Custom/2", HWID: "fixed-hwid", DeviceOS: "Android", OSVersion: "15", DeviceModel: "Pixel", Headers: map[string]string{"X-Provider-Flavor": "mobile"}}
	custom := fetch()
	if custom.Fingerprint == baseline.Fingerprint || received[2].Get("User-Agent") != "Custom/2" || received[2].Get("X-Hwid") != "fixed-hwid" || received[2].Get("X-Device-Os") != "Android" || received[2].Get("X-Ver-Os") != "15" || received[2].Get("X-Device-Model") != "Pixel" || received[2].Get("X-Provider-Flavor") != "mobile" {
		t.Fatal("custom profile missing or cache identity unchanged")
	}
	source.HTTP.Profile = &egressv1alpha1.HTTPClientProfile{Mode: "Clean"}
	clean := fetch()
	if clean.Fingerprint == custom.Fingerprint {
		t.Fatal("clean profile reused custom cache")
	}
	for _, key := range []string{"User-Agent", "X-Hwid", "X-Device-Os", "X-Ver-Os", "X-Device-Model", "X-Provider-Flavor"} {
		if values := received[3].Values(key); len(values) != 0 {
			t.Fatalf("clean request sent %s: %q", key, values)
		}
	}
	other := pool.DeepCopy()
	other.UID = "other-pool-uid"
	source.HTTP.Profile = nil
	config, err := operatoradapter.ResolveHTTP(ctx, reader, other, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.HTTP.Acquire(ctx); err != nil || received[4].Get("X-Hwid") == firstHWID {
		t.Fatal("unrelated source shares HWID")
	}
}

func TestHTTPProfileRejectsUnsafeHeaders(t *testing.T) {
	ctx := context.Background()
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: "uid"}}
	source := egressv1alpha1.SubscriptionSource{ID: "main", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "value"}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"value": []byte("https://example.com/sub?token=private-canary")}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(secret).Build()
	for _, profile := range []*egressv1alpha1.HTTPClientProfile{
		{Headers: map[string]string{"Authorization": "Bearer secret-canary"}},
		{Headers: map[string]string{"Host": "elsewhere"}},
		{Headers: map[string]string{"X-Bad": "line\r\nInjected: yes"}},
		{Headers: map[string]string{"X-HWID": "override"}},
		{Mode: "Clean", UserAgent: "conflict"},
		{UserAgent: "bad\nvalue"},
	} {
		source.HTTP.Profile = profile
		_, err := operatoradapter.ResolveHTTP(ctx, reader, pool, source)
		if err == nil || strings.Contains(err.Error(), "canary") {
			t.Fatalf("unsafe profile result: %v", err)
		}
	}
}

func TestHTTPSecretHeaderWireAndRotation(t *testing.T) {
	var observed string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	ctx := context.Background()
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Namespace: "egress", UID: "uid"}}
	source := egressv1alpha1.SubscriptionSource{ID: "main", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{
		URLSecretRef:  egressv1alpha1.SecretKeyReference{Name: "url", Key: "value"},
		SecretHeaders: []egressv1alpha1.HTTPSecretHeader{{Name: "X-Api-Key", SecretRef: egressv1alpha1.SecretKeyReference{Name: "header", Key: "value"}}},
		AllowHTTP:     true, AllowPrivateNetworks: true,
	}}
	urlSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"value": []byte(server.URL)}}
	headerSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "header", Namespace: "egress"}, Data: map[string][]byte{"value": []byte("synthetic-one")}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(urlSecret, headerSecret).Build()
	first, err := operatoradapter.ResolveHTTP(ctx, reader, pool, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.HTTP.Acquire(ctx); err != nil || observed != "synthetic-one" {
		t.Fatal("Secret header missing")
	}
	headerSecret.Data["value"] = []byte("synthetic-two")
	if err := reader.Update(ctx, headerSecret); err != nil {
		t.Fatal(err)
	}
	second, err := operatoradapter.ResolveHTTP(ctx, reader, pool, source)
	if err != nil || second.Fingerprint == first.Fingerprint {
		t.Fatal("Secret header rotation kept cache")
	}
	if _, err := second.HTTP.Acquire(ctx); err != nil || observed != "synthetic-two" {
		t.Fatal("rotated Secret header missing")
	}
}
