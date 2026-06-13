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
	corev1 "k8s.io/api/core/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var _ = Describe("TemporalNamespace Webhook", func() {
	ctx := context.Background()
	validator := TemporalNamespaceCustomValidator{}

	It("admits a namespace with a clusterRef", func() {
		ns := &temporalv1alpha1.TemporalNamespace{
			Spec: temporalv1alpha1.TemporalNamespaceSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "tc"},
			},
		}
		_, err := validator.ValidateCreate(ctx, ns)
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects a namespace without a clusterRef", func() {
		ns := &temporalv1alpha1.TemporalNamespace{}
		_, err := validator.ValidateCreate(ctx, ns)
		Expect(err).To(HaveOccurred())
		_, err = validator.ValidateUpdate(ctx, ns, ns)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("TemporalSearchAttribute Webhook", func() {
	ctx := context.Background()
	validator := TemporalSearchAttributeCustomValidator{}

	newSA := func(saType string) *temporalv1alpha1.TemporalSearchAttribute {
		return &temporalv1alpha1.TemporalSearchAttribute{
			Spec: temporalv1alpha1.TemporalSearchAttributeSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "tc"},
				Namespace:  "default",
				Name:       "CustomerId",
				Type:       saType,
			},
		}
	}

	It("admits a valid Keyword attribute", func() {
		_, err := validator.ValidateCreate(ctx, newSA("Keyword"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects an invalid type", func() {
		_, err := validator.ValidateCreate(ctx, newSA("Banana"))
		Expect(err).To(HaveOccurred())
	})

	It("rejects an empty clusterRef", func() {
		sa := newSA("Keyword")
		sa.Spec.ClusterRef.Name = ""
		_, err := validator.ValidateCreate(ctx, sa)
		Expect(err).To(HaveOccurred())
	})

	It("rejects changing the immutable type on update", func() {
		oldSA := newSA("Keyword")
		newAttr := newSA("Text")
		_, err := validator.ValidateUpdate(ctx, oldSA, newAttr)
		Expect(err).To(HaveOccurred())
	})

	It("admits an update that keeps the type", func() {
		oldSA := newSA("Keyword")
		newAttr := newSA("Keyword")
		_, err := validator.ValidateUpdate(ctx, oldSA, newAttr)
		Expect(err).NotTo(HaveOccurred())
	})
})
