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

// ManagedHostedClusterSpec defines the desired state of ManagedHostedCluster.
type ManagedHostedClusterSpec struct {
	// clusterID is the unique identifier for this cluster.
	// +required
	ClusterID string `json:"clusterID"`

	// versionStreamRef references a VersionStream that determines
	// the target OCP version for this cluster.
	// +required
	VersionStreamRef VersionStreamReference `json:"versionStreamRef"`

	// upgradeScheduleRef optionally references an UpgradeSchedule in the same
	// namespace that controls when version upgrades are applied.
	// If not set, upgrades apply immediately when the VersionStream target changes.
	// +optional
	UpgradeScheduleRef *NameReference `json:"upgradeScheduleRef,omitempty"`

	// hostedCluster defines the HyperShift HostedCluster configuration for this cluster.
	// +required
	HostedCluster HostedClusterSpec `json:"hostedCluster"`
}

// VersionStreamReference is a reference to a cluster-scoped VersionStream resource.
type VersionStreamReference struct {
	// name is the name of the VersionStream resource.
	// +required
	Name string `json:"name"`
}

// NameReference is a reference to a resource by name in the same namespace.
type NameReference struct {
	// name is the name of the resource.
	// +required
	Name string `json:"name"`
}

// ManagedHostedClusterStatus defines the observed state of ManagedHostedCluster.
type ManagedHostedClusterStatus struct {
	// conditions represent the current state of the ManagedHostedCluster resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ManagedHostedCluster is the Schema for the managedhostedclusters API
type ManagedHostedCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ManagedHostedCluster
	// +required
	Spec ManagedHostedClusterSpec `json:"spec"`

	// status defines the observed state of ManagedHostedCluster
	// +optional
	Status ManagedHostedClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ManagedHostedClusterList contains a list of ManagedHostedCluster
type ManagedHostedClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ManagedHostedCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedHostedCluster{}, &ManagedHostedClusterList{})
}
