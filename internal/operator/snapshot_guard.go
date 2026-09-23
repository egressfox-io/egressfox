package operator

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/state"
)

var ErrSnapshotObsolete = errors.New("Kubernetes input snapshot is obsolete")

// SnapshotGuard prevents completed slow work from publishing after one of the
// Kubernetes objects that produced it has changed. Kubernetes cannot provide a
// transaction across these objects, so the check is deliberately performed at
// the final publication boundary.
type SnapshotGuard struct {
	reader        client.Reader
	gateway       objectRevision
	pool          objectRevision
	secrets       map[types.NamespacedName]ResourceRevision
	cache         *state.Store
	cacheVersions map[string]CacheVersion
}

func (g *SnapshotGuard) BindCache(store *state.Store, versions map[string]CacheVersion) {
	g.cache = store
	g.cacheVersions = versions
}

type objectRevision struct {
	name            types.NamespacedName
	uid             types.UID
	generation      int64
	resourceVersion string
}

func NewSnapshotGuard(reader client.Reader, gateway *egressv1alpha1.EgressGateway, pool *egressv1alpha1.ProxyPool, secrets map[types.NamespacedName]ResourceRevision) (*SnapshotGuard, error) {
	if reader == nil || gateway == nil || pool == nil || gateway.UID == "" || pool.UID == "" {
		return nil, ErrSnapshotObsolete
	}
	copySecrets := make(map[types.NamespacedName]ResourceRevision, len(secrets))
	for name, revision := range secrets {
		copySecrets[name] = revision
	}
	return &SnapshotGuard{
		reader:  reader,
		gateway: objectRevision{name: types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name}, uid: gateway.UID, generation: gateway.Generation, resourceVersion: gateway.ResourceVersion},
		pool:    objectRevision{name: types.NamespacedName{Namespace: pool.Namespace, Name: pool.Name}, uid: pool.UID, generation: pool.Generation, resourceVersion: pool.ResourceVersion},
		secrets: copySecrets,
	}, nil
}

func (g *SnapshotGuard) Check(ctx context.Context) error {
	gateway := &egressv1alpha1.EgressGateway{}
	if err := g.reader.Get(ctx, g.gateway.name, gateway); err != nil || !sameRevision(g.gateway, gateway.UID, gateway.Generation, gateway.ResourceVersion) {
		return ErrSnapshotObsolete
	}
	pool := &egressv1alpha1.ProxyPool{}
	if err := g.reader.Get(ctx, g.pool.name, pool); err != nil || !sameRevision(g.pool, pool.UID, pool.Generation, pool.ResourceVersion) {
		return ErrSnapshotObsolete
	}
	for name, expected := range g.secrets {
		secret := &corev1.Secret{}
		err := g.reader.Get(ctx, name, secret)
		if expected.UID == "" && expected.ResourceVersion == "" {
			if !apierrors.IsNotFound(err) {
				return ErrSnapshotObsolete
			}
			continue
		}
		if err != nil || secret.UID != expected.UID || secret.ResourceVersion != expected.ResourceVersion {
			return ErrSnapshotObsolete
		}
	}
	for key, expected := range g.cacheVersions {
		actual, validatedAt, found, err := g.cache.SourceCacheVersion(ctx, key)
		if err != nil || found != expected.Present || found && (actual != expected.Digest || !validatedAt.Equal(expected.ValidatedAt)) {
			return ErrSnapshotObsolete
		}
	}
	return nil
}

func sameRevision(expected objectRevision, uid types.UID, generation int64, resourceVersion string) bool {
	return uid == expected.uid && generation == expected.generation && resourceVersion == expected.resourceVersion
}
