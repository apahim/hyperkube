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

// VersionStreamSpec defines the desired state of VersionStream.
type VersionStreamSpec struct {
	// targetVersion is the desired OCP version as major.minor (e.g. "4.16").
	// The controller resolves this to the latest patch version available in
	// the Cincinnati update graph for the configured channel.
	// Changing this triggers upgrades for all clusters referencing this stream,
	// subject to their individual UpgradeSchedule constraints.
	// +required
	TargetVersion string `json:"targetVersion"`

	// channelGroup is the Cincinnati channel group used to resolve
	// the target version (e.g. "stable", "fast", "candidate").
	// Defaults to "stable" if not set.
	// +optional
	// +kubebuilder:default="stable"
	ChannelGroup string `json:"channelGroup,omitempty"`

	// arch is the CPU architecture for the release image.
	// Defaults to "amd64" if not set.
	// +optional
	// +kubebuilder:default="amd64"
	Arch string `json:"arch,omitempty"`
}

// VersionStreamStatus defines the observed state of VersionStream.
type VersionStreamStatus struct {
	// releaseImage is the fully-qualified pullspec resolved from Cincinnati
	// for the target version (e.g. "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64").
	// Empty until the image is successfully resolved.
	// +optional
	ReleaseImage string `json:"releaseImage,omitempty"`

	// resolvedVersion is the exact OCP version that was resolved
	// (e.g. "4.16.3"), representing the latest patch for the target major.minor.
	// +optional
	ResolvedVersion string `json:"resolvedVersion,omitempty"`

	// channel is the Cincinnati channel used for version resolution
	// (e.g. "stable-4.16").
	// +optional
	Channel string `json:"channel,omitempty"`

	// conditions represent the current state of the VersionStream resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=".spec.targetVersion"
// +kubebuilder:printcolumn:name="Resolved",type=string,JSONPath=".status.resolvedVersion"
// +kubebuilder:printcolumn:name="Image Resolved",type=string,JSONPath=".status.conditions[?(@.type=='ImageResolved')].status"

// VersionStream tracks the target OCP version for a group of clusters.
// When the target version changes, all associated ManagedHostedClusters
// are upgraded (subject to their UpgradeSchedule constraints).
type VersionStream struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of VersionStream
	// +required
	Spec VersionStreamSpec `json:"spec"`

	// status defines the observed state of VersionStream
	// +optional
	Status VersionStreamStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// VersionStreamList contains a list of VersionStream
type VersionStreamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []VersionStream `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VersionStream{}, &VersionStreamList{})
}
