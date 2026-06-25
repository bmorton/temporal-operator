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

package v1alpha1

// Condition types reported on Temporal resource status.
const (
	// ConditionReady indicates the resource is fully reconciled and operational.
	ConditionReady = "Ready"
	// ConditionAvailable indicates the resource is serving requests.
	ConditionAvailable = "Available"
	// ConditionProgressing indicates the resource is being created or updated.
	ConditionProgressing = "Progressing"
	// ConditionDegraded indicates the resource failed to reach or maintain its desired state.
	ConditionDegraded = "Degraded"
	// ConditionPersistenceReachable indicates the datastores are reachable.
	ConditionPersistenceReachable = "PersistenceReachable"
	// ConditionSchemaReady indicates the persistence schema is at the desired version.
	ConditionSchemaReady = "SchemaReady"
	// ConditionMTLSReady indicates mTLS certificates are provisioned and valid.
	ConditionMTLSReady = "MTLSReady"
	// ConditionUpgradeBlocked indicates an upgrade cannot proceed.
	ConditionUpgradeBlocked = "UpgradeBlocked"
	// ConditionShardCountLocked indicates the immutable shard count is locked in.
	ConditionShardCountLocked = "ShardCountLocked"
)

// Condition reasons reported on Temporal resource status.
const (
	// ReasonNotImplemented indicates the reconciler is a scaffold only.
	ReasonNotImplemented = "NotImplemented"
	// ReasonReconciling indicates reconciliation is in progress.
	ReasonReconciling = "Reconciling"
	// ReasonPersistenceUnreachable indicates the datastores could not be reached.
	ReasonPersistenceUnreachable = "PersistenceUnreachable"
	// ReasonSchemaMigrating indicates a schema migration is running.
	ReasonSchemaMigrating = "SchemaMigrating"
	// ReasonInspecting indicates schema inspection is in progress.
	ReasonInspecting = "Inspecting"
	// ReasonShardCountImmutable indicates an attempt to change the immutable shard count.
	ReasonShardCountImmutable = "ShardCountImmutable"
	// ReasonVersionUnsupported indicates the requested version is not in the support matrix.
	ReasonVersionUnsupported = "VersionUnsupported"
	// ReasonUpgradePathInvalid indicates the requested upgrade path is not allowed.
	ReasonUpgradePathInvalid = "UpgradePathInvalid"
	// ReasonRolloutInProgress indicates a rollout is underway.
	ReasonRolloutInProgress = "RolloutInProgress"
	// ReasonAllServicesReady indicates all services are ready.
	ReasonAllServicesReady = "AllServicesReady"
	// ReasonDeletionPrevented indicates deletion was blocked by preventDeletion.
	ReasonDeletionPrevented = "DeletionPrevented"
	// ReasonClusterMetadataImmutable indicates an attempt to change an immutable clusterMetadata field.
	ReasonClusterMetadataImmutable = "ClusterMetadataImmutable"
	// ReasonIsGlobalImmutable indicates an attempt to change the isGlobal field after creation.
	ReasonIsGlobalImmutable = "IsGlobalImmutable"
	// ReasonPeersConnected indicates all replication peers are connected.
	ReasonPeersConnected = "PeersConnected"
	// ReasonPeerUnreachable indicates a replication peer could not be reached.
	ReasonPeerUnreachable = "PeerUnreachable"
	// ReasonReplicationDrift indicates the observed replication config drifted from desired.
	ReasonReplicationDrift = "ReplicationConfigDrift"
	// ReasonActiveClusterInvalid indicates the active cluster selection is invalid.
	ReasonActiveClusterInvalid = "ActiveClusterInvalid"
	// ReasonWorkflowRunNotPermitted indicates the target's WorkflowRunPolicy denied the run.
	ReasonWorkflowRunNotPermitted = "WorkflowRunNotPermitted"
	// ReasonWorkflowRunning indicates the workflow is currently running.
	ReasonWorkflowRunning = "WorkflowRunning"
	// ReasonWorkflowFinished indicates the workflow reached a terminal state.
	ReasonWorkflowFinished = "WorkflowFinished"
	// ReasonClusterNotFound indicates the referenced Temporal target was not found.
	ReasonClusterNotFound = "ClusterNotFound"
	// ReasonClusterNotReady indicates the referenced Temporal target is not ready.
	ReasonClusterNotReady = "ClusterNotReady"
)
