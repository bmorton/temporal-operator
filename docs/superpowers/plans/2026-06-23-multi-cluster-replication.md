# Multi-Cluster Replication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable the Temporal Operator to render multi-cluster replication configuration in the `clusterMetadata` section of the Temporal server config, following the v1.14+ approach (local cluster only in config, remote connections via CLI).

**Architecture:** Replace the opaque `ClusterMetadataSpec.Raw` passthrough with typed, validated fields. Wire those fields into the config template builder so the rendered `config.yaml` reflects the CR-specified cluster metadata. Add an `IsGlobal` field to `TemporalNamespace` so users can create global namespaces. Validate immutability of replication-identity fields.

**Tech Stack:** Go, Kubebuilder CRD types, Go text/template, gRPC (Temporal API), Ginkgo/Gomega tests

## Global Constraints

- Go version pinned in `go.mod` (1.26.3)
- Supported Temporal versions: 1.27.0–1.31.1
- All CRD changes require `make generate manifests` afterward
- Commit messages follow Conventional Commits with DCO sign-off (`git commit -s`)
- Run `make lint` before completing any task
- No comments in code unless requested

---

### Task 1: Replace ClusterMetadataSpec with typed fields

**Files:**
- Modify: `api/v1alpha1/shared_types.go:205-210`
- Modify: `api/v1alpha1/temporalcluster_types.go:78-80` (comment update only)
- Regenerate: `api/v1alpha1/zz_generated.deepcopy.go` (via `make generate`)

**Interfaces:**
- Consumes: nothing (this is the first task)
- Produces: `ClusterMetadataSpec` struct with fields: `EnableGlobalNamespace bool`, `FailoverVersionIncrement *int32`, `CurrentClusterName string`, `InitialFailoverVersion *int32`, `MasterClusterName string`

- [ ] **Step 1: Write the failing test**

Add a test in `internal/webhook/v1alpha1/temporalcluster_webhook_test.go` (inside the existing Ginkgo `Context("Validation webhook on create")` block) that creates a cluster with `clusterMetadata` set and verifies the typed fields are accepted. This test will fail because the `Raw` field still exists and the typed fields don't exist yet.

However, since the CRD type change is purely structural (the old `Raw` field is never consumed), the "test" here is really the compilation itself — if we reference new fields that don't exist, it won't compile. So instead, write a unit test in the config template test file that exercises the new fields.

In `internal/temporal/configtemplate_test.go`, add a new test case to the `TestRenderConfigGolden` map:

```go
"multi-cluster": func() *temporalv1alpha1.TemporalCluster {
    c := baseCluster()
    c.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        EnableGlobalNamespace:   true,
        FailoverVersionIncrement: ptrInt32(100),
        CurrentClusterName:      "clusterA",
        InitialFailoverVersion:  ptrInt32(1),
        MasterClusterName:       "clusterA",
    }
    return c
},
```

Note: `ptrInt32` already exists in the webhook package but not in the test package. Add a helper in the test file:

```go
func ptrInt32(v int32) *int32 { return &v }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/temporal/ -run TestRenderConfigGolden/multi-cluster -v`
Expected: FAIL — `ClusterMetadataSpec` has no field `EnableGlobalNamespace`, `FailoverVersionIncrement`, etc.

- [ ] **Step 3: Replace the ClusterMetadataSpec type**

In `api/v1alpha1/shared_types.go`, replace lines 205-210:

Old:
```go
// ClusterMetadataSpec is a passthrough for multi-cluster metadata.
type ClusterMetadataSpec struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Raw *runtime.RawExtension `json:"raw,omitempty"`
}
```

New:
```go
// ClusterMetadataSpec configures multi-cluster replication.
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

Also remove the `"k8s.io/apimachinery/pkg/runtime"` import from `shared_types.go` if it becomes unused (check: `runtime` is also used by `DynamicConfigValue` and `UISpec` and `ArchivalSpec` and `AuthorizationSpec`, so it stays).

In `api/v1alpha1/temporalcluster_types.go`, update the comment on line 78:

Old:
```go
// ClusterMetadata is a passthrough for multi-cluster setup.
```

New:
```go
// ClusterMetadata configures multi-cluster replication.
```

- [ ] **Step 4: Run make generate manifests**

Run: `make generate manifests`
This regenerates `zz_generated.deepcopy.go` and the CRD manifests.

- [ ] **Step 5: Add the ptrInt32 helper and multi-cluster test case**

In `internal/temporal/configtemplate_test.go`, add the helper and test case from Step 1.

- [ ] **Step 6: Run the test to verify it compiles but golden file fails**

Run: `go test ./internal/temporal/ -run TestRenderConfigGolden/multi-cluster -v`
Expected: FAIL — golden file `testdata/golden/1.31/multi-cluster.yaml` doesn't exist yet.

- [ ] **Step 7: Update golden files**

Run: `go test ./internal/temporal/ -run TestRenderConfigGolden -update -v`
This creates `testdata/golden/1.31/multi-cluster.yaml`. The existing golden files will also be regenerated — they should still match since the defaults haven't changed.

Verify existing golden files didn't change:
Run: `git diff internal/temporal/testdata/golden/`
Expected: Only `multi-cluster.yaml` is new; no changes to existing files.

- [ ] **Step 8: Run all config template tests**

Run: `go test ./internal/temporal/ -run TestRenderConfigGolden -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add api/v1alpha1/shared_types.go api/v1alpha1/temporalcluster_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/bases/ internal/temporal/configtemplate_test.go internal/temporal/testdata/golden/1.31/multi-cluster.yaml
git commit -s -m "feat(api): replace ClusterMetadataSpec passthrough with typed replication fields"
```

---

### Task 2: Wire ClusterMetadata fields into ConfigData and BuildConfigData

**Files:**
- Modify: `internal/temporal/configtemplate.go:146-168` (ConfigData struct)
- Modify: `internal/temporal/configtemplate.go:392-426` (BuildConfigData function)
- Modify: `internal/temporal/templates/config_template.yaml:158-169` (clusterMetadata template block)

**Interfaces:**
- Consumes: `ClusterMetadataSpec` from Task 1
- Produces: `ConfigData` with `FailoverVersionIncrement int` and `InitialFailoverVersion int` fields; config template rendering dynamic `clusterMetadata`

- [ ] **Step 1: Write the failing test**

The multi-cluster golden test from Task 1 already exists. After the `BuildConfigData` changes, the rendered output for the "multi-cluster" test case should show the custom values instead of the hardcoded defaults. The golden file was generated in Task 1 with the *current* (hardcoded) output, so it will need to be regenerated after this task.

But first, let's add a targeted unit test. In `internal/temporal/configtemplate_test.go`, add:

```go
func TestBuildConfigDataClusterMetadata(t *testing.T) {
	c := baseCluster()
	c.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
		EnableGlobalNamespace:   true,
		FailoverVersionIncrement: ptrInt32(100),
		CurrentClusterName:      "clusterA",
		InitialFailoverVersion:  ptrInt32(1),
		MasterClusterName:       "clusterA",
	}
	data, err := BuildConfigData(c, BuildOptions{
		DefaultStorePassword:    "p",
		VisibilityStorePassword: "p",
	})
	if err != nil {
		t.Fatalf("BuildConfigData: %v", err)
	}
	if !data.EnableGlobalNamespace {
		t.Error("EnableGlobalNamespace should be true")
	}
	if data.FailoverVersionIncrement != 100 {
		t.Errorf("FailoverVersionIncrement = %d, want 100", data.FailoverVersionIncrement)
	}
	if data.CurrentClusterName != "clusterA" {
		t.Errorf("CurrentClusterName = %q, want %q", data.CurrentClusterName, "clusterA")
	}
	if data.InitialFailoverVersion != 1 {
		t.Errorf("InitialFailoverVersion = %d, want 1", data.InitialFailoverVersion)
	}
	if data.MasterClusterName != "clusterA" {
		t.Errorf("MasterClusterName = %q, want %q", data.MasterClusterName, "clusterA")
	}
}

func TestBuildConfigDataClusterMetadataDefaults(t *testing.T) {
	data, err := BuildConfigData(baseCluster(), BuildOptions{
		DefaultStorePassword:    "p",
		VisibilityStorePassword: "p",
	})
	if err != nil {
		t.Fatalf("BuildConfigData: %v", err)
	}
	if data.EnableGlobalNamespace != false {
		t.Error("default EnableGlobalNamespace should be false")
	}
	if data.FailoverVersionIncrement != 10 {
		t.Errorf("default FailoverVersionIncrement = %d, want 10", data.FailoverVersionIncrement)
	}
	if data.CurrentClusterName != "active" {
		t.Errorf("default CurrentClusterName = %q, want %q", data.CurrentClusterName, "active")
	}
	if data.InitialFailoverVersion != 1 {
		t.Errorf("default InitialFailoverVersion = %d, want 1", data.InitialFailoverVersion)
	}
	if data.MasterClusterName != "active" {
		t.Errorf("default MasterClusterName = %q, want %q", data.MasterClusterName, "active")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/temporal/ -run TestBuildConfigDataClusterMetadata -v`
Expected: FAIL — `ConfigData` has no `FailoverVersionIncrement` or `InitialFailoverVersion` fields.

- [ ] **Step 3: Add fields to ConfigData struct**

In `internal/temporal/configtemplate.go`, in the `ConfigData` struct (after line 164), add two fields:

```go
	FailoverVersionIncrement int
	InitialFailoverVersion   int
```

- [ ] **Step 4: Wire BuildConfigData to read from ClusterMetadata**

In `internal/temporal/configtemplate.go`, in `BuildConfigData()`, after the `data := &ConfigData{...}` block (around line 410), add:

```go
	if cm := cluster.Spec.ClusterMetadata; cm != nil {
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
```

Note: The `data := &ConfigData{...}` block currently hardcodes `CurrentClusterName: "active"` and `MasterClusterName: "active"`. Change these defaults in the struct literal:

```go
		CurrentClusterName:     "active",
		MasterClusterName:      "active",
		FailoverVersionIncrement: 10,
		InitialFailoverVersion:   1,
```

Then the `if cm := ...` block overrides them when `ClusterMetadata` is set.

- [ ] **Step 5: Update config_template.yaml to use dynamic values**

In `internal/temporal/templates/config_template.yaml`, replace lines 158-169:

Old:
```yaml
clusterMetadata:
    enableGlobalNamespace: {{ .EnableGlobalNamespace }}
    failoverVersionIncrement: 10
    masterClusterName: {{ .MasterClusterName | quote }}
    currentClusterName: {{ .CurrentClusterName | quote }}
    clusterInformation:
        {{ .CurrentClusterName }}:
            enabled: true
            initialFailoverVersion: 1
            rpcName: "frontend"
            rpcAddress: {{ printf "127.0.0.1:%d" (int .Services.frontend.GRPCPort) | quote }}
            httpAddress: {{ printf "127.0.0.1:%d" (int .Services.frontend.HTTPPort) | quote }}
```

New:
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

- [ ] **Step 6: Run the unit tests**

Run: `go test ./internal/temporal/ -run TestBuildConfigDataClusterMetadata -v`
Expected: PASS

- [ ] **Step 7: Regenerate golden files (now with dynamic clusterMetadata)**

Run: `go test ./internal/temporal/ -run TestRenderConfigGolden -update -v`

Verify that existing golden files still have the same `clusterMetadata` block (hardcoded defaults should produce identical output):
Run: `git diff internal/temporal/testdata/golden/1.31/postgres-no-mtls.yaml`
Expected: No diff (or only whitespace changes) for existing golden files. The `multi-cluster.yaml` golden file should now show the custom values.

- [ ] **Step 8: Run all config template tests**

Run: `go test ./internal/temporal/ -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/temporal/configtemplate.go internal/temporal/templates/config_template.yaml internal/temporal/configtemplate_test.go internal/temporal/testdata/golden/
git commit -s -m "feat(controller): wire ClusterMetadata fields into config template"
```

---

### Task 3: Add webhook validation for ClusterMetadata

**Files:**
- Modify: `internal/webhook/v1alpha1/temporalcluster_webhook.go:123-172` (validateSpec)
- Modify: `internal/webhook/v1alpha1/temporalcluster_webhook.go:185-225` (ValidateUpdate)
- Modify: `internal/webhook/v1alpha1/temporalcluster_webhook_test.go`

**Interfaces:**
- Consumes: `ClusterMetadataSpec` from Task 1
- Produces: validation errors when `enableGlobalNamespace: true` without required fields; immutability checks on update

- [ ] **Step 1: Write the failing test**

In `internal/webhook/v1alpha1/temporalcluster_webhook_test.go`, add new test cases inside the Ginkgo `Context("Validation webhook on create")` block:

```go
It("admits a cluster with clusterMetadata when enableGlobalNamespace is false", func() {
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        CurrentClusterName: "clusterA",
    }
    _, err := validator.ValidateCreate(ctx, obj)
    Expect(err).NotTo(HaveOccurred())
})

It("rejects enableGlobalNamespace without currentClusterName", func() {
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        EnableGlobalNamespace: true,
    }
    _, err := validator.ValidateCreate(ctx, obj)
    Expect(err).To(HaveOccurred())
})

It("rejects enableGlobalNamespace without failoverVersionIncrement", func() {
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        EnableGlobalNamespace: true,
        CurrentClusterName:    "clusterA",
    }
    _, err := validator.ValidateCreate(ctx, obj)
    Expect(err).To(HaveOccurred())
})

It("rejects enableGlobalNamespace without initialFailoverVersion", func() {
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        EnableGlobalNamespace:   true,
        CurrentClusterName:      "clusterA",
        FailoverVersionIncrement: ptrInt32(100),
    }
    _, err := validator.ValidateCreate(ctx, obj)
    Expect(err).To(HaveOccurred())
})

It("admits a valid multi-cluster configuration", func() {
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        EnableGlobalNamespace:   true,
        CurrentClusterName:      "clusterA",
        FailoverVersionIncrement: ptrInt32(100),
        InitialFailoverVersion:  ptrInt32(1),
    }
    _, err := validator.ValidateCreate(ctx, obj)
    Expect(err).NotTo(HaveOccurred())
})
```

Note: `ptrInt32` already exists in the webhook package (line 56). It's unexported so it's accessible within the same package test.

Also add immutability tests in the `Context("Validation webhook on update")` block:

```go
It("rejects changing failoverVersionIncrement", func() {
    oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        FailoverVersionIncrement: ptrInt32(100),
    }
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        FailoverVersionIncrement: ptrInt32(200),
    }
    _, err := validator.ValidateUpdate(ctx, oldObj, obj)
    Expect(err).To(HaveOccurred())
})

It("rejects changing initialFailoverVersion", func() {
    oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        InitialFailoverVersion: ptrInt32(1),
    }
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        InitialFailoverVersion: ptrInt32(2),
    }
    _, err := validator.ValidateUpdate(ctx, oldObj, obj)
    Expect(err).To(HaveOccurred())
})

It("rejects changing currentClusterName", func() {
    oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        CurrentClusterName: "clusterA",
    }
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        CurrentClusterName: "clusterB",
    }
    _, err := validator.ValidateUpdate(ctx, oldObj, obj)
    Expect(err).To(HaveOccurred())
})

It("admits changing masterClusterName on update", func() {
    oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        MasterClusterName: "clusterA",
    }
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        MasterClusterName: "clusterB",
    }
    _, err := validator.ValidateUpdate(ctx, oldObj, obj)
    Expect(err).NotTo(HaveOccurred())
})

It("admits enabling enableGlobalNamespace on update", func() {
    oldObj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{}
    obj.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
        EnableGlobalNamespace:   true,
        CurrentClusterName:      "clusterA",
        FailoverVersionIncrement: ptrInt32(100),
        InitialFailoverVersion:  ptrInt32(1),
    }
    _, err := validator.ValidateUpdate(ctx, oldObj, obj)
    Expect(err).NotTo(HaveOccurred())
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/webhook/v1alpha1/ -run "TemporalCluster" -v`
Expected: Several FAIL — validation logic doesn't exist yet.

- [ ] **Step 3: Add clusterMetadata validation to validateSpec**

In `internal/webhook/v1alpha1/temporalcluster_webhook.go`, add to the `validateSpec` method (after the dynamicConfig check, around line 169):

```go
if cm := cluster.Spec.ClusterMetadata; cm != nil && cm.EnableGlobalNamespace {
	cmPath := specPath.Child("clusterMetadata")
	if cm.CurrentClusterName == "" {
		errs = append(errs, field.Required(cmPath.Child("currentClusterName"),
			"currentClusterName is required when enableGlobalNamespace is true"))
	}
	if cm.FailoverVersionIncrement == nil || *cm.FailoverVersionIncrement < 1 {
		errs = append(errs, field.Required(cmPath.Child("failoverVersionIncrement"),
			"failoverVersionIncrement is required and must be >= 1 when enableGlobalNamespace is true"))
	}
	if cm.InitialFailoverVersion == nil || *cm.InitialFailoverVersion < 1 {
		errs = append(errs, field.Required(cmPath.Child("initialFailoverVersion"),
			"initialFailoverVersion is required and must be >= 1 when enableGlobalNamespace is true"))
	}
}
```

- [ ] **Step 4: Add immutability checks to ValidateUpdate**

In `internal/webhook/v1alpha1/temporalcluster_webhook.go`, add to the `ValidateUpdate` method (after the default store driver check, around line 219):

```go
if oldCM, newCM := oldCluster.Spec.ClusterMetadata, newCluster.Spec.ClusterMetadata; oldCM != nil && newCM != nil {
	cmPath := specPath.Child("clusterMetadata")
	if oldCM.FailoverVersionIncrement != nil && newCM.FailoverVersionIncrement != nil && *oldCM.FailoverVersionIncrement != *newCM.FailoverVersionIncrement {
		errs = append(errs, field.Invalid(cmPath.Child("failoverVersionIncrement"), newCM.FailoverVersionIncrement,
			"failoverVersionIncrement is immutable"))
	}
	if oldCM.InitialFailoverVersion != nil && newCM.InitialFailoverVersion != nil && *oldCM.InitialFailoverVersion != *newCM.InitialFailoverVersion {
		errs = append(errs, field.Invalid(cmPath.Child("initialFailoverVersion"), newCM.InitialFailoverVersion,
			"initialFailoverVersion is immutable"))
	}
	if oldCM.CurrentClusterName != "" && newCM.CurrentClusterName != "" && oldCM.CurrentClusterName != newCM.CurrentClusterName {
		errs = append(errs, field.Invalid(cmPath.Child("currentClusterName"), newCM.CurrentClusterName,
			"currentClusterName is immutable"))
	}
}
```

- [ ] **Step 5: Run the webhook tests**

Run: `go test ./internal/webhook/v1alpha1/ -run "TemporalCluster" -v`
Expected: PASS

- [ ] **Step 6: Run the full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/webhook/v1alpha1/temporalcluster_webhook.go internal/webhook/v1alpha1/temporalcluster_webhook_test.go
git commit -s -m "feat(webhook): add ClusterMetadata validation and immutability checks"
```

---

### Task 4: Add IsGlobal field to TemporalNamespace and wire it to the gRPC API

**Files:**
- Modify: `api/v1alpha1/temporalnamespace_types.go:24-53` (TemporalNamespaceSpec)
- Modify: `internal/temporal/client.go:44-49` (NamespaceParams)
- Modify: `internal/temporal/client.go:96-138` (Register + Update methods)
- Modify: `internal/controller/temporalnamespace_controller.go:187-198` (namespaceParams)
- Regenerate: `api/v1alpha1/zz_generated.deepcopy.go` (via `make generate`)

**Interfaces:**
- Consumes: `TemporalNamespaceSpec.IsGlobal` (new field)
- Produces: `NamespaceParams.IsGlobal` passed to `Register`/`Update` gRPC calls

- [ ] **Step 1: Write the failing test**

In `internal/controller/temporalnamespace_controller.go` test (if exists) or add a simple unit test. Since the namespace controller uses envtest/Ginkgo, let's test at the gRPC client level. In `internal/temporal/client.go`, the `NamespaceParams` struct needs an `IsGlobal` field.

Add a test in a new file `internal/temporal/client_test.go`:

```go
package temporal

import (
	"testing"
)

func TestNamespaceParamsIsGlobal(t *testing.T) {
	params := NamespaceParams{
		Name:            "test",
		Description:     "desc",
		OwnerEmail:      "owner@example.com",
		RetentionPeriod: 72 * 60 * 60 * 1e9,
		IsGlobal:        true,
	}
	if !params.IsGlobal {
		t.Error("IsGlobal should be true")
	}
}
```

Run: `go test ./internal/temporal/ -run TestNamespaceParamsIsGlobal -v`
Expected: FAIL — `NamespaceParams` has no `IsGlobal` field.

- [ ] **Step 2: Add IsGlobal to TemporalNamespaceSpec**

In `api/v1alpha1/temporalnamespace_types.go`, add after the `DriftDetection` field (around line 52):

```go
	// IsGlobal marks the namespace as global for multi-cluster replication.
	// +optional
	IsGlobal bool `json:"isGlobal,omitempty"`
```

- [ ] **Step 3: Run make generate manifests**

Run: `make generate manifests`

- [ ] **Step 4: Add IsGlobal to NamespaceParams**

In `internal/temporal/client.go`, add to the `NamespaceParams` struct (after `RetentionPeriod`):

```go
	IsGlobal bool
```

- [ ] **Step 5: Wire IsGlobal into Register gRPC call**

In `internal/temporal/client.go`, modify the `Register` method. The `RegisterNamespaceRequest` has a `IsGlobalNamespace` field:

```go
func (c *grpcNamespaceClient) Register(ctx context.Context, params NamespaceParams) error {
	_, err := c.workflow.RegisterNamespace(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        params.Name,
		Description:                      params.Description,
		OwnerEmail:                       params.OwnerEmail,
		WorkflowExecutionRetentionPeriod: durationpb.New(params.RetentionPeriod),
		IsGlobalNamespace:                params.IsGlobal,
	})
	return err
}
```

- [ ] **Step 6: Wire IsGlobal into Update gRPC call**

In `internal/temporal/client.go`, modify the `Update` method. The `UpdateNamespaceRequest`'s `UpdateInfo` has no `IsGlobalNamespace` field — it's only set on register. So no change needed for `Update`.

- [ ] **Step 7: Wire IsGlobal into namespaceParams helper**

In `internal/controller/temporalnamespace_controller.go`, modify the `namespaceParams` function:

Old:
```go
func namespaceParams(ns *temporalv1alpha1.TemporalNamespace) temporal.NamespaceParams {
	retention := 72 * time.Hour
	if ns.Spec.RetentionPeriod != nil {
		retention = ns.Spec.RetentionPeriod.Duration
	}
	return temporal.NamespaceParams{
		Name:            ns.Name,
		Description:     ns.Spec.Description,
		OwnerEmail:      ns.Spec.OwnerEmail,
		RetentionPeriod: retention,
	}
}
```

New:
```go
func namespaceParams(ns *temporalv1alpha1.TemporalNamespace) temporal.NamespaceParams {
	retention := 72 * time.Hour
	if ns.Spec.RetentionPeriod != nil {
		retention = ns.Spec.RetentionPeriod.Duration
	}
	return temporal.NamespaceParams{
		Name:            ns.Name,
		Description:     ns.Spec.Description,
		OwnerEmail:      ns.Spec.OwnerEmail,
		RetentionPeriod: retention,
		IsGlobal:        ns.Spec.IsGlobal,
	}
}
```

- [ ] **Step 8: Run the client test**

Run: `go test ./internal/temporal/ -run TestNamespaceParamsIsGlobal -v`
Expected: PASS

- [ ] **Step 9: Run the full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add api/v1alpha1/temporalnamespace_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/bases/ internal/temporal/client.go internal/controller/temporalnamespace_controller.go internal/temporal/client_test.go
git commit -s -m "feat(namespace): add isGlobal field for multi-cluster replication namespaces"
```

---

### Task 5: Add multi-cluster example and update documentation

**Files:**
- Create: `examples/multi-cluster/cluster-a.yaml`
- Create: `examples/multi-cluster/cluster-b.yaml`
- Create: `examples/multi-cluster/global-namespace.yaml`

**Interfaces:**
- Consumes: All typed fields from Tasks 1-4
- Produces: Example YAMLs for users

- [ ] **Step 1: Create cluster-a.yaml**

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
    defaultStore:
      sql:
        pluginName: postgres12
        host: postgres.default.svc
        port: 5432
        database: temporal
        user: temporal
        passwordSecretRef:
          name: temporal-pg
          key: password
    visibilityStore:
      sql:
        pluginName: postgres12
        host: postgres.default.svc
        port: 5432
        database: temporal_visibility
        user: temporal
        passwordSecretRef:
          name: temporal-pg
          key: password
```

- [ ] **Step 2: Create cluster-b.yaml**

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: cluster-b
spec:
  version: 1.31.1
  numHistoryShards: 512
  clusterMetadata:
    enableGlobalNamespace: true
    failoverVersionIncrement: 100
    currentClusterName: "clusterB"
    initialFailoverVersion: 2
    masterClusterName: "clusterA"
  mtls:
    provider: cert-manager
    issuerRef:
      name: shared-ca-issuer
      kind: ClusterIssuer
    frontend:
      dnsNames:
        - "cluster-b.example.com"
  persistence:
    defaultStore:
      sql:
        pluginName: postgres12
        host: postgres.default.svc
        port: 5432
        database: temporal
        user: temporal
        passwordSecretRef:
          name: temporal-pg
          key: password
    visibilityStore:
      sql:
        pluginName: postgres12
        host: postgres.default.svc
        port: 5432
        database: temporal_visibility
        user: temporal
        passwordSecretRef:
          name: temporal-pg
          key: password
```

- [ ] **Step 3: Create global-namespace.yaml**

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalNamespace
metadata:
  name: my-global-ns
spec:
  clusterRef:
    name: cluster-a
  isGlobal: true
  retentionPeriod: 72h
```

- [ ] **Step 4: Verify the examples are valid YAML**

Run: `for f in examples/multi-cluster/*.yaml; do kubeval "$f" || echo "SKIP: $f (CRD not installed)"; done`
Or simply check syntax: `for f in examples/multi-cluster/*.yaml; do python3 -c "import yaml; yaml.safe_load(open('$f'))" && echo "OK: $f"; done`

- [ ] **Step 5: Commit**

```bash
git add examples/multi-cluster/
git commit -s -m "docs: add multi-cluster replication example"
```

---

### Task 6: Lint and final verification

**Files:**
- Potentially all modified files (lint fixes)

**Interfaces:**
- Consumes: All changes from Tasks 1-5
- Produces: Clean lint pass

- [ ] **Step 1: Run make generate manifests**

Run: `make generate manifests`
Ensure all generated code is up to date.

- [ ] **Step 2: Run make build**

Run: `make build`
Expected: PASS — code compiles cleanly.

- [ ] **Step 3: Run make test**

Run: `make test`
Expected: PASS — all unit + envtest suites pass.

- [ ] **Step 4: Run make lint**

Run: `make lint`
Expected: PASS — no lint errors.

If lint fails, fix the issues and re-run.

- [ ] **Step 5: Commit any lint fixes**

```bash
git add -A
git commit -s -m "chore: lint fixes for multi-cluster replication"
```
(Only if there are lint fixes to commit.)
