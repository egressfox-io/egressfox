package operator_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "value"}, AllowHTTP: true, AllowPrivateNetworks: true, AllowLoopback: true,
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
	source.HTTP.Profile = nil
	source.HTTP.ClientIdentity = "invalid-reference"
	if _, err := operatoradapter.ResolveHTTP(ctx, reader, pool, source); err == nil || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid subscription identity was admitted or leaked")
	}
	source.HTTP.ClientIdentity = "stable:duplicate"
	pool.Spec.Sources = []egressv1alpha1.SubscriptionSource{source, {ID: "other", Format: source.Format, HTTP: &egressv1alpha1.HTTPSource{ClientIdentity: "stable:duplicate"}}}
	if _, err := operatoradapter.ResolveHTTP(ctx, reader, pool, source); err == nil {
		t.Fatal("two Pool sources reused one logical subscription identity")
	}
}

func TestHTTPClientIdentityContinuityAndLegacyMigration(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Hwid"))
		_, _ = w.Write([]byte("trojan://synthetic@example.com:443"))
	}))
	defer server.Close()
	ctx := context.Background()
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("11111111-2222-3333-4444-555555555555")}}
	src := egressv1alpha1.SubscriptionSource{ID: "primary", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "value"}, AllowHTTP: true, AllowLoopback: true}}
	pool.Spec.Sources = []egressv1alpha1.SubscriptionSource{src}
	urlSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"value": []byte(server.URL)}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(urlSecret).Build()
	fetch := func(current *egressv1alpha1.ProxyPool) operatoradapter.HTTPConfig {
		t.Helper()
		config, err := operatoradapter.ResolveHTTP(ctx, reader, current, current.Spec.Sources[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := config.HTTP.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
		return config
	}
	base := fetch(pool)
	automatic := seen[len(seen)-1]
	if automatic != "F0131178-AC86-813E-855B-16AD650E68C5" {
		t.Fatal("legacy automatic HWID changed during migration")
	}
	var workers sync.WaitGroup
	results := make(chan operatoradapter.HTTPConfig, 32)
	errors := make(chan error, 32)
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			config, err := operatoradapter.ResolveHTTP(ctx, reader, pool, pool.Spec.Sources[0])
			if err != nil {
				errors <- err
				return
			}
			results <- config
		}()
	}
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	for config := range results {
		if config.Fingerprint != base.Fingerprint {
			t.Fatal("concurrent identity resolution diverged")
		}
	}
	oldIdentity := "legacy:" + string(pool.UID) + "/primary"
	pool.Spec.Sources[0].HTTP.ClientIdentity = oldIdentity
	if explicitLegacy := fetch(pool); explicitLegacy.Fingerprint != base.Fingerprint || seen[len(seen)-1] != automatic {
		t.Fatal("explicit legacy identity changed existing assignment or cache identity")
	}
	urlSecret.Data["value"] = []byte(server.URL + "?renewed=1")
	if err := reader.Update(ctx, urlSecret); err != nil {
		t.Fatal(err)
	}
	if rotatedURL := fetch(pool); rotatedURL.Fingerprint == base.Fingerprint || seen[len(seen)-1] != automatic {
		t.Fatal("URL Secret rotation reused cache identity or changed HWID")
	}
	pool.Spec.Sources[0].ID = "renamed"
	pool.Spec.Sources[0].Format = egressv1alpha1.SourceFormatAuto
	pool.Spec.Sources[0].HTTP.Profile = &egressv1alpha1.HTTPClientProfile{UserAgent: "Different/1", DeviceOS: "Android"}
	if updated := fetch(pool); updated.Fingerprint == base.Fingerprint || seen[len(seen)-1] != automatic {
		t.Fatal("renamed/reconfigured subscription rotated HWID or kept incompatible cache")
	}
	moved := pool.DeepCopy()
	moved.UID = types.UID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if movedConfig := fetch(moved); movedConfig.Key == base.Key || seen[len(seen)-1] != automatic {
		t.Fatal("explicit continuity reference failed across Pool move")
	}
	moved.Spec.Sources[0].HTTP.Profile.HWID = "fixed-user-hwid"
	fetch(moved)
	if seen[len(seen)-1] != "fixed-user-hwid" {
		t.Fatal("explicit HWID override did not take precedence")
	}
	moved.Spec.Sources[0].HTTP.Profile.HWID = ""
	fetch(moved)
	if seen[len(seen)-1] != automatic {
		t.Fatal("removing override did not restore automatic assignment")
	}
	moved.Spec.Sources[0].HTTP.Profile = &egressv1alpha1.HTTPClientProfile{Mode: "Clean"}
	fetch(moved)
	if seen[len(seen)-1] != "" {
		t.Fatal("clean mode transmitted an HWID")
	}
	moved.Spec.Sources[0].HTTP.Profile = nil
	fetch(moved)
	if seen[len(seen)-1] != automatic {
		t.Fatal("leaving clean mode reset the automatic HWID")
	}
	moved.Spec.Sources[0].HTTP.ClientIdentity = "stable:portable-subscription-2"
	fetch(moved)
	if seen[len(seen)-1] == automatic {
		t.Fatal("explicit identity reset did not rotate automatic HWID")
	}
	moved.Spec.Sources[0].HTTP.ClientIdentity = ""
	fetch(moved)
	if seen[len(seen)-1] == automatic {
		t.Fatal("recreated source unexpectedly retained deleted Pool identity")
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
		AllowHTTP:     true, AllowPrivateNetworks: true, AllowLoopback: true,
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
