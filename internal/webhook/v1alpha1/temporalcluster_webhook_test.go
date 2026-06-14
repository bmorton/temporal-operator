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

func validCluster() *temporalv1alpha1.TemporalCluster {
	sql := func(db string) *temporalv1alpha1.SQLDatastoreSpec {
		return &temporalv1alpha1.SQLDatastoreSpec{
			PluginName:        "postgres12",
			Host:              "postgres.default.svc",
			Port:              5432,
			Database:          db,
			User:              "temporal",
			PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "store", Key: "password"},
		}
	}
	return &temporalv1alpha1.TemporalCluster{
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version:          "1.31.1",
			NumHistoryShards: 512,
			Persistence: temporalv1alpha1.PersistenceSpec{
				DefaultStore:    temporalv1alpha1.DatastoreSpec{SQL: sql("temporal")},
				VisibilityStore: temporalv1alpha1.DatastoreSpec{SQL: sql("temporal_visibility")},
			},
		},
	}
}

var _ = Describe("TemporalCluster Webhook", func() {
	var (
		obj       *temporalv1alpha1.TemporalCluster
		oldObj    *temporalv1alpha1.TemporalCluster
		validator TemporalClusterCustomValidator
		defaulter TemporalClusterCustomDefaulter
		ctx       context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		obj = validCluster()
		oldObj = validCluster()
		validator = TemporalClusterCustomValidator{}
		defaulter = TemporalClusterCustomDefaulter{}
	})

	Context("Defaulting webhook", func() {
		It("defaults image, replicas, metrics and the initial-shards annotation", func() {
			obj.Spec.Image = ""
			Expect(defaulter.Default(ctx, obj)).To(Succeed())

			Expect(obj.Spec.Image).To(Equal("temporalio/server:1.31.1"))
			Expect(obj.Spec.Services.Frontend.Replicas).To(HaveValue(Equal(int32(2))))
			Expect(obj.Spec.Services.History.Replicas).To(HaveValue(Equal(int32(3))))
			Expect(obj.Spec.Services.Matching.Replicas).To(HaveValue(Equal(int32(2))))
			Expect(obj.Spec.Services.Worker.Replicas).To(HaveValue(Equal(int32(1))))
			Expect(obj.Spec.Metrics).NotTo(BeNil())
			Expect(obj.Spec.Metrics.Enabled).To(BeTrue())
			Expect(obj.Spec.Metrics.Port).To(Equal(int32(9090)))
			Expect(obj.Annotations).To(HaveKeyWithValue(InitialShardsAnnotation, "512"))
		})

		It("defaults mtls refresh/renew intervals when mtls is set", func() {
			obj.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager"}
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.MTLS.RefreshInterval).NotTo(BeNil())
			Expect(obj.Spec.MTLS.RenewBefore).NotTo(BeNil())
		})
	})

	Context("Validation webhook on create", func() {
		It("admits a valid cluster", func() {
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an unsupported version", func() {
			obj.Spec.Version = "9.9.9"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects setting both sql and cassandra on the default store", func() {
			obj.Spec.Persistence.DefaultStore.Cassandra = &temporalv1alpha1.CassandraDatastoreSpec{
				Hosts: []string{"c"}, Port: 9042, Keyspace: "temporal",
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects cert-manager mtls without an issuerRef", func() {
			obj.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager"}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Validation webhook on update", func() {
		It("rejects changing numHistoryShards", func() {
			obj.Spec.NumHistoryShards = 1024
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects a non-adjacent version jump", func() {
			oldObj.Spec.Version = "1.27.0"
			obj.Spec.Version = "1.31.1"
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits a patch-level version bump within the same minor", func() {
			oldObj.Spec.Version = "1.31.0"
			obj.Spec.Version = "1.31.1"
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects changing the default store driver", func() {
			obj.Spec.Persistence.DefaultStore.SQL = nil
			obj.Spec.Persistence.DefaultStore.Cassandra = &temporalv1alpha1.CassandraDatastoreSpec{
				Hosts: []string{"c"}, Port: 9042, Keyspace: "temporal",
			}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Validation webhook on delete", func() {
		It("rejects deletion when preventDeletion is set", func() {
			obj.Spec.PreventDeletion = true
			_, err := validator.ValidateDelete(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits deletion by default", func() {
			_, err := validator.ValidateDelete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
