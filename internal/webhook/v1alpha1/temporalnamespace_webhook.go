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

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// log is for logging in this package.
var temporalnamespacelog = logf.Log.WithName("temporalnamespace-resource")

// SetupTemporalNamespaceWebhookWithManager registers the webhook for TemporalNamespace in the manager.
func SetupTemporalNamespaceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalNamespace{}).
		WithValidator(&TemporalNamespaceCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalnamespace,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalnamespaces,verbs=create;update,versions=v1alpha1,name=vtemporalnamespace-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalNamespaceCustomValidator struct is responsible for validating the TemporalNamespace resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TemporalNamespaceCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ admission.Validator[*temporalv1alpha1.TemporalNamespace] = &TemporalNamespaceCustomValidator{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type TemporalNamespace.
func (v *TemporalNamespaceCustomValidator) ValidateCreate(_ context.Context, temporalnamespace *temporalv1alpha1.TemporalNamespace) (admission.Warnings, error) {
	temporalnamespacelog.Info("Validation for TemporalNamespace upon creation", "name", temporalnamespace.GetName())

	if temporalnamespace.Spec.ClusterRef.Name == "" {
		return nil, fmt.Errorf("spec.clusterRef.name must not be empty")
	}

	if err := validateReplication(temporalnamespace); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type TemporalNamespace.
func (v *TemporalNamespaceCustomValidator) ValidateUpdate(_ context.Context, oldNS, newNS *temporalv1alpha1.TemporalNamespace) (admission.Warnings, error) {
	temporalnamespacelog.Info("Validation for TemporalNamespace upon update", "name", newNS.GetName())

	if newNS.Spec.ClusterRef.Name == "" {
		return nil, fmt.Errorf("spec.clusterRef.name must not be empty")
	}

	if oldNS.Spec.IsGlobal != newNS.Spec.IsGlobal {
		return nil, fmt.Errorf("%s: isGlobal is immutable after creation", temporalv1alpha1.ReasonIsGlobalImmutable)
	}

	if err := validateReplication(newNS); err != nil {
		return nil, err
	}

	return nil, nil
}

// validateReplication enforces the replication invariants: clusters and
// activeCluster are only valid on a global namespace, and a configured
// activeCluster must be one of the listed clusters.
func validateReplication(ns *temporalv1alpha1.TemporalNamespace) error {
	if !ns.Spec.IsGlobal {
		if len(ns.Spec.Clusters) > 0 || ns.Spec.ActiveCluster != "" {
			return fmt.Errorf("spec.clusters and spec.activeCluster require spec.isGlobal=true")
		}
		return nil
	}
	if ns.Spec.ActiveCluster != "" && len(ns.Spec.Clusters) > 0 {
		found := false
		for _, c := range ns.Spec.Clusters {
			if c == ns.Spec.ActiveCluster {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: spec.activeCluster %q must be one of spec.clusters", temporalv1alpha1.ReasonActiveClusterInvalid, ns.Spec.ActiveCluster)
		}
	}
	return nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type TemporalNamespace.
func (v *TemporalNamespaceCustomValidator) ValidateDelete(_ context.Context, temporalnamespace *temporalv1alpha1.TemporalNamespace) (admission.Warnings, error) {
	temporalnamespacelog.Info("Validation for TemporalNamespace upon deletion", "name", temporalnamespace.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
