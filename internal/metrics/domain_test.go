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

	"github.com/bmorton/temporal-operator/internal/metrics"
)

func TestCleanupAbandonedCounter(t *testing.T) {
	metrics.CleanupAbandoned.Reset()
	metrics.CleanupAbandoned.WithLabelValues("TemporalNamespace", "default").Inc()
	metrics.CleanupAbandoned.WithLabelValues("TemporalNamespace", "default").Inc()

	expected := `
# HELP temporal_operator_cleanup_abandoned_total Number of times remote cleanup was abandoned after the cleanup deadline elapsed.
# TYPE temporal_operator_cleanup_abandoned_total counter
temporal_operator_cleanup_abandoned_total{kind="TemporalNamespace",namespace="default"} 2
`
	if err := testutil.CollectAndCompare(metrics.CleanupAbandoned, strings.NewReader(expected)); err != nil {
		t.Error(err)
	}
}

func TestUpgradePhaseSecondsGauge(t *testing.T) {
	metrics.UpgradePhaseSeconds.Reset()
	metrics.UpgradePhaseSeconds.WithLabelValues("default", "tc", "RollingFrontend").Set(120)

	expected := `
# HELP temporal_operator_upgrade_phase_seconds Seconds the current upgrade phase has been active.
# TYPE temporal_operator_upgrade_phase_seconds gauge
temporal_operator_upgrade_phase_seconds{name="tc",namespace="default",phase="RollingFrontend"} 120
`
	if err := testutil.CollectAndCompare(metrics.UpgradePhaseSeconds, strings.NewReader(expected)); err != nil {
		t.Error(err)
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	metrics.Register()
	metrics.Register() // must not panic with AlreadyRegisteredError
}
