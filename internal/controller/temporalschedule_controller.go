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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const scheduleFinalizer = "temporal.bmor10.com/schedule"

// scheduleDriftRequeue is how often a schedule is re-asserted (existence + pause).
const scheduleDriftRequeue = 5 * time.Minute

// TemporalScheduleReconciler reconciles TemporalSchedule objects.
type TemporalScheduleReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ClientFactory builds the Temporal schedule client; injectable for tests.
	ClientFactory temporal.ScheduleClientFactory
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalschedules/finalizers,verbs=update

// Reconcile creates, updates, pauses, or deletes a Temporal schedule.
func (r *TemporalScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sched temporalv1alpha1.TemporalSchedule
	if err := r.Get(ctx, req.NamespacedName, &sched); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var cluster temporalv1alpha1.TemporalCluster
	clusterKey := types.NamespacedName{Namespace: sched.Namespace, Name: sched.Spec.ClusterRef.Name}
	if err := r.Get(ctx, clusterKey, &cluster); err != nil {
		if !sched.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizerAndForget(ctx, &sched)
		}
		r.setReady(&sched, metav1.ConditionFalse, "ClusterNotFound", "referenced TemporalCluster not found")
		return ctrl.Result{RequeueAfter: scheduleDriftRequeue}, r.statusUpdate(ctx, &sched)
	}

	tlsConfig, err := clusterTLSConfig(ctx, r.Client, &cluster)
	if err != nil {
		if !sched.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizerAndForget(ctx, &sched)
		}
		return ctrl.Result{}, fmt.Errorf("building temporal client tls: %w", err)
	}
	sc, err := r.clientFactory()(ctx, frontendAddress(&cluster), tlsConfig)
	if err != nil {
		if !sched.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizerAndForget(ctx, &sched)
		}
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
	defer func() { _ = sc.Close() }()

	if !sched.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &sched, sc)
	}

	if !controllerutil.ContainsFinalizer(&sched, scheduleFinalizer) {
		controllerutil.AddFinalizer(&sched, scheduleFinalizer)
		if err := r.Update(ctx, &sched); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionReady) {
		r.setReady(&sched, metav1.ConditionFalse, "ClusterNotReady", "waiting for the TemporalCluster to become ready")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &sched)
	}

	if err := r.reconcileSchedule(ctx, &sched, sc); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: scheduleDriftRequeue}, r.statusUpdate(ctx, &sched)
}

func (r *TemporalScheduleReconciler) reconcileSchedule(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule, sc temporal.ScheduleClient) error {
	log := logf.FromContext(ctx)
	params, err := scheduleParams(sched)
	if err != nil {
		r.setReady(sched, metav1.ConditionFalse, "InvalidSpec", err.Error())
		return nil
	}
	specHash, err := computeSpecHash(params)
	if err != nil {
		return fmt.Errorf("hashing schedule spec: %w", err)
	}

	_, err = sc.Describe(ctx, params.Namespace, params.ScheduleID)
	switch {
	case errors.Is(err, temporal.ErrScheduleNotFound):
		if err := sc.Create(ctx, params); err != nil {
			return fmt.Errorf("creating schedule: %w", err)
		}
		log.Info("created schedule", "scheduleID", params.ScheduleID)
		sched.Status.LastAppliedSpecHash = specHash
	case err != nil:
		return fmt.Errorf("describing schedule: %w", err)
	default:
		if sched.Status.LastAppliedSpecHash != specHash {
			if err := sc.Update(ctx, params); err != nil {
				return fmt.Errorf("updating schedule: %w", err)
			}
			log.Info("updated schedule to apply spec change", "scheduleID", params.ScheduleID)
			sched.Status.LastAppliedSpecHash = specHash
		} else if err := r.reconcilePause(ctx, sc, params); err != nil {
			return err
		}
	}

	info, err := sc.Describe(ctx, params.Namespace, params.ScheduleID)
	if err != nil {
		return fmt.Errorf("describing schedule: %w", err)
	}
	now := metav1.Now()
	sched.Status.ScheduleID = params.ScheduleID
	sched.Status.Created = true
	sched.Status.Paused = info.Paused
	sched.Status.Notes = info.Notes
	sched.Status.RunningWorkflows = int32(info.RunningWorkflows)
	sched.Status.NextActionTimes = nil
	for _, t := range info.NextActionTimes {
		sched.Status.NextActionTimes = append(sched.Status.NextActionTimes, metav1.NewTime(t))
	}
	sched.Status.LastUpdated = &now
	r.setReady(sched, metav1.ConditionTrue, "Reconciled", "schedule is reconciled")
	return nil
}

func (r *TemporalScheduleReconciler) reconcilePause(ctx context.Context, sc temporal.ScheduleClient, params temporal.ScheduleParams) error {
	info, err := sc.Describe(ctx, params.Namespace, params.ScheduleID)
	if err != nil {
		return fmt.Errorf("describing schedule: %w", err)
	}
	if params.State.Paused == info.Paused {
		return nil
	}
	if params.State.Paused {
		return sc.Pause(ctx, params.Namespace, params.ScheduleID, params.State.Notes)
	}
	return sc.Unpause(ctx, params.Namespace, params.ScheduleID, params.State.Notes)
}

func (r *TemporalScheduleReconciler) reconcileDelete(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule, sc temporal.ScheduleClient) error {
	log := logf.FromContext(ctx)
	if controllerutil.ContainsFinalizer(sched, scheduleFinalizer) {
		if sched.Spec.AllowDeletion {
			id := resolveScheduleID(sched)
			if err := sc.Delete(ctx, sched.Spec.Namespace, id); err != nil && !errors.Is(err, temporal.ErrScheduleNotFound) {
				return fmt.Errorf("deleting schedule: %w", err)
			}
			log.Info("deleted temporal schedule", "scheduleID", id)
		}
		controllerutil.RemoveFinalizer(sched, scheduleFinalizer)
		if err := r.Update(ctx, sched); err != nil {
			return err
		}
	}
	return nil
}

// removeFinalizerAndForget removes the schedule finalizer and returns a clean
// result. It is used when the TemporalCluster (or its TLS/client) is
// unreachable during deletion — there is nothing to clean up remotely, so we
// just unblock GC.
func (r *TemporalScheduleReconciler) removeFinalizerAndForget(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule) error {
	if controllerutil.ContainsFinalizer(sched, scheduleFinalizer) {
		controllerutil.RemoveFinalizer(sched, scheduleFinalizer)
		if err := r.Update(ctx, sched); err != nil {
			return err
		}
	}
	return nil
}

func (r *TemporalScheduleReconciler) clientFactory() temporal.ScheduleClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewScheduleClient
}

func (r *TemporalScheduleReconciler) setReady(sched *temporalv1alpha1.TemporalSchedule, status metav1.ConditionStatus, reason, message string) {
	sched.Status.ObservedGeneration = sched.Generation
	meta.SetStatusCondition(&sched.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: sched.Generation,
	})
}

func (r *TemporalScheduleReconciler) statusUpdate(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, sched))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalSchedule{}).
		Named("temporalschedule").
		Complete(r)
}

func resolveScheduleID(sched *temporalv1alpha1.TemporalSchedule) string {
	if sched.Spec.ScheduleID != "" {
		return sched.Spec.ScheduleID
	}
	return sched.Name
}

func computeSpecHash(params temporal.ScheduleParams) (string, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// scheduleParams converts a TemporalSchedule CR into temporal.ScheduleParams.
func scheduleParams(sched *temporalv1alpha1.TemporalSchedule) (temporal.ScheduleParams, error) {
	a := sched.Spec.Action.StartWorkflow
	action := temporal.StartWorkflowParams{
		WorkflowType:     a.WorkflowType,
		TaskQueue:        a.TaskQueue,
		WorkflowID:       a.WorkflowID,
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
				return temporal.ScheduleParams{}, fmt.Errorf("invalid retryPolicy.backoffCoefficient: %w", err)
			}
			backoff = f
		}
		action.Retry = &temporal.RetryParams{
			InitialInterval:        durPtr(a.RetryPolicy.InitialInterval),
			BackoffCoefficient:     backoff,
			MaximumInterval:        durPtr(a.RetryPolicy.MaximumInterval),
			MaximumAttempts:        a.RetryPolicy.MaximumAttempts,
			NonRetryableErrorTypes: a.RetryPolicy.NonRetryableErrorTypes,
		}
	}

	spec := temporal.ScheduleSpecParams{
		CronStrings:      sched.Spec.Schedule.Calendars,
		StartTime:        timePtr(sched.Spec.Schedule.StartTime),
		EndTime:          timePtr(sched.Spec.Schedule.EndTime),
		Jitter:           durPtr(sched.Spec.Schedule.Jitter),
		TimezoneName:     sched.Spec.Schedule.TimezoneName,
		Calendars:        calendars(sched.Spec.Schedule.StructuredCalendar),
		ExcludeCalendars: calendars(sched.Spec.Schedule.ExcludeStructuredCalendar),
	}
	for _, iv := range sched.Spec.Schedule.Intervals {
		spec.Intervals = append(spec.Intervals, temporal.IntervalParams{
			Every:  iv.Every.Duration,
			Offset: durPtr(iv.Offset),
		})
	}

	var policies temporal.SchedulePolicyParams
	if p := sched.Spec.Policies; p != nil {
		policies = temporal.SchedulePolicyParams{
			OverlapPolicy:          p.OverlapPolicy,
			CatchupWindow:          durPtr(p.CatchupWindow),
			PauseOnFailure:         p.PauseOnFailure,
			KeepOriginalWorkflowID: p.KeepOriginalWorkflowID,
		}
	}

	var state temporal.ScheduleStateParams
	if s := sched.Spec.State; s != nil {
		state = temporal.ScheduleStateParams{
			Paused:           s.Paused,
			Notes:            s.Notes,
			LimitedActions:   s.LimitedActions,
			RemainingActions: s.RemainingActions,
		}
	}

	return temporal.ScheduleParams{
		ScheduleID: resolveScheduleID(sched),
		Namespace:  sched.Spec.Namespace,
		Spec:       spec,
		Action:     action,
		Policies:   policies,
		State:      state,
	}, nil
}

func calendars(in []temporalv1alpha1.StructuredCalendarSpec) []temporal.StructuredCalendarParams {
	if len(in) == 0 {
		return nil
	}
	out := make([]temporal.StructuredCalendarParams, 0, len(in))
	for _, c := range in {
		out = append(out, temporal.StructuredCalendarParams{
			Second:     ranges(c.Second),
			Minute:     ranges(c.Minute),
			Hour:       ranges(c.Hour),
			DayOfMonth: ranges(c.DayOfMonth),
			Month:      ranges(c.Month),
			Year:       ranges(c.Year),
			DayOfWeek:  ranges(c.DayOfWeek),
			Comment:    c.Comment,
		})
	}
	return out
}

func ranges(in []temporalv1alpha1.CalendarRange) []temporal.RangeParams {
	if len(in) == 0 {
		return nil
	}
	out := make([]temporal.RangeParams, 0, len(in))
	for _, r := range in {
		out = append(out, temporal.RangeParams{Start: r.Start, End: r.End, Step: r.Step})
	}
	return out
}

func rawList(in []runtime.RawExtension) [][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(in))
	for _, r := range in {
		out = append(out, r.Raw)
	}
	return out
}

func rawMap(in map[string]runtime.RawExtension) map[string][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(in))
	for k, r := range in {
		out[k] = r.Raw
	}
	return out
}

func durPtr(d *metav1.Duration) *time.Duration {
	if d == nil {
		return nil
	}
	v := d.Duration
	return &v
}

func timePtr(t *metav1.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.Time
	return &v
}
