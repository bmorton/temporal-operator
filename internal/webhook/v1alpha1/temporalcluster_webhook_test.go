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
	"k8s.io/apimachinery/pkg/runtime"

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

		It("admits a cluster with clusterMetadata when enableGlobalNamespace is false", func() {
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				CurrentClusterName: "clusterA",
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects enableGlobalNamespace without currentClusterName", func() {
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				EnableGlobalNamespace: true,
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects enableGlobalNamespace without failoverVersionIncrement", func() {
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				EnableGlobalNamespace: true,
				CurrentClusterName:    "clusterA",
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects enableGlobalNamespace without initialFailoverVersion", func() {
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				EnableGlobalNamespace:    true,
				CurrentClusterName:       "clusterA",
				FailoverVersionIncrement: ptrInt32(100),
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits a valid multi-cluster configuration", func() {
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				EnableGlobalNamespace:    true,
				CurrentClusterName:       "clusterA",
				FailoverVersionIncrement: ptrInt32(100),
				InitialFailoverVersion:   ptrInt32(1),
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
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

		It("rejects changing failoverVersionIncrement", func() {
			oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				FailoverVersionIncrement: ptrInt32(100),
			}
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				FailoverVersionIncrement: ptrInt32(200),
			}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects changing initialFailoverVersion", func() {
			oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				InitialFailoverVersion: ptrInt32(1),
			}
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				InitialFailoverVersion: ptrInt32(2),
			}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects changing currentClusterName", func() {
			oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				CurrentClusterName: "clusterA",
			}
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				CurrentClusterName: "clusterB",
			}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits changing masterClusterName on update", func() {
			oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				MasterClusterName: "clusterA",
			}
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				MasterClusterName: "clusterB",
			}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("admits enabling enableGlobalNamespace on update", func() {
			oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{}
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				EnableGlobalNamespace:    true,
				CurrentClusterName:       "clusterA",
				FailoverVersionIncrement: ptrInt32(100),
				InitialFailoverVersion:   ptrInt32(1),
			}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects removing clusterMetadata when previously set", func() {
			oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				CurrentClusterName: "clusterA",
			}
			obj.Spec.ClusterMetadata = nil
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects clearing a previously-set failoverVersionIncrement", func() {
			oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				FailoverVersionIncrement: ptrInt32(100),
			}
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("rejects clearing a previously-set currentClusterName", func() {
			oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				CurrentClusterName: "clusterA",
			}
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits setting clusterMetadata when previously nil", func() {
			oldObj.Spec.ClusterMetadata = nil
			obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				EnableGlobalNamespace:    true,
				CurrentClusterName:       "clusterA",
				FailoverVersionIncrement: ptrInt32(100),
				InitialFailoverVersion:   ptrInt32(1),
			}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
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

	Context("Validation of ui.auth", func() {
		It("rejects ui.auth.enabled without clientID, clientSecretRef, or callbackURL", func() {
			obj.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth:    &temporalv1alpha1.UIAuthSpec{Enabled: true},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("clientID"))
		})

		It("rejects ui.auth when both entra.tenantID and providerURL are set", func() {
			obj.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth: &temporalv1alpha1.UIAuthSpec{
					Enabled:         true,
					Entra:           &temporalv1alpha1.EntraUIAuthSpec{TenantID: "t"},
					ProviderURL:     "https://example.com/oidc",
					ClientID:        "c",
					ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "s", Key: "k"},
					CallbackURL:     "https://x/cb",
				},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not both"))
		})

		It("rejects ui.auth when entra is non-nil but tenantID is empty and no providerURL", func() {
			obj.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth: &temporalv1alpha1.UIAuthSpec{
					Enabled:         true,
					Entra:           &temporalv1alpha1.EntraUIAuthSpec{},
					ClientID:        "c",
					ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "s", Key: "k"},
					CallbackURL:     "https://x/cb",
				},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("providerURL"))
		})

		It("admits a complete ui.auth OIDC configuration", func() {
			obj.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth: &temporalv1alpha1.UIAuthSpec{
					Enabled:         true,
					Entra:           &temporalv1alpha1.EntraUIAuthSpec{TenantID: "t"},
					ClientID:        "c",
					ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "s", Key: "k"},
					CallbackURL:     "https://x/cb",
				},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects ui.auth.extraEnv when a value is not a string", func() {
			obj.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth: &temporalv1alpha1.UIAuthSpec{
					Enabled:         true,
					Entra:           &temporalv1alpha1.EntraUIAuthSpec{TenantID: "t"},
					ClientID:        "c",
					ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "s", Key: "k"},
					CallbackURL:     "https://x/cb",
					ExtraEnv:        &runtime.RawExtension{Raw: []byte(`{"FOO":{"nested":true}}`)},
				},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("admits ui.auth.extraEnv when it is a valid string map", func() {
			obj.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth: &temporalv1alpha1.UIAuthSpec{
					Enabled:         true,
					Entra:           &temporalv1alpha1.EntraUIAuthSpec{TenantID: "t"},
					ClientID:        "c",
					ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "s", Key: "k"},
					CallbackURL:     "https://x/cb",
					ExtraEnv:        &runtime.RawExtension{Raw: []byte(`{"FOO":"bar"}`)},
				},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
