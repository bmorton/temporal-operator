# Multi-Cluster Replication Support

## Summary

Enable the Temporal Operator to render multi-cluster replication configuration in the `clusterMetadata` section of the Temporal server config file, following the Temporal v1.14+ approach where only the local cluster is declared in config and remote cluster connections are established via the `temporal operator cluster upsert` CLI.

## Motivation

The operator currently has an opaque `ClusterMetadataSpec.Raw` field that accepts arbitrary JSON via `runtime.RawExtension`, but the controller and config template builder completely ignore it. The config template hardcodes single-cluster mode:

- `enableGlobalNamespace: false`
- `currentClusterName: "active"`
- `masterClusterName: "active"`
- Single `clusterInformation` entry pointing to `127.0.0.1`

This makes it impossible to configure multi-cluster replication through the operator, even though Temporal server supports it.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Scope | Config-only (no auto CLI) | Matches Temporal v1.14+ guidance; remote connections via CLI |
| CRD fields | Typed, replacing `Raw` | Validation, documentation, IDE support |
| Remote clusters in CRD | No | v1.14+ approach: only local cluster in config |
| mTLS cross-cluster | Frontend SANs only | Users add reachable DNS names to existing `mtls.frontend.dnsNames` |
| Version target | v1.14+ only | All supported Temporal versions (1.27+) are post-v1.14 |
| `Raw` field removal | Immediate | Never consumed by the operator; no backward-compat concern |

## CRD Changes

### ClusterMetadataSpec (replace existing type)

**File:** `api/v1alpha1/shared_types.go`

Remove:

```go
type ClusterMetadataSpec struct {
    Raw *runtime.RawExtension `json:"raw,omitempty"`
}
```

Replace with:

```go
type ClusterMetadataSpec struct {
    // +optional
    EnableGlobalNamespace bool `json:"enableGlobalNamespace,omitempty"`

    // +kubebuilder:validation:Minimum=1
    // +optional
    FailoverVersionIncrement *int32 `json:"failoverVersionIncrement,omitempty"`

    // +kubebuilder:validation:MinLength=1
    // +optional
    CurrentClusterName string `json:"currentClusterName,omitempty"`

    // +kubebuilder:validation:Minimum=1
    // +optional
    InitialFailoverVersion *int32 `json:"initialFailoverVersion,omitempty"`

    // +kubebuilder:validation:MinLength=1
    // +optional
    MasterClusterName string `json:"masterClusterName,omitempty"`
}
```

**Defaults** (applied in `BuildConfigData` when `clusterMetadata` is nil or fields are zero):

| Field | Default | Matches existing behavior |
|-------|---------|--------------------------|
| `enableGlobalNamespace` | `false` | Yes |
| `failoverVersionIncrement` | `10` | Yes (hardcoded in template) |
| `currentClusterName` | `"active"` | Yes |
| `masterClusterName` | `"active"` | Yes |
| `initialFailoverVersion` | `1` | Yes (hardcoded in template) |

### TemporalNamespaceSpec (add isGlobal field)

**File:** `api/v1alpha1/temporalnamespace_types.go`

Add:

```go
// +optional
IsGlobal bool `json:"isGlobal,omitempty"`
```

This maps to the `--global` flag on `temporal operator namespace create`. Required for creating global namespaces that participate in replication.

## Config Template Changes

### ConfigData struct

**File:** `internal/temporal/configtemplate.go`

Add two fields to `ConfigData`:

```go
FailoverVersionIncrement int
InitialFailoverVersion   int
```

Existing fields `EnableGlobalNamespace`, `CurrentClusterName`, `MasterClusterName` remain. Neither `FailoverVersionIncrement` nor `InitialFailoverVersion` currently exist in the struct — they are hardcoded in the template as `10` and `1` respectively.

### BuildConfigData()

**File:** `internal/temporal/configtemplate.go` (~line 392)

Replace hardcoded values with reads from `cluster.Spec.ClusterMetadata`:

```go
func BuildConfigData(cluster *v1alpha1.TemporalCluster, opts BuildOptions) ConfigData {
    // ... existing code ...

    cm := cluster.Spec.ClusterMetadata
    if cm != nil {
        data.EnableGlobalNamespace = cm.EnableGlobalNamespace
        if cm.FailoverVersionIncrement != nil {
            data.FailoverVersionIncrement = int(*cm.FailoverVersionIncrement)
        }
        if cm.CurrentClusterName != "" {
            data.CurrentClusterName = cm.CurrentClusterName
        }
        if cm.MasterClusterName != "" {
            data.MasterClusterName = cm.MasterClusterName
        }
        if cm.InitialFailoverVersion != nil {
            data.InitialFailoverVersion = int(*cm.InitialFailoverVersion)
        }
    }
    // ... rest of existing code ...
}
```

### config_template.yaml

**File:** `internal/temporal/templates/config_template.yaml` (lines 158-169)

Update `clusterInformation` local entry to use `InitialFailoverVersion`:

```yaml
clusterMetadata:
    enableGlobalNamespace: {{ .EnableGlobalNamespace }}
    failoverVersionIncrement: {{ .FailoverVersionIncrement }}
    masterClusterName: {{ .MasterClusterName | quote }}
    currentClusterName: {{ .CurrentClusterName | quote }}
    clusterInformation:
        {{ .CurrentClusterName }}:
            enabled: true
            initialFailoverVersion: {{ .InitialFailoverVersion }}
            rpcName: "frontend"
            rpcAddress: {{ printf "127.0.0.1:%d" (int .Services.frontend.GRPCPort) | quote }}
            httpAddress: {{ printf "127.0.0.1:%d" (int .Services.frontend.HTTPPort) | quote }}
```

The `dcRedirectionPolicy` section (currently hardcoded to `policy: "noop"`) remains unchanged — users can override it via `dynamicConfig` passthrough.

## Webhook Validation

### ClusterMetadata validation

When `enableGlobalNamespace == true`, enforce:

- `currentClusterName` must be non-empty
- `failoverVersionIncrement` must be set and > 0
- `initialFailoverVersion` must be set and > 0

### Immutability constraints

The following fields are **immutable after creation** (changing them would break an active replication setup):

- `clusterMetadata.failoverVersionIncrement` — must match across all participating clusters
- `clusterMetadata.initialFailoverVersion` — identifies this cluster uniquely
- `clusterMetadata.currentClusterName` — identifies this cluster in the replication group

`masterClusterName` and `enableGlobalNamespace` may be changed (e.g., failover changes `masterClusterName`, disabling replication changes `enableGlobalNamespace`).

### Implementation

Add validation in the webhook handler (`internal/webhook/v1alpha1/`). The existing webhook pattern validates on CREATE and UPDATE. On UPDATE, compare old vs new for immutability.

## Namespace Client Changes

### TemporalNamespace reconciliation

**File:** `internal/controller/temporalnamespace_controller.go`

When creating or updating a namespace, pass the `IsGlobal` flag to the gRPC `RegisterNamespace` / `UpdateNamespace` request. This maps to the `isGlobalNamespace` field in the Temporal API.

## mTLS Documentation

No CRD changes for mTLS. The existing `mtls.frontend.dnsNames` field already supports adding extra DNS names to the frontend certificate. For cross-cluster replication, users need to:

1. Add the cross-cluster reachable address to `mtls.frontend.dnsNames` so the frontend certificate includes it as a SAN
2. Ensure both clusters trust the same CA (share a cert-manager `ClusterIssuer` or root CA Secret)

This should be documented in an example YAML rather than encoded in the CRD.

## Example Usage

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: cluster-a
spec:
  version: 1.31.1
  numHistoryShards: 512
  clusterMetadata:
    enableGlobalNamespace: true
    failoverVersionIncrement: 100
    currentClusterName: "clusterA"
    initialFailoverVersion: 1
    masterClusterName: "clusterA"
  mtls:
    provider: cert-manager
    issuerRef:
      name: shared-ca-issuer
      kind: ClusterIssuer
    frontend:
      dnsNames:
        - "cluster-a.example.com"
  persistence:
    # ... standard persistence config ...
```

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalNamespace
metadata:
  name: my-global-ns
spec:
  clusterRef:
    name: cluster-a
  isGlobal: true
```

After both clusters are deployed, the user runs:

```shell
# On cluster A
temporal operator cluster upsert --frontend_address="cluster-b.example.com:7233"

# On cluster B
temporal operator cluster upsert --frontend_address="cluster-a.example.com:7233"
```

## Files Changed

| File | Change |
|------|--------|
| `api/v1alpha1/shared_types.go` | Replace `ClusterMetadataSpec` type |
| `api/v1alpha1/temporalnamespace_types.go` | Add `IsGlobal` field |
| `internal/temporal/configtemplate.go` | Wire `ClusterMetadata` fields into `BuildConfigData` |
| `internal/temporal/templates/config_template.yaml` | Use `InitialFailoverVersion`, `FailoverVersionIncrement` dynamically |
| `internal/webhook/v1alpha1/temporalcluster_webhook.go` | Add validation for `ClusterMetadata` |
| `internal/controller/temporalnamespace_controller.go` | Pass `IsGlobal` to gRPC namespace API |
| `examples/` | Add multi-cluster example YAML |
| Golden test files (`internal/temporal/testdata/`) | Update expected outputs |

## Out of Scope

- Automatic remote cluster connection (CLI `upsert` step)
- Cross-cluster CA sharing mechanism
- Failover orchestration
- `dcRedirectionPolicy` configuration (users can set via `dynamicConfig` passthrough)
- Pre-v1.14 static remote cluster config in `clusterInformation`
