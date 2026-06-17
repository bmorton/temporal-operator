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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// serverSideApply performs a server-side apply of a typed object using the
// controller-runtime v0.23 apply-configuration API. Typed objects built in this
// package do not carry TypeMeta, so the object's GroupVersionKind is resolved
// from the scheme and set on the unstructured representation before applying.
func serverSideApply(ctx context.Context, c client.Client, scheme *runtime.Scheme, obj client.Object, owner client.FieldOwner) error {
	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		return err
	}
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}
	u := &unstructured.Unstructured{Object: raw}
	u.GetObjectKind().SetGroupVersionKind(gvk)
	return c.Apply(ctx, client.ApplyConfigurationFromUnstructured(u), owner, client.ForceOwnership)
}
