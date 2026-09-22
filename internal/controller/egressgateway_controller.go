package controller

import (
	"context"
	"errors"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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

type GatewayRuntime interface {
	Reconcile(context.Context, *egressv1alpha1.EgressGateway, string) (operatoradapter.RuntimeOutcome, error)
	Cleanup(context.Context, *egressv1alpha1.EgressGateway) error
}

// +kubebuilder:rbac:groups=egressfox.io,resources=egressgateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=egressfox.io,resources=egressgateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=egressfox.io,resources=proxypools,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

type EgressGatewayReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Pipeline GatewayPipeline
	Runtime  GatewayRuntime
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
	pipelineErr := err
	managed := operatoradapter.IsManaged(gateway)
	var runtimeOutcome operatoradapter.RuntimeOutcome
	var runtimeErr error
	if managed {
		generation := outcome.PublishedGeneration
		if generation == "" {
			generation = gateway.Status.PublishedGeneration
		}
		if r.Runtime == nil {
			runtimeErr = errors.New("managed runtime is unavailable")
		} else {
			runtimeOutcome, runtimeErr = r.Runtime.Reconcile(ctx, gateway, generation)
		}
	} else if r.Runtime != nil && pipelineErr == nil && outcome.Selected > 0 {
		runtimeErr = r.Runtime.Cleanup(ctx, gateway)
	}
	gateway.Status.ObservedGeneration = gateway.Generation
	if pipelineErr != nil {
		reason, message := pipelineCondition(pipelineErr)
		if apierrors.IsNotFound(pipelineErr) {
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
			publishedReason := "SecretPublished"
			publishedMessage := "the validated artifact is stored in the owned Secret; runtime activation is unobserved"
			if managed {
				publishedReason = "GenerationPublished"
				publishedMessage = "the validated artifact is stored in an owned immutable generation Secret"
				gateway.Status.PublishedGeneration = outcome.PublishedGeneration
			}
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionPublished, metav1.ConditionTrue, publishedReason, publishedMessage, gateway.Generation, now()))
			apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionReady, metav1.ConditionTrue, "DesiredConfigurationPublished", "the desired validated configuration is published", gateway.Generation, now()))
			if outcome.Changed || gateway.Status.LastPublishedTime == nil {
				stamp := metav1.NewTime(now().UTC())
				gateway.Status.LastPublishedTime = &stamp
			}
		}
	}
	if managed {
		r.applyManagedStatus(gateway, runtimeOutcome, pipelineErr, runtimeErr, now())
	} else {
		r.applyBYOStatus(gateway, runtimeErr, now())
	}
	if !sameStatus(before, gateway.Status) {
		if updateErr := r.Status().Update(ctx, gateway); updateErr != nil && !apierrors.IsConflict(updateErr) {
			return ctrl.Result{}, updateErr
		}
	}
	if pipelineErr != nil || runtimeErr != nil {
		return ctrl.Result{RequeueAfter: requeueAfter(durationValue(pool.Spec.RefreshInterval), gateway.UID)}, nil
	}
	return ctrl.Result{RequeueAfter: requeueAfter(durationValue(pool.Spec.RefreshInterval), gateway.UID)}, nil
}

func (r *EgressGatewayReconciler) applyManagedStatus(gateway *egressv1alpha1.EgressGateway, outcome operatoradapter.RuntimeOutcome, pipelineErr, runtimeErr error, now time.Time) {
	if outcome.ServiceName != "" {
		gateway.Status.ServiceName = outcome.ServiceName
	}
	if outcome.ClientAuthSecretName != "" {
		gateway.Status.ClientAuthSecretName = outcome.ClientAuthSecretName
	}
	if outcome.ActiveGeneration != "" {
		gateway.Status.ActiveGeneration = outcome.ActiveGeneration
	}
	if runtimeErr != nil {
		reason, _ := pipelineCondition(runtimeErr)
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionActivated, metav1.ConditionFalse, reason, "the desired managed generation is not activated", gateway.Generation, now))
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionRuntimeReady, metav1.ConditionUnknown, reason, "managed runtime readiness could not be observed", gateway.Generation, now))
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionDegraded, metav1.ConditionTrue, reason, "managed runtime reconciliation failed safely", gateway.Generation, now))
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionReady, metav1.ConditionFalse, reason, "the desired managed runtime is not ready", gateway.Generation, now))
		return
	}
	if pipelineErr != nil || gateway.Status.PublishedGeneration == "" {
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionActivated, metav1.ConditionFalse, "DesiredGenerationUnavailable", "no current desired managed generation can be activated", gateway.Generation, now))
	} else if outcome.Activated && outcome.ActiveGeneration == gateway.Status.PublishedGeneration {
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionActivated, metav1.ConditionTrue, "ExactGenerationReady", "the managed process started with the published generation", gateway.Generation, now))
	} else {
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionActivated, metav1.ConditionFalse, outcome.Reason, "the published generation has not completed activation", gateway.Generation, now))
	}
	if outcome.RuntimeReady {
		message := "the active managed generation accepts authenticated SOCKS connections"
		if !outcome.Activated {
			message = "a previous active generation remains ready while the desired generation is unresolved"
		}
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionRuntimeReady, metav1.ConditionTrue, outcome.Reason, message, gateway.Generation, now))
	} else {
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionRuntimeReady, metav1.ConditionFalse, outcome.Reason, "no managed generation is currently ready", gateway.Generation, now))
	}
	degraded := outcome.Degraded || pipelineErr != nil
	degradedStatus := metav1.ConditionFalse
	degradedReason := "RuntimeHealthy"
	degradedMessage := "the desired managed generation has no detected rollout failure"
	if degraded {
		degradedStatus = metav1.ConditionTrue
		degradedReason = outcome.Reason
		if pipelineErr != nil {
			degradedReason, _ = pipelineCondition(pipelineErr)
		}
		degradedMessage = "the desired generation is unavailable or failed while the last known good runtime is preserved where possible"
	}
	apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionDegraded, degradedStatus, degradedReason, degradedMessage, gateway.Generation, now))
	ready := pipelineErr == nil && outcome.Activated && outcome.RuntimeReady && outcome.ActiveGeneration == gateway.Status.PublishedGeneration
	if ready {
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionReady, metav1.ConditionTrue, "ManagedRuntimeReady", "the desired published generation is active and its authenticated listener is ready", gateway.Generation, now))
	} else {
		apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionReady, metav1.ConditionFalse, "ManagedRuntimeNotReady", "the desired published generation is not active and ready", gateway.Generation, now))
	}
}

func (r *EgressGatewayReconciler) applyBYOStatus(gateway *egressv1alpha1.EgressGateway, cleanupErr error, now time.Time) {
	reason := "BYORuntimeUnobserved"
	message := "BYO runtime activation is not observed"
	if cleanupErr != nil {
		reason = "ManagedCleanupFailed"
		message = "owned managed resources could not be removed safely"
	}
	apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionActivated, metav1.ConditionUnknown, reason, message, gateway.Generation, now))
	apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionRuntimeReady, metav1.ConditionUnknown, reason, "BYO runtime readiness is not observed", gateway.Generation, now))
	degraded := metav1.ConditionFalse
	if cleanupErr != nil {
		degraded = metav1.ConditionTrue
	}
	apimeta.SetStatusCondition(&gateway.Status.Conditions, condition(ConditionDegraded, degraded, reason, message, gateway.Generation, now))
	if cleanupErr == nil {
		gateway.Status.PublishedGeneration = ""
		gateway.Status.ActiveGeneration = ""
		gateway.Status.ServiceName = ""
		gateway.Status.ClientAuthSecretName = ""
	}
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
	case "authentication_conflict", "generation_ownership", "deployment_conflict", "service_conflict", "networkpolicy_conflict", "owned_conflict":
		return "OwnershipConflict", "a managed runtime object exists but is not owned by this gateway"
	case "authentication", "authentication_invalid", "authentication_read", "authentication_create", "authentication_random":
		return "AuthenticationUnavailable", "managed client authentication could not be prepared"
	case "image_unconfigured":
		return "RuntimeImageUnavailable", "the trusted managed runtime image is not configured"
	case "managed_runtime", "generation_list", "generation_receipt", "generation_random", "generation_collision", "generation_create", "deployment_read", "deployment_create", "deployment_update", "service_read", "service_create", "service_update", "networkpolicy_read", "networkpolicy_create", "networkpolicy_update", "pod_list", "legacy_list", "legacy_delete", "owned_read", "owned_delete":
		return "RuntimeReconcileFailed", "the managed runtime could not converge safely"
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
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Owns(&networkingv1.NetworkPolicy{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
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
