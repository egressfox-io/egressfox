package operator

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/policy"
)

const (
	ManagedSOCKSPort               = 1080
	ManagedUsername                = "egressfox"
	BasicAuthSecretType            = corev1.SecretTypeBasicAuth
	ComponentLabel                 = "egressfox.io/component"
	GatewayUIDLabel                = "egressfox.io/gateway-uid"
	GenerationAnnotation           = "egressfox.io/generation"
	ClientUsernameDataKey          = ".egressfox-client-username"
	ClientPasswordDataKey          = ".egressfox-client-password"
	componentAuthentication        = "client-auth"
	componentGeneration            = "runtime-generation"
	componentRuntime               = "runtime"
	managedConfigMountPath         = "/etc/egressfox/config"
	managedAuthenticationMountPath = "/etc/egressfox/auth"
)

var ErrManagedRuntime = errors.New("managed runtime failed")

type ManagedRuntimeError struct{ code string }

func (e *ManagedRuntimeError) Error() string  { return "managed runtime failed code=" + e.code }
func (e *ManagedRuntimeError) Unwrap() error  { return ErrManagedRuntime }
func (e *ManagedRuntimeError) Code() string   { return e.code }
func managedRuntimeFailure(code string) error { return &ManagedRuntimeError{code: code} }

type ManagedRuntimeConfig struct {
	Client client.Client
	Scheme *runtime.Scheme
	Image  string
	Random io.Reader
}

// ManagedRuntime owns only the bounded M7 child set. It never executes proxy
// behavior and never formats credentials into logs, metadata or process arguments.
type ManagedRuntime struct {
	client client.Client
	scheme *runtime.Scheme
	image  string
	random io.Reader
}

type RuntimeOutcome struct {
	PublishedGeneration  string
	ActiveGeneration     string
	ServiceName          string
	ClientAuthSecretName string
	Activated            bool
	RuntimeReady         bool
	Degraded             bool
	Reason               string
}

func NewManagedRuntime(config ManagedRuntimeConfig) (*ManagedRuntime, error) {
	if config.Client == nil || config.Scheme == nil {
		return nil, managedRuntimeFailure("configuration")
	}
	randomSource := config.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &ManagedRuntime{client: config.Client, scheme: config.Scheme, image: strings.TrimSpace(config.Image), random: randomSource}, nil
}

func IsManaged(gateway *egressv1alpha1.EgressGateway) bool {
	return gateway != nil && gateway.Spec.Runtime != nil && gateway.Spec.Runtime.Managed != nil
}

func (r *ManagedRuntime) Prepare(ctx context.Context, gateway *egressv1alpha1.EgressGateway) (policy.Listener, types.NamespacedName, ResourceRevision, error) {
	if !IsManaged(gateway) {
		return policy.Listener{}, types.NamespacedName{}, ResourceRevision{}, managedRuntimeFailure("mode")
	}
	secret, err := r.ensureAuthentication(ctx, gateway)
	if err != nil {
		return policy.Listener{}, types.NamespacedName{}, ResourceRevision{}, err
	}
	username := string(secret.Data[corev1.BasicAuthUsernameKey])
	password := string(secret.Data[corev1.BasicAuthPasswordKey])
	listener, err := policy.NewManagedSOCKSListener(ManagedSOCKSPort, username, password)
	if err != nil {
		return policy.Listener{}, types.NamespacedName{}, ResourceRevision{}, managedRuntimeFailure("authentication")
	}
	name := client.ObjectKeyFromObject(secret)
	return listener, name, ResourceRevision{UID: secret.UID, ResourceVersion: secret.ResourceVersion}, nil
}

func (r *ManagedRuntime) Publisher(gateway *egressv1alpha1.EgressGateway, guard PublicationGuard) (*GenerationPublisher, error) {
	if !IsManaged(gateway) || guard == nil {
		return nil, managedRuntimeFailure("configuration")
	}
	return &GenerationPublisher{client: r.client, scheme: r.scheme, owner: gateway.DeepCopy(), guard: guard, random: r.random}, nil
}

func (r *ManagedRuntime) ensureAuthentication(ctx context.Context, gateway *egressv1alpha1.EgressGateway) (*corev1.Secret, error) {
	name := types.NamespacedName{Namespace: gateway.Namespace, Name: managedResourceName(gateway, "client-auth")}
	current := &corev1.Secret{}
	if err := r.client.Get(ctx, name, current); err == nil {
		if !ownedByGateway(current, gateway) || current.Type != BasicAuthSecretType || current.Immutable == nil || !*current.Immutable || len(current.Data) != 2 {
			return nil, managedRuntimeFailure("authentication_conflict")
		}
		if _, err := policy.NewManagedSOCKSListener(ManagedSOCKSPort, string(current.Data[corev1.BasicAuthUsernameKey]), string(current.Data[corev1.BasicAuthPasswordKey])); err != nil {
			return nil, managedRuntimeFailure("authentication_invalid")
		}
		return current, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, managedRuntimeFailure("authentication_read")
	}

	username, password, recovered := r.recoverAuthentication(ctx, gateway)
	if !recovered {
		randomBytes := make([]byte, 32)
		if _, err := io.ReadFull(r.random, randomBytes); err != nil {
			return nil, managedRuntimeFailure("authentication_random")
		}
		username = ManagedUsername
		password = base64.RawURLEncoding.EncodeToString(randomBytes)
	}
	immutable := true
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace, Labels: managedLabels(gateway, componentAuthentication)},
		Type:       BasicAuthSecretType,
		Immutable:  &immutable,
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte(username),
			corev1.BasicAuthPasswordKey: []byte(password),
		},
	}
	if err := controllerutil.SetControllerReference(gateway, desired, r.scheme); err != nil {
		return nil, managedRuntimeFailure("owner")
	}
	if err := r.client.Create(ctx, desired); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, managedRuntimeFailure("authentication_conflict")
		}
		return nil, managedRuntimeFailure("authentication_create")
	}
	return desired, nil
}

func (r *ManagedRuntime) recoverAuthentication(ctx context.Context, gateway *egressv1alpha1.EgressGateway) (string, string, bool) {
	for _, name := range []string{gateway.Status.ActiveGeneration, gateway.Status.PublishedGeneration} {
		if name == "" {
			continue
		}
		secret := &corev1.Secret{}
		if err := r.client.Get(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: name}, secret); err != nil || !ownedByGateway(secret, gateway) || secret.Type != EngineSecretType {
			continue
		}
		username := string(secret.Data[ClientUsernameDataKey])
		password := string(secret.Data[ClientPasswordDataKey])
		if _, err := policy.NewManagedSOCKSListener(ManagedSOCKSPort, username, password); err == nil {
			return username, password, true
		}
	}
	return "", "", false
}

// GenerationPublisher stores exact validated bytes in immutable Secrets. The
// random public identifier is deliberately unrelated to the protected receipt.
type GenerationPublisher struct {
	client      client.Client
	scheme      *runtime.Scheme
	owner       *egressv1alpha1.EgressGateway
	guard       PublicationGuard
	random      io.Reader
	currentName string
}

func (p *GenerationPublisher) String() string {
	return "managed generation publisher target=<redacted>"
}

func (p *GenerationPublisher) GenerationName() string { return p.currentName }

func (p *GenerationPublisher) CurrentReceipt(ctx context.Context) (artifact.Receipt, bool, error) {
	secrets, err := p.generations(ctx)
	if err != nil {
		return artifact.Receipt{}, false, err
	}
	if len(secrets) == 0 {
		return artifact.Receipt{}, false, nil
	}
	current := &secrets[len(secrets)-1]
	requested := p.currentName
	if requested == "" {
		requested = p.owner.Status.PublishedGeneration
	}
	if requested != "" {
		for index := range secrets {
			if secrets[index].Name == requested {
				current = &secrets[index]
				break
			}
		}
	}
	receipt, ok := generationReceipt(current)
	if !ok {
		return artifact.Receipt{}, false, managedRuntimeFailure("generation_receipt")
	}
	p.currentName = current.Name
	return receipt, true, nil
}

func (p *GenerationPublisher) Publish(ctx context.Context, validated artifact.Validated) (artifact.Publication, error) {
	if p.guard == nil {
		return artifact.Publication{}, managedRuntimeFailure("configuration")
	}
	if err := p.guard.Check(ctx); err != nil {
		return artifact.Publication{}, secretPublicationFailure("snapshot_obsolete")
	}
	content := validated.Reveal()
	receiptBytes, err := validated.ProtectedReceipt()
	if err != nil {
		return artifact.Publication{}, secretPublicationFailure("artifact")
	}
	configKey, ok := configDataKey(validated.Profile())
	if !ok || validated.Profile().Engine.String() != profileEngine(p.owner.Spec.Engine) {
		return artifact.Publication{}, secretPublicationFailure("profile")
	}
	authentication := &corev1.Secret{}
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: p.owner.Namespace, Name: managedResourceName(p.owner, "client-auth")}, authentication); err != nil || !ownedByGateway(authentication, p.owner) || authentication.Type != BasicAuthSecretType {
		return artifact.Publication{}, managedRuntimeFailure("authentication_read")
	}
	username := authentication.Data[corev1.BasicAuthUsernameKey]
	password := authentication.Data[corev1.BasicAuthPasswordKey]
	if len(content)+len(receiptBytes)+len(username)+len(password) > MaxSecretDataBytes {
		return artifact.Publication{}, secretPublicationFailure("payload_too_large")
	}
	secrets, err := p.generations(ctx)
	if err != nil {
		return artifact.Publication{}, err
	}
	for index := range secrets {
		secret := &secrets[index]
		if bytes.Equal(secret.Data[configKey], content) && bytes.Equal(secret.Data[ReceiptDataKey], receiptBytes) && bytes.Equal(secret.Data[ClientUsernameDataKey], username) && bytes.Equal(secret.Data[ClientPasswordDataKey], password) && len(secret.Data) == 4 {
			p.currentName = secret.Name
			return artifact.Publication{Profile: validated.Profile(), Changed: false}, nil
		}
	}
	tokenBytes := make([]byte, 10)
	if _, err := io.ReadFull(p.random, tokenBytes); err != nil {
		return artifact.Publication{}, managedRuntimeFailure("generation_random")
	}
	token := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(tokenBytes))
	immutable := true
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedResourceName(p.owner, "gen") + "-" + token,
			Namespace: p.owner.Namespace,
			Labels:    managedLabels(p.owner, componentGeneration),
		},
		Type:      EngineSecretType,
		Immutable: &immutable,
		Data: map[string][]byte{
			configKey: content, ReceiptDataKey: receiptBytes,
			ClientUsernameDataKey: append([]byte(nil), username...), ClientPasswordDataKey: append([]byte(nil), password...),
		},
	}
	desired.Labels[EngineLabel] = validated.Profile().Engine.String()
	if err := controllerutil.SetControllerReference(p.owner, desired, p.scheme); err != nil {
		return artifact.Publication{}, managedRuntimeFailure("owner")
	}
	if err := p.client.Create(ctx, desired); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return artifact.Publication{}, managedRuntimeFailure("generation_collision")
		}
		return artifact.Publication{}, managedRuntimeFailure("generation_create")
	}
	p.currentName = desired.Name
	return artifact.Publication{Profile: validated.Profile(), Changed: true}, nil
}

func (p *GenerationPublisher) generations(ctx context.Context) ([]corev1.Secret, error) {
	list := &corev1.SecretList{}
	if err := p.client.List(ctx, list, client.InNamespace(p.owner.Namespace), client.MatchingLabels{ManagedByLabel: "egressfox", ComponentLabel: componentGeneration, GatewayUIDLabel: string(p.owner.UID)}); err != nil {
		return nil, managedRuntimeFailure("generation_list")
	}
	result := make([]corev1.Secret, 0, len(list.Items))
	for index := range list.Items {
		secret := list.Items[index]
		if !ownedByGateway(&secret, p.owner) || secret.Type != EngineSecretType || secret.Immutable == nil || !*secret.Immutable {
			return nil, managedRuntimeFailure("generation_ownership")
		}
		result = append(result, secret)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreationTimestamp.Equal(&result[j].CreationTimestamp) {
			return result[i].Name < result[j].Name
		}
		return result[i].CreationTimestamp.Before(&result[j].CreationTimestamp)
	})
	return result, nil
}

func generationReceipt(secret *corev1.Secret) (artifact.Receipt, bool) {
	for _, key := range []string{"config.yaml", "config.json"} {
		if _, ok := artifact.MatchesProtectedReceipt(secret.Data[key], secret.Data[ReceiptDataKey]); ok {
			receipt, err := artifact.RestoreReceipt(secret.Data[ReceiptDataKey])
			return receipt, err == nil
		}
	}
	return artifact.Receipt{}, false
}

func (r *ManagedRuntime) Reconcile(ctx context.Context, gateway *egressv1alpha1.EgressGateway, generation string) (RuntimeOutcome, error) {
	outcome := RuntimeOutcome{PublishedGeneration: generation, ActiveGeneration: gateway.Status.ActiveGeneration, ServiceName: managedResourceName(gateway, "proxy"), ClientAuthSecretName: managedResourceName(gateway, "client-auth")}
	if !IsManaged(gateway) {
		return outcome, managedRuntimeFailure("mode")
	}
	if generation == "" {
		outcome.Reason = "GenerationUnavailable"
		return outcome, nil
	}
	if r.image == "" {
		return outcome, managedRuntimeFailure("image_unconfigured")
	}
	if err := r.ensureService(ctx, gateway); err != nil {
		return outcome, err
	}
	if err := r.ensureNetworkPolicy(ctx, gateway); err != nil {
		return outcome, err
	}
	deployment, err := r.ensureDeployment(ctx, gateway, generation)
	if err != nil {
		return outcome, err
	}
	outcome.RuntimeReady = deployment.Status.AvailableReplicas > 0 && deployment.Status.ReadyReplicas > 0
	outcome.Activated = deploymentComplete(deployment, generation)
	if outcome.Activated {
		outcome.ActiveGeneration = generation
		outcome.RuntimeReady = true
		outcome.Reason = "ExactGenerationReady"
		if err := r.cleanupGenerations(ctx, gateway, generation, gateway.Status.ActiveGeneration); err != nil {
			return outcome, err
		}
		if err := r.removeLegacyOutput(ctx, gateway); err != nil {
			return outcome, err
		}
		return outcome, nil
	}
	if deploymentTimedOut(deployment) {
		outcome.Degraded = true
		outcome.Reason = "ProgressDeadlineExceeded"
	} else if outcome.RuntimeReady {
		outcome.Reason = "PreviousGenerationReady"
	} else {
		outcome.Reason = "ActivationPending"
	}
	return outcome, nil
}

func (r *ManagedRuntime) Cleanup(ctx context.Context, gateway *egressv1alpha1.EgressGateway) error {
	for _, object := range []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: managedResourceName(gateway, "runtime"), Namespace: gateway.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: managedResourceName(gateway, "proxy"), Namespace: gateway.Namespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: managedResourceName(gateway, "proxy"), Namespace: gateway.Namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: managedResourceName(gateway, "client-auth"), Namespace: gateway.Namespace}},
	} {
		if err := r.deleteOwned(ctx, gateway, object); err != nil {
			return err
		}
	}
	list := &corev1.SecretList{}
	if err := r.client.List(ctx, list, client.InNamespace(gateway.Namespace), client.MatchingLabels{ManagedByLabel: "egressfox", ComponentLabel: componentGeneration, GatewayUIDLabel: string(gateway.UID)}); err != nil {
		return managedRuntimeFailure("generation_list")
	}
	for index := range list.Items {
		if err := r.deleteOwned(ctx, gateway, &list.Items[index]); err != nil {
			return err
		}
	}
	return nil
}

func (r *ManagedRuntime) ensureDeployment(ctx context.Context, gateway *egressv1alpha1.EgressGateway, generation string) (*appsv1.Deployment, error) {
	name := types.NamespacedName{Namespace: gateway.Namespace, Name: managedResourceName(gateway, "runtime")}
	current := &appsv1.Deployment{}
	err := r.client.Get(ctx, name, current)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, managedRuntimeFailure("deployment_read")
	}
	if err == nil && !ownedByGateway(current, gateway) {
		return nil, managedRuntimeFailure("deployment_conflict")
	}
	desired := desiredDeployment(gateway, name.Name, r.image, generation)
	if err := controllerutil.SetControllerReference(gateway, desired, r.scheme); err != nil {
		return nil, managedRuntimeFailure("owner")
	}
	if apierrors.IsNotFound(err) {
		if err := r.client.Create(ctx, desired); err != nil {
			return nil, managedRuntimeFailure("deployment_create")
		}
		return desired, nil
	}
	if reflect.DeepEqual(current.Spec, desired.Spec) && reflect.DeepEqual(current.Labels, desired.Labels) && reflect.DeepEqual(current.OwnerReferences, desired.OwnerReferences) {
		return current, nil
	}
	updated := current.DeepCopy()
	updated.Labels = desired.Labels
	updated.OwnerReferences = desired.OwnerReferences
	updated.Spec = desired.Spec
	if err := r.client.Update(ctx, updated); err != nil {
		return nil, managedRuntimeFailure("deployment_update")
	}
	return updated, nil
}

func (r *ManagedRuntime) ensureService(ctx context.Context, gateway *egressv1alpha1.EgressGateway) error {
	name := types.NamespacedName{Namespace: gateway.Namespace, Name: managedResourceName(gateway, "proxy")}
	current := &corev1.Service{}
	err := r.client.Get(ctx, name, current)
	if err != nil && !apierrors.IsNotFound(err) {
		return managedRuntimeFailure("service_read")
	}
	if err == nil && !ownedByGateway(current, gateway) {
		return managedRuntimeFailure("service_conflict")
	}
	desired := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace, Labels: managedLabels(gateway, componentRuntime)}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: runtimeSelector(gateway), Ports: []corev1.ServicePort{{Name: "socks", Protocol: corev1.ProtocolTCP, Port: ManagedSOCKSPort, TargetPort: intstr.FromString("socks")}}}}
	if err := controllerutil.SetControllerReference(gateway, desired, r.scheme); err != nil {
		return managedRuntimeFailure("owner")
	}
	if apierrors.IsNotFound(err) {
		if err := r.client.Create(ctx, desired); err != nil {
			return managedRuntimeFailure("service_create")
		}
		return nil
	}
	if current.Spec.Type != corev1.ServiceTypeClusterIP || !reflect.DeepEqual(current.Spec.Selector, desired.Spec.Selector) || !reflect.DeepEqual(current.Spec.Ports, desired.Spec.Ports) || !reflect.DeepEqual(current.Labels, desired.Labels) || !reflect.DeepEqual(current.OwnerReferences, desired.OwnerReferences) {
		updated := current.DeepCopy()
		updated.Labels = desired.Labels
		updated.OwnerReferences = desired.OwnerReferences
		updated.Spec.Type = corev1.ServiceTypeClusterIP
		updated.Spec.Selector = desired.Spec.Selector
		updated.Spec.Ports = desired.Spec.Ports
		if err := r.client.Update(ctx, updated); err != nil {
			return managedRuntimeFailure("service_update")
		}
	}
	return nil
}

func (r *ManagedRuntime) ensureNetworkPolicy(ctx context.Context, gateway *egressv1alpha1.EgressGateway) error {
	name := types.NamespacedName{Namespace: gateway.Namespace, Name: managedResourceName(gateway, "proxy")}
	current := &networkingv1.NetworkPolicy{}
	err := r.client.Get(ctx, name, current)
	if err != nil && !apierrors.IsNotFound(err) {
		return managedRuntimeFailure("networkpolicy_read")
	}
	if err == nil && !ownedByGateway(current, gateway) {
		return managedRuntimeFailure("networkpolicy_conflict")
	}
	protocol := corev1.ProtocolTCP
	port := intstr.FromInt(ManagedSOCKSPort)
	desired := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace, Labels: managedLabels(gateway, componentRuntime)}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: runtimeSelector(gateway)}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: gateway.Namespace}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}}}}}}
	if err := controllerutil.SetControllerReference(gateway, desired, r.scheme); err != nil {
		return managedRuntimeFailure("owner")
	}
	if apierrors.IsNotFound(err) {
		if err := r.client.Create(ctx, desired); err != nil {
			return managedRuntimeFailure("networkpolicy_create")
		}
		return nil
	}
	if !reflect.DeepEqual(current.Spec, desired.Spec) || !reflect.DeepEqual(current.Labels, desired.Labels) || !reflect.DeepEqual(current.OwnerReferences, desired.OwnerReferences) {
		updated := current.DeepCopy()
		updated.Labels = desired.Labels
		updated.OwnerReferences = desired.OwnerReferences
		updated.Spec = desired.Spec
		if err := r.client.Update(ctx, updated); err != nil {
			return managedRuntimeFailure("networkpolicy_update")
		}
	}
	return nil
}

func desiredDeployment(gateway *egressv1alpha1.EgressGateway, name, image, generation string) *appsv1.Deployment {
	replicas, history, progress, termination := int32(1), int32(2), int32(120), int64(30)
	zero, one := intstr.FromInt(0), intstr.FromInt(1)
	readOnly, noPrivilege, noToken := true, false, false
	runAs := int64(65532)
	stateSize, temporarySize := resource.MustParse("256Mi"), resource.MustParse("16Mi")
	configKey := "config.json"
	command := []string{"/usr/local/libexec/egressfox/egressfox-engine-s"}
	args := []string{"run", "-c", managedConfigMountPath + "/" + configKey, "-D", "/var/lib/engine"}
	if gateway.Spec.Engine == egressv1alpha1.EngineMihomo {
		configKey = "config.yaml"
		command = []string{"/usr/local/libexec/egressfox/mihomo"}
		args = []string{"-f", managedConfigMountPath + "/" + configKey, "-d", "/var/lib/engine"}
	}
	labels := runtimeSelector(gateway)
	podLabels := mapsCopy(labels)
	podLabels[ManagedByLabel] = "egressfox"
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gateway.Namespace, Labels: managedLabels(gateway, componentRuntime)},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas, RevisionHistoryLimit: &history, ProgressDeadlineSeconds: &progress, MinReadySeconds: 2,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType, RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: &zero, MaxSurge: &one}},
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels, Annotations: map[string]string{GenerationAnnotation: generation}},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: &noToken, TerminationGracePeriodSeconds: &termination,
					SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &readOnly, RunAsUser: &runAs, RunAsGroup: &runAs, FSGroup: &runAs, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
					Containers: []corev1.Container{{
						Name: "engine", Image: image, ImagePullPolicy: corev1.PullIfNotPresent, Command: command, Args: args,
						Ports:           []corev1.ContainerPort{{Name: "socks", ContainerPort: ManagedSOCKSPort, Protocol: corev1.ProtocolTCP}},
						ReadinessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/local/bin/egressfox-healthcheck", "--address", "127.0.0.1:1080", "--username-file", managedAuthenticationMountPath + "/username", "--password-file", managedAuthenticationMountPath + "/password"}}}, InitialDelaySeconds: 1, PeriodSeconds: 2, TimeoutSeconds: 2, FailureThreshold: 5, SuccessThreshold: 1},
						Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")}},
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &noPrivilege, ReadOnlyRootFilesystem: &readOnly, RunAsNonRoot: &readOnly, RunAsUser: &runAs, RunAsGroup: &runAs, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
						VolumeMounts:    []corev1.VolumeMount{{Name: "configuration", MountPath: managedConfigMountPath, ReadOnly: true}, {Name: "authentication", MountPath: managedAuthenticationMountPath, ReadOnly: true}, {Name: "state", MountPath: "/var/lib/engine"}, {Name: "temporary", MountPath: "/tmp"}},
					}},
					Volumes: []corev1.Volume{
						{Name: "configuration", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: generation, Items: []corev1.KeyToPath{{Key: configKey, Path: configKey}}}}},
						{Name: "authentication", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: managedResourceName(gateway, "client-auth"), Items: []corev1.KeyToPath{{Key: corev1.BasicAuthUsernameKey, Path: "username"}, {Key: corev1.BasicAuthPasswordKey, Path: "password"}}}}},
						{Name: "state", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &stateSize}}},
						{Name: "temporary", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &temporarySize}}},
					},
				},
			},
		},
	}
}

func deploymentComplete(deployment *appsv1.Deployment, generation string) bool {
	return deployment.Spec.Template.Annotations[GenerationAnnotation] == generation && deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.UpdatedReplicas == 1 && deployment.Status.Replicas == 1 && deployment.Status.ReadyReplicas == 1 && deployment.Status.AvailableReplicas == 1 && deployment.Status.UnavailableReplicas == 0
}

func deploymentTimedOut(deployment *appsv1.Deployment) bool {
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionFalse && condition.Reason == "ProgressDeadlineExceeded" {
			return true
		}
	}
	return false
}

func (r *ManagedRuntime) cleanupGenerations(ctx context.Context, gateway *egressv1alpha1.EgressGateway, active, previous string) error {
	keep := map[string]struct{}{active: {}}
	if previous != "" {
		keep[previous] = struct{}{}
	}
	pods := &corev1.PodList{}
	if err := r.client.List(ctx, pods, client.InNamespace(gateway.Namespace), client.MatchingLabels(runtimeSelector(gateway))); err != nil {
		return managedRuntimeFailure("pod_list")
	}
	for index := range pods.Items {
		pod := &pods.Items[index]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.Secret != nil {
				keep[volume.Secret.SecretName] = struct{}{}
			}
		}
	}
	list := &corev1.SecretList{}
	if err := r.client.List(ctx, list, client.InNamespace(gateway.Namespace), client.MatchingLabels{ManagedByLabel: "egressfox", ComponentLabel: componentGeneration, GatewayUIDLabel: string(gateway.UID)}); err != nil {
		return managedRuntimeFailure("generation_list")
	}
	sort.Slice(list.Items, func(i, j int) bool {
		if list.Items[i].CreationTimestamp.Equal(&list.Items[j].CreationTimestamp) {
			return list.Items[i].Name < list.Items[j].Name
		}
		return list.Items[i].CreationTimestamp.Before(&list.Items[j].CreationTimestamp)
	})
	for index := len(list.Items) - 2; index < len(list.Items); index++ {
		if index >= 0 {
			keep[list.Items[index].Name] = struct{}{}
		}
	}
	for index := range list.Items {
		secret := &list.Items[index]
		if _, ok := keep[secret.Name]; ok {
			continue
		}
		if err := r.deleteOwned(ctx, gateway, secret); err != nil {
			return err
		}
	}
	return nil
}

func (r *ManagedRuntime) removeLegacyOutput(ctx context.Context, gateway *egressv1alpha1.EgressGateway) error {
	list := &corev1.SecretList{}
	if err := r.client.List(ctx, list, client.InNamespace(gateway.Namespace), client.MatchingLabels{ManagedByLabel: "egressfox"}); err != nil {
		return managedRuntimeFailure("legacy_list")
	}
	for index := range list.Items {
		secret := &list.Items[index]
		if secret.Type != EngineSecretType || secret.Labels[ComponentLabel] == componentGeneration || !ownedByGateway(secret, gateway) {
			continue
		}
		if err := r.client.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			return managedRuntimeFailure("legacy_delete")
		}
	}
	return nil
}

func (r *ManagedRuntime) deleteOwned(ctx context.Context, gateway *egressv1alpha1.EgressGateway, object client.Object) error {
	if err := r.client.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return managedRuntimeFailure("owned_read")
	}
	if !ownedByGateway(object, gateway) {
		return managedRuntimeFailure("owned_conflict")
	}
	if err := r.client.Delete(ctx, object); err != nil && !apierrors.IsNotFound(err) {
		return managedRuntimeFailure("owned_delete")
	}
	return nil
}

func ownedByGateway(object metav1.Object, gateway *egressv1alpha1.EgressGateway) bool {
	owner := metav1.GetControllerOf(object)
	return owner != nil && owner.APIVersion == egressv1alpha1.GroupVersion.String() && owner.Kind == "EgressGateway" && owner.Name == gateway.Name && owner.UID == gateway.UID
}

func managedLabels(gateway *egressv1alpha1.EgressGateway, component string) map[string]string {
	return map[string]string{ManagedByLabel: "egressfox", ComponentLabel: component, GatewayUIDLabel: string(gateway.UID), EngineLabel: profileEngine(gateway.Spec.Engine)}
}

func runtimeSelector(gateway *egressv1alpha1.EgressGateway) map[string]string {
	return map[string]string{"app.kubernetes.io/name": "egressfox-gateway", GatewayUIDLabel: string(gateway.UID)}
}

func managedResourceName(gateway *egressv1alpha1.EgressGateway, suffix string) string {
	hash := sha256.Sum256([]byte(gateway.Namespace + "\x00" + gateway.Name + "\x00" + string(gateway.UID)))
	base := strings.ReplaceAll(strings.Trim(strings.ToLower(gateway.Name), "-."), ".", "-")
	if len(base) > 36 {
		base = strings.TrimRight(base[:36], "-.")
	}
	if base == "" {
		base = "gateway"
	}
	return fmt.Sprintf("%s-%x-%s", base, hash[:4], suffix)
}

func profileEngine(engine egressv1alpha1.Engine) string {
	switch engine {
	case egressv1alpha1.EngineMihomo:
		return artifact.EngineMihomo.String()
	case egressv1alpha1.EngineSingBox:
		return artifact.EngineSingBox.String()
	default:
		return "unknown"
	}
}

func mapsCopy(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

var _ fmt.Stringer = (*GenerationPublisher)(nil)
