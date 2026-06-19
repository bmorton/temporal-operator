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

package model

// BadgeState is the visual state of a status badge.
type BadgeState string

const (
	BadgeOK      BadgeState = "ok"
	BadgeWarn    BadgeState = "warn"
	BadgeError   BadgeState = "error"
	BadgePending BadgeState = "pending"
	BadgeUnknown BadgeState = "unknown"
)

// ClusterSummary is one row/card on the overview.
type ClusterSummary struct {
	Namespace   string
	Name        string
	Version     string
	Shards      int32
	Phase       string
	Ready       BadgeState
	Persistence BadgeState
	MTLSEnabled bool
	MTLS        BadgeState
	Upgrading   bool
	Age         string
}

// ServiceRow reports readiness of one Temporal service.
type ServiceRow struct {
	Name    string
	Ready   int32
	Desired int32
	Version string
	State   BadgeState
}

// ConditionRow is one status condition.
type ConditionRow struct {
	Type    string
	Status  string
	Reason  string
	Message string
	State   BadgeState
}

// PersistenceInfo summarizes datastore state.
type PersistenceInfo struct {
	Reachable   BadgeState
	SchemaReady BadgeState
}

// EndpointsInfo holds resolved endpoints.
type EndpointsInfo struct {
	Frontend string
	UI       string
	Metrics  string
}

// HasAny reports whether at least one endpoint is set.
func (e EndpointsInfo) HasAny() bool {
	return e.Frontend != "" || e.UI != "" || e.Metrics != ""
}

// UpgradeInfo describes an in-flight upgrade.
type UpgradeInfo struct {
	Active       bool
	FromVersion  string
	ToVersion    string
	Phase        string
	Rollbackable bool
}

// RelatedResource is a satellite CRD tied to a cluster.
type RelatedResource struct {
	Kind   string
	Name   string
	Ready  BadgeState
	Detail string
}

// ClusterDetail is the full per-cluster view.
type ClusterDetail struct {
	ClusterSummary
	Conditions  []ConditionRow
	Services    []ServiceRow
	Persistence PersistenceInfo
	Endpoints   EndpointsInfo
	Upgrade     UpgradeInfo
	Related     []RelatedResource
}

// JoinPath joins the UI base path with a sub-path (which must start with "/"),
// avoiding a duplicate leading slash that would produce a protocol-relative URL.
func JoinPath(basePath, sub string) string {
	if basePath == "" || basePath == "/" {
		return sub
	}
	return basePath + sub
}
