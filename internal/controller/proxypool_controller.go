package controller

import (
	"context"
	"hash/fnv"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
)

const ProxyPoolSecretIndex = "egressfox.io/proxypool-secret"

// +kubebuilder:rbac:groups=egressfox.io,resources=proxypools,verbs=get;list;watch
// +kubebuilder:rbac:groups=egressfox.io,resources=proxypools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

type ProxyPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Now    func() time.Time
}

func (r *ProxyPoolReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	pool := &egressv1alpha1.ProxyPool{}
	if err := r.Get(ctx, request.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	before := pool.DeepCopy().Status
	result, err := operatoradapter.BuildPool(ctx, r.Client, pool)
	countsChanged := pool.Status.AcceptedEndpoints != int32(result.Inventory.Len()) || pool.Status.RejectedRecords != int32(result.Rejected) || pool.Status.UnsupportedRecords != int32(result.Unsupported)
	generationChanged := pool.Status.ObservedGeneration != pool.Generation
	pool.Status.ObservedGeneration = pool.Generation
	if err != nil {
		apimeta.SetStatusCondition(&pool.Status.Conditions, condition(ConditionSourcesReady, metav1.ConditionFalse, "SnapshotRejected", "one or more source snapshots are unavailable or rejected", pool.Generation, now()))
		apimeta.SetStatusCondition(&pool.Status.Conditions, condition(ConditionReady, metav1.ConditionFalse, "SourcesNotReady", "the pool has no current admitted inventory", pool.Generation, now()))
	} else {
		pool.Status.AcceptedEndpoints = int32(result.Inventory.Len())
		pool.Status.RejectedRecords = int32(result.Rejected)
		pool.Status.UnsupportedRecords = int32(result.Unsupported)
		if pool.Status.LastInventoryChangeTime == nil || countsChanged || generationChanged {
			stamp := metav1.NewTime(now().UTC())
			pool.Status.LastInventoryChangeTime = &stamp
		}
		apimeta.SetStatusCondition(&pool.Status.Conditions, condition(ConditionSourcesReady, metav1.ConditionTrue, "SnapshotsAdmitted", "all source snapshots were admitted", pool.Generation, now()))
		apimeta.SetStatusCondition(&pool.Status.Conditions, condition(ConditionReady, metav1.ConditionTrue, "InventoryReady", "the current admitted inventory is available", pool.Generation, now()))
	}
	if !sameStatus(before, pool.Status) {
		if updateErr := r.Status().Update(ctx, pool); updateErr != nil && !apierrors.IsConflict(updateErr) {
			return ctrl.Result{}, updateErr
		}
	}
	if err != nil {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: requeueAfter(durationValue(pool.Spec.RefreshInterval), pool.UID)}, nil
}

func (r *ProxyPoolReconciler) SetupWithManager(manager ctrl.Manager) error {
	if err := manager.GetFieldIndexer().IndexField(context.Background(), &egressv1alpha1.ProxyPool{}, ProxyPoolSecretIndex, func(object client.Object) []string {
		pool := object.(*egressv1alpha1.ProxyPool)
		values := make([]string, 0, len(pool.Spec.Sources)+1)
		for _, source := range pool.Spec.Sources {
			values = append(values, source.SecretRef.Name)
		}
		values = append(values, pool.Spec.Probe.TargetSecretRef.Name)
		return unique(values)
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(manager).
		For(&egressv1alpha1.ProxyPool{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, deletionPredicate{}))).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.poolsForSecret), builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Complete(r)
}

func (r *ProxyPoolReconciler) poolsForSecret(ctx context.Context, object client.Object) []ctrl.Request {
	secret := object.(*corev1.Secret)
	list := &egressv1alpha1.ProxyPoolList{}
	if err := r.List(ctx, list, client.InNamespace(secret.Namespace), client.MatchingFields{ProxyPoolSecretIndex: secret.Name}); err != nil {
		return nil
	}
	requests := make([]ctrl.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: list.Items[i].Namespace, Name: list.Items[i].Name}})
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].NamespacedName.String() < requests[j].NamespacedName.String() })
	return requests
}

func refreshInterval(value time.Duration) time.Duration {
	if value < 30*time.Second || value > 24*time.Hour {
		return 5 * time.Minute
	}
	return value
}

func durationValue(value *metav1.Duration) time.Duration {
	if value == nil {
		return 0
	}
	return value.Duration
}

func requeueAfter(value time.Duration, uid types.UID) time.Duration {
	base := refreshInterval(value)
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(uid))
	// A stable 0–10% spread avoids synchronized refreshes without making the
	// same object nondeterministic across controller restarts.
	return base + time.Duration(uint64(base)*uint64(hash.Sum32()%1001)/10_000)
}

func unique(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	return result
}

type deletionPredicate struct{ predicate.Funcs }

func (deletionPredicate) Delete(event.DeleteEvent) bool { return true }
