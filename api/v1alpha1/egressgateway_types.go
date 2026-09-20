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

type Engine string

const (
	EngineMihomo  Engine = "Mihomo"
	EngineSingBox Engine = "SingBox"
)

type LocalReference struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
}

type ListenerSpec struct {
	// M3 accepts loopback listeners only.
	// +kubebuilder:default="127.0.0.1"
	// +kubebuilder:validation:Enum="127.0.0.1";"::1"
	Address string `json:"address,omitempty"`
	// +kubebuilder:default=1080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// EgressGatewaySpec binds one pool to a pinned engine renderer and owned output Secret.
type EgressGatewaySpec struct {
	PoolRef LocalReference `json:"poolRef"`
	// +kubebuilder:validation:Enum=Mihomo;SingBox
	Engine   Engine       `json:"engine"`
	Listener ListenerSpec `json:"listener,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	OutputSecretName string `json:"outputSecretName"`
}

// EgressGatewayStatus never reports credentials, endpoint IDs or artifact digests.
type EgressGatewayStatus struct {
	ObservedGeneration int64        `json:"observedGeneration,omitempty"`
	EligibleEndpoints  int32        `json:"eligibleEndpoints,omitempty"`
	SelectedEndpoints  int32        `json:"selectedEndpoints,omitempty"`
	LastPublishedTime  *metav1.Time `json:"lastPublishedTime,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Engine",type=string,JSONPath=`.spec.engine`
// +kubebuilder:printcolumn:name="Published",type=string,JSONPath=`.status.conditions[?(@.type=="Published")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// EgressGateway requests validated engine configuration publication; it owns no engine workload.
type EgressGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              EgressGatewaySpec   `json:"spec"`
	Status            EgressGatewayStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

type EgressGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []EgressGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &EgressGateway{}, &EgressGatewayList{})
		return nil
	})
}
