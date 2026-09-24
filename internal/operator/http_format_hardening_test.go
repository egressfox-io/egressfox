package operator_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
	"github.com/egressfox-io/egressfox/internal/state"
)

func TestFormatTransitionsInvalidateCacheWithoutRotatingHWID(t *testing.T) {
	const uri = "trojan://synthetic-password@edge.example.com:443?security=tls"
	const jsonBody = `["` + uri + `"]`
	type request struct{ hwid, validator string }
	var mu sync.Mutex
	var requests []request
	body, tag := uri, `"uri"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, request{r.Header.Get("X-Hwid"), r.Header.Get("If-None-Match")})
		currentBody, currentTag := body, tag
		mu.Unlock()
		if r.Header.Get("If-None-Match") == currentTag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", currentTag)
		_, _ = w.Write([]byte(currentBody))
	}))
	defer server.Close()
	ctx := context.Background()
	pool := &egressv1alpha1.ProxyPool{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "egress", UID: types.UID("11111111-2222-3333-4444-555555555555"), Generation: 1}, Spec: egressv1alpha1.ProxyPoolSpec{Sources: []egressv1alpha1.SubscriptionSource{{ID: "provider", Format: egressv1alpha1.SourceFormatURIList, HTTP: &egressv1alpha1.HTTPSource{URLSecretRef: egressv1alpha1.SecretKeyReference{Name: "url", Key: "value"}, AllowHTTP: true, AllowLoopback: true}}}}}
	urlSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "url", Namespace: "egress"}, Data: map[string][]byte{"value": []byte(server.URL)}}
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(pool, urlSecret).Build()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(directory, "state.db"), state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	refresh := func(at time.Time) error {
		t.Helper()
		_, err := operatoradapter.RefreshHTTP(ctx, reader, store, pool, pool.Spec.Sources[0], at)
		return err
	}
	updateFormat := func(format egressv1alpha1.SourceFormat) {
		t.Helper()
		pool.Spec.Sources[0].Format = format
		if err := reader.Update(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	if err := refresh(now); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	automatic := requests[0].hwid
	mu.Unlock()
	if automatic == "" {
		t.Fatal("automatic HWID missing")
	}
	updateFormat(egressv1alpha1.SourceFormatAuto)
	config, err := operatoradapter.ResolveHTTP(ctx, reader, pool, pool.Spec.Sources[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.LoadSourceCache(ctx, config.Key, config.Fingerprint); err != nil || found {
		t.Fatal("format change retained incompatible cache")
	}
	if err := refresh(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if requests[len(requests)-1].validator != "" || requests[len(requests)-1].hwid != automatic {
		t.Fatal("URIList to Auto reused validator or rotated HWID")
	}
	body, tag = jsonBody, `"json"`
	mu.Unlock()
	if err := refresh(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	updateFormat(egressv1alpha1.SourceFormatURIList)
	if err := refresh(now.Add(3 * time.Minute)); err == nil {
		t.Fatal("explicit URIList accepted JSON")
	}
	mu.Lock()
	if requests[len(requests)-1].validator != "" {
		mu.Unlock()
		t.Fatal("Auto to URIList retained incompatible validator")
	}
	mu.Unlock()
	updateFormat(egressv1alpha1.SourceFormatAuto)
	if err := refresh(now.Add(4 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, request := range requests {
		if request.hwid != automatic {
			t.Fatal("format or provider response change rotated HWID")
		}
	}
}
