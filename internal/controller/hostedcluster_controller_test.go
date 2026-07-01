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

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("HostedCluster Controller", func() {
	Context("Building HostedCluster from spec", func() {
		It("should produce an unstructured object with correct GVK", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: hcpv1alpha1.ManagedHostedClusterSpec{
					ClusterID: "my-cluster-abc",
					VersionStreamRef: hcpv1alpha1.VersionStreamReference{
						Name: "stable",
					},
					HostedCluster: hcpv1alpha1.HostedClusterSpec{
						Release: hcpv1alpha1.ReleaseSpec{
							Image: "placeholder:latest",
						},
						InfraID: "my-cluster-abc",
						Platform: hcpv1alpha1.PlatformSpec{
							Type: "None",
						},
					},
				},
			}

			obj, err := buildHostedCluster(mhc, "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64", "stable-4.16")
			Expect(err).NotTo(HaveOccurred())
			Expect(obj).NotTo(BeNil())

			Expect(obj.GetAPIVersion()).To(Equal("hypershift.openshift.io/v1beta1"))
			Expect(obj.GetKind()).To(Equal("HostedCluster"))
		})

		It("should override the release image with the VersionStream-resolved value", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
				Spec: hcpv1alpha1.ManagedHostedClusterSpec{
					ClusterID: "test-001",
					VersionStreamRef: hcpv1alpha1.VersionStreamReference{
						Name: "stable",
					},
					HostedCluster: hcpv1alpha1.HostedClusterSpec{
						Release: hcpv1alpha1.ReleaseSpec{
							Image: "original-image:v1",
						},
						InfraID: "test-001",
						Platform: hcpv1alpha1.PlatformSpec{
							Type: "None",
						},
					},
				},
			}

			resolvedImage := "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"
			obj, err := buildHostedCluster(mhc, resolvedImage, "stable-4.16")
			Expect(err).NotTo(HaveOccurred())

			spec, ok := obj.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())

			release, ok := spec["release"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(release["image"]).To(Equal(resolvedImage))
		})

		It("should propagate infraID from the spec", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
				Spec: hcpv1alpha1.ManagedHostedClusterSpec{
					ClusterID: "infra-test",
					VersionStreamRef: hcpv1alpha1.VersionStreamReference{
						Name: "stable",
					},
					HostedCluster: hcpv1alpha1.HostedClusterSpec{
						Release: hcpv1alpha1.ReleaseSpec{
							Image: "placeholder",
						},
						InfraID: "my-infra-id",
						Platform: hcpv1alpha1.PlatformSpec{
							Type: "None",
						},
					},
				},
			}

			obj, err := buildHostedCluster(mhc, "image:latest", "stable-4.16")
			Expect(err).NotTo(HaveOccurred())

			spec, ok := obj.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(spec["infraID"]).To(Equal("my-infra-id"))
		})

		It("should populate all hidden fields with defaults and derived values", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster"},
				Spec: hcpv1alpha1.ManagedHostedClusterSpec{
					ClusterID: "cluster-123",
					VersionStreamRef: hcpv1alpha1.VersionStreamReference{
						Name: "stable",
					},
					HostedCluster: hcpv1alpha1.HostedClusterSpec{
						Release: hcpv1alpha1.ReleaseSpec{Image: "placeholder"},
						InfraID: "test-defaults",
					},
				},
			}

			spec := mhc.Spec.HostedCluster
			populateHostedClusterSpec(&spec, mhc, "stable-4.16")

			// Static defaults
			Expect(spec.Platform.Type).To(Equal("GCP"))

			Expect(spec.Networking).NotTo(BeNil())
			Expect(spec.Networking.ClusterNetwork).To(HaveLen(1))
			Expect(spec.Networking.ClusterNetwork[0].CIDR).To(Equal("10.132.0.0/14"))
			Expect(spec.Networking.ServiceNetwork).To(HaveLen(1))
			Expect(spec.Networking.ServiceNetwork[0].CIDR).To(Equal("172.31.0.0/16"))
			Expect(spec.Networking.NetworkType).To(Equal("OVNKubernetes"))

			Expect(spec.Etcd).NotTo(BeNil())
			Expect(spec.Etcd.ManagementType).To(Equal("Managed"))
			Expect(spec.Etcd.Managed).NotTo(BeNil())
			Expect(spec.Etcd.Managed.Storage.Type).To(Equal("PersistentVolume"))

			Expect(spec.Services).To(HaveLen(4))
			Expect(spec.Services[0].Service).To(Equal("APIServer"))
			Expect(spec.Services[0].ServicePublishingStrategy.Type).To(Equal("Route"))
			Expect(spec.Services[1].Service).To(Equal("OAuthServer"))
			Expect(spec.Services[1].ServicePublishingStrategy.Type).To(Equal("Route"))
			Expect(spec.Services[2].Service).To(Equal("Konnectivity"))
			Expect(spec.Services[2].ServicePublishingStrategy.Type).To(Equal("Route"))
			Expect(spec.Services[3].Service).To(Equal("Ignition"))
			Expect(spec.Services[3].ServicePublishingStrategy.Type).To(Equal("Route"))

			// Per-cluster derived values
			Expect(spec.ClusterID).To(Equal("cluster-123"))
			Expect(spec.Channel).To(Equal("stable-4.16"))
			Expect(spec.ControllerAvailabilityPolicy).To(Equal("HighlyAvailable"))
			Expect(spec.PullSecret).NotTo(BeNil())
			Expect(spec.PullSecret.Name).To(Equal("pull-secret"))
			Expect(spec.ServiceAccountSigningKey).NotTo(BeNil())
			Expect(spec.ServiceAccountSigningKey.Name).To(Equal("my-cluster-signing-key"))

			Expect(spec.Capabilities).NotTo(BeNil())
			Expect(spec.Capabilities.Disabled).To(ConsistOf("ImageRegistry", "Console", "Ingress"))

			Expect(spec.Configuration).NotTo(BeNil())
			Expect(spec.Configuration.Authentication).NotTo(BeNil())
			Expect(spec.Configuration.Authentication.Type).To(Equal("OIDC"))
			Expect(spec.Configuration.Authentication.OIDCProviders).To(HaveLen(1))
			Expect(spec.Configuration.Authentication.OIDCProviders[0].Name).To(Equal("google"))
			Expect(spec.Configuration.Authentication.OIDCProviders[0].Issuer.IssuerURL).To(Equal("https://accounts.google.com"))
			Expect(spec.Configuration.Authentication.OIDCProviders[0].Issuer.Audiences).To(ConsistOf("32555940559.apps.googleusercontent.com"))
			Expect(spec.Configuration.Authentication.OIDCProviders[0].ClaimMappings.Username.Claim).To(Equal("email"))
			Expect(spec.Configuration.Authentication.OIDCProviders[0].ClaimMappings.Groups).NotTo(BeNil())
			Expect(spec.Configuration.Authentication.OIDCProviders[0].ClaimMappings.Groups.Claim).To(Equal("hd"))
		})

		It("should not override user-provided networking, etcd, or capabilities", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "preserve-test"},
				Spec: hcpv1alpha1.ManagedHostedClusterSpec{
					ClusterID: "preserve-test",
					VersionStreamRef: hcpv1alpha1.VersionStreamReference{
						Name: "stable",
					},
					HostedCluster: hcpv1alpha1.HostedClusterSpec{
						Release: hcpv1alpha1.ReleaseSpec{Image: "placeholder"},
						InfraID: "test-preserve",
						Networking: &hcpv1alpha1.ClusterNetworkingSpec{
							ClusterNetwork: []hcpv1alpha1.NetworkRange{{CIDR: "10.0.0.0/8"}},
						},
						Etcd: &hcpv1alpha1.EtcdSpec{
							ManagementType: "Unmanaged",
						},
					},
				},
			}

			spec := mhc.Spec.HostedCluster
			populateHostedClusterSpec(&spec, mhc, "stable-4.16")

			Expect(spec.Platform.Type).To(Equal("GCP"))
			Expect(spec.Networking.ClusterNetwork[0].CIDR).To(Equal("10.0.0.0/8"))
			Expect(spec.Etcd.ManagementType).To(Equal("Unmanaged"))
		})

		It("should include platform configuration in the output", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "platform-test"},
				Spec: hcpv1alpha1.ManagedHostedClusterSpec{
					ClusterID: "platform-test",
					VersionStreamRef: hcpv1alpha1.VersionStreamReference{
						Name: "stable",
					},
					HostedCluster: hcpv1alpha1.HostedClusterSpec{
						Release: hcpv1alpha1.ReleaseSpec{
							Image: "placeholder",
						},
						InfraID: "platform-test",
						Platform: hcpv1alpha1.PlatformSpec{
							Type: "GCP",
						},
					},
				},
			}

			obj, err := buildHostedCluster(mhc, "image:latest", "stable-4.16")
			Expect(err).NotTo(HaveOccurred())

			spec, ok := obj.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())

			platform, ok := spec["platform"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(platform["type"]).To(Equal("GCP"))
		})

		It("should set HyperShift annotations on the output object", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "annotations-test"},
				Spec: hcpv1alpha1.ManagedHostedClusterSpec{
					ClusterID: "annotations-test",
					VersionStreamRef: hcpv1alpha1.VersionStreamReference{
						Name: "stable",
					},
					HostedCluster: hcpv1alpha1.HostedClusterSpec{
						Release: hcpv1alpha1.ReleaseSpec{Image: "placeholder"},
						InfraID: "annotations-test",
					},
				},
			}

			obj, err := buildHostedCluster(mhc, "image:latest", "stable-4.16")
			Expect(err).NotTo(HaveOccurred())

			annotations := obj.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("hypershift.openshift.io/pod-security-admission-label-override", "baseline"))
			Expect(annotations).To(HaveKeyWithValue("hypershift.openshift.io/skip-kas-conflict-san-validation", "true"))
		})

		It("should propagate clusterID and channel in the output", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "derived-test"},
				Spec: hcpv1alpha1.ManagedHostedClusterSpec{
					ClusterID: "derived-cluster-id",
					VersionStreamRef: hcpv1alpha1.VersionStreamReference{
						Name: "fast",
					},
					HostedCluster: hcpv1alpha1.HostedClusterSpec{
						Release: hcpv1alpha1.ReleaseSpec{Image: "placeholder"},
						InfraID: "derived-test",
					},
				},
			}

			obj, err := buildHostedCluster(mhc, "image:latest", "fast-4.17")
			Expect(err).NotTo(HaveOccurred())

			spec, ok := obj.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(spec["clusterID"]).To(Equal("derived-cluster-id"))
			Expect(spec["channel"]).To(Equal("fast-4.17"))
			Expect(spec["controllerAvailabilityPolicy"]).To(Equal("HighlyAvailable"))

			pullSecret, ok := spec["pullSecret"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(pullSecret["name"]).To(Equal("pull-secret"))

			signingKey, ok := spec["serviceAccountSigningKey"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(signingKey["name"]).To(Equal("derived-test-signing-key"))
		})
	})
})
