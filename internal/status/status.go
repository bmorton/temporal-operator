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

// Package status provides uniform condition and status-update handling for all
// Temporal CRDs, replacing the per-controller setReady/statusUpdate pairs.
package status

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Object is any Temporal CRD that reports conditions. All eight API types
// satisfy it via api/v1alpha1/accessors.go.
type Object interface {
	client.Object
	GetConditions() *[]metav1.Condition
	SetObservedGeneration(int64)
}

// Set records a condition, stamping the object's current generation on both the
// status and the condition. It only mutates the in-memory object; call Update to
// persist.
func Set(obj Object, condType string, s metav1.ConditionStatus, reason, message string) {
	gen := obj.GetGeneration()
	obj.SetObservedGeneration(gen)
	meta.SetStatusCondition(obj.GetConditions(), metav1.Condition{
		Type:               condType,
		Status:             s,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: gen,
	})
}

// IsTrue reports whether the named condition is currently True.
func IsTrue(obj Object, condType string) bool {
	return meta.IsStatusConditionTrue(*obj.GetConditions(), condType)
}

// Update persists the status subresource, retrying on conflict.
//
// On conflict we refresh only the resourceVersion from the API server and retry
// with our own status intact. We deliberately do not merge the server's status:
// each resource has exactly one controller writing its status, so a conflict
// means a stale read of our own earlier write, not a competing author.
//
// A NotFound error is returned as nil — the object was deleted while we
// reconciled, which is not a failure.
func Update(ctx context.Context, c client.Client, obj Object) error {
	key := client.ObjectKeyFromObject(obj)

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		updateErr := c.Status().Update(ctx, obj)
		if updateErr == nil || !apierrors.IsConflict(updateErr) {
			return updateErr
		}

		fresh, ok := obj.DeepCopyObject().(Object)
		if !ok {
			return updateErr
		}
		if getErr := c.Get(ctx, key, fresh); getErr != nil {
			return getErr
		}
		obj.SetResourceVersion(fresh.GetResourceVersion())
		return updateErr
	})

	return client.IgnoreNotFound(err)
}
