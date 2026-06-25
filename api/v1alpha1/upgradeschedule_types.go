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

// UpgradeScheduleSpec defines the desired state of UpgradeSchedule.
// Fields will be defined as upgrade scheduling requirements are finalized
// (e.g. maintenance windows, cron expressions, blackout periods).
type UpgradeScheduleSpec struct {
}

// UpgradeScheduleStatus defines the observed state of UpgradeSchedule.
type UpgradeScheduleStatus struct {
	// conditions represent the current state of the UpgradeSchedule resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// UpgradeSchedule defines when version upgrades are allowed for a cluster.
// Referenced by ManagedHostedCluster via upgradeScheduleRef.
type UpgradeSchedule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of UpgradeSchedule
	// +required
	Spec UpgradeScheduleSpec `json:"spec"`

	// status defines the observed state of UpgradeSchedule
	// +optional
	Status UpgradeScheduleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// UpgradeScheduleList contains a list of UpgradeSchedule
type UpgradeScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []UpgradeSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UpgradeSchedule{}, &UpgradeScheduleList{})
}
