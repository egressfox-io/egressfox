// Package operator adapts Kubernetes objects to the Kubernetes-independent core.
package operator

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/source"
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
	if reader == nil || pool == nil || pool.Namespace == "" || pool.UID == "" || len(pool.Spec.Sources) == 0 {
		return PoolResult{}, poolFailure("request")
	}
	set := source.Set{}
	result := PoolResult{SourceRevisions: make(map[types.NamespacedName]ResourceRevision, len(pool.Spec.Sources))}
	for _, desired := range pool.Spec.Sources {
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
		format, ok := sourceFormat(desired.Format)
		if !ok {
			return PoolResult{}, poolFailure("source_format")
		}
		snapshot, report, err := source.Parse(payload, source.ParseOptions{
			Format: format,
			Limits: source.DefaultLimits(),
			Admission: source.Admission{
				AllowPartial:    desired.AllowPartial,
				AllowedProtocol: []endpoint.Protocol{endpoint.ProtocolVLESS, endpoint.ProtocolTrojan},
				DenyInsecureTLS: !pool.Spec.AllowInsecureTLS,
			},
		})
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
	}
	result.Inventory, _ = set.Inventory()
	return result, nil
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
