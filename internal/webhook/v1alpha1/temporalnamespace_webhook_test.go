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

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var _ = Describe("TemporalNamespace Webhook", func() {
	var (
		obj       *temporalv1alpha1.TemporalNamespace
		oldObj    *temporalv1alpha1.TemporalNamespace
		validator TemporalNamespaceCustomValidator
		ctx       context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		obj = &temporalv1alpha1.TemporalNamespace{
			Spec: temporalv1alpha1.TemporalNamespaceSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: "test"},
			},
		}
		oldObj = &temporalv1alpha1.TemporalNamespace{
			Spec: temporalv1alpha1.TemporalNamespaceSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: "test"},
			},
		}
		validator = TemporalNamespaceCustomValidator{}
	})

	Context("Validation on create", func() {
		It("rejects an empty clusterRef.name", func() {
			obj.Spec.ClusterRef.Name = ""
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits a valid namespace", func() {
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects clusters set while isGlobal is false", func() {
			obj.Spec.IsGlobal = false
			obj.Spec.Clusters = []string{"a", "b"}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects activeCluster set while isGlobal is false", func() {
			obj.Spec.IsGlobal = false
			obj.Spec.ActiveCluster = "a"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects an activeCluster that is not a member of clusters", func() {
			obj.Spec.IsGlobal = true
			obj.Spec.Clusters = []string{"a", "b"}
			obj.Spec.ActiveCluster = "c"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits a valid global namespace", func() {
			obj.Spec.IsGlobal = true
			obj.Spec.Clusters = []string{"a", "b"}
			obj.Spec.ActiveCluster = "a"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Validation on update", func() {
		It("rejects changing isGlobal after creation", func() {
			oldObj.Spec.IsGlobal = true
			obj.Spec.IsGlobal = false
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits update when isGlobal is unchanged", func() {
			oldObj.Spec.IsGlobal = true
			obj.Spec.IsGlobal = true
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects setting isGlobal from false to true after creation", func() {
			oldObj.Spec.IsGlobal = false
			obj.Spec.IsGlobal = true
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects an invalid activeCluster on update", func() {
			oldObj.Spec.IsGlobal = true
			obj.Spec.IsGlobal = true
			obj.Spec.Clusters = []string{"a", "b"}
			obj.Spec.ActiveCluster = "c"
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits a valid failover update", func() {
			oldObj.Spec.IsGlobal = true
			oldObj.Spec.Clusters = []string{"a", "b"}
			oldObj.Spec.ActiveCluster = "a"
			obj.Spec.IsGlobal = true
			obj.Spec.Clusters = []string{"a", "b"}
			obj.Spec.ActiveCluster = "b"
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
