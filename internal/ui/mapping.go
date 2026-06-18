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

package ui

import (
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// serviceOrder is the stable display order for Temporal services.
var serviceOrder = []string{"frontend", "history", "matching", "worker", "internal-frontend"}

func badgeForCondition(conds []metav1.Condition, condType string) BadgeState {
	c := findCondition(conds, condType)
	if c == nil {
		return BadgeUnknown
	}
	switch c.Status {
	case metav1.ConditionTrue:
		return BadgeOK
	case metav1.ConditionFalse:
		return BadgeError
	default:
		return BadgePending
	}
}

func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

func age(t metav1.Time) string {
	if t.IsZero() {
		return ""
	}
	return duration(time.Since(t.Time))
}

func duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

// SummaryFromCluster maps a TemporalCluster to its overview view-model.
func SummaryFromCluster(c *temporalv1alpha1.TemporalCluster) ClusterSummary {
	return ClusterSummary{
		Namespace:   c.Namespace,
		Name:        c.Name,
		Version:     c.Spec.Version,
		Shards:      c.Spec.NumHistoryShards,
		Phase:       c.Status.Phase,
		Ready:       badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionReady),
		Persistence: badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionPersistenceReachable),
		MTLSEnabled: c.Spec.MTLS != nil,
		MTLS:        mtlsBadge(c),
		Upgrading:   c.Status.Upgrade != nil,
		Age:         age(c.CreationTimestamp),
	}
}

func mtlsBadge(c *temporalv1alpha1.TemporalCluster) BadgeState {
	if c.Spec.MTLS == nil {
		return BadgeUnknown
	}
	return badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionMTLSReady)
}

func serviceRows(c *temporalv1alpha1.TemporalCluster) []ServiceRow {
	rows := make([]ServiceRow, 0, len(c.Status.Services))
	for _, name := range serviceOrder {
		st, ok := c.Status.Services[name]
		if !ok {
			continue
		}
		rows = append(rows, ServiceRow{
			Name:    name,
			Ready:   st.Ready,
			Desired: st.Desired,
			Version: st.Version,
			State:   serviceState(st),
		})
	}
	return rows
}

func serviceState(st temporalv1alpha1.ServiceStatus) BadgeState {
	switch {
	case st.Desired == 0:
		return BadgeUnknown
	case st.Ready == 0:
		return BadgeError
	case st.Ready < st.Desired:
		return BadgeWarn
	default:
		return BadgeOK
	}
}

func conditionRows(conds []metav1.Condition) []ConditionRow {
	rows := make([]ConditionRow, 0, len(conds))
	for i := range conds {
		c := conds[i]
		rows = append(rows, ConditionRow{
			Type:    c.Type,
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
			State:   badgeForCondition(conds, c.Type),
		})
	}
	return rows
}

func upgradeInfo(c *temporalv1alpha1.TemporalCluster) UpgradeInfo {
	u := c.Status.Upgrade
	if u == nil {
		return UpgradeInfo{}
	}
	return UpgradeInfo{
		Active:       true,
		FromVersion:  u.FromVersion,
		ToVersion:    u.ToVersion,
		Phase:        u.Phase,
		Rollbackable: u.Rollbackable,
	}
}

func persistenceInfo(c *temporalv1alpha1.TemporalCluster) PersistenceInfo {
	return PersistenceInfo{
		Reachable:   badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionPersistenceReachable),
		SchemaReady: badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionSchemaReady),
	}
}

func endpointsInfo(c *temporalv1alpha1.TemporalCluster) EndpointsInfo {
	return EndpointsInfo{
		Frontend: c.Status.Endpoints.Frontend,
		UI:       c.Status.Endpoints.UI,
		Metrics:  c.Status.Endpoints.Metrics,
	}
}

func relatedNamespaces(items []temporalv1alpha1.TemporalNamespace, cluster string) []RelatedResource {
	out := make([]RelatedResource, 0)
	for i := range items {
		if items[i].Spec.ClusterRef.Name != cluster {
			continue
		}
		detail := ""
		if items[i].Status.Registered {
			detail = "registered"
		}
		out = append(out, RelatedResource{
			Kind:   "TemporalNamespace",
			Name:   items[i].Name,
			Ready:  badgeForCondition(items[i].Status.Conditions, temporalv1alpha1.ConditionReady),
			Detail: detail,
		})
	}
	return out
}

func relatedClients(items []temporalv1alpha1.TemporalClusterClient, cluster string) []RelatedResource {
	out := make([]RelatedResource, 0)
	for i := range items {
		if items[i].Spec.ClusterRef.Name != cluster {
			continue
		}
		detail := ""
		if items[i].Status.SecretRef != nil {
			detail = "secret: " + items[i].Status.SecretRef.Name
		}
		out = append(out, RelatedResource{
			Kind:   "TemporalClusterClient",
			Name:   items[i].Name,
			Ready:  badgeForCondition(items[i].Status.Conditions, temporalv1alpha1.ConditionReady),
			Detail: detail,
		})
	}
	return out
}

func relatedSearchAttributes(items []temporalv1alpha1.TemporalSearchAttribute, cluster string) []RelatedResource {
	out := make([]RelatedResource, 0)
	for i := range items {
		if items[i].Spec.ClusterRef.Name != cluster {
			continue
		}
		out = append(out, RelatedResource{
			Kind:  "TemporalSearchAttribute",
			Name:  items[i].Name,
			Ready: badgeForCondition(items[i].Status.Conditions, temporalv1alpha1.ConditionReady),
		})
	}
	return out
}
