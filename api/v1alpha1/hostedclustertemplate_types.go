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

// HostedClusterTemplateSpec defines the desired state of HostedClusterTemplate.
type HostedClusterTemplateSpec struct {
	// version is the OCP version this template targets (e.g. "4.16").
	// The hostedcluster controller selects the template whose version
	// matches the VersionStream's target version.
	// +required
	Version string `json:"version"`

	// template is a raw YAML Go template for a complete HostedCluster CR spec.
	// Rendered with values from ManagedHostedCluster at reconcile time.
	// Supports Go text/template syntax (e.g. {{ .ClusterID }}).
	// +required
	Template string `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// HostedClusterTemplate is a cluster-scoped template for creating HostedCluster CRs.
// It contains a versioned Go template that is rendered with per-cluster values.
type HostedClusterTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of HostedClusterTemplate
	// +required
	Spec HostedClusterTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// HostedClusterTemplateList contains a list of HostedClusterTemplate
type HostedClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []HostedClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HostedClusterTemplate{}, &HostedClusterTemplateList{})
}
