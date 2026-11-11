/*
Copyright 2026 Brian Morton.

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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var _ = Describe("resolveTarget", func() {
	It("returns ErrTargetNotFound for a missing reference", func() {
		ref := temporalv1alpha1.ClusterReference{Name: "nope"}
		_, err := resolveTarget(ctx, k8sClient, "default", ref)
		Expect(errors.Is(err, ErrTargetNotFound)).To(BeTrue())
	})

	It("resolves a TemporalDevServer to a plaintext address", func() {
		dev := &temporalv1alpha1.TemporalDevServer{
			ObjectMeta: metav1.ObjectMeta{Name: "dev-resolve", Namespace: "default"},
			Spec:       temporalv1alpha1.TemporalDevServerSpec{Version: "latest"},
		}
		Expect(k8sClient.Create(ctx, dev)).To(Succeed())

		ref := temporalv1alpha1.ClusterReference{Name: "dev-resolve", Kind: temporalv1alpha1.ClusterKindTemporalDevServer}
		target, err := resolveTarget(ctx, k8sClient, "default", ref)
		Expect(err).NotTo(HaveOccurred())
		Expect(target.TLSConfig).To(BeNil())
		Expect(target.Address).To(Equal("dev-resolve-devserver.default.svc:7233"))
		Expect(target.Ready).To(BeFalse())
	})
})

var _ = Describe("effectiveWorkflowRunPolicy", func() {
	It("defaults TemporalCluster to disabled when nil", func() {
		p := effectiveWorkflowRunPolicy(temporalv1alpha1.ClusterKindTemporalCluster, nil)
		Expect(p.Enabled).To(BeFalse())
	})
	It("defaults TemporalDevServer to enabled when nil", func() {
		p := effectiveWorkflowRunPolicy(temporalv1alpha1.ClusterKindTemporalDevServer, nil)
		Expect(p.Enabled).To(BeTrue())
		Expect(p.AllowedNamespaces).To(BeEmpty())
	})
	It("passes an explicit policy through unchanged", func() {
		in := &temporalv1alpha1.WorkflowRunPolicy{Enabled: true, AllowedTaskQueues: []string{"q"}}
		p := effectiveWorkflowRunPolicy(temporalv1alpha1.ClusterKindTemporalCluster, in)
		Expect(p.Enabled).To(BeTrue())
		Expect(p.AllowedTaskQueues).To(Equal([]string{"q"}))
	})
})
