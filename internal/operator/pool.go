// Package operator adapts Kubernetes objects to the Kubernetes-independent core.
package operator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/source"
	"github.com/egressfox-io/egressfox/internal/state"
)

var ErrPool = errors.New("pool snapshot failed")

type PoolError struct{ code string }

func (e *PoolError) Error() string { return "pool snapshot failed code=" + e.code }
func (e *PoolError) Unwrap() error { return ErrPool }
func (e *PoolError) Code() string  { return e.code }

func poolFailure(code string) error { return &PoolError{code: code} }

type PoolResult struct {
	Inventory       endpoint.Inventory
	Accepted        int
	Rejected        int
	Unsupported     int
	SourceRevisions map[types.NamespacedName]ResourceRevision
	Sources         []SourceState
	CacheVersions   map[string]CacheVersion
}

type CacheVersion struct {
	Digest      [32]byte
	ValidatedAt time.Time
	Present     bool
}

type SourceState struct {
	ID          string
	State       string
	Reason      string
	LastSuccess *time.Time
}

// ResourceRevision identifies the exact Kubernetes object version whose bytes
// participated in a reconciliation snapshot. It contains no object data.
type ResourceRevision struct {
	UID             types.UID
	ResourceVersion string
}

// BuildPool reads only explicitly referenced same-namespace Secret keys and
// transactionally builds the current M1 inventory. Raw values never enter errors.
func BuildPool(ctx context.Context, reader client.Reader, pool *egressv1alpha1.ProxyPool) (PoolResult, error) {
	return BuildPoolWithCache(ctx, reader, pool, nil, time.Now())
}

func BuildPoolWithCache(ctx context.Context, reader client.Reader, pool *egressv1alpha1.ProxyPool, cache *state.Store, now time.Time) (PoolResult, error) {
	if reader == nil || pool == nil || pool.Namespace == "" || pool.UID == "" || len(pool.Spec.Sources) == 0 {
		return PoolResult{}, poolFailure("request")
	}
	set := source.Set{}
	result := PoolResult{SourceRevisions: make(map[types.NamespacedName]ResourceRevision, len(pool.Spec.Sources)), CacheVersions: make(map[string]CacheVersion)}
	for _, desired := range pool.Spec.Sources {
		if desired.HTTP != nil {
			if desired.SecretRef != nil || cache == nil {
				return PoolResult{}, poolFailure("source_variant")
			}
			if err := recordHTTPReferences(ctx, reader, pool, desired, result.SourceRevisions); err != nil {
				return PoolResult{}, err
			}
			config, err := ResolveHTTP(ctx, reader, pool, desired)
			if err != nil {
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Unavailable", Reason: "Configuration"})
				continue
			}
			for name, revision := range config.Revisions {
				result.SourceRevisions[name] = revision
			}
			result.CacheVersions[config.Key] = CacheVersion{}
			entry, found, err := cache.LoadSourceCache(ctx, config.Key, config.Fingerprint)
			if err != nil {
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Unavailable", Reason: "CacheCorrupt"})
				continue
			}
			if !found {
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Unavailable", Reason: "CacheMissing"})
				continue
			}
			result.CacheVersions[config.Key] = CacheVersion{Digest: sha256.Sum256(entry.Body), ValidatedAt: entry.ValidatedAt, Present: true}
			maxStale := maxSourceStale(desired.HTTP.MaxStale)
			if now.Before(entry.ValidatedAt.Add(-5 * time.Minute)) {
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Unavailable", Reason: "CacheClockInvalid"})
				continue
			}
			if !now.Before(entry.ValidatedAt.Add(maxStale)) {
				last := entry.ValidatedAt
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Expired", Reason: "CacheExpired", LastSuccess: &last})
				continue
			}
			inline, err := source.NewInline(config.SourceID, entry.Body, source.DefaultLimits())
			if err != nil {
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Unavailable", Reason: "CacheCorrupt"})
				continue
			}
			payload, err := inline.Acquire(ctx)
			if err != nil {
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Unavailable", Reason: "CacheCorrupt"})
				continue
			}
			snapshot, report, err := parseSource(payload, desired, pool)
			if err != nil {
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Unavailable", Reason: "CacheCorrupt"})
				continue
			}
			set, err = set.Replace(snapshot, desired.AllowEmpty)
			if err != nil {
				result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Unavailable", Reason: "CacheCorrupt"})
				continue
			}
			result.Accepted += report.Accepted
			result.Rejected += report.Rejected()
			result.Unsupported += report.Unsupported
			last := entry.ValidatedAt
			stateName, reason := "Fresh", "RecentlyValidated"
			if now.Sub(last) > 3*refreshDuration(pool.Spec.RefreshInterval)/2 {
				stateName, reason = "Cached", "CacheFallback"
			}
			result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: stateName, Reason: reason, LastSuccess: &last})
			continue
		}
		if desired.SecretRef == nil {
			return PoolResult{}, poolFailure("source_variant")
		}
		name := types.NamespacedName{Namespace: pool.Namespace, Name: desired.SecretRef.Name}
		secret := &corev1.Secret{}
		if err := reader.Get(ctx, name, secret); err != nil {
			return PoolResult{}, poolFailure("source_unavailable")
		}
		result.SourceRevisions[name] = ResourceRevision{UID: secret.UID, ResourceVersion: secret.ResourceVersion}
		data, found := secret.Data[desired.SecretRef.Key]
		if !found {
			return PoolResult{}, poolFailure("source_key_missing")
		}
		sourceID, err := endpoint.NewSourceID(fmt.Sprintf("k8s-%s-%s", pool.UID, desired.ID))
		if err != nil {
			return PoolResult{}, poolFailure("source_id")
		}
		inline, err := source.NewInline(sourceID, data, source.DefaultLimits())
		if err != nil {
			return PoolResult{}, poolFailure("source_limits")
		}
		payload, err := inline.Acquire(ctx)
		if err != nil {
			return PoolResult{}, poolFailure("source_acquire")
		}
		snapshot, report, err := parseSource(payload, desired, pool)
		if err != nil {
			return PoolResult{}, poolFailure("source_rejected")
		}
		set, err = set.Replace(snapshot, desired.AllowEmpty)
		if err != nil {
			return PoolResult{}, poolFailure("snapshot_rejected")
		}
		result.Accepted += report.Accepted
		result.Rejected += report.Rejected()
		result.Unsupported += report.Unsupported
		result.Sources = append(result.Sources, SourceState{ID: desired.ID, State: "Fresh", Reason: "SecretAdmitted"})
	}
	result.Inventory, _ = set.Inventory()
	return result, nil
}

func recordHTTPReferences(ctx context.Context, reader client.Reader, pool *egressv1alpha1.ProxyPool, desired egressv1alpha1.SubscriptionSource, revisions map[types.NamespacedName]ResourceRevision) error {
	refs := []egressv1alpha1.SecretKeyReference{desired.HTTP.URLSecretRef}
	if desired.HTTP.AuthorizationSecretRef != nil {
		refs = append(refs, *desired.HTTP.AuthorizationSecretRef)
	}
	for _, ref := range refs {
		name := types.NamespacedName{Namespace: pool.Namespace, Name: ref.Name}
		secret := &corev1.Secret{}
		if err := reader.Get(ctx, name, secret); err != nil {
			if !apierrors.IsNotFound(err) {
				return poolFailure("source_unavailable")
			}
			revisions[name] = ResourceRevision{}
			continue
		}
		revisions[name] = ResourceRevision{UID: secret.UID, ResourceVersion: secret.ResourceVersion}
	}
	return nil
}

func parseSource(payload source.Payload, desired egressv1alpha1.SubscriptionSource, pool *egressv1alpha1.ProxyPool) (source.Snapshot, source.Report, error) {
	format, ok := sourceFormat(desired.Format)
	if !ok {
		return source.Snapshot{}, source.Report{}, poolFailure("source_format")
	}
	return source.Parse(payload, source.ParseOptions{Format: format, Limits: source.DefaultLimits(), Admission: source.Admission{AllowPartial: desired.AllowPartial, AllowedProtocol: []endpoint.Protocol{endpoint.ProtocolVLESS, endpoint.ProtocolTrojan}, DenyInsecureTLS: !pool.Spec.AllowInsecureTLS}})
}

func maxSourceStale(value *metav1.Duration) time.Duration {
	if value == nil || value.Duration < time.Minute || value.Duration > 7*24*time.Hour {
		return 24 * time.Hour
	}
	return value.Duration
}

func refreshDuration(value *metav1.Duration) time.Duration {
	if value == nil || value.Duration < 30*time.Second || value.Duration > 24*time.Hour {
		return 5 * time.Minute
	}
	return value.Duration
}

type HTTPConfig struct {
	Key         string
	Fingerprint [32]byte
	SourceID    endpoint.SourceID
	HTTP        source.HTTP
	Revisions   map[types.NamespacedName]ResourceRevision
}

func ResolveHTTP(ctx context.Context, reader client.Reader, pool *egressv1alpha1.ProxyPool, desired egressv1alpha1.SubscriptionSource) (HTTPConfig, error) {
	if desired.HTTP == nil || desired.SecretRef != nil || pool == nil || pool.UID == "" {
		return HTTPConfig{}, poolFailure("source_variant")
	}
	id, err := endpoint.NewSourceID(fmt.Sprintf("k8s-%s-%s", pool.UID, desired.ID))
	if err != nil {
		return HTTPConfig{}, poolFailure("source_id")
	}
	revisions := map[types.NamespacedName]ResourceRevision{}
	read := func(ref egressv1alpha1.SecretKeyReference) ([]byte, error) {
		name := types.NamespacedName{Namespace: pool.Namespace, Name: ref.Name}
		secret := &corev1.Secret{}
		if err := reader.Get(ctx, name, secret); err != nil {
			return nil, poolFailure("source_unavailable")
		}
		revisions[name] = ResourceRevision{UID: secret.UID, ResourceVersion: secret.ResourceVersion}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return nil, poolFailure("source_key_missing")
		}
		return value, nil
	}
	urlBytes, err := read(desired.HTTP.URLSecretRef)
	if err != nil {
		return HTTPConfig{}, err
	}
	var authorization []byte
	if desired.HTTP.AuthorizationSecretRef != nil {
		authorization, err = read(*desired.HTTP.AuthorizationSecretRef)
		if err != nil {
			return HTTPConfig{}, err
		}
	}
	if len(authorization) > 8192 {
		return HTTPConfig{}, poolFailure("source_authorization")
	}
	for _, value := range authorization {
		if value < 0x20 || value == 0x7f {
			return HTTPConfig{}, poolFailure("source_authorization")
		}
	}
	headers := http.Header{}
	if len(authorization) > 0 {
		headers.Set("Authorization", string(authorization))
	}
	httpSource, err := source.NewHTTP(id, string(urlBytes), source.HTTPOptions{AllowHTTP: desired.HTTP.AllowHTTP, AllowPrivateNetworks: desired.HTTP.AllowPrivateNetworks, AllowInsecureTLS: desired.HTTP.AllowInsecureTLS, Headers: headers})
	if err != nil {
		return HTTPConfig{}, poolFailure("source_http")
	}
	identity, _ := json.Marshal(struct {
		Variant                                                                                        string
		URL                                                                                            string
		Authorization                                                                                  string
		Format                                                                                         egressv1alpha1.SourceFormat
		AllowEmpty, AllowPartial, AllowInsecureTLS, AllowHTTP, AllowPrivateNetworks, SourceInsecureTLS bool
	}{"HTTP", string(urlBytes), string(authorization), desired.Format, desired.AllowEmpty, desired.AllowPartial, pool.Spec.AllowInsecureTLS, desired.HTTP.AllowHTTP, desired.HTTP.AllowPrivateNetworks, desired.HTTP.AllowInsecureTLS})
	return HTTPConfig{Key: string(pool.UID) + "/" + desired.ID, Fingerprint: sha256.Sum256(identity), SourceID: id, HTTP: httpSource, Revisions: revisions}, nil
}

func sourceFormat(value egressv1alpha1.SourceFormat) (source.Format, bool) {
	switch value {
	case egressv1alpha1.SourceFormatURIList:
		return source.FormatURIList, true
	case egressv1alpha1.SourceFormatBase64URIList:
		return source.FormatBase64URIList, true
	default:
		return source.FormatAuto, false
	}
}
