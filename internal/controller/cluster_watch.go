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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// clusterUnavailableRequeue is the fixed, short delay used when a Temporal
// frontend is reachable but not yet accepting RPCs. Requeuing on a fixed
// interval (rather than returning an error) keeps the dependent object off
// controller-runtime's exponential backoff queue, so it registers promptly once
// the frontend finishes starting.
const clusterUnavailableRequeue = 5 * time.Second

// isTransientClusterErr reports whether err is a transient connectivity error
// from a Temporal frontend that is up but not yet serving RPCs. Such errors
// should be retried on a short fixed interval rather than triggering
// exponential backoff. Terminal errors (invalid arguments, permission denied,
// bad TLS material, etc.) return false so they surface as real reconcile
// errors. status.Code unwraps fmt-wrapped errors via errors.As.
func isTransientClusterErr(err error) bool {
	if err == nil {
		return false
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return true
	default:
		return false
	}
}

// refTargets reports whether ref points at the target named name of the given
// kind. An empty ref.Kind defaults to TemporalCluster (matching resolveTarget).
func refTargets(ref temporalv1alpha1.ClusterReference, kind, name string) bool {
	refKind := ref.Kind
	if refKind == "" {
		refKind = temporalv1alpha1.ClusterKindTemporalCluster
	}
	return refKind == kind && ref.Name == name
}

// targetReadyStatus returns the Ready condition status of a watched Temporal
// target (TemporalCluster or TemporalDevServer), or ConditionUnknown if the
// object is neither type or has no Ready condition.
func targetReadyStatus(obj client.Object) metav1.ConditionStatus {
	var conds []metav1.Condition
	switch o := obj.(type) {
	case *temporalv1alpha1.TemporalCluster:
		conds = o.Status.Conditions
	case *temporalv1alpha1.TemporalDevServer:
		conds = o.Status.Conditions
	default:
		return metav1.ConditionUnknown
	}
	if c := meta.FindStatusCondition(conds, temporalv1alpha1.ConditionReady); c != nil {
		return c.Status
	}
	return metav1.ConditionUnknown
}

// clusterReadinessChanged limits watch-driven enqueues of dependent CRs to
// meaningful target changes: a create (so an already-Ready target still triggers
// dependents created afterward), a generation change, or a transition of the
// Ready condition. Routine status writes that do not move Ready are ignored to
// avoid re-reconciling every dependent on each cluster status update.
var clusterReadinessChanged predicate.Predicate = predicate.Funcs{
	CreateFunc: func(event.CreateEvent) bool { return true },
	DeleteFunc: func(event.DeleteEvent) bool { return false },
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return false
		}
		if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
			return true
		}
		return targetReadyStatus(e.ObjectOld) != targetReadyStatus(e.ObjectNew)
	},
	GenericFunc: func(event.GenericEvent) bool { return false },
}
