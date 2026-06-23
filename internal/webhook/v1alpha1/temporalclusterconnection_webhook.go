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

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// log is for logging in this package.
var temporalclusterconnectionlog = logf.Log.WithName("temporalclusterconnection-resource")

// SetupTemporalClusterConnectionWebhookWithManager registers the webhook for
// TemporalClusterConnection in the manager.
func SetupTemporalClusterConnectionWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalClusterConnection{}).
		WithValidator(&TemporalClusterConnectionCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalclusterconnection,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalclusterconnections,verbs=create;update,versions=v1alpha1,name=vtemporalclusterconnection-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalClusterConnectionCustomValidator validates TemporalClusterConnection resources.
type TemporalClusterConnectionCustomValidator struct{}

var _ admission.Validator[*temporalv1alpha1.TemporalClusterConnection] = &TemporalClusterConnectionCustomValidator{}

func validatePeers(c *temporalv1alpha1.TemporalClusterConnection) field.ErrorList {
	var errs field.ErrorList
	peersPath := field.NewPath("spec", "peers")
	seen := map[string]bool{}
	for i, p := range c.Spec.Peers {
		path := peersPath.Index(i)
		if p.Name == "" {
			errs = append(errs, field.Required(path.Child("name"), "peer name is required"))
		}
		if seen[p.Name] {
			errs = append(errs, field.Duplicate(path.Child("name"), p.Name))
		}
		seen[p.Name] = true

		hasRef := p.ClusterRef != nil && p.ClusterRef.Name != ""
		hasAddr := p.FrontendAddress != ""
		switch {
		case hasRef && hasAddr:
			errs = append(errs, field.Invalid(path, p.Name, "set exactly one of clusterRef or frontendAddress"))
		case !hasRef && !hasAddr:
			errs = append(errs, field.Invalid(path, p.Name, "one of clusterRef or frontendAddress is required"))
		}
		if p.TLSSecretRef != nil && !hasAddr {
			errs = append(errs, field.Invalid(path.Child("tlsSecretRef"), p.TLSSecretRef.Name, "tlsSecretRef is only valid with frontendAddress"))
		}
	}
	return errs
}

// ValidateCreate implements admission.Validator.
func (v *TemporalClusterConnectionCustomValidator) ValidateCreate(_ context.Context, c *temporalv1alpha1.TemporalClusterConnection) (admission.Warnings, error) {
	temporalclusterconnectionlog.Info("Validation for TemporalClusterConnection upon creation", "name", c.GetName())

	if len(c.Spec.Peers) < 2 {
		return nil, fmt.Errorf("spec.peers must contain at least two peers")
	}
	if errs := validatePeers(c); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

// ValidateUpdate implements admission.Validator.
func (v *TemporalClusterConnectionCustomValidator) ValidateUpdate(_ context.Context, _, newC *temporalv1alpha1.TemporalClusterConnection) (admission.Warnings, error) {
	temporalclusterconnectionlog.Info("Validation for TemporalClusterConnection upon update", "name", newC.GetName())

	if len(newC.Spec.Peers) < 2 {
		return nil, fmt.Errorf("spec.peers must contain at least two peers")
	}
	if errs := validatePeers(newC); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

// ValidateDelete implements admission.Validator.
func (v *TemporalClusterConnectionCustomValidator) ValidateDelete(_ context.Context, _ *temporalv1alpha1.TemporalClusterConnection) (admission.Warnings, error) {
	return nil, nil
}
