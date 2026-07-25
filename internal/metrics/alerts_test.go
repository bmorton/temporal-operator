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
	"os"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestAlertsReferenceRealMetrics guards against the classic drift where an
// alert outlives the metric or condition it queries.
func TestAlertsReferenceRealMetrics(t *testing.T) {
	raw, err := os.ReadFile("../../config/prometheus/alerts.yaml")
	if err != nil {
		t.Fatalf("reading alerts: %v", err)
	}

	var rule struct {
		Spec struct {
			Groups []struct {
				Name  string `json:"name"`
				Rules []struct {
					Alert string `json:"alert"`
					Expr  string `json:"expr"`
					For   string `json:"for"`
				} `json:"rules"`
			} `json:"groups"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &rule); err != nil {
		t.Fatalf("parsing alerts: %v", err)
	}

	wantAlerts := map[string]bool{
		"TemporalClusterUpgradeStalled": false,
		"TemporalSchemaMigrationFailed": false,
		"TemporalResourceDegraded":      false,
		"TemporalCleanupAbandoned":      false,
	}

	knownSeries := []string{
		"temporal_operator_resource_condition",
		"temporal_operator_cleanup_abandoned_total",
		"temporal_operator_upgrade_phase_seconds",
		"temporal_operator_schema_job_attempts",
		"temporal_operator_target_unreachable_total",
	}

	for _, group := range rule.Spec.Groups {
		for _, r := range group.Rules {
			if _, expected := wantAlerts[r.Alert]; !expected {
				t.Errorf("unexpected alert %q; add it to wantAlerts if intentional", r.Alert)
				continue
			}
			wantAlerts[r.Alert] = true

			found := false
			for _, series := range knownSeries {
				if strings.Contains(r.Expr, series) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("alert %q queries no known metric: %s", r.Alert, r.Expr)
			}
			if r.For == "" {
				t.Errorf("alert %q has no 'for' duration; it would fire on a single scrape", r.Alert)
			}
		}
	}

	for name, seen := range wantAlerts {
		if !seen {
			t.Errorf("alert %q is missing from alerts.yaml", name)
		}
	}
}

// TestConditionAlertComparisons guards the gauge semantics of
// temporal_operator_resource_condition: the metric emits one series per
// condition with the condition status as a label and the gauge VALUE set to 1
// only when status is True. Consequently:
//   - an expr matching status="True"  must compare == 1  (True series carry value 1)
//   - an expr matching status="False" must compare == 0  (False series always carry value 0)
//
// A comparison like status="False" == 1 can never match and will silently
// never fire, even for a critical condition.
func TestConditionAlertComparisons(t *testing.T) {
	raw, err := os.ReadFile("../../config/prometheus/alerts.yaml")
	if err != nil {
		t.Fatalf("reading alerts: %v", err)
	}

	var rule struct {
		Spec struct {
			Groups []struct {
				Rules []struct {
					Alert string `json:"alert"`
					Expr  string `json:"expr"`
				} `json:"rules"`
			} `json:"groups"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &rule); err != nil {
		t.Fatalf("parsing alerts: %v", err)
	}

	const metric = "temporal_operator_resource_condition"

	for _, group := range rule.Spec.Groups {
		for _, r := range group.Rules {
			expr := r.Expr
			if !strings.Contains(expr, metric) {
				continue
			}

			hasTrue := strings.Contains(expr, `status="True"`)
			hasFalse := strings.Contains(expr, `status="False"`)

			if hasTrue && !strings.Contains(expr, "== 1") {
				t.Errorf(
					"alert %q: expr matches status=\"True\" but does not compare == 1.\n"+
						"  The %s gauge is 1 when the condition is True; use == 1.\n"+
						"  expr: %s",
					r.Alert, metric, expr,
				)
			}
			if hasFalse && !strings.Contains(expr, "== 0") {
				t.Errorf(
					"alert %q: expr matches status=\"False\" but does not compare == 0.\n"+
						"  The %s gauge is 0 when the condition is False (value 1 is only set for True).\n"+
						"  A comparison like == 1 with status=\"False\" can never match and will never fire.\n"+
						"  expr: %s",
					r.Alert, metric, expr,
				)
			}
		}
	}
}
