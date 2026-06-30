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

// HostedClusterSpec defines the desired state of a HyperShift HostedCluster.
// These types are a curated subset of the upstream HyperShift API, copied
// here to avoid a direct Go module dependency on the HyperShift operator.
type HostedClusterSpec struct {
	// release specifies the OCP release image for the hosted cluster.
	// +required
	Release ReleaseSpec `json:"release"`

	// infraID is the unique infrastructure identifier for this cluster.
	// +required
	InfraID string `json:"infraID"`

	// platform specifies the underlying infrastructure provider for the cluster.
	// +required
	Platform PlatformSpec `json:"platform" openapi:"hidden"`

	// networking defines the networking configuration for the cluster.
	// +optional
	Networking *ClusterNetworkingSpec `json:"networking,omitempty" openapi:"hidden"`

	// etcd defines the etcd configuration for the hosted control plane.
	// +optional
	Etcd *EtcdSpec `json:"etcd,omitempty" openapi:"hidden"`

	// services defines the service publishing strategy for control plane services.
	// +optional
	Services []ServicePublishingStrategyMapping `json:"services,omitempty" openapi:"hidden"`
}

// ReleaseSpec defines the target OCP release.
type ReleaseSpec struct {
	// image is the release image pullspec (e.g. quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64).
	// +required
	Image string `json:"image"`
}

// PlatformSpec specifies the underlying infrastructure provider configuration.
type PlatformSpec struct {
	// type is the infrastructure provider type (e.g. "None", "GCP", "AWS").
	// +required
	Type string `json:"type"`
}

// ClusterNetworkingSpec defines the networking configuration for the cluster.
type ClusterNetworkingSpec struct {
	// clusterNetwork is the list of IP address pools for pod IPs.
	// +optional
	ClusterNetwork []NetworkRange `json:"clusterNetwork,omitempty"`

	// serviceNetwork is the list of IP address pools for service IPs.
	// +optional
	ServiceNetwork []NetworkRange `json:"serviceNetwork,omitempty"`
}

// NetworkRange defines a network CIDR range.
type NetworkRange struct {
	// cidr is the IP address range in CIDR notation (e.g. 10.132.0.0/14).
	// +required
	CIDR string `json:"cidr"`
}

// EtcdSpec defines the etcd configuration.
type EtcdSpec struct {
	// managementType defines how etcd is managed (e.g. "Managed", "Unmanaged").
	// +required
	ManagementType string `json:"managementType"`

	// managed defines configuration when managementType is "Managed".
	// +optional
	Managed *ManagedEtcdSpec `json:"managed,omitempty"`
}

// ManagedEtcdSpec defines configuration for managed etcd.
type ManagedEtcdSpec struct {
	// storage defines the storage configuration for etcd.
	// +required
	Storage ManagedEtcdStorageSpec `json:"storage"`
}

// ManagedEtcdStorageSpec defines etcd storage configuration.
type ManagedEtcdStorageSpec struct {
	// type is the storage backend type (e.g. "PersistentVolume").
	// +required
	Type string `json:"type"`
}

// ServicePublishingStrategyMapping defines how a control plane service is published.
type ServicePublishingStrategyMapping struct {
	// service is the name of the service (e.g. "APIServer", "OAuthServer", "Konnectivity", "Ignition").
	// +required
	Service string `json:"service"`

	// servicePublishingStrategy defines how the service is made available.
	// +required
	ServicePublishingStrategy ServicePublishingStrategy `json:"servicePublishingStrategy"`
}

// ServicePublishingStrategy specifies how to publish a service.
type ServicePublishingStrategy struct {
	// type is the publishing strategy (e.g. "LoadBalancer", "Route", "NodePort").
	// +required
	Type string `json:"type"`
}
