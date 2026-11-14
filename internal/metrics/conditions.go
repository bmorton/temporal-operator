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

package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	opstatus "github.com/bmorton/temporal-operator/internal/status"
)

var conditionDesc = prometheus.NewDesc(
	namespacePrefix+"_resource_condition",
	"Current status of each condition on each Temporal resource (1 when the condition is True).",
	[]string{"kind", "namespace", "name", "type", "status", "reason"},
	nil,
)

// listers maps each kind name to a factory for its List type. Adding a CRD means
// adding one line here; nothing else in the metrics layer changes.
var listers = map[string]func() client.ObjectList{
	"TemporalCluster":           func() client.ObjectList { return &temporalv1alpha1.TemporalClusterList{} },
	"TemporalClusterClient":     func() client.ObjectList { return &temporalv1alpha1.TemporalClusterClientList{} },
	"TemporalClusterConnection": func() client.ObjectList { return &temporalv1alpha1.TemporalClusterConnectionList{} },
	"TemporalDevServer":         func() client.ObjectList { return &temporalv1alpha1.TemporalDevServerList{} },
	"TemporalNamespace":         func() client.ObjectList { return &temporalv1alpha1.TemporalNamespaceList{} },
	"TemporalSchedule":          func() client.ObjectList { return &temporalv1alpha1.TemporalScheduleList{} },
	"TemporalSearchAttribute":   func() client.ObjectList { return &temporalv1alpha1.TemporalSearchAttributeList{} },
	"TemporalWorkflowRun":       func() client.ObjectList { return &temporalv1alpha1.TemporalWorkflowRunList{} },
}

// ConditionCollector exports every condition on every Temporal resource.
//
// It collects on scrape from the manager's cache rather than writing gauges
// during reconcile. That avoids stale series for deleted objects and means new
// conditions become queryable without touching this file.
type ConditionCollector struct {
	client client.Client
}

// NewConditionCollector builds a collector reading through the given client.
func NewConditionCollector(c client.Client) *ConditionCollector {
	return &ConditionCollector{client: c}
}

// Describe implements prometheus.Collector.
func (c *ConditionCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- conditionDesc
}

// Collect implements prometheus.Collector. Errors listing a kind are skipped
// rather than failing the whole scrape: a partial view beats none.
func (c *ConditionCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	for kind, newList := range listers {
		list := newList()
		if err := c.client.List(ctx, list); err != nil {
			continue
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			continue
		}
		for _, item := range items {
			obj, ok := item.(opstatus.Object)
			if !ok {
				continue
			}
			for _, cond := range *obj.GetConditions() {
				value := 0.0
				if cond.Status == metav1.ConditionTrue {
					value = 1.0
				}
				ch <- prometheus.MustNewConstMetric(
					conditionDesc, prometheus.GaugeValue, value,
					kind, obj.GetNamespace(), obj.GetName(),
					cond.Type, string(cond.Status), cond.Reason,
				)
			}
		}
	}
}
