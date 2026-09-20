package operator

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	"github.com/egressfox-io/egressfox/internal/artifact"
)

const (
	EngineSecretType corev1.SecretType = "egressfox.io/engine-config"
	ReceiptDataKey                     = ".egressfox-receipt"
	ManagedByLabel                     = "app.kubernetes.io/managed-by"
	EngineLabel                        = "egressfox.io/engine"
	// MaxSecretDataBytes leaves headroom below Kubernetes' 1 MiB Secret limit
	// for object metadata and wire-format overhead.
	MaxSecretDataBytes = 900 * 1024
)

var ErrSecretPublication = errors.New("Secret publication failed")

type SecretPublicationError struct{ code string }

func (e *SecretPublicationError) Error() string { return "Secret publication failed code=" + e.code }
func (e *SecretPublicationError) Unwrap() error { return ErrSecretPublication }
func (e *SecretPublicationError) Code() string  { return e.code }

func secretPublicationFailure(code string) error { return &SecretPublicationError{code: code} }

// SecretPublisher publishes one Gateway's validated artifact to an exclusively
// owned Kubernetes Secret. It never formats Secret data or protected receipts.
type SecretPublisher struct {
	client client.Client
	scheme *runtime.Scheme
	owner  *egressv1alpha1.EgressGateway
	name   types.NamespacedName
	guard  PublicationGuard
}

// PublicationGuard rechecks the Kubernetes objects used to construct a
// candidate immediately before it can replace the current output.
type PublicationGuard interface {
	Check(context.Context) error
}

func NewSecretPublisher(kubeClient client.Client, scheme *runtime.Scheme, owner *egressv1alpha1.EgressGateway, name string, guards ...PublicationGuard) (*SecretPublisher, error) {
	if kubeClient == nil || scheme == nil || owner == nil || owner.Namespace == "" || owner.Name == "" || owner.UID == "" || name == "" || len(guards) > 1 {
		return nil, secretPublicationFailure("configuration")
	}
	var guard PublicationGuard
	if len(guards) == 1 {
		if guards[0] == nil {
			return nil, secretPublicationFailure("configuration")
		}
		guard = guards[0]
	}
	return &SecretPublisher{client: kubeClient, scheme: scheme, owner: owner.DeepCopy(), name: types.NamespacedName{Namespace: owner.Namespace, Name: name}, guard: guard}, nil
}

func (p *SecretPublisher) String() string { return "Kubernetes Secret publisher target=<redacted>" }

func (p *SecretPublisher) CurrentReceipt(ctx context.Context) (artifact.Receipt, bool, error) {
	secret := &corev1.Secret{}
	if err := p.client.Get(ctx, p.name, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return artifact.Receipt{}, false, nil
		}
		return artifact.Receipt{}, false, secretPublicationFailure("read")
	}
	if !p.owned(secret) {
		return artifact.Receipt{}, false, secretPublicationFailure("ownership")
	}
	receiptBytes := secret.Data[ReceiptDataKey]
	for _, key := range []string{"config.yaml", "config.json"} {
		if profile, ok := artifact.MatchesProtectedReceipt(secret.Data[key], receiptBytes); ok {
			receipt, err := artifact.RestoreReceipt(receiptBytes)
			if err != nil || profile.Engine.String() != secret.Labels[EngineLabel] {
				return artifact.Receipt{}, false, secretPublicationFailure("receipt")
			}
			return receipt, true, nil
		}
	}
	return artifact.Receipt{}, false, secretPublicationFailure("receipt")
}

func (p *SecretPublisher) Publish(ctx context.Context, validated artifact.Validated) (artifact.Publication, error) {
	if validated.Profile().Engine.String() != p.profileEngine() {
		return artifact.Publication{}, secretPublicationFailure("profile")
	}
	if p.guard != nil {
		if err := p.guard.Check(ctx); err != nil {
			return artifact.Publication{}, secretPublicationFailure("snapshot_obsolete")
		}
	}
	content := validated.Reveal()
	receipt, err := validated.ProtectedReceipt()
	if err != nil {
		return artifact.Publication{}, secretPublicationFailure("artifact")
	}
	configKey, ok := configDataKey(validated.Profile())
	if !ok {
		return artifact.Publication{}, secretPublicationFailure("profile")
	}
	if len(content)+len(receipt) > MaxSecretDataBytes {
		return artifact.Publication{}, secretPublicationFailure("payload_too_large")
	}
	current := &corev1.Secret{}
	err = p.client.Get(ctx, p.name, current)
	if apierrors.IsNotFound(err) {
		desired := &corev1.Secret{
			ObjectMeta: metav1Object(p.name),
			Type:       EngineSecretType,
			Data:       map[string][]byte{configKey: content, ReceiptDataKey: receipt},
		}
		p.setManagedLabels(desired)
		if err := controllerutil.SetControllerReference(p.owner, desired, p.scheme); err != nil {
			return artifact.Publication{}, secretPublicationFailure("owner")
		}
		if err := p.client.Create(ctx, desired); err != nil {
			return artifact.Publication{}, secretPublicationFailure("create")
		}
		return artifact.Publication{Profile: validated.Profile(), Changed: true}, nil
	}
	if err != nil {
		return artifact.Publication{}, secretPublicationFailure("read")
	}
	if !p.owned(current) {
		return artifact.Publication{}, secretPublicationFailure("ownership")
	}
	if bytes.Equal(current.Data[configKey], content) && bytes.Equal(current.Data[ReceiptDataKey], receipt) && len(current.Data) == 2 {
		return artifact.Publication{Profile: validated.Profile(), Changed: false}, nil
	}
	updated := current.DeepCopy()
	updated.Type = EngineSecretType
	updated.Data = map[string][]byte{configKey: content, ReceiptDataKey: receipt}
	p.setManagedLabels(updated)
	if err := p.client.Update(ctx, updated); err != nil {
		return artifact.Publication{}, secretPublicationFailure("update")
	}
	return artifact.Publication{Profile: validated.Profile(), Changed: true}, nil
}

func (p *SecretPublisher) owned(secret *corev1.Secret) bool {
	if secret.Type != EngineSecretType {
		return false
	}
	owner := metav1.GetControllerOf(secret)
	return owner != nil && owner.APIVersion == egressv1alpha1.GroupVersion.String() && owner.Kind == "EgressGateway" && owner.Name == p.owner.Name && owner.UID == p.owner.UID
}

func (p *SecretPublisher) setManagedLabels(secret *corev1.Secret) {
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	secret.Labels[ManagedByLabel] = "egressfox"
	secret.Labels[EngineLabel] = p.profileEngine()
}

func (p *SecretPublisher) profileEngine() string {
	switch p.owner.Spec.Engine {
	case egressv1alpha1.EngineMihomo:
		return artifact.EngineMihomo.String()
	case egressv1alpha1.EngineSingBox:
		return artifact.EngineSingBox.String()
	default:
		return "unknown"
	}
}

func configDataKey(profile artifact.Profile) (string, bool) {
	switch profile.Engine {
	case artifact.EngineMihomo:
		return "config.yaml", true
	case artifact.EngineSingBox:
		return "config.json", true
	default:
		return "", false
	}
}

func metav1Object(name types.NamespacedName) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace}
}

var _ fmt.Stringer = (*SecretPublisher)(nil)
