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

package metrics_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/metrics"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := temporalv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	return s
}

func TestConditionCollectorEmitsOnePerCondition(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "tc"
	cluster.Namespace = "default"
	cluster.Status.Conditions = []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllServicesReady"},
		{Type: "UpgradeBlocked", Status: metav1.ConditionFalse, Reason: "UpgradeProgressing"},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(cluster).Build()
	collector := metrics.NewConditionCollector(c)

	expected := `
# HELP temporal_operator_resource_condition Current status of each condition on each Temporal resource (1 when the condition is True).
# TYPE temporal_operator_resource_condition gauge
temporal_operator_resource_condition{kind="TemporalCluster",name="tc",namespace="default",reason="AllServicesReady",status="True",type="Ready"} 1
temporal_operator_resource_condition{kind="TemporalCluster",name="tc",namespace="default",reason="UpgradeProgressing",status="False",type="UpgradeBlocked"} 0
`
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expected)); err != nil {
		t.Error(err)
	}
}

func TestConditionCollectorDropsDeletedResources(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	collector := metrics.NewConditionCollector(c)

	if got := testutil.CollectAndCount(collector); got != 0 {
		t.Errorf("collected %d series with no resources present, want 0", got)
	}
}

func TestConditionCollectorCoversAllKinds(t *testing.T) {
	objs := []client.Object{
		withReady(&temporalv1alpha1.TemporalCluster{}),
		withReady(&temporalv1alpha1.TemporalClusterClient{}),
		withReady(&temporalv1alpha1.TemporalClusterConnection{}),
		withReady(&temporalv1alpha1.TemporalDevServer{}),
		withReady(&temporalv1alpha1.TemporalNamespace{}),
		withReady(&temporalv1alpha1.TemporalSchedule{}),
		withReady(&temporalv1alpha1.TemporalSearchAttribute{}),
		withReady(&temporalv1alpha1.TemporalWorkflowRun{}),
	}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(objs...).Build()

	if got := testutil.CollectAndCount(metrics.NewConditionCollector(c)); got != 8 {
		t.Errorf("collected %d series, want 8 (one Ready condition per kind)", got)
	}
}

// withReady names an object and gives it a single Ready condition.
func withReady[T interface {
	client.Object
	GetConditions() *[]metav1.Condition
}](obj T) client.Object {
	obj.SetName("x")
	obj.SetNamespace("default")
	*obj.GetConditions() = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ok"}}
	return obj
}
