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
	"errors"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const workflowRunFinalizer = "temporal.bmor10.com/workflowrun"

// workflowRunPollInterval is how often a running workflow's status is refreshed.
const workflowRunPollInterval = 10 * time.Second

// TemporalWorkflowRunReconciler reconciles TemporalWorkflowRun objects.
type TemporalWorkflowRunReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ClientFactory builds the Temporal workflow-run client; injectable for tests.
	ClientFactory temporal.WorkflowRunClientFactory
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalworkflowruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalworkflowruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalworkflowruns/finalizers,verbs=update

// Reconcile starts a one-off workflow, tracks its status, and cleans up via TTL.
func (r *TemporalWorkflowRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var run temporalv1alpha1.TemporalWorkflowRun
	if err := r.Get(ctx, req.NamespacedName, &run); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	target, err := resolveTarget(ctx, r.Client, run.Namespace, run.Spec.ClusterRef)
	if err != nil {
		if !run.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizer(ctx, &run)
		}
		if errors.Is(err, ErrTargetNotFound) {
			r.setReady(&run, metav1.ConditionFalse, temporalv1alpha1.ReasonClusterNotFound, "referenced Temporal target not found")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &run)
		}
		return ctrl.Result{}, err
	}

	wc, err := r.clientFactory()(ctx, target.Address, target.TLSConfig)
	if err != nil {
		if !run.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizer(ctx, &run)
		}
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
	defer func() { _ = wc.Close() }()

	if !run.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &run, wc)
	}

	if !controllerutil.ContainsFinalizer(&run, workflowRunFinalizer) {
		controllerutil.AddFinalizer(&run, workflowRunFinalizer)
		if err := r.Update(ctx, &run); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !target.Ready {
		r.setReady(&run, metav1.ConditionFalse, temporalv1alpha1.ReasonClusterNotReady, "waiting for the Temporal target to become ready")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &run)
	}

	return r.reconcileRun(ctx, &run, wc, target.WorkflowRunPolicy)
}

func (r *TemporalWorkflowRunReconciler) reconcileRun(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun, wc temporal.WorkflowRunClient, policy temporalv1alpha1.WorkflowRunPolicy) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	wfID := resolveWorkflowID(run)
	taskQueue := run.Spec.Workflow.TaskQueue

	// Start the workflow once.
	if run.Status.RunID == "" {
		if err := checkWorkflowRunPolicy(policy, run.Spec.Namespace, taskQueue); err != nil {
			r.setReady(run, metav1.ConditionFalse, temporalv1alpha1.ReasonWorkflowRunNotPermitted, err.Error())
			return ctrl.Result{}, r.statusUpdate(ctx, run)
		}
		params, err := workflowRunParams(run)
		if err != nil {
			r.setReady(run, metav1.ConditionFalse, "InvalidSpec", err.Error())
			return ctrl.Result{}, r.statusUpdate(ctx, run)
		}
		runID, err := wc.Start(ctx, run.Spec.Namespace, string(run.UID), params)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("starting workflow: %w", err)
		}
		log.Info("started workflow", "workflowID", wfID, "runID", runID)
		run.Status.WorkflowID = wfID
		run.Status.RunID = runID
		run.Status.WorkflowType = run.Spec.Workflow.WorkflowType
		run.Status.TaskQueue = taskQueue
		run.Status.Phase = "Running"
		r.setReady(run, metav1.ConditionTrue, temporalv1alpha1.ReasonWorkflowRunning, "workflow started")
	}

	// Refresh observed state.
	info, err := wc.Describe(ctx, run.Spec.Namespace, wfID, run.Status.RunID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("describing workflow: %w", err)
	}
	run.Status.Phase = temporal.PhaseFromStatus(info.Status)
	run.Status.HistoryLength = info.HistoryLength
	if info.StartTime != nil {
		t := metav1.NewTime(*info.StartTime)
		run.Status.StartTime = &t
	}
	if info.CloseTime != nil {
		t := metav1.NewTime(*info.CloseTime)
		run.Status.CloseTime = &t
	}

	if !temporal.IsTerminalStatus(info.Status) {
		r.setReady(run, metav1.ConditionTrue, temporalv1alpha1.ReasonWorkflowRunning, "workflow is running")
		return ctrl.Result{RequeueAfter: workflowRunPollInterval}, r.statusUpdate(ctx, run)
	}

	// Terminal: record completion, failure, and Finished condition once.
	if run.Status.CompletionTime == nil {
		now := metav1.Now()
		run.Status.CompletionTime = &now
	}
	if info.Failure != nil {
		run.Status.Failure = &temporalv1alpha1.WorkflowRunFailure{Message: info.Failure.Message, Type: info.Failure.Type}
	}
	meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type: "Finished", Status: metav1.ConditionTrue,
		Reason: temporalv1alpha1.ReasonWorkflowFinished, Message: "workflow reached a terminal state: " + run.Status.Phase,
		ObservedGeneration: run.Generation,
	})
	r.setReady(run, metav1.ConditionTrue, temporalv1alpha1.ReasonWorkflowFinished, "workflow finished: "+run.Status.Phase)
	if err := r.statusUpdate(ctx, run); err != nil {
		return ctrl.Result{}, err
	}

	// TTL cleanup.
	if run.Spec.TTLSecondsAfterFinished != nil {
		deadline := run.Status.CompletionTime.Add(time.Duration(*run.Spec.TTLSecondsAfterFinished) * time.Second)
		if remaining := time.Until(deadline); remaining > 0 {
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
		log.Info("deleting workflow run after TTL", "name", run.Name)
		return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, run))
	}
	return ctrl.Result{}, nil
}

func (r *TemporalWorkflowRunReconciler) reconcileDelete(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun, wc temporal.WorkflowRunClient) error {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(run, workflowRunFinalizer) {
		return nil
	}
	// Apply the cancellation policy only if the workflow is still running.
	if run.Status.RunID != "" && run.Status.CompletionTime == nil {
		wfID := resolveWorkflowID(run)
		switch run.Spec.CancellationPolicy {
		case "Cancel":
			if err := wc.Cancel(ctx, run.Spec.Namespace, wfID, run.Status.RunID, "TemporalWorkflowRun deleted"); err != nil && !errors.Is(err, temporal.ErrWorkflowNotFound) {
				return fmt.Errorf("cancelling workflow: %w", err)
			}
			log.Info("requested workflow cancellation on delete", "workflowID", wfID)
		case "Terminate":
			if err := wc.Terminate(ctx, run.Spec.Namespace, wfID, run.Status.RunID, "TemporalWorkflowRun deleted"); err != nil && !errors.Is(err, temporal.ErrWorkflowNotFound) {
				return fmt.Errorf("terminating workflow: %w", err)
			}
			log.Info("terminated workflow on delete", "workflowID", wfID)
		}
	}
	return r.removeFinalizer(ctx, run)
}

func (r *TemporalWorkflowRunReconciler) removeFinalizer(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun) error {
	if controllerutil.ContainsFinalizer(run, workflowRunFinalizer) {
		controllerutil.RemoveFinalizer(run, workflowRunFinalizer)
		if err := r.Update(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

func (r *TemporalWorkflowRunReconciler) clientFactory() temporal.WorkflowRunClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewWorkflowRunClient
}

func (r *TemporalWorkflowRunReconciler) setReady(run *temporalv1alpha1.TemporalWorkflowRun, status metav1.ConditionStatus, reason, message string) {
	run.Status.ObservedGeneration = run.Generation
	meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionReady, Status: status,
		Reason: reason, Message: message, ObservedGeneration: run.Generation,
	})
}

func (r *TemporalWorkflowRunReconciler) statusUpdate(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, run))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalWorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalWorkflowRun{}).
		Named("temporalworkflowrun").
		Complete(r)
}

func resolveWorkflowID(run *temporalv1alpha1.TemporalWorkflowRun) string {
	if run.Spec.Workflow.WorkflowID != "" {
		return run.Spec.Workflow.WorkflowID
	}
	return run.Name
}

// checkWorkflowRunPolicy enforces the target's effective WorkflowRunPolicy.
func checkWorkflowRunPolicy(p temporalv1alpha1.WorkflowRunPolicy, namespace, taskQueue string) error {
	if !p.Enabled {
		return fmt.Errorf("workflow runs are not enabled on the referenced Temporal target")
	}
	if len(p.AllowedNamespaces) > 0 && !contains(p.AllowedNamespaces, namespace) {
		return fmt.Errorf("namespace %q is not in the target's allowedNamespaces", namespace)
	}
	if len(p.AllowedTaskQueues) > 0 && !contains(p.AllowedTaskQueues, taskQueue) {
		return fmt.Errorf("task queue %q is not in the target's allowedTaskQueues", taskQueue)
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// workflowRunParams maps a TemporalWorkflowRun's workflow spec to StartWorkflowParams.
func workflowRunParams(run *temporalv1alpha1.TemporalWorkflowRun) (temporal.StartWorkflowParams, error) {
	a := run.Spec.Workflow
	params := temporal.StartWorkflowParams{
		WorkflowType:     a.WorkflowType,
		TaskQueue:        a.TaskQueue,
		WorkflowID:       resolveWorkflowID(run),
		Args:             rawList(a.Args),
		ExecutionTimeout: durPtr(a.WorkflowExecutionTimeout),
		RunTimeout:       durPtr(a.WorkflowRunTimeout),
		TaskTimeout:      durPtr(a.WorkflowTaskTimeout),
		IDReusePolicy:    a.WorkflowIDReusePolicy,
		Memo:             rawMap(a.Memo),
		SearchAttributes: rawMap(a.SearchAttributes),
	}
	if a.RetryPolicy != nil {
		backoff := 2.0
		if a.RetryPolicy.BackoffCoefficient != "" {
			f, err := strconv.ParseFloat(a.RetryPolicy.BackoffCoefficient, 64)
			if err != nil {
				return temporal.StartWorkflowParams{}, fmt.Errorf("invalid retryPolicy.backoffCoefficient: %w", err)
			}
			backoff = f
		}
		params.Retry = &temporal.RetryParams{
			InitialInterval:        durPtr(a.RetryPolicy.InitialInterval),
			BackoffCoefficient:     backoff,
			MaximumInterval:        durPtr(a.RetryPolicy.MaximumInterval),
			MaximumAttempts:        a.RetryPolicy.MaximumAttempts,
			NonRetryableErrorTypes: a.RetryPolicy.NonRetryableErrorTypes,
		}
	}
	return params, nil
}
