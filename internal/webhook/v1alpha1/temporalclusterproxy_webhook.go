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

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// defaultProxyImage is the pinned s2s-proxy image used when Spec.Image is unset.
const defaultProxyImage = "temporalio/s2s-proxy:v0.2.1"

const (
	tlsProviderCertManager = "cert-manager"
	tlsProviderSecret      = "secret"
)

var temporalclusterproxylog = logf.Log.WithName("temporalclusterproxy-resource")

// SetupTemporalClusterProxyWebhookWithManager registers the webhook for
// TemporalClusterProxy in the manager.
func SetupTemporalClusterProxyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalClusterProxy{}).
		WithValidator(&TemporalClusterProxyCustomValidator{}).
		WithDefaulter(&TemporalClusterProxyCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalclusterproxy,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalclusterproxies,verbs=create;update,versions=v1alpha1,name=vtemporalclusterproxy-v1alpha1.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/mutate-temporal-bmor10-com-v1alpha1-temporalclusterproxy,mutating=true,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalclusterproxies,verbs=create;update,versions=v1alpha1,name=mtemporalclusterproxy-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalClusterProxyCustomDefaulter defaults optional fields on TemporalClusterProxy.
type TemporalClusterProxyCustomDefaulter struct{}

var _ admission.Defaulter[*temporalv1alpha1.TemporalClusterProxy] = &TemporalClusterProxyCustomDefaulter{}

func (d *TemporalClusterProxyCustomDefaulter) Default(_ context.Context, cr *temporalv1alpha1.TemporalClusterProxy) error {
	if cr.Spec.Mux.TLS.Provider == "" {
		cr.Spec.Mux.TLS.Provider = tlsProviderCertManager
	}
	if cr.Spec.Peer.EnableConnection == nil {
		enable := true
		cr.Spec.Peer.EnableConnection = &enable
	}
	if cr.Spec.Image == "" {
		cr.Spec.Image = defaultProxyImage
	}
	return nil
}

// TemporalClusterProxyCustomValidator validates TemporalClusterProxy resources.
type TemporalClusterProxyCustomValidator struct{}

var _ admission.Validator[*temporalv1alpha1.TemporalClusterProxy] = &TemporalClusterProxyCustomValidator{}

func validateMuxRole(cr *temporalv1alpha1.TemporalClusterProxy) field.ErrorList {
	var errs field.ErrorList
	muxPath := field.NewPath("spec", "mux")
	switch cr.Spec.Mux.Role {
	case temporalv1alpha1.ProxyRoleServer:
		if cr.Spec.Mux.Server == nil {
			errs = append(errs, field.Required(muxPath.Child("server"), "server is required for role=server"))
		}
		if cr.Spec.Mux.Client != nil {
			errs = append(errs, field.Invalid(muxPath.Child("client"), "client", "client must be unset for role=server"))
		}
	case temporalv1alpha1.ProxyRoleClient:
		if cr.Spec.Mux.Client == nil || cr.Spec.Mux.Client.ServerAddress == "" {
			errs = append(errs, field.Required(muxPath.Child("client", "serverAddress"), "client.serverAddress is required for role=client"))
		}
		if cr.Spec.Mux.Server != nil {
			errs = append(errs, field.Invalid(muxPath.Child("server"), "server", "server must be unset for role=client"))
		}
	default:
		errs = append(errs, field.NotSupported(muxPath.Child("role"), cr.Spec.Mux.Role,
			[]string{temporalv1alpha1.ProxyRoleServer, temporalv1alpha1.ProxyRoleClient}))
	}
	return errs
}

func validateMuxTLS(cr *temporalv1alpha1.TemporalClusterProxy) field.ErrorList {
	var errs field.ErrorList
	tlsPath := field.NewPath("spec", "mux", "tls")
	switch cr.Spec.Mux.TLS.Provider {
	case "", tlsProviderCertManager:
		if cr.Spec.Mux.TLS.IssuerRef == nil || cr.Spec.Mux.TLS.IssuerRef.Name == "" {
			errs = append(errs, field.Required(tlsPath.Child("issuerRef"), "issuerRef is required for provider=cert-manager"))
		}
	case tlsProviderSecret:
		if cr.Spec.Mux.TLS.SecretRef == nil || cr.Spec.Mux.TLS.SecretRef.Name == "" {
			errs = append(errs, field.Required(tlsPath.Child("secretRef"), "secretRef is required for provider=secret"))
		}
	default:
		errs = append(errs, field.NotSupported(tlsPath.Child("provider"), cr.Spec.Mux.TLS.Provider,
			[]string{tlsProviderCertManager, tlsProviderSecret}))
	}
	return errs
}

func validateClusterProxy(cr *temporalv1alpha1.TemporalClusterProxy) field.ErrorList {
	var errs field.ErrorList
	specPath := field.NewPath("spec")

	if cr.Spec.LocalClusterRef.Name == "" {
		errs = append(errs, field.Required(specPath.Child("localClusterRef", "name"), "local cluster reference is required"))
	}
	if cr.Spec.Peer.Name == "" {
		errs = append(errs, field.Required(specPath.Child("peer", "name"), "peer name is required"))
	}
	if cr.Spec.Peer.Name != "" && cr.Spec.Peer.Name == cr.Spec.LocalClusterName {
		errs = append(errs, field.Invalid(specPath.Child("peer", "name"), cr.Spec.Peer.Name, "peer name must differ from localClusterName"))
	}

	errs = append(errs, validateMuxRole(cr)...)
	errs = append(errs, validateMuxTLS(cr)...)
	return errs
}

// ValidateCreate implements admission.Validator.
func (v *TemporalClusterProxyCustomValidator) ValidateCreate(_ context.Context, cr *temporalv1alpha1.TemporalClusterProxy) (admission.Warnings, error) {
	temporalclusterproxylog.Info("validate create", "name", cr.GetName())
	if errs := validateClusterProxy(cr); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

// ValidateUpdate implements admission.Validator.
func (v *TemporalClusterProxyCustomValidator) ValidateUpdate(_ context.Context, oldCR, newCR *temporalv1alpha1.TemporalClusterProxy) (admission.Warnings, error) {
	temporalclusterproxylog.Info("validate update", "name", newCR.GetName())
	var errs field.ErrorList
	if oldCR.Spec.Mux.Role != newCR.Spec.Mux.Role {
		errs = append(errs, field.Forbidden(field.NewPath("spec", "mux", "role"), "mux.role is immutable"))
	}
	if oldCR.Spec.LocalClusterRef.Name != newCR.Spec.LocalClusterRef.Name {
		errs = append(errs, field.Forbidden(field.NewPath("spec", "localClusterRef"), "localClusterRef is immutable"))
	}
	errs = append(errs, validateClusterProxy(newCR)...)
	if len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

// ValidateDelete implements admission.Validator.
func (v *TemporalClusterProxyCustomValidator) ValidateDelete(_ context.Context, _ *temporalv1alpha1.TemporalClusterProxy) (admission.Warnings, error) {
	return nil, nil
}
