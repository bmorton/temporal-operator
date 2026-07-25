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

// Package metrics exposes the operator's domain metrics. Reconcile rate,
// latency, and error metrics already come from controller-runtime; only state
// that conditions cannot express lives here.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const namespacePrefix = "temporal_operator"

var (
	// CleanupAbandoned counts give-ups on remote cleanup. Any increase means a
	// Temporal-side object was orphaned and needs manual attention.
	CleanupAbandoned = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: namespacePrefix + "_cleanup_abandoned_total",
		Help: "Number of times remote cleanup was abandoned after the cleanup deadline elapsed.",
	}, []string{"kind", "namespace"})

	// TargetUnreachable counts failures to resolve or dial a Temporal target
	// that the operator believes exists. Rising counts show a flapping frontend
	// before it causes an abandonment.
	TargetUnreachable = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: namespacePrefix + "_target_unreachable_total",
		Help: "Number of failures to reach a Temporal target that is believed to exist.",
	}, []string{"kind", "namespace"})

	// UpgradePhaseSeconds reports how long the current upgrade phase has run,
	// which is what an alert compares against the stall timeout.
	UpgradePhaseSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: namespacePrefix + "_upgrade_phase_seconds",
		Help: "Seconds the current upgrade phase has been active.",
	}, []string{"namespace", "name", "phase"})

	// SchemaJobAttempts reports consecutive failed schema-migration attempts.
	SchemaJobAttempts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: namespacePrefix + "_schema_job_attempts",
		Help: "Consecutive failed schema migration attempts for a store.",
	}, []string{"namespace", "name", "store"})
)

var registerOnce sync.Once

// Register adds the domain metrics to controller-runtime's registry, which is
// already served on the manager's authenticated metrics endpoint. Safe to call
// more than once.
func Register() {
	registerOnce.Do(func() {
		ctrlmetrics.Registry.MustRegister(
			CleanupAbandoned,
			TargetUnreachable,
			UpgradePhaseSeconds,
			SchemaJobAttempts,
		)
	})
}
