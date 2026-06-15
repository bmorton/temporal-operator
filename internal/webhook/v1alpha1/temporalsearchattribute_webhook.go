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
var temporalsearchattributelog = logf.Log.WithName("temporalsearchattribute-resource")

// SetupTemporalSearchAttributeWebhookWithManager registers the webhook for TemporalSearchAttribute in the manager.
func SetupTemporalSearchAttributeWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalSearchAttribute{}).
		WithCustomValidator(&TemporalSearchAttributeCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalsearchattribute,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalsearchattributes,verbs=create;update,versions=v1alpha1,name=vtemporalsearchattribute-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalSearchAttributeCustomValidator struct is responsible for validating the TemporalSearchAttribute resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TemporalSearchAttributeCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &TemporalSearchAttributeCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type TemporalSearchAttribute.
func (v *TemporalSearchAttributeCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	temporalsearchattribute, ok := obj.(*temporalv1alpha1.TemporalSearchAttribute)
	if !ok {
		return nil, fmt.Errorf("expected a TemporalSearchAttribute object but got %T", obj)
	}
	temporalsearchattributelog.Info("Validation for TemporalSearchAttribute upon creation", "name", temporalsearchattribute.GetName())

	if temporalsearchattribute.Spec.ClusterRef.Name == "" {
		return nil, fmt.Errorf("spec.clusterRef.name must not be empty")
	}
	if !isValidSearchAttributeType(temporalsearchattribute.Spec.Type) {
		return nil, fmt.Errorf("spec.type %q is not a valid search attribute type", temporalsearchattribute.Spec.Type)
	}
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type TemporalSearchAttribute.
func (v *TemporalSearchAttributeCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	newSA, ok := newObj.(*temporalv1alpha1.TemporalSearchAttribute)
	if !ok {
		return nil, fmt.Errorf("expected a TemporalSearchAttribute object for the newObj but got %T", newObj)
	}
	oldSA, ok := oldObj.(*temporalv1alpha1.TemporalSearchAttribute)
	if !ok {
		return nil, fmt.Errorf("expected a TemporalSearchAttribute object for the oldObj but got %T", oldObj)
	}
	temporalsearchattributelog.Info("Validation for TemporalSearchAttribute upon update", "name", newSA.GetName())

	if newSA.Spec.ClusterRef.Name == "" {
		return nil, fmt.Errorf("spec.clusterRef.name must not be empty")
	}
	if newSA.Spec.Type != oldSA.Spec.Type {
		return nil, fmt.Errorf("spec.type is immutable (was %q)", oldSA.Spec.Type)
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type TemporalSearchAttribute.
func (v *TemporalSearchAttributeCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	temporalsearchattribute, ok := obj.(*temporalv1alpha1.TemporalSearchAttribute)
	if !ok {
		return nil, fmt.Errorf("expected a TemporalSearchAttribute object but got %T", obj)
	}
	temporalsearchattributelog.Info("Validation for TemporalSearchAttribute upon deletion", "name", temporalsearchattribute.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

// validSearchAttributeTypes is the set of accepted search attribute types.
var validSearchAttributeTypes = map[string]struct{}{
	"Keyword":     {},
	"Text":        {},
	"Int":         {},
	"Double":      {},
	"Bool":        {},
	"Datetime":    {},
	"KeywordList": {},
}

func isValidSearchAttributeType(t string) bool {
	_, ok := validSearchAttributeTypes[t]
	return ok
}
