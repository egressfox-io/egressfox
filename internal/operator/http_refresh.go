package operator

import (
	"bytes"
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/source"
	"github.com/egressfox-io/egressfox/internal/state"
)

// RefreshHTTP admits a response before replacing private cache state. An API and
// Secret reread prevents obsolete in-flight work from committing after rotation.
func RefreshHTTP(ctx context.Context, reader client.Reader, store *state.Store, pool *egressv1alpha1.ProxyPool, desired egressv1alpha1.SubscriptionSource, now time.Time) (bool, error) {
	config, err := ResolveHTTP(ctx, reader, pool, desired)
	if err != nil {
		return false, err
	}
	entry, found, cacheErr := store.LoadSourceCache(ctx, config.Key, config.Fingerprint)
	if cacheErr != nil {
		found = false
	}
	usable := found && !now.Before(entry.ValidatedAt.Add(-5*time.Minute)) && now.Before(entry.ValidatedAt.Add(maxSourceStale(desired.HTTP.MaxStale)))
	etag, modified := "", ""
	if usable {
		etag, modified = entry.ETag, entry.LastModified
	}
	deadline, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, err := source.RetryFetch(deadline, func(attemptCtx context.Context) (source.FetchResult, error) {
		return config.HTTP.Fetch(attemptCtx, etag, modified)
	}, nil)
	if err != nil {
		return false, poolFailure("source_acquire")
	}
	if result.NotModified && !usable {
		result, err = config.HTTP.Fetch(deadline, "", "")
		if err != nil || result.NotModified {
			return false, poolFailure("source_not_modified_without_cache")
		}
	}
	current := &egressv1alpha1.ProxyPool{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: pool.Name}, current); err != nil || current.UID != pool.UID || current.Generation != pool.Generation {
		return false, poolFailure("source_obsolete")
	}
	var stillDesired *egressv1alpha1.SubscriptionSource
	for i := range current.Spec.Sources {
		if current.Spec.Sources[i].ID == desired.ID {
			stillDesired = &current.Spec.Sources[i]
			break
		}
	}
	if stillDesired == nil || stillDesired.HTTP == nil {
		return false, poolFailure("source_obsolete")
	}
	latest, err := ResolveHTTP(ctx, reader, current, *stillDesired)
	if err != nil || latest.Fingerprint != config.Fingerprint {
		return false, poolFailure("source_obsolete")
	}
	if result.NotModified {
		if !usable {
			return false, poolFailure("source_not_modified_without_cache")
		}
		if err := store.ValidateSourceCache(ctx, config.Key, config.Fingerprint, now, result.ETag, result.LastModified); err != nil {
			return false, poolFailure("cache_validate")
		}
		return false, nil
	}
	snapshot, _, err := parseSource(result.Payload, desired, pool)
	if err != nil {
		return false, poolFailure("source_rejected")
	}
	if _, err := (source.Set{}).Replace(snapshot, desired.AllowEmpty); err != nil {
		return false, poolFailure("snapshot_rejected")
	}
	newEntry := state.SourceCache{Key: config.Key, Fingerprint: config.Fingerprint, Body: result.Payload.Bytes(), ContentType: result.Payload.ContentType(), ETag: result.ETag, LastModified: result.LastModified, AcceptedAt: now, ValidatedAt: now}
	if err := store.SaveSourceCache(ctx, newEntry); err != nil {
		return false, poolFailure("cache_write")
	}
	return !usable || !bytes.Equal(entry.Body, newEntry.Body), nil
}
