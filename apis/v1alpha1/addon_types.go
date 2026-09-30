/*
Copyright The Kubernetes Authors.

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
	"k8s.io/apimachinery/pkg/types"
)

// AddonClass identifies an addon manager and its shared parameters.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories=multicluster
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
type AddonClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AddonClassSpec   `json:"spec"`
	Status            AddonClassStatus `json:"status,omitempty"`
}

type AddonClassSpec struct {
	// ControllerName identifies the manager responsible for Addons using this Class.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*\/[A-Za-z0-9\/\-._~%!$&'()*+,;=:]+$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="controllerName is immutable"
	ControllerName string `json:"controllerName"`
	// ParametersRef points to shared parameters. The manager validates the referenced kind's scope.
	// +optional
	ParametersRef *AddonClassParametersReference `json:"parametersRef,omitempty"`
}

type AddonClassParametersReference struct {
	// Group is empty for a core API kind.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Group string `json:"group"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-zA-Z]([-a-zA-Z0-9]*[a-zA-Z0-9])?$`
	Kind string `json:"kind"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace string `json:"namespace,omitempty"`
}

type AddonClassStatus struct {
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type AddonClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AddonClass `json:"items"`
}

// Addon requests an installation on the ClusterProfile in its namespace.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories=multicluster
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
type Addon struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AddonSpec   `json:"spec"`
	Status            AddonStatus `json:"status,omitempty"`
}

type AddonSpec struct {
	ClassRef          AddonClassReference          `json:"classRef"`
	ClusterProfileRef AddonClusterProfileReference `json:"clusterProfileRef"`
	// +optional
	ParametersRef *AddonParametersReference `json:"parametersRef,omitempty"`
	// +optional
	// +kubebuilder:default=Delete
	DeletionPolicy AddonDeletionPolicy `json:"deletionPolicy,omitempty"`
}

type AddonClassReference struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
	// Namespace defaults to the Addon's namespace when omitted.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace string `json:"namespace,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterProfileRef is immutable"
type AddonClusterProfileReference struct {
	// Name selects a ClusterProfile in the Addon's namespace.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
}

type AddonParametersReference struct {
	// Group is empty for a core API kind.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Group string `json:"group"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-zA-Z]([-a-zA-Z0-9]*[a-zA-Z0-9])?$`
	Kind string `json:"kind"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
}

// +kubebuilder:validation:Enum=Delete;Orphan
type AddonDeletionPolicy string

const (
	AddonDeletionPolicyDelete AddonDeletionPolicy = "Delete"
	AddonDeletionPolicyOrphan AddonDeletionPolicy = "Orphan"
)

type AddonStatus struct {
	// ClassRef records the last Class accepted by the manager.
	// +optional
	ClassRef *AddonClassStatusReference `json:"classRef,omitempty"`
	// +optional
	ClusterProfileRef *AddonClusterProfileStatusReference `json:"clusterProfileRef,omitempty"`
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type AddonClassStatusReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// +kubebuilder:validation:MinLength=1
	ControllerName string `json:"controllerName"`
}

// +kubebuilder:validation:XValidation:rule="size(self.uid) > 0",message="uid must not be empty"
type AddonClusterProfileStatusReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string    `json:"name"`
	UID  types.UID `json:"uid"`
}

// +kubebuilder:object:root=true
type AddonList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Addon `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AddonClass{}, &AddonClassList{}, &Addon{}, &AddonList{})
}
