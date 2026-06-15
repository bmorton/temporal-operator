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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// log is for logging in this package.
var temporalnamespacelog = logf.Log.WithName("temporalnamespace-resource")

// SetupTemporalNamespaceWebhookWithManager registers the webhook for TemporalNamespace in the manager.
func SetupTemporalNamespaceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalNamespace{}).
		WithCustomValidator(&TemporalNamespaceCustomValidator{}).
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

var _ webhook.CustomValidator = &TemporalNamespaceCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type TemporalNamespace.
func (v *TemporalNamespaceCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	temporalnamespace, ok := obj.(*temporalv1alpha1.TemporalNamespace)
	if !ok {
		return nil, fmt.Errorf("expected a TemporalNamespace object but got %T", obj)
	}
	temporalnamespacelog.Info("Validation for TemporalNamespace upon creation", "name", temporalnamespace.GetName())

	if temporalnamespace.Spec.ClusterRef.Name == "" {
		return nil, fmt.Errorf("spec.clusterRef.name must not be empty")
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type TemporalNamespace.
func (v *TemporalNamespaceCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	temporalnamespace, ok := newObj.(*temporalv1alpha1.TemporalNamespace)
	if !ok {
		return nil, fmt.Errorf("expected a TemporalNamespace object for the newObj but got %T", newObj)
	}
	temporalnamespacelog.Info("Validation for TemporalNamespace upon update", "name", temporalnamespace.GetName())

	if temporalnamespace.Spec.ClusterRef.Name == "" {
		return nil, fmt.Errorf("spec.clusterRef.name must not be empty")
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type TemporalNamespace.
func (v *TemporalNamespaceCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	temporalnamespace, ok := obj.(*temporalv1alpha1.TemporalNamespace)
	if !ok {
		return nil, fmt.Errorf("expected a TemporalNamespace object but got %T", obj)
	}
	temporalnamespacelog.Info("Validation for TemporalNamespace upon deletion", "name", temporalnamespace.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
