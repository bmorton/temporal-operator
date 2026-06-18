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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var _ = Describe("TemporalSchedule Webhook", func() {
	var validator TemporalScheduleCustomValidator
	ctx := context.Background()

	valid := func() *temporalv1alpha1.TemporalSchedule {
		return &temporalv1alpha1.TemporalSchedule{
			ObjectMeta: metav1.ObjectMeta{Name: "sched-1"},
			Spec: temporalv1alpha1.TemporalScheduleSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "cluster"},
				Namespace:  "orders",
				Schedule:   temporalv1alpha1.ScheduleSpec{Calendars: []string{"0 9 * * *"}},
				Action: temporalv1alpha1.ScheduleActionSpec{
					StartWorkflow: temporalv1alpha1.StartWorkflowAction{
						WorkflowType: "W", TaskQueue: "tq",
					},
				},
			},
		}
	}

	BeforeEach(func() { validator = TemporalScheduleCustomValidator{} })

	It("admits a valid schedule", func() {
		Expect(validator.ValidateCreate(ctx, valid())).Error().NotTo(HaveOccurred())
	})

	It("rejects a missing workflowType", func() {
		s := valid()
		s.Spec.Action.StartWorkflow.WorkflowType = ""
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("rejects no time source when not paused", func() {
		s := valid()
		s.Spec.Schedule = temporalv1alpha1.ScheduleSpec{}
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("admits no time source when paused", func() {
		s := valid()
		s.Spec.Schedule = temporalv1alpha1.ScheduleSpec{}
		s.Spec.State = &temporalv1alpha1.ScheduleStateSpec{Paused: true}
		Expect(validator.ValidateCreate(ctx, s)).Error().NotTo(HaveOccurred())
	})

	It("rejects invalid overlap policy", func() {
		s := valid()
		s.Spec.Policies = &temporalv1alpha1.SchedulePoliciesSpec{OverlapPolicy: "Nope"}
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("rejects invalid JSON args", func() {
		s := valid()
		s.Spec.Action.StartWorkflow.Args = []runtime.RawExtension{{Raw: []byte(`{bad`)}}
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("rejects changing scheduleID", func() {
		oldS := valid()
		oldS.Spec.ScheduleID = "a"
		newS := valid()
		newS.Spec.ScheduleID = "b"
		Expect(validator.ValidateUpdate(ctx, oldS, newS)).Error().To(HaveOccurred())
	})

	It("rejects changing spec.namespace", func() {
		oldS := valid()
		newS := valid()
		newS.Spec.Namespace = "other-namespace"
		Expect(validator.ValidateUpdate(ctx, oldS, newS)).Error().To(HaveOccurred())
	})

	It("rejects backoffCoefficient less than one", func() {
		s := valid()
		s.Spec.Action.StartWorkflow.RetryPolicy = &temporalv1alpha1.RetryPolicySpec{BackoffCoefficient: "0.5"}
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("rejects a backoffCoefficient that does not parse", func() {
		s := valid()
		s.Spec.Action.StartWorkflow.RetryPolicy = &temporalv1alpha1.RetryPolicySpec{BackoffCoefficient: "notanumber"}
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("admits a valid backoffCoefficient", func() {
		s := valid()
		s.Spec.Action.StartWorkflow.RetryPolicy = &temporalv1alpha1.RetryPolicySpec{BackoffCoefficient: "2.0"}
		Expect(validator.ValidateCreate(ctx, s)).Error().NotTo(HaveOccurred())
	})

	It("rejects invalid workflowIDReusePolicy", func() {
		s := valid()
		s.Spec.Action.StartWorkflow.WorkflowIDReusePolicy = "Bogus"
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("admits equivalent default and explicit scheduleID on update", func() {
		oldS := valid()
		newS := valid()
		newS.Spec.ScheduleID = "sched-1"
		Expect(validator.ValidateUpdate(ctx, oldS, newS)).Error().NotTo(HaveOccurred())
	})
})
