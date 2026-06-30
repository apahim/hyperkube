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

			obj, err := buildHostedCluster(mhc, "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64")
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
			obj, err := buildHostedCluster(mhc, resolvedImage)
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

			obj, err := buildHostedCluster(mhc, "image:latest")
			Expect(err).NotTo(HaveOccurred())

			spec, ok := obj.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(spec["infraID"]).To(Equal("my-infra-id"))
		})

		It("should include platform configuration in the output", func() {
			mhc := &hcpv1alpha1.ManagedHostedCluster{
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

			obj, err := buildHostedCluster(mhc, "image:latest")
			Expect(err).NotTo(HaveOccurred())

			spec, ok := obj.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())

			platform, ok := spec["platform"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(platform["type"]).To(Equal("GCP"))
		})
	})
})
