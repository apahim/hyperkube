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
)

// CustomRoleSpec defines the desired state of CustomRole.
type CustomRoleSpec struct {
	// permissions is the list of granular permission names this role grants.
	// Valid values: cluster.create, cluster.list, cluster.get, cluster.update,
	// cluster.delete, cluster.kubeconfig, policy.manage
	// +required
	// +kubebuilder:validation:MinItems=1
	Permissions []string `json:"permissions"`

	// description is a human-readable description of this custom role.
	// +optional
	Description string `json:"description,omitempty"`

	// conditions are optional Cedar condition expressions that scope the permissions.
	// Each condition is a Cedar 'when' clause body (without the 'when' keyword).
	// Example: 'resource.labels.env == "staging"'
	// +optional
	Conditions []string `json:"conditions,omitempty"`
}

// CustomRoleStatus defines the observed state of CustomRole.
type CustomRoleStatus struct {
	// conditions represent the current state of the CustomRole.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CustomRole is the Schema for the customroles API.
// Customers can create custom roles by aggregating granular permissions.
type CustomRole struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of CustomRole
	// +required
	Spec CustomRoleSpec `json:"spec"`

	// status defines the observed state of CustomRole
	// +optional
	Status CustomRoleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CustomRoleList contains a list of CustomRole
type CustomRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CustomRole `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CustomRole{}, &CustomRoleList{})
}
