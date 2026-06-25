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
	"crypto/tls"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enumspb "go.temporal.io/api/enums/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// fakeWorkflowRunClient is an in-memory WorkflowRunClient. status drives Describe.
type fakeWorkflowRunClient struct {
	started              []string
	canceled, terminated []string
	status               enumspb.WorkflowExecutionStatus
	failure              *temporal.WorkflowFailure
}

func (f *fakeWorkflowRunClient) Start(_ context.Context, _, _ string, p temporal.StartWorkflowParams) (string, error) {
	f.started = append(f.started, p.WorkflowID)
	if f.status == enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED {
		f.status = enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING
	}
	return "run-" + p.WorkflowID, nil
}

func (f *fakeWorkflowRunClient) Describe(_ context.Context, _, _, _ string) (*temporal.WorkflowExecutionInfo, error) {
	return &temporal.WorkflowExecutionInfo{Status: f.status, RunID: "run", Failure: f.failure}, nil
}

func (f *fakeWorkflowRunClient) Cancel(_ context.Context, _, wfID, _, _ string) error {
	f.canceled = append(f.canceled, wfID)
	return nil
}

func (f *fakeWorkflowRunClient) Terminate(_ context.Context, _, wfID, _, _ string) error {
	f.terminated = append(f.terminated, wfID)
	return nil
}

func (f *fakeWorkflowRunClient) Close() error { return nil }

var _ = Describe("TemporalWorkflowRun reconciler", func() {
	const testNamespace = "default"
	ctx := context.Background()
	var counter int
	var fake *fakeWorkflowRunClient

	var factory temporal.WorkflowRunClientFactory = func(_ context.Context, _ string, _ *tls.Config) (temporal.WorkflowRunClient, error) {
		return fake, nil
	}
	reconciler := func() *TemporalWorkflowRunReconciler {
		return &TemporalWorkflowRunReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
	}

	newReadyCluster := func(name string, policy *temporalv1alpha1.WorkflowRunPolicy) *temporalv1alpha1.TemporalCluster {
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       validClusterSpec("1.31.1"),
		}
		c.Spec.WorkflowRunPolicy = policy
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionReady, Status: metav1.ConditionTrue, Reason: "Ready", Message: "ready",
		})
		Expect(k8sClient.Status().Update(ctx, c)).To(Succeed())
		return c
	}

	newRun := func(name, cluster string) *temporalv1alpha1.TemporalWorkflowRun {
		return &temporalv1alpha1.TemporalWorkflowRun{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: temporalv1alpha1.TemporalWorkflowRunSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster},
				Namespace:  "orders",
				Workflow:   temporalv1alpha1.StartWorkflowAction{WorkflowType: "Greet", TaskQueue: "tq"},
			},
		}
	}

	BeforeEach(func() {
		counter++
		fake = &fakeWorkflowRunClient{}
	})

	It("starts the workflow and records Running status", func() {
		c := newReadyCluster(fmt.Sprintf("cluster-%d", counter), &temporalv1alpha1.WorkflowRunPolicy{Enabled: true})
		run := newRun(fmt.Sprintf("run-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, run)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, run) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.started).To(ContainElement(run.Name))
		var got temporalv1alpha1.TemporalWorkflowRun
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal("Running"))
		Expect(got.Status.RunID).NotTo(BeEmpty())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
	})

	It("denies the run when policy is disabled and starts nothing", func() {
		c := newReadyCluster(fmt.Sprintf("cluster-%d", counter), &temporalv1alpha1.WorkflowRunPolicy{Enabled: false})
		run := newRun(fmt.Sprintf("run-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, run)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, run) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.started).To(BeEmpty())
		var got temporalv1alpha1.TemporalWorkflowRun
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.RunID).To(BeEmpty())
		cond := meta.FindStatusCondition(got.Status.Conditions, temporalv1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal(temporalv1alpha1.ReasonWorkflowRunNotPermitted))
	})

	It("captures failure and sets Finished when the workflow fails", func() {
		c := newReadyCluster(fmt.Sprintf("cluster-%d", counter), &temporalv1alpha1.WorkflowRunPolicy{Enabled: true})
		fake.status = enumspb.WORKFLOW_EXECUTION_STATUS_FAILED
		fake.failure = &temporal.WorkflowFailure{Message: "boom", Type: "MyError"}
		run := newRun(fmt.Sprintf("run-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, run)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, run) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var got temporalv1alpha1.TemporalWorkflowRun
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal("Failed"))
		Expect(got.Status.Failure).NotTo(BeNil())
		Expect(got.Status.Failure.Message).To(Equal("boom"))
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, "Finished")).To(BeTrue())
	})

	It("terminates a running workflow on delete with cancellationPolicy=Terminate", func() {
		c := newReadyCluster(fmt.Sprintf("cluster-%d", counter), &temporalv1alpha1.WorkflowRunPolicy{Enabled: true})
		run := newRun(fmt.Sprintf("run-%d", counter), c.Name)
		run.Spec.CancellationPolicy = "Terminate"
		Expect(k8sClient.Create(ctx, run)).To(Succeed())

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req) // adds finalizer + starts
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, run)).To(Succeed())
		_, err = reconciler().Reconcile(ctx, req) // handles deletion
		Expect(err).NotTo(HaveOccurred())
		Expect(fake.terminated).NotTo(BeEmpty())
	})
})
