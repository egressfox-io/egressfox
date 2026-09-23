/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type SourceFormat string

const (
	SourceFormatURIList       SourceFormat = "URIList"
	SourceFormatBase64URIList SourceFormat = "Base64URIList"
)

type SelectionStrategy string

const (
	SelectionStatic        SelectionStrategy = "Static"
	SelectionLowestLatency SelectionStrategy = "LowestLatency"
	SelectionAdaptive      SelectionStrategy = "Adaptive"
)

// SecretKeyReference selects one key from a Secret in the resource namespace.
type SecretKeyReference struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key"`
}

// +kubebuilder:validation:XValidation:rule="has(self.secretRef) != has(self.http)",message="exactly one of secretRef or http is required"
type SubscriptionSource struct {
	// ID is stable source provenance and must not contain a URL or credential.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9.-]*$`
	ID string `json:"id"`
	// SecretRef is the legacy Secret-backed source input.
	// +optional
	SecretRef *SecretKeyReference `json:"secretRef,omitempty"`
	// HTTP enables managed refresh from a Secret-backed URL.
	// +optional
	HTTP *HTTPSource `json:"http,omitempty"`
	// +kubebuilder:validation:Enum=URIList;Base64URIList
	Format SourceFormat `json:"format"`
	// AllowEmpty accepts an authoritative empty snapshot for this source.
	// +optional
	AllowEmpty bool `json:"allowEmpty,omitempty"`
	// AllowPartial commits accepted records when other records are rejected.
	// +optional
	AllowPartial bool `json:"allowPartial,omitempty"`
}

type HTTPSource struct {
	URLSecretRef SecretKeyReference `json:"urlSecretRef"`
	// +optional
	AuthorizationSecretRef *SecretKeyReference `json:"authorizationSecretRef,omitempty"`
	// +optional
	AllowHTTP bool `json:"allowHTTP,omitempty"`
	// +optional
	AllowPrivateNetworks bool `json:"allowPrivateNetworks,omitempty"`
	// AllowInsecureTLS disables certificate verification for this source only.
	// +optional
	AllowInsecureTLS bool `json:"allowInsecureTLS,omitempty"`
	// MaxStale bounds cache fallback from the last successful remote validation.
	// +kubebuilder:default="24h"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1m') && duration(self) <= duration('168h')",message="maxStale must be between 1m and 168h"
	MaxStale *metav1.Duration `json:"maxStale,omitempty"`
}

type ProbeSpec struct {
	TargetSecretRef SecretKeyReference `json:"targetSecretRef"`
	// +kubebuilder:default=204
	// +kubebuilder:validation:Minimum=100
	// +kubebuilder:validation:Maximum=599
	ExpectedStatus int32 `json:"expectedStatus,omitempty"`
	// +kubebuilder:default="5s"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('100ms') && duration(self) <= duration('2m')",message="timeout must be between 100ms and 2m"
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// +optional
	AllowHTTP bool `json:"allowHTTP,omitempty"`
	// +optional
	AllowPrivateTargets bool `json:"allowPrivateTargets,omitempty"`
	// +optional
	AllowPrivateEndpoints bool `json:"allowPrivateEndpoints,omitempty"`
}

type SelectionSpec struct {
	// +kubebuilder:default=Adaptive
	// +kubebuilder:validation:Enum=Static;LowestLatency;Adaptive
	Strategy SelectionStrategy `json:"strategy,omitempty"`
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10000
	TopN int32 `json:"topN,omitempty"`
}

// ProxyPoolSpec defines source admission, probe authorization and selection intent.
type ProxyPoolSpec struct {
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=id
	Sources   []SubscriptionSource `json:"sources"`
	Probe     ProbeSpec            `json:"probe"`
	Selection SelectionSpec        `json:"selection,omitempty"`
	// AllowInsecureTLS admits endpoint records that disable certificate verification.
	// +optional
	AllowInsecureTLS bool `json:"allowInsecureTLS,omitempty"`
	// +kubebuilder:default="5m"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('30s') && duration(self) <= duration('24h')",message="refreshInterval must be between 30s and 24h"
	RefreshInterval *metav1.Duration `json:"refreshInterval,omitempty"`
}

// ProxyPoolStatus contains safe, bounded aggregate state only.
type ProxyPoolStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	AcceptedEndpoints  int32 `json:"acceptedEndpoints,omitempty"`
	RejectedRecords    int32 `json:"rejectedRecords,omitempty"`
	UnsupportedRecords int32 `json:"unsupportedRecords,omitempty"`
	// LastInventoryChangeTime advances only when a successful admission changes
	// the bounded inventory summary or observes a new spec generation.
	LastInventoryChangeTime *metav1.Time `json:"lastInventoryChangeTime,omitempty"`
	// Sources contains bounded, credential-free per-source state.
	// +listType=map
	// +listMapKey=id
	Sources []SourceStatus `json:"sources,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type SourceStatus struct {
	ID              string       `json:"id"`
	State           string       `json:"state"`
	Reason          string       `json:"reason,omitempty"`
	LastSuccessTime *metav1.Time `json:"lastSuccessTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Endpoints",type=integer,JSONPath=`.status.acceptedEndpoints`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyPool describes a bounded set of Secret-backed or managed HTTP subscriptions.
type ProxyPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              ProxyPoolSpec   `json:"spec"`
	Status            ProxyPoolStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

type ProxyPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ProxyPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ProxyPool{}, &ProxyPoolList{})
		return nil
	})
}
