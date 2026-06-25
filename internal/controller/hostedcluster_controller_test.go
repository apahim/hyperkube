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
)

const testTemplate = `apiVersion: hypershift.openshift.io/v1beta1
kind: HostedCluster
spec:
  release:
    image: {{ .ReleaseImage }}
  infraID: {{ .ClusterID }}
  platform:
    type: None
`

var _ = Describe("HostedCluster Controller", func() {
	Context("Template rendering", func() {
		It("should render ClusterID and ReleaseImage into the template", func() {
			data := TemplateData{
				ClusterID:    "my-cluster-abc",
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64",
			}

			obj, err := renderTemplate(testTemplate, data)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj).NotTo(BeNil())

			Expect(obj.GetAPIVersion()).To(Equal("hypershift.openshift.io/v1beta1"))
			Expect(obj.GetKind()).To(Equal("HostedCluster"))

			spec, ok := obj.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(spec["infraID"]).To(Equal("my-cluster-abc"))

			release, ok := spec["release"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(release["image"]).To(Equal("quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"))
		})

		It("should fail on invalid template syntax", func() {
			_, err := renderTemplate("{{ .Invalid", TemplateData{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parsing template"))
		})

		It("should fail on invalid YAML output", func() {
			_, err := renderTemplate("not: valid: yaml: {{", TemplateData{})
			Expect(err).To(HaveOccurred())
		})
	})
})
