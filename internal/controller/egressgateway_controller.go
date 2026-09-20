package controller

import (
	"context"
	"errors"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
)

const GatewayPoolIndex = "egressfox.io/gateway-pool"

type GatewayPipeline interface {
	Run(context.Context, *egressv1alpha1.EgressGateway, *egressv1alpha1.ProxyPool) (operatoradapter.GatewayOutcome, error)
}

// +kubebuilder:rbac:groups=egressfox.io,resources=egressgateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=egressfox.io,resources=egressgateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=egressfox.io,resources=proxypools,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

type EgressGatewayReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Pipeline GatewayPipeline
	Now      func() time.Time
}

func (r *EgressGatewayReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	gateway := &egressv1alpha1.EgressGateway{}
	if err := r.Get(ctx, request.NamespacedName, gateway); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	pool := &egressv1alpha1.ProxyPool{}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	before := gateway.DeepCopy().Status
	err := r.Get(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Spec.PoolRef.Name}, pool)
	var outcome operatoradapter.GatewayOutcome
	if err == nil && r.Pipeline != nil {
		outcome, err = r.Pipeline.Run(ctx, gateway, pool)
	}
	gateway.Status.ObservedGeneration = gateway.Generation
	if err != nil {
		reason, message := pipelineCondition(err)
		if apierrors.IsNotFound(err) {
			reason, message = "PoolNotFound", "the referenced pool does not exist"
		}
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionSelectionReady, metav1.ConditionFalse, reason, "selection for the current desired generation is unavailable", gateway.Generation, now()))
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionConfigurationValid, metav1.ConditionFalse, reason, message, gateway.Generation, now()))
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionPublished, metav1.ConditionFalse, reason, "the previous owned configuration, if any, was retained", gateway.Generation, now()))
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionReady, metav1.ConditionFalse, reason, "the desired configuration is not published", gateway.Generation, now()))
	} else {
		gateway.Status.EligibleEndpoints = int32(outcome.Eligible)
		gateway.Status.SelectedEndpoints = int32(outcome.Selected)
		if outcome.Selected == 0 {
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionSelectionReady, metav1.ConditionFalse, "NoEligibleEndpoints", "no endpoint has sufficient current evidence", gateway.Generation, now()))
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionConfigurationValid, metav1.ConditionUnknown, "SelectionNotReady", "no candidate artifact was rendered or validated", gateway.Generation, now()))
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionPublished, metav1.ConditionFalse, "LastKnownGoodRetained", "no replacement was published", gateway.Generation, now()))
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionReady, metav1.ConditionFalse, "SelectionNotReady", "the desired selection is empty", gateway.Generation, now()))
		} else {
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionSelectionReady, metav1.ConditionTrue, "SelectionReady", "the bounded selection is ready", gateway.Generation, now()))
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionConfigurationValid, metav1.ConditionTrue, "NativeValidationPassed", "the exact artifact passed native engine validation", gateway.Generation, now()))
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionPublished, metav1.ConditionTrue, "SecretPublished", "the validated artifact is stored in the owned Secret; runtime activation is unobserved", gateway.Generation, now()))
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionReady, metav1.ConditionTrue, "DesiredConfigurationPublished", "the desired validated configuration is published", gateway.Generation, now()))
			if outcome.Changed || gateway.Status.LastPublishedTime == nil {
				stamp := metav1.NewTime(now().UTC())
				gateway.Status.LastPublishedTime = &stamp
			}
		}
	}
	if !sameStatus(before, gateway.Status) {
		if updateErr := r.Status().Update(ctx, gateway); updateErr != nil && !apierrors.IsConflict(updateErr) {
			return ctrl.Result{}, updateErr
		}
	}
	if err != nil {
		return ctrl.Result{RequeueAfter: requeueAfter(durationValue(pool.Spec.RefreshInterval), gateway.UID)}, nil
	}
	return ctrl.Result{RequeueAfter: requeueAfter(durationValue(pool.Spec.RefreshInterval), gateway.UID)}, nil
}

func pipelineCondition(err error) (string, string) {
	var coded interface{ Code() string }
	if !errors.As(err, &coded) {
		return "PipelineFailed", "the desired configuration could not be validated and published"
	}
	switch coded.Code() {
	case "source_unavailable", "source_key_missing", "source_acquire", "target_unavailable", "target_key_missing":
		return "SourceUnavailable", "a referenced source or probe target is unavailable"
	case "source_rejected", "snapshot_rejected", "source_format", "source_limits", "source_id", "target_invalid", "inventory_empty":
		return "SourceRejected", "the current source snapshot could not be admitted"
	case "probe_executor", "probe_scheduler", "probe_schedule":
		return "ProbeFailed", "bounded probe execution could not complete"
	case "selection", "selection_context":
		return "SelectionFailed", "the current inventory and evidence could not be selected"
	case "native_validation", "checker":
		return "NativeValidationFailed", "the candidate did not pass required native engine validation"
	case "render", "engine", "listener":
		return "RenderFailed", "the selected configuration could not be rendered"
	case "publication", "publisher", "publication_too_large":
		return "PublicationFailed", "the validated artifact could not replace the owned output Secret"
	case "publication_conflict":
		return "PublicationConflict", "the requested output Secret is not owned by this gateway"
	case "snapshot_obsolete":
		return "SnapshotObsolete", "an input changed while reconciliation was in progress"
	case "persistence", "evidence_store", "reconciler":
		return "PersistenceFailed", "protected reconciliation state could not be read or updated"
	default:
		return "PipelineFailed", "the desired configuration could not be validated and published"
	}
}

func (r *EgressGatewayReconciler) SetupWithManager(manager ctrl.Manager) error {
	if err := manager.GetFieldIndexer().IndexField(context.Background(), &egressv1alpha1.EgressGateway{}, GatewayPoolIndex, func(object client.Object) []string {
		return []string{object.(*egressv1alpha1.EgressGateway).Spec.PoolRef.Name}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(manager).
		For(&egressv1alpha1.EgressGateway{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, deletionPredicate{}))).
		Watches(&egressv1alpha1.ProxyPool{}, handler.EnqueueRequestsFromMapFunc(r.gatewaysForPool), builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.gatewaysForSecret), builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Owns(&corev1.Secret{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Complete(r)
}

func (r *EgressGatewayReconciler) gatewaysForSecret(ctx context.Context, object client.Object) []ctrl.Request {
	secret := object.(*corev1.Secret)
	pools := &egressv1alpha1.ProxyPoolList{}
	if err := r.List(ctx, pools, client.InNamespace(secret.Namespace), client.MatchingFields{ProxyPoolSecretIndex: secret.Name}); err != nil {
		return nil
	}
	seen := map[types.NamespacedName]struct{}{}
	for i := range pools.Items {
		for _, request := range r.gatewaysForPool(ctx, &pools.Items[i]) {
			seen[request.NamespacedName] = struct{}{}
		}
	}
	requests := make([]ctrl.Request, 0, len(seen))
	for name := range seen {
		requests = append(requests, ctrl.Request{NamespacedName: name})
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].NamespacedName.String() < requests[j].NamespacedName.String() })
	return requests
}

func (r *EgressGatewayReconciler) gatewaysForPool(ctx context.Context, object client.Object) []ctrl.Request {
	pool := object.(*egressv1alpha1.ProxyPool)
	list := &egressv1alpha1.EgressGatewayList{}
	if err := r.List(ctx, list, client.InNamespace(pool.Namespace), client.MatchingFields{GatewayPoolIndex: pool.Name}); err != nil {
		return nil
	}
	requests := make([]ctrl.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: list.Items[i].Namespace, Name: list.Items[i].Name}})
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].NamespacedName.String() < requests[j].NamespacedName.String() })
	return requests
}
