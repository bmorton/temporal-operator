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
	"fmt"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// InitialShardsAnnotation records the numHistoryShards value chosen at creation
// time. It is the source of truth for the shard-count immutability check.
const InitialShardsAnnotation = "temporal.bmor10.com/initial-shards"

var temporalclusterlog = logf.Log.WithName("temporalcluster-resource")

// SetupTemporalClusterWebhookWithManager registers the webhook for TemporalCluster in the manager.
func SetupTemporalClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalCluster{}).
		WithValidator(&TemporalClusterCustomValidator{}).
		WithDefaulter(&TemporalClusterCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-temporal-bmor10-com-v1alpha1-temporalcluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalclusters,verbs=create;update,versions=v1alpha1,name=mtemporalcluster-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalClusterCustomDefaulter sets default values on TemporalCluster resources.
type TemporalClusterCustomDefaulter struct{}

var _ admission.Defaulter[*temporalv1alpha1.TemporalCluster] = &TemporalClusterCustomDefaulter{}

func ptrInt32(v int32) *int32 { return &v }

func defaultReplicas(svc **temporalv1alpha1.ServiceSpec, replicas int32) {
	if *svc == nil {
		*svc = &temporalv1alpha1.ServiceSpec{}
	}
	if (*svc).Replicas == nil {
		(*svc).Replicas = ptrInt32(replicas)
	}
}

// Default implements admission.Defaulter.
func (d *TemporalClusterCustomDefaulter) Default(_ context.Context, cluster *temporalv1alpha1.TemporalCluster) error {
	temporalclusterlog.Info("Defaulting for TemporalCluster", "name", cluster.GetName())

	if cluster.Spec.Image == "" {
		cluster.Spec.Image = "temporalio/server:" + cluster.Spec.Version
	}

	defaultReplicas(&cluster.Spec.Services.Frontend, 2)
	defaultReplicas(&cluster.Spec.Services.History, 3)
	defaultReplicas(&cluster.Spec.Services.Matching, 2)
	defaultReplicas(&cluster.Spec.Services.Worker, 1)

	if cluster.Spec.Metrics == nil {
		cluster.Spec.Metrics = &temporalv1alpha1.MetricsSpec{Enabled: true, Port: 9090}
	} else if cluster.Spec.Metrics.Port == 0 {
		cluster.Spec.Metrics.Port = 9090
	}

	if cluster.Spec.UI != nil && cluster.Spec.UI.Version == "" {
		if uiVersion := temporal.DefaultUIVersion(cluster.Spec.Version); uiVersion != "" {
			cluster.Spec.UI.Version = uiVersion
		}
	}

	if cluster.Spec.MTLS != nil {
		if cluster.Spec.MTLS.RefreshInterval == nil {
			cluster.Spec.MTLS.RefreshInterval = &metav1.Duration{Duration: 720 * time.Hour}
		}
		if cluster.Spec.MTLS.RenewBefore == nil {
			cluster.Spec.MTLS.RenewBefore = &metav1.Duration{Duration: 240 * time.Hour}
		}
	}

	// Stamp the initial shard count exactly once (on creation, when absent).
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	if _, ok := cluster.Annotations[InitialShardsAnnotation]; !ok {
		shards := cluster.Spec.NumHistoryShards
		if shards == 0 {
			shards = 512
		}
		cluster.Annotations[InitialShardsAnnotation] = strconv.Itoa(int(shards))
	}

	return nil
}

// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalclusters,verbs=create;update;delete,versions=v1alpha1,name=vtemporalcluster-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalClusterCustomValidator validates TemporalCluster resources.
type TemporalClusterCustomValidator struct{}

var _ admission.Validator[*temporalv1alpha1.TemporalCluster] = &TemporalClusterCustomValidator{}

func (v *TemporalClusterCustomValidator) validateSpec(cluster *temporalv1alpha1.TemporalCluster) field.ErrorList {
	var errs field.ErrorList
	specPath := field.NewPath("spec")

	errs = append(errs, validateVersion(cluster, specPath)...)
	errs = append(errs, validatePersistence(cluster, specPath)...)
	errs = append(errs, validateMTLS(cluster, specPath)...)
	errs = append(errs, validateDynamicConfig(cluster, specPath)...)
	errs = append(errs, validateClusterMetadata(cluster, specPath)...)

	return errs
}

func validateVersion(cluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	if !temporal.IsSupported(cluster.Spec.Version) {
		return field.ErrorList{field.Invalid(specPath.Child("version"), cluster.Spec.Version,
			fmt.Sprintf("%s: version is not in the supported matrix %v", temporalv1alpha1.ReasonVersionUnsupported, temporal.SupportedVersions()))}
	}
	return nil
}

func validatePersistence(cluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList
	pPath := specPath.Child("persistence")

	def := cluster.Spec.Persistence.DefaultStore
	if (def.SQL == nil) == (def.Cassandra == nil) {
		errs = append(errs, field.Invalid(pPath.Child("defaultStore"), nil,
			"exactly one of sql or cassandra must be set"))
	}

	vis := cluster.Spec.Persistence.VisibilityStore
	visBackends := 0
	if vis.SQL != nil {
		visBackends++
	}
	if vis.Cassandra != nil {
		visBackends++
	}
	if vis.Elasticsearch != nil {
		visBackends++
	}
	if visBackends != 1 {
		errs = append(errs, field.Invalid(pPath.Child("visibilityStore"), nil,
			"exactly one of sql, cassandra, or elasticsearch must be set"))
	}
	if vis.Cassandra != nil {
		if info, ok := temporal.Get(cluster.Spec.Version); ok && !info.CassandraVisibilitySupported {
			errs = append(errs, field.Invalid(pPath.Child("visibilityStore", "cassandra"), nil,
				fmt.Sprintf("Cassandra visibility is not supported on Temporal %s", cluster.Spec.Version)))
		}
	}

	return errs
}

func validateMTLS(cluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	if cluster.Spec.MTLS != nil && cluster.Spec.MTLS.Provider == "cert-manager" && cluster.Spec.MTLS.IssuerRef == nil {
		return field.ErrorList{field.Required(specPath.Child("mtls", "issuerRef"),
			"issuerRef is required when mtls.provider is cert-manager")}
	}
	return nil
}

func validateDynamicConfig(cluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	if cluster.Spec.DynamicConfig != nil {
		if _, _, err := temporal.RenderDynamicConfig(cluster.Spec.DynamicConfig, cluster.Spec.Version); err != nil {
			return field.ErrorList{field.Invalid(specPath.Child("dynamicConfig"), nil, err.Error())}
		}
	}
	return nil
}

func validateClusterMetadata(cluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	if cm := cluster.Spec.ClusterMetadata; cm != nil && cm.EnableGlobalNamespace {
		cmPath := specPath.Child("clusterMetadata")
		var errs field.ErrorList
		if cm.CurrentClusterName == "" {
			errs = append(errs, field.Required(cmPath.Child("currentClusterName"),
				"currentClusterName is required when enableGlobalNamespace is true"))
		}
		if cm.FailoverVersionIncrement == nil || *cm.FailoverVersionIncrement < 1 {
			errs = append(errs, field.Required(cmPath.Child("failoverVersionIncrement"),
				"failoverVersionIncrement is required and must be >= 1 when enableGlobalNamespace is true"))
		}
		if cm.InitialFailoverVersion == nil || *cm.InitialFailoverVersion < 1 {
			errs = append(errs, field.Required(cmPath.Child("initialFailoverVersion"),
				"initialFailoverVersion is required and must be >= 1 when enableGlobalNamespace is true"))
		}
		return errs
	}
	return nil
}

// ValidateCreate implements admission.Validator.
func (v *TemporalClusterCustomValidator) ValidateCreate(_ context.Context, cluster *temporalv1alpha1.TemporalCluster) (admission.Warnings, error) {
	temporalclusterlog.Info("Validation for TemporalCluster upon creation", "name", cluster.GetName())

	if errs := v.validateSpec(cluster); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

// ValidateUpdate implements admission.Validator.
func (v *TemporalClusterCustomValidator) ValidateUpdate(_ context.Context, oldCluster, newCluster *temporalv1alpha1.TemporalCluster) (admission.Warnings, error) {
	temporalclusterlog.Info("Validation for TemporalCluster upon update", "name", newCluster.GetName())

	errs := v.validateSpec(newCluster)
	specPath := field.NewPath("spec")

	errs = append(errs, validateShardCountImmutable(oldCluster, newCluster, specPath)...)
	errs = append(errs, validateUpgradePath(oldCluster, newCluster, specPath)...)
	errs = append(errs, validateStoreDriverImmutable(oldCluster, newCluster, specPath)...)
	errs = append(errs, validateClusterMetadataImmutable(oldCluster, newCluster, specPath)...)

	if len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

func validateShardCountImmutable(oldCluster, newCluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	initial := oldCluster.Spec.NumHistoryShards
	if v, ok := oldCluster.Annotations[InitialShardsAnnotation]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			initial = int32(parsed)
		}
	}
	if newCluster.Spec.NumHistoryShards != initial {
		return field.ErrorList{field.Invalid(specPath.Child("numHistoryShards"), newCluster.Spec.NumHistoryShards,
			fmt.Sprintf("%s: numHistoryShards is immutable (was %d)", temporalv1alpha1.ReasonShardCountImmutable, initial))}
	}
	return nil
}

func validateUpgradePath(oldCluster, newCluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	if newCluster.Spec.Version != oldCluster.Spec.Version {
		allowed, err := temporal.CanUpgrade(oldCluster.Spec.Version, newCluster.Spec.Version)
		if err != nil || !allowed {
			return field.ErrorList{field.Invalid(specPath.Child("version"), newCluster.Spec.Version,
				fmt.Sprintf("%s: cannot upgrade from %s to %s", temporalv1alpha1.ReasonUpgradePathInvalid, oldCluster.Spec.Version, newCluster.Spec.Version))}
		}
	}
	return nil
}

func validateStoreDriverImmutable(oldCluster, newCluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	oldDriverSQL := oldCluster.Spec.Persistence.DefaultStore.SQL != nil
	newDriverSQL := newCluster.Spec.Persistence.DefaultStore.SQL != nil
	if oldDriverSQL != newDriverSQL {
		return field.ErrorList{field.Invalid(specPath.Child("persistence", "defaultStore"), nil,
			"the default store driver cannot be changed")}
	}
	return nil
}

func validateClusterMetadataImmutable(oldCluster, newCluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	oldCM := oldCluster.Spec.ClusterMetadata
	newCM := newCluster.Spec.ClusterMetadata
	if oldCM == nil {
		return nil
	}
	cmPath := specPath.Child("clusterMetadata")
	var errs field.ErrorList
	if newCM == nil {
		errs = append(errs, field.Invalid(cmPath, nil,
			fmt.Sprintf("%s: clusterMetadata cannot be removed once set", temporalv1alpha1.ReasonClusterMetadataImmutable)))
		return errs
	}
	if oldCM.FailoverVersionIncrement != nil {
		if newCM.FailoverVersionIncrement == nil || *oldCM.FailoverVersionIncrement != *newCM.FailoverVersionIncrement {
			errs = append(errs, field.Invalid(cmPath.Child("failoverVersionIncrement"), newCM.FailoverVersionIncrement,
				fmt.Sprintf("%s: failoverVersionIncrement is immutable", temporalv1alpha1.ReasonClusterMetadataImmutable)))
		}
	}
	if oldCM.InitialFailoverVersion != nil {
		if newCM.InitialFailoverVersion == nil || *oldCM.InitialFailoverVersion != *newCM.InitialFailoverVersion {
			errs = append(errs, field.Invalid(cmPath.Child("initialFailoverVersion"), newCM.InitialFailoverVersion,
				fmt.Sprintf("%s: initialFailoverVersion is immutable", temporalv1alpha1.ReasonClusterMetadataImmutable)))
		}
	}
	if oldCM.CurrentClusterName != "" {
		if newCM.CurrentClusterName == "" || oldCM.CurrentClusterName != newCM.CurrentClusterName {
			errs = append(errs, field.Invalid(cmPath.Child("currentClusterName"), newCM.CurrentClusterName,
				fmt.Sprintf("%s: currentClusterName is immutable", temporalv1alpha1.ReasonClusterMetadataImmutable)))
		}
	}
	return errs
}

// ValidateDelete implements admission.Validator.
func (v *TemporalClusterCustomValidator) ValidateDelete(_ context.Context, cluster *temporalv1alpha1.TemporalCluster) (admission.Warnings, error) {
	temporalclusterlog.Info("Validation for TemporalCluster upon deletion", "name", cluster.GetName())

	if cluster.Spec.PreventDeletion {
		return nil, fmt.Errorf("%s: deletion is blocked by spec.preventDeletion", temporalv1alpha1.ReasonDeletionPrevented)
	}
	return nil, nil
}
