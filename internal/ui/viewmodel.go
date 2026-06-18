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

import "github.com/bmorton/temporal-operator/internal/ui/model"

// BadgeState is the visual state of a status badge.
type BadgeState = model.BadgeState

const (
	BadgeOK      = model.BadgeOK
	BadgeWarn    = model.BadgeWarn
	BadgeError   = model.BadgeError
	BadgePending = model.BadgePending
	BadgeUnknown = model.BadgeUnknown
)

// ClusterSummary is one row/card on the overview.
type ClusterSummary = model.ClusterSummary

// ServiceRow reports readiness of one Temporal service.
type ServiceRow = model.ServiceRow

// ConditionRow is one status condition.
type ConditionRow = model.ConditionRow

// PersistenceInfo summarizes datastore state.
type PersistenceInfo = model.PersistenceInfo

// EndpointsInfo holds resolved endpoints.
type EndpointsInfo = model.EndpointsInfo

// UpgradeInfo describes an in-flight upgrade.
type UpgradeInfo = model.UpgradeInfo

// RelatedResource is a satellite CRD tied to a cluster.
type RelatedResource = model.RelatedResource

// ClusterDetail is the full per-cluster view.
type ClusterDetail = model.ClusterDetail
