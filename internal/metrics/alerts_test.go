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
