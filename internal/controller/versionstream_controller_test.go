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
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hcpv1alpha1 "github.com/apahim/hyperkube/api/v1alpha1"
)

type mockResolver struct {
	image           string
	resolvedVersion string
	channel         string
	err             error
}

func (m *mockResolver) ResolveVersion(_ context.Context, _, _, _ string) (string, string, string, error) {
	return m.image, m.resolvedVersion, m.channel, m.err
}

var _ = Describe("VersionStream Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-versionstream"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind VersionStream")
			vs := &hcpv1alpha1.VersionStream{}
			err := k8sClient.Get(ctx, typeNamespacedName, vs)
			if err != nil && errors.IsNotFound(err) {
				resource := &hcpv1alpha1.VersionStream{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					Spec: hcpv1alpha1.VersionStreamSpec{
						TargetVersion: "4.16",
						ChannelGroup:  "stable",
						Arch:          "amd64",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &hcpv1alpha1.VersionStream{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance VersionStream")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should resolve the version and update status on success", func() {
			resolver := &mockResolver{
				image:           "quay.io/ocp-release:4.16.3-x86_64",
				resolvedVersion: "4.16.3",
				channel:         "stable-4.16",
			}

			controllerReconciler := &VersionStreamReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Resolver: resolver,
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			var vs hcpv1alpha1.VersionStream
			Expect(k8sClient.Get(ctx, typeNamespacedName, &vs)).To(Succeed())
			Expect(vs.Status.ReleaseImage).To(Equal("quay.io/ocp-release:4.16.3-x86_64"))
			Expect(vs.Status.ResolvedVersion).To(Equal("4.16.3"))
			Expect(vs.Status.Channel).To(Equal("stable-4.16"))

			cond := meta.FindStatusCondition(vs.Status.Conditions, "ImageResolved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("Resolved"))
		})

		It("should set condition to False on resolution failure", func() {
			resolver := &mockResolver{
				err: fmt.Errorf("Cincinnati unreachable"),
			}

			controllerReconciler := &VersionStreamReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Resolver: resolver,
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

			var vs hcpv1alpha1.VersionStream
			Expect(k8sClient.Get(ctx, typeNamespacedName, &vs)).To(Succeed())
			Expect(vs.Status.ReleaseImage).To(BeEmpty())

			cond := meta.FindStatusCondition(vs.Status.Conditions, "ImageResolved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ResolutionFailed"))
			Expect(cond.Message).To(ContainSubstring("Cincinnati unreachable"))
		})

		It("should return no error for a non-existent resource", func() {
			resolver := &mockResolver{}
			controllerReconciler := &VersionStreamReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Resolver: resolver,
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "does-not-exist"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})
