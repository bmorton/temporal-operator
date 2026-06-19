# Temporal Upgrade/Migration Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `TemporalMigration` CRD plus a managed gRPC "director" proxy that lets users gradually shift traffic from an external Temporal cluster to an operator-managed one via drain-based cutover (try-new-then-fallback routing).

**Architecture:** A `TemporalMigration` CR provisions a standalone proxy Deployment + Service. While `spec.cutover=false` the proxy is a 100% passthrough to the external source cluster. When flipped to `true`, new workflow starts route to the target cluster while operations on existing workflows try the target and fall back to the source on `NotFound`. The controller watches the source cluster's running-workflow count (control-plane visibility query) and reports drain progress until the migration is Complete. Teardown is manual (delete the CR; owner-ref GC removes the proxy).

**Tech Stack:** Go, Kubebuilder/controller-runtime v0.23, `go.temporal.io/api` (raw gRPC `WorkflowService`/`OperatorService` protos), grpc-go, cert-manager, envtest for controller tests.

**Spec:** `docs/superpowers/specs/2026-06-18-temporal-upgrade-proxy-design.md`

---

## Conventions (apply to every new Go file)

- Start every new `.go` file with the Apache license header used across the repo (copy the 16-line block from the top of `api/v1alpha1/temporalschedule_types.go`, copyright "Brian Morton").
- Module path: `github.com/bmorton/temporal-operator`. API group: `temporal.bmor10.com`.
- After changing API types run `make generate manifests`. Build with `make build`. Test with `make test` (envtest — do NOT use bare `go test ./...` for controller/envtest packages). Pure packages (`internal/proxy/...`) can use `go test ./internal/proxy/...`. Lint with `make lint`.
- Commits: Conventional Commits, signed off (`git commit -s`), include the `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>` trailer.

## File structure

| File | Responsibility |
|---|---|
| `api/v1alpha1/temporalmigration_types.go` | CRD spec/status types + kubebuilder markers + `init()` registration |
| `api/v1alpha1/conditions.go` (modify) | New condition type/reason constants |
| `api/v1alpha1/zz_generated.deepcopy.go` (generated) | deepcopy for new types |
| `internal/proxy/codec.go` | Raw passthrough gRPC codec (forwards bytes, no proto types) |
| `internal/proxy/classify.go` | Pure method classification (start / existing-op / poll / passthrough) |
| `internal/proxy/director.go` | Routing decision: given mode + method → primary/secondary backend |
| `internal/proxy/handler.go` | gRPC `UnknownServiceHandler`: unary forward + NotFound fallback + stream passthrough |
| `internal/proxy/config.go` | Proxy runtime config struct + file loader |
| `internal/proxy/server.go` | Wires gRPC server with raw codec + handler + backend dials |
| `cmd/migration-proxy/main.go` | Proxy binary entrypoint |
| `internal/resources/migrationproxy.go` | Builders: proxy Deployment, Service, ConfigMap |
| `internal/temporal/migration.go` | `MigrationClient`: ListNamespaces + CountRunningWorkflows (visibility) |
| `internal/controller/temporalmigration_controller.go` | Reconcile: provision proxy, phase machine, drain detection, status |
| `internal/controller/temporalmigration_controller_test.go` | envtest controller tests |
| `cmd/main.go` (modify) | Register the new reconciler |
| `Dockerfile` (modify) | Build + ship the second `migration-proxy` binary |
| `Makefile` (modify) | `build` compiles both binaries |
| `config/samples/` + `examples/` | Sample `TemporalMigration` CR |
| `docs/` | User-facing migration guide |

---

## Task 1: CRD API types

**Files:**
- Create: `api/v1alpha1/temporalmigration_types.go`
- Test: `api/v1alpha1/temporalmigration_types_test.go`

- [ ] **Step 1: Write a failing test for default helpers**

Create `api/v1alpha1/temporalmigration_types_test.go` (Apache header + package `v1alpha1`):

```go
package v1alpha1

import "testing"

func TestNamespaceMappingTargetOrSource(t *testing.T) {
	m := NamespaceMapping{Source: "orders"}
	if got := m.TargetOrSource(); got != "orders" {
		t.Fatalf("default target = %q, want orders", got)
	}
	m.Target = "orders-new"
	if got := m.TargetOrSource(); got != "orders-new" {
		t.Fatalf("explicit target = %q, want orders-new", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./api/v1alpha1/ -run TestNamespaceMappingTargetOrSource`
Expected: FAIL — `NamespaceMapping` / `TargetOrSource` undefined.

- [ ] **Step 3: Create the types file**

Create `api/v1alpha1/temporalmigration_types.go` (Apache header, package `v1alpha1`):

```go
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemporalMigrationSpec defines a migration from an external source Temporal
// cluster to an operator-managed target TemporalCluster via a managed proxy.
type TemporalMigrationSpec struct {
	// Source describes the EXTERNAL Temporal cluster being migrated away from.
	Source SourceClusterSpec `json:"source"`

	// TargetRef references the operator-managed TemporalCluster to migrate to.
	TargetRef corev1.LocalObjectReference `json:"targetRef"`

	// Namespaces to migrate. Empty means all namespaces present on the source.
	// +optional
	Namespaces []NamespaceMapping `json:"namespaces,omitempty"`

	// Cutover is the manual gate. false keeps the proxy in passthrough mode
	// (100% to source). true routes new starts to the target and falls back to
	// the source for operations on existing workflows.
	// +optional
	Cutover bool `json:"cutover,omitempty"`

	// Proxy tunes the provisioned proxy Deployment.
	// +optional
	Proxy *ProxySpec `json:"proxy,omitempty"`
}

// SourceClusterSpec describes how to reach the external source frontend.
type SourceClusterSpec struct {
	// Address is the source frontend host:port (e.g. "old-temporal:7233").
	Address string `json:"address"`

	// TLS configures how the proxy connects to the source.
	// +optional
	TLS *SourceTLSSpec `json:"tls,omitempty"`
}

// SourceTLSSpec configures TLS/mTLS from the proxy to the source frontend.
type SourceTLSSpec struct {
	// Enabled turns on TLS to the source frontend.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// SecretRef holds ca.crt (and optional tls.crt/tls.key for client mTLS).
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`

	// ServerName overrides SNI / certificate verification name.
	// +optional
	ServerName string `json:"serverName,omitempty"`
}

// NamespaceMapping maps a source namespace to a target namespace.
type NamespaceMapping struct {
	Source string `json:"source"`
	// +optional
	Target string `json:"target,omitempty"`
}

// TargetOrSource returns the target namespace, defaulting to the source name.
func (m NamespaceMapping) TargetOrSource() string {
	if m.Target != "" {
		return m.Target
	}
	return m.Source
}

// ProxySpec tunes the provisioned proxy Deployment.
type ProxySpec struct {
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Image overrides the proxy image. Defaults to the operator image.
	// +optional
	Image string `json:"image,omitempty"`
}

// TemporalMigrationStatus is the observed state of a TemporalMigration.
type TemporalMigrationStatus struct {
	// Phase is one of Pending, Passthrough, Cutover, Draining, Complete.
	// +optional
	Phase string `json:"phase,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ProxyEndpoint is the Service address clients should target.
	// +optional
	ProxyEndpoint string `json:"proxyEndpoint,omitempty"`

	// Draining reports per-namespace source running-workflow counts.
	// +optional
	Draining []NamespaceDrainStatus `json:"draining,omitempty"`

	// CutoverTime records when cutover was first enabled.
	// +optional
	CutoverTime *metav1.Time `json:"cutoverTime,omitempty"`
}

// NamespaceDrainStatus reports drain progress for one source namespace.
type NamespaceDrainStatus struct {
	Namespace string `json:"namespace"`
	// +optional
	SourceRunningWorkflows int64 `json:"sourceRunningWorkflows"`
	// +optional
	Drained bool `json:"drained"`
}

// Migration phase constants.
const (
	MigrationPhasePending     = "Pending"
	MigrationPhasePassthrough = "Passthrough"
	MigrationPhaseCutover     = "Cutover"
	MigrationPhaseDraining    = "Draining"
	MigrationPhaseComplete    = "Complete"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tm
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Cutover",type=boolean,JSONPath=`.spec.cutover`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.proxyEndpoint`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalMigration is the Schema for the temporalmigrations API.
type TemporalMigration struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec TemporalMigrationSpec `json:"spec"`
	// +optional
	Status TemporalMigrationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalMigrationList contains a list of TemporalMigration.
type TemporalMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalMigration `json:"items"`
}

func init() {
	registerType(&TemporalMigration{}, &TemporalMigrationList{})
}
```

- [ ] **Step 4: Generate deepcopy + run the test**

Run: `make generate && go test ./api/v1alpha1/ -run TestNamespaceMappingTargetOrSource`
Expected: deepcopy regenerates without error; test PASSES.

- [ ] **Step 5: Commit**

```bash
git add api/v1alpha1/temporalmigration_types.go api/v1alpha1/temporalmigration_types_test.go api/v1alpha1/zz_generated.deepcopy.go
git commit -s -m "feat(api): add TemporalMigration CRD types"
```

---

## Task 2: Migration condition constants + CRD manifests

**Files:**
- Modify: `api/v1alpha1/conditions.go`

- [ ] **Step 1: Add condition constants**

In `api/v1alpha1/conditions.go`, add to the condition-types `const` block:

```go
	// ConditionProxyReady indicates the migration proxy is provisioned and serving.
	ConditionProxyReady = "ProxyReady"
	// ConditionSourceReachable indicates the source frontend is reachable.
	ConditionSourceReachable = "SourceReachable"
	// ConditionTargetReachable indicates the target frontend is reachable.
	ConditionTargetReachable = "TargetReachable"
	// ConditionMigrationDraining indicates the source cluster is draining.
	ConditionMigrationDraining = "MigrationDraining"
	// ConditionMigrationComplete indicates the source cluster has fully drained.
	ConditionMigrationComplete = "MigrationComplete"
```

And to the reasons `const` block:

```go
	// ReasonProxyProvisioning indicates the proxy Deployment is being created.
	ReasonProxyProvisioning = "ProxyProvisioning"
	// ReasonPassthrough indicates the proxy is forwarding all traffic to the source.
	ReasonPassthrough = "Passthrough"
	// ReasonCutoverEnabled indicates new workflows route to the target.
	ReasonCutoverEnabled = "CutoverEnabled"
	// ReasonDraining indicates the source still has running workflows.
	ReasonDraining = "Draining"
	// ReasonDrained indicates the source has no running workflows.
	ReasonDrained = "Drained"
	// ReasonUnreachable indicates a frontend could not be reached.
	ReasonUnreachable = "Unreachable"
```

- [ ] **Step 2: Generate manifests**

Run: `make manifests`
Expected: a new CRD file `config/crd/bases/temporal.bmor10.com_temporalmigrations.yaml` is created.

- [ ] **Step 3: Verify build compiles**

Run: `make build`
Expected: compiles clean.

- [ ] **Step 4: Commit**

```bash
git add api/v1alpha1/conditions.go config/crd/bases/temporal.bmor10.com_temporalmigrations.yaml config/crd/kustomization.yaml
git commit -s -m "feat(api): add migration conditions and generate TemporalMigration CRD"
```

---

## Task 3: Raw passthrough gRPC codec

The proxy must forward arbitrary `WorkflowService`/`OperatorService` methods without knowing their proto types. A raw codec treats every message as opaque bytes.

**Files:**
- Create: `internal/proxy/codec.go`
- Test: `internal/proxy/codec_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/proxy/codec_test.go` (Apache header, package `proxy`):

```go
package proxy

import (
	"bytes"
	"testing"
)

func TestRawCodecRoundTrip(t *testing.T) {
	c := RawCodec{}
	in := &Frame{Payload: []byte("hello-temporal")}
	b, err := c.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := &Frame{}
	if err := c.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("payload = %q, want %q", out.Payload, in.Payload)
	}
	if c.Name() != "proxy-raw" {
		t.Fatalf("name = %q, want proxy-raw", c.Name())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/ -run TestRawCodecRoundTrip`
Expected: FAIL — `RawCodec` / `Frame` undefined.

- [ ] **Step 3: Implement the codec**

Create `internal/proxy/codec.go` (Apache header, package `proxy`):

```go
package proxy

import (
	"fmt"

	"google.golang.org/grpc/encoding"
)

// CodecName is the registered name of the raw passthrough codec.
const CodecName = "proxy-raw"

// Frame is an opaque gRPC message: the raw wire bytes, never deserialized.
type Frame struct {
	Payload []byte
}

// RawCodec forwards message bytes verbatim so the proxy can relay any method
// without depending on its protobuf types.
type RawCodec struct{}

func (RawCodec) Marshal(v any) ([]byte, error) {
	f, ok := v.(*Frame)
	if !ok {
		return nil, fmt.Errorf("proxy: RawCodec expects *Frame, got %T", v)
	}
	return f.Payload, nil
}

func (RawCodec) Unmarshal(data []byte, v any) error {
	f, ok := v.(*Frame)
	if !ok {
		return fmt.Errorf("proxy: RawCodec expects *Frame, got %T", v)
	}
	f.Payload = data
	return nil
}

func (RawCodec) Name() string { return CodecName }

func init() {
	encoding.RegisterCodec(RawCodec{})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/proxy/ -run TestRawCodecRoundTrip`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/codec.go internal/proxy/codec_test.go
git commit -s -m "feat(proxy): add raw passthrough gRPC codec"
```

---

## Task 4: Method classification

Classify each gRPC full-method name into a routing class. This is the only Temporal-specific knowledge in the data plane and is a pure function.

**Files:**
- Create: `internal/proxy/classify.go`
- Test: `internal/proxy/classify_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/proxy/classify_test.go` (Apache header, package `proxy`):

```go
package proxy

import "testing"

func TestClassify(t *testing.T) {
	cases := map[string]MethodClass{
		"/temporal.api.workflowservice.v1.WorkflowService/StartWorkflowExecution":           ClassStart,
		"/temporal.api.workflowservice.v1.WorkflowService/SignalWithStartWorkflowExecution":  ClassStart,
		"/temporal.api.workflowservice.v1.WorkflowService/SignalWorkflowExecution":           ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/QueryWorkflow":                     ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/DescribeWorkflowExecution":         ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/TerminateWorkflowExecution":        ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/RequestCancelWorkflowExecution":    ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/GetWorkflowExecutionHistory":       ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/PollWorkflowTaskQueue":             ClassPoll,
		"/temporal.api.workflowservice.v1.WorkflowService/PollActivityTaskQueue":             ClassPoll,
		"/temporal.api.workflowservice.v1.WorkflowService/RegisterNamespace":                 ClassPassthrough,
		"/grpc.health.v1.Health/Check":                                                       ClassPassthrough,
	}
	for method, want := range cases {
		if got := Classify(method); got != want {
			t.Errorf("Classify(%q) = %v, want %v", method, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/ -run TestClassify`
Expected: FAIL — `Classify`/`MethodClass` undefined.

- [ ] **Step 3: Implement classification**

Create `internal/proxy/classify.go` (Apache header, package `proxy`):

```go
package proxy

import "strings"

// MethodClass is the routing category of a gRPC method.
type MethodClass int

const (
	// ClassPassthrough routes to the active default backend with no fallback
	// (cluster-wide reads/writes, namespace ops, health, OperatorService).
	ClassPassthrough MethodClass = iota
	// ClassStart begins a new workflow and routes to the target on cutover.
	ClassStart
	// ClassExisting operates on an existing workflow: try target, fall back to source.
	ClassExisting
	// ClassPoll is a worker task-queue poll: routes to the source for the alpha.
	ClassPoll
)

// startMethods begin a new workflow execution.
var startMethods = map[string]struct{}{
	"StartWorkflowExecution":          {},
	"SignalWithStartWorkflowExecution": {},
	"ExecuteMultiOperation":            {},
}

// existingMethods operate on an already-running workflow execution.
var existingMethods = map[string]struct{}{
	"SignalWorkflowExecution":              {},
	"QueryWorkflow":                        {},
	"DescribeWorkflowExecution":            {},
	"TerminateWorkflowExecution":           {},
	"RequestCancelWorkflowExecution":       {},
	"ResetWorkflowExecution":               {},
	"GetWorkflowExecutionHistory":          {},
	"GetWorkflowExecutionHistoryReverse":   {},
	"UpdateWorkflowExecution":              {},
	"PollWorkflowExecutionUpdate":          {},
}

// pollMethods are worker long-poll task-queue reads.
var pollMethods = map[string]struct{}{
	"PollWorkflowTaskQueue":  {},
	"PollActivityTaskQueue":  {},
	"PollNexusTaskQueue":     {},
}

// Classify maps a gRPC full-method name to its routing class. Only the
// WorkflowService is specially handled; everything else is passthrough.
func Classify(fullMethod string) MethodClass {
	const wf = "/temporal.api.workflowservice.v1.WorkflowService/"
	if !strings.HasPrefix(fullMethod, wf) {
		return ClassPassthrough
	}
	name := strings.TrimPrefix(fullMethod, wf)
	if _, ok := startMethods[name]; ok {
		return ClassStart
	}
	if _, ok := existingMethods[name]; ok {
		return ClassExisting
	}
	if _, ok := pollMethods[name]; ok {
		return ClassPoll
	}
	return ClassPassthrough
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/proxy/ -run TestClassify`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/classify.go internal/proxy/classify_test.go
git commit -s -m "feat(proxy): add gRPC method classification"
```

---

## Task 5: Director — routing decision

Given the current mode and a method class, decide the primary backend and the optional fallback backend. Pure logic over two backend handles, unit-tested without gRPC.

**Files:**
- Create: `internal/proxy/director.go`
- Test: `internal/proxy/director_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/proxy/director_test.go` (Apache header, package `proxy`):

```go
package proxy

import "testing"

func TestDirectorRoute(t *testing.T) {
	d := Director{Mode: ModePassthrough}
	// Passthrough: everything to source, no fallback.
	for _, c := range []MethodClass{ClassStart, ClassExisting, ClassPoll, ClassPassthrough} {
		primary, secondary := d.Route(c)
		if primary != BackendSource || secondary != BackendNone {
			t.Fatalf("passthrough class %v -> (%v,%v), want (source,none)", c, primary, secondary)
		}
	}

	d.Mode = ModeCutover
	tests := []struct {
		class             MethodClass
		primary, fallback Backend
	}{
		{ClassStart, BackendTarget, BackendNone},
		{ClassExisting, BackendTarget, BackendSource},
		{ClassPoll, BackendSource, BackendNone},
		{ClassPassthrough, BackendTarget, BackendNone},
	}
	for _, tt := range tests {
		primary, secondary := d.Route(tt.class)
		if primary != tt.primary || secondary != tt.fallback {
			t.Errorf("cutover class %v -> (%v,%v), want (%v,%v)", tt.class, primary, secondary, tt.primary, tt.fallback)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/ -run TestDirectorRoute`
Expected: FAIL — `Director`/`Mode`/`Backend` undefined.

- [ ] **Step 3: Implement the director**

Create `internal/proxy/director.go` (Apache header, package `proxy`):

```go
package proxy

// Mode is the proxy's current routing mode.
type Mode string

const (
	// ModePassthrough forwards all traffic to the source cluster.
	ModePassthrough Mode = "passthrough"
	// ModeCutover routes new workflows to the target with source fallback.
	ModeCutover Mode = "cutover"
)

// Backend identifies a routing destination.
type Backend int

const (
	// BackendNone means no (fallback) backend.
	BackendNone Backend = iota
	// BackendSource is the external source cluster.
	BackendSource
	// BackendTarget is the operator-managed target cluster.
	BackendTarget
)

// Director decides routing based on the current mode.
type Director struct {
	Mode Mode
}

// Route returns the primary backend and an optional fallback backend
// (BackendNone when there is no fallback) for a method class.
func (d Director) Route(class MethodClass) (primary, fallback Backend) {
	if d.Mode == ModePassthrough {
		return BackendSource, BackendNone
	}
	// Cutover mode.
	switch class {
	case ClassStart:
		return BackendTarget, BackendNone
	case ClassExisting:
		return BackendTarget, BackendSource
	case ClassPoll:
		return BackendSource, BackendNone
	default: // ClassPassthrough
		return BackendTarget, BackendNone
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/proxy/ -run TestDirectorRoute`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/director.go internal/proxy/director_test.go
git commit -s -m "feat(proxy): add routing director"
```

---

## Task 6: Proxy handler — unary forward + NotFound fallback + stream passthrough

A single gRPC `UnknownServiceHandler` relays every method. Unary calls are buffered (one request frame), invoked against the primary backend, and on `codes.NotFound` retried against the fallback. Non-unary methods are piped transparently to the primary backend (no fallback). gRPC headers/trailers are propagated.

**Files:**
- Create: `internal/proxy/handler.go`
- Test: `internal/proxy/handler_test.go`

- [ ] **Step 1: Write the failing test (table-driven, using in-process backends)**

Create `internal/proxy/handler_test.go` (Apache header, package `proxy`). It stands up two in-process gRPC servers that implement a tiny test service via the raw codec, then drives the handler:

```go
package proxy

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeBackend echoes its name, or returns NotFound when notFound is true.
type fakeBackend struct {
	name     string
	notFound bool
	lastCall string
}

func (b *fakeBackend) handler(srv any, stream grpc.ServerStream) error {
	var in Frame
	if err := stream.RecvMsg(&in); err != nil {
		return err
	}
	if b.notFound {
		return status.Error(codes.NotFound, "workflow not found")
	}
	return stream.SendMsg(&Frame{Payload: []byte(b.name)})
}

func startFake(t *testing.T, b *fakeBackend) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.ForceServerCodec(RawCodec{}), grpc.UnknownServiceHandler(b.handler))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithInsecure())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestHandlerFallbackOnNotFound(t *testing.T) {
	target := &fakeBackend{name: "target", notFound: true}
	source := &fakeBackend{name: "source"}
	h := &Handler{
		Director: Director{Mode: ModeCutover},
		Source:   startFake(t, source),
		Target:   startFake(t, target),
	}
	// Drive an existing-workflow unary method through the proxy server.
	resp := callThroughProxy(t, h, "/temporal.api.workflowservice.v1.WorkflowService/SignalWorkflowExecution", []byte("req"))
	if string(resp) != "source" {
		t.Fatalf("response = %q, want source (fallback)", resp)
	}
}

func TestHandlerStartGoesToTarget(t *testing.T) {
	target := &fakeBackend{name: "target"}
	source := &fakeBackend{name: "source"}
	h := &Handler{Director: Director{Mode: ModeCutover}, Source: startFake(t, source), Target: startFake(t, target)}
	resp := callThroughProxy(t, h, "/temporal.api.workflowservice.v1.WorkflowService/StartWorkflowExecution", []byte("req"))
	if string(resp) != "target" {
		t.Fatalf("response = %q, want target", resp)
	}
}

func TestHandlerPassthroughGoesToSource(t *testing.T) {
	target := &fakeBackend{name: "target"}
	source := &fakeBackend{name: "source"}
	h := &Handler{Director: Director{Mode: ModePassthrough}, Source: startFake(t, source), Target: startFake(t, target)}
	resp := callThroughProxy(t, h, "/temporal.api.workflowservice.v1.WorkflowService/StartWorkflowExecution", []byte("req"))
	if string(resp) != "source" {
		t.Fatalf("response = %q, want source", resp)
	}
}

// callThroughProxy runs the proxy handler as a server and makes one raw unary call.
func callThroughProxy(t *testing.T, h *Handler, method string, req []byte) []byte {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.ForceServerCodec(RawCodec{}), grpc.UnknownServiceHandler(h.Stream))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithInsecure())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	out := &Frame{}
	if err := conn.Invoke(context.Background(), method, &Frame{Payload: req}, out, grpc.ForceCodec(RawCodec{})); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	return out.Payload
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/ -run TestHandler`
Expected: FAIL — `Handler` undefined.

- [ ] **Step 3: Implement the handler**

Create `internal/proxy/handler.go` (Apache header, package `proxy`):

```go
package proxy

import (
	"context"
	"io"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Handler relays gRPC calls to the source/target backends per the Director.
type Handler struct {
	mu       sync.RWMutex
	Director Director
	Source   *grpc.ClientConn
	Target   *grpc.ClientConn
}

// SetMode atomically updates the routing mode (used on config reload).
func (h *Handler) SetMode(m Mode) {
	h.mu.Lock()
	h.Director.Mode = m
	h.mu.Unlock()
}

func (h *Handler) conn(b Backend) *grpc.ClientConn {
	switch b {
	case BackendSource:
		return h.Source
	case BackendTarget:
		return h.Target
	default:
		return nil
	}
}

// Stream is the gRPC UnknownServiceHandler entrypoint.
func (h *Handler) Stream(_ any, stream grpc.ServerStream) error {
	method, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Error(codes.Internal, "proxy: no method in stream")
	}
	h.mu.RLock()
	director := h.Director
	h.mu.RUnlock()

	class := Classify(method)
	primary, fallback := director.Route(class)

	// Poll and any non-unary/streaming method: transparent single-backend pipe.
	if class == ClassPoll {
		return h.pipe(stream, h.conn(primary), method)
	}
	return h.unary(stream, method, primary, fallback)
}

// unary buffers one request frame, invokes the primary backend, and retries the
// fallback backend on NotFound.
func (h *Handler) unary(stream grpc.ServerStream, method string, primary, fallback Backend) error {
	var req Frame
	if err := stream.RecvMsg(&req); err != nil {
		if err == io.EOF {
			return status.Error(codes.Internal, "proxy: empty request")
		}
		return err
	}
	outCtx := forwardContext(stream.Context())

	resp, header, trailer, err := invoke(outCtx, h.conn(primary), method, &req)
	if err != nil && fallback != BackendNone && status.Code(err) == codes.NotFound {
		resp, header, trailer, err = invoke(outCtx, h.conn(fallback), method, &req)
	}
	if len(header) > 0 {
		_ = stream.SetHeader(header)
	}
	if len(trailer) > 0 {
		stream.SetTrailer(trailer)
	}
	if err != nil {
		return err
	}
	return stream.SendMsg(resp)
}

func invoke(ctx context.Context, conn *grpc.ClientConn, method string, req *Frame) (*Frame, metadata.MD, metadata.MD, error) {
	resp := &Frame{}
	var header, trailer metadata.MD
	err := conn.Invoke(ctx, method, req, resp,
		grpc.ForceCodec(RawCodec{}), grpc.Header(&header), grpc.Trailer(&trailer))
	return resp, header, trailer, err
}

// pipe transparently relays a (possibly streaming) call to a single backend.
func (h *Handler) pipe(stream grpc.ServerStream, conn *grpc.ClientConn, method string) error {
	outCtx := forwardContext(stream.Context())
	clientStream, err := conn.NewStream(outCtx,
		&grpc.StreamDesc{ServerStreams: true, ClientStreams: true}, method, grpc.ForceCodec(RawCodec{}))
	if err != nil {
		return err
	}
	errc := make(chan error, 2)
	// client -> backend
	go func() {
		for {
			var f Frame
			if err := stream.RecvMsg(&f); err != nil {
				_ = clientStream.CloseSend()
				errc <- err
				return
			}
			if err := clientStream.SendMsg(&f); err != nil {
				errc <- err
				return
			}
		}
	}()
	// backend -> client
	go func() {
		for {
			var f Frame
			if err := clientStream.RecvMsg(&f); err != nil {
				errc <- err
				return
			}
			if err := stream.SendMsg(&f); err != nil {
				errc <- err
				return
			}
		}
	}()
	err = <-errc
	if err == io.EOF {
		if md, mdErr := clientStream.Header(); mdErr == nil {
			_ = stream.SetHeader(md)
		}
		stream.SetTrailer(clientStream.Trailer())
		return nil
	}
	return err
}

// forwardContext copies inbound metadata onto an outbound context.
func forwardContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	return metadata.NewOutgoingContext(ctx, md.Copy())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/proxy/ -run TestHandler -v`
Expected: all three handler tests PASS.

- [ ] **Step 5: Run the whole proxy package + lint**

Run: `go test ./internal/proxy/ && make lint`
Expected: PASS / clean.

- [ ] **Step 6: Commit**

```bash
git add internal/proxy/handler.go internal/proxy/handler_test.go
git commit -s -m "feat(proxy): add relay handler with NotFound fallback"
```

---

## Task 7: Proxy config + server wiring

The proxy reads a config file (mounted from a ConfigMap) describing the mode and both backends, dials them (with optional TLS to the source and mTLS to the target), and serves gRPC with the raw codec + handler.

**Files:**
- Create: `internal/proxy/config.go`
- Create: `internal/proxy/server.go`
- Test: `internal/proxy/config_test.go`

- [ ] **Step 1: Write the failing config test**

Create `internal/proxy/config_test.go` (Apache header, package `proxy`):

```go
package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
mode: cutover
listen: ":7233"
source:
  address: old:7233
target:
  address: new-frontend:7233
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Mode != ModeCutover {
		t.Errorf("mode = %q, want cutover", cfg.Mode)
	}
	if cfg.Source.Address != "old:7233" || cfg.Target.Address != "new-frontend:7233" {
		t.Errorf("addresses = %q / %q", cfg.Source.Address, cfg.Target.Address)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/ -run TestLoadConfig`
Expected: FAIL — `LoadConfig`/`Config` undefined.

- [ ] **Step 3: Implement config**

Create `internal/proxy/config.go` (Apache header, package `proxy`). Uses `sigs.k8s.io/yaml` (already a transitive dep via controller-runtime):

```go
package proxy

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// Config is the proxy's runtime configuration, loaded from a mounted file.
type Config struct {
	Mode   Mode          `json:"mode"`
	Listen string        `json:"listen"`
	Source BackendConfig `json:"source"`
	Target BackendConfig `json:"target"`
}

// BackendConfig describes how to dial one upstream cluster frontend.
type BackendConfig struct {
	Address string `json:"address"`
	// TLS, when non-nil, enables TLS to this backend.
	TLS *BackendTLS `json:"tls,omitempty"`
}

// BackendTLS configures TLS/mTLS to a backend. Paths point at mounted secrets.
type BackendTLS struct {
	CAFile     string `json:"caFile,omitempty"`
	CertFile   string `json:"certFile,omitempty"`
	KeyFile    string `json:"keyFile,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	// Insecure skips server cert verification (testing only).
	Insecure bool `json:"insecure,omitempty"`
}

// LoadConfig reads and parses the proxy config file.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading proxy config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parsing proxy config: %w", err)
	}
	if cfg.Listen == "" {
		cfg.Listen = ":7233"
	}
	if cfg.Mode == "" {
		cfg.Mode = ModePassthrough
	}
	return &cfg, nil
}
```

- [ ] **Step 4: Run config test**

Run: `go test ./internal/proxy/ -run TestLoadConfig`
Expected: PASS.

- [ ] **Step 5: Implement the server (no new unit test; covered by handler tests + binary smoke test)**

Create `internal/proxy/server.go` (Apache header, package `proxy`):

```go
package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Server is a configured migration proxy ready to serve.
type Server struct {
	grpc    *grpc.Server
	lis     net.Listener
	handler *Handler
}

// NewServer dials both backends and builds the gRPC server from cfg.
func NewServer(cfg *Config) (*Server, error) {
	source, err := dial(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("dialing source: %w", err)
	}
	target, err := dial(cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("dialing target: %w", err)
	}
	h := &Handler{Director: Director{Mode: cfg.Mode}, Source: source, Target: target}

	lis, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Listen, err)
	}
	s := grpc.NewServer(
		grpc.ForceServerCodec(RawCodec{}),
		grpc.UnknownServiceHandler(h.Stream),
	)
	return &Server{grpc: s, lis: lis, handler: h}, nil
}

// Serve blocks serving gRPC until the server is stopped.
func (s *Server) Serve() error { return s.grpc.Serve(s.lis) }

// Stop gracefully stops the server.
func (s *Server) Stop() { s.grpc.GracefulStop() }

func dial(b BackendConfig) (*grpc.ClientConn, error) {
	creds := insecure.NewCredentials()
	if b.TLS != nil {
		tc, err := tlsConfig(b.TLS)
		if err != nil {
			return nil, err
		}
		creds = credentials.NewTLS(tc)
	}
	return grpc.NewClient(b.Address, grpc.WithTransportCredentials(creds))
}

func tlsConfig(t *BackendTLS) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: t.ServerName, InsecureSkipVerify: t.Insecure} //nolint:gosec // Insecure is opt-in for testing
	if t.CAFile != "" {
		ca, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("invalid CA in %s", t.CAFile)
		}
		cfg.RootCAs = pool
	}
	if t.CertFile != "" && t.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
```

- [ ] **Step 6: Run the package + lint**

Run: `go test ./internal/proxy/ && make lint`
Expected: PASS / clean.

- [ ] **Step 7: Commit**

```bash
git add internal/proxy/config.go internal/proxy/config_test.go internal/proxy/server.go
git commit -s -m "feat(proxy): add config loader and gRPC server wiring"
```

---

## Task 8: The `migration-proxy` binary

**Files:**
- Create: `cmd/migration-proxy/main.go`

- [ ] **Step 1: Implement the entrypoint**

Create `cmd/migration-proxy/main.go` (Apache header, package `main`):

```go
package main

import (
	"flag"
	"log"

	"github.com/bmorton/temporal-operator/internal/proxy"
)

func main() {
	configPath := flag.String("config", "/etc/migration-proxy/config.yaml", "path to proxy config file")
	flag.Parse()

	cfg, err := proxy.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	srv, err := proxy.NewServer(cfg)
	if err != nil {
		log.Fatalf("starting server: %v", err)
	}
	log.Printf("migration-proxy listening on %s (mode=%s, source=%s, target=%s)",
		cfg.Listen, cfg.Mode, cfg.Source.Address, cfg.Target.Address)
	if err := srv.Serve(); err != nil {
		log.Fatalf("serving: %v", err)
	}
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build -o /tmp/migration-proxy ./cmd/migration-proxy`
Expected: builds clean; binary produced.

- [ ] **Step 3: Commit**

```bash
git add cmd/migration-proxy/main.go
git commit -s -m "feat(proxy): add migration-proxy binary entrypoint"
```

---

## Task 9: Proxy resource builders (Deployment, Service, ConfigMap)

Pure builders that turn a `TemporalMigration` + rendered proxy config into Kubernetes objects. The controller sets owner references and applies them. Mirrors the pattern in `internal/resources/builders_test.go` (pure, no client).

**Files:**
- Create: `internal/resources/migrationproxy.go`
- Test: `internal/resources/migrationproxy_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/resources/migrationproxy_test.go` (Apache header, package `resources`):

```go
package resources

import (
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func testMigration() *temporalv1alpha1.TemporalMigration {
	return &temporalv1alpha1.TemporalMigration{}
}

func TestBuildMigrationProxyService(t *testing.T) {
	m := testMigration()
	m.Name = "orders-migration"
	m.Namespace = "temporal-system"
	svc := BuildMigrationProxyService(m)
	if svc.Name != "orders-migration-proxy" {
		t.Errorf("service name = %q", svc.Name)
	}
	if svc.Namespace != "temporal-system" {
		t.Errorf("service namespace = %q", svc.Namespace)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 7233 {
		t.Errorf("service ports = %+v, want one port 7233", svc.Spec.Ports)
	}
}

func TestBuildMigrationProxyDeploymentConfigHash(t *testing.T) {
	m := testMigration()
	m.Name = "orders-migration"
	m.Namespace = "temporal-system"
	dep := BuildMigrationProxyDeployment(m, "img:latest", "deadbeef")
	got := dep.Spec.Template.Annotations[ConfigHashAnnotation]
	if got != "deadbeef" {
		t.Errorf("config-hash annotation = %q, want deadbeef", got)
	}
	if dep.Spec.Template.Spec.Containers[0].Image != "img:latest" {
		t.Errorf("image = %q", dep.Spec.Template.Spec.Containers[0].Image)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resources/ -run TestBuildMigrationProxy`
Expected: FAIL — builders undefined.

- [ ] **Step 3: Implement the builders**

Create `internal/resources/migrationproxy.go` (Apache header, package `resources`):

```go
package resources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// MigrationProxyComponent is the component label value for proxy resources.
const MigrationProxyComponent = "migration-proxy"

// proxyFrontendPort is the gRPC port the proxy listens on (Temporal default).
const proxyFrontendPort int32 = 7233

// MigrationProxyName returns the proxy resource name for a migration.
func MigrationProxyName(m *temporalv1alpha1.TemporalMigration) string {
	return m.Name + "-proxy"
}

func migrationProxyLabels(m *temporalv1alpha1.TemporalMigration) map[string]string {
	return map[string]string{
		LabelName:      nameValue,
		LabelInstance:  m.Name,
		LabelComponent: MigrationProxyComponent,
		LabelManagedBy: managedByValue,
	}
}

// BuildMigrationProxyConfigMap renders the proxy config file into a ConfigMap.
func BuildMigrationProxyConfigMap(m *temporalv1alpha1.TemporalMigration, renderedConfig string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MigrationProxyName(m),
			Namespace: m.Namespace,
			Labels:    migrationProxyLabels(m),
		},
		Data: map[string]string{"config.yaml": renderedConfig},
	}
}

// BuildMigrationProxyService exposes the proxy frontend port.
func BuildMigrationProxyService(m *temporalv1alpha1.TemporalMigration) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MigrationProxyName(m),
			Namespace: m.Namespace,
			Labels:    migrationProxyLabels(m),
		},
		Spec: corev1.ServiceSpec{
			Selector: migrationProxyLabels(m),
			Ports: []corev1.ServicePort{{
				Name:       "grpc",
				Port:       proxyFrontendPort,
				TargetPort: intstr.FromInt32(proxyFrontendPort),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

// BuildMigrationProxyDeployment builds the proxy Deployment. configHash is
// stamped on the pod template so config changes (e.g. cutover) trigger a rollout.
func BuildMigrationProxyDeployment(m *temporalv1alpha1.TemporalMigration, image, configHash string) *appsv1.Deployment {
	replicas := int32(1)
	if m.Spec.Proxy != nil && m.Spec.Proxy.Replicas != nil {
		replicas = *m.Spec.Proxy.Replicas
	}
	if m.Spec.Proxy != nil && m.Spec.Proxy.Image != "" {
		image = m.Spec.Proxy.Image
	}
	var resources corev1.ResourceRequirements
	if m.Spec.Proxy != nil {
		resources = m.Spec.Proxy.Resources
	}
	labels := migrationProxyLabels(m)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MigrationProxyName(m),
			Namespace: m.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: map[string]string{ConfigHashAnnotation: configHash},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "migration-proxy",
						Image:   image,
						Command: []string{"/migration-proxy"},
						Args:    []string{"--config=/etc/migration-proxy/config.yaml"},
						Ports: []corev1.ContainerPort{{
							Name:          "grpc",
							ContainerPort: proxyFrontendPort,
							Protocol:      corev1.ProtocolTCP,
						}},
						Resources: resources,
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config",
							MountPath: "/etc/migration-proxy",
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: MigrationProxyName(m)},
							},
						},
					}},
				},
			},
		},
	}
}
```

> **Note:** the alpha mounts only the ConfigMap. Source/target TLS secret mounts (extra volumes + `caFile`/`certFile`/`keyFile` paths in config) are added in Task 11's config rendering when `spec.source.tls` is set; keep the volume list extensible. For the first iteration, source TLS is supported by referencing a secret the controller mounts — implement the secret-mount volumes here only if `spec.source.tls.secretRef` is set (guard with a nil check). For the minimal first pass, document that mounting is added alongside config rendering.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resources/ -run TestBuildMigrationProxy`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resources/migrationproxy.go internal/resources/migrationproxy_test.go
git commit -s -m "feat(resources): add migration proxy builders"
```

---

## Task 10: Visibility / drain `MigrationClient`

A control-plane client the controller uses to list source namespaces and count running workflows. Wraps `WorkflowService` `ListNamespaces` + `CountWorkflowExecutions`.

**Files:**
- Create: `internal/temporal/migration.go`
- Test: `internal/temporal/migration_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/temporal/migration_test.go` (Apache header, package `temporal`). It uses the `CountQuery` helper (pure string builder) so no network is needed:

```go
package temporal

import "testing"

func TestRunningWorkflowsQuery(t *testing.T) {
	if got := RunningWorkflowsQuery(); got != `ExecutionStatus="Running"` {
		t.Fatalf("query = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/temporal/ -run TestRunningWorkflowsQuery`
Expected: FAIL — `RunningWorkflowsQuery` undefined.

- [ ] **Step 3: Implement the client + interface**

Create `internal/temporal/migration.go` (Apache header, package `temporal`):

```go
package temporal

import (
	"context"
	"crypto/tls"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	workflowservice "go.temporal.io/api/workflowservice/v1"
)

// RunningWorkflowsQuery is the visibility query selecting open workflows.
func RunningWorkflowsQuery() string { return `ExecutionStatus="Running"` }

// MigrationClient inspects a cluster for migration drain detection.
type MigrationClient interface {
	// ListNamespaces returns non-system namespace names.
	ListNamespaces(ctx context.Context) ([]string, error)
	// CountRunningWorkflows returns the count of open workflows in a namespace.
	CountRunningWorkflows(ctx context.Context, namespace string) (int64, error)
	Close() error
}

// MigrationClientFactory builds a MigrationClient for a frontend address.
type MigrationClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (MigrationClient, error)

type grpcMigrationClient struct {
	conn     *grpc.ClientConn
	workflow workflowservice.WorkflowServiceClient
}

// NewMigrationClient dials a frontend and returns a MigrationClient.
func NewMigrationClient(_ context.Context, address string, tlsConfig *tls.Config) (MigrationClient, error) {
	creds := insecure.NewCredentials()
	if tlsConfig != nil {
		creds = credentials.NewTLS(tlsConfig)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &grpcMigrationClient{conn: conn, workflow: workflowservice.NewWorkflowServiceClient(conn)}, nil
}

func (c *grpcMigrationClient) ListNamespaces(ctx context.Context) ([]string, error) {
	var out []string
	var token []byte
	for {
		resp, err := c.workflow.ListNamespaces(ctx, &workflowservice.ListNamespacesRequest{
			PageSize:      100,
			NextPageToken: token,
		})
		if err != nil {
			return nil, err
		}
		for _, ns := range resp.GetNamespaces() {
			name := ns.GetNamespaceInfo().GetName()
			if name == "temporal-system" {
				continue
			}
			out = append(out, name)
		}
		token = resp.GetNextPageToken()
		if len(token) == 0 {
			break
		}
	}
	return out, nil
}

func (c *grpcMigrationClient) CountRunningWorkflows(ctx context.Context, namespace string) (int64, error) {
	resp, err := c.workflow.CountWorkflowExecutions(ctx, &workflowservice.CountWorkflowExecutionsRequest{
		Namespace: namespace,
		Query:     RunningWorkflowsQuery(),
	})
	if err != nil {
		return 0, err
	}
	return resp.GetCount(), nil
}

func (c *grpcMigrationClient) Close() error { return c.conn.Close() }
```

- [ ] **Step 4: Run test + build**

Run: `go test ./internal/temporal/ -run TestRunningWorkflowsQuery && make build`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add internal/temporal/migration.go internal/temporal/migration_test.go
git commit -s -m "feat(temporal): add migration visibility client"
```

---

## Task 11: Render proxy config (pure helper)

Turns a `TemporalMigration` + its target `TemporalCluster` + mode into a `proxy.Config` and the list of secret mounts the proxy pod needs. Pure and unit-tested.

**Files:**
- Create: `internal/controller/temporalmigration_config.go`
- Test: `internal/controller/temporalmigration_config_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/controller/temporalmigration_config_test.go` (Apache header, package `controller`):

```go
package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/proxy"
)

func TestRenderProxyConfigPassthrough(t *testing.T) {
	m := &temporalv1alpha1.TemporalMigration{}
	m.Name = "mig"
	m.Namespace = "temporal-system"
	m.Spec.Source.Address = "old:7233"
	m.Spec.Cutover = false
	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "newcluster"
	cluster.Namespace = "temporal-system"

	cfg, mounts, err := renderProxyConfig(m, cluster)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != proxy.ModePassthrough {
		t.Errorf("mode = %q, want passthrough", cfg.Mode)
	}
	if cfg.Source.Address != "old:7233" {
		t.Errorf("source = %q", cfg.Source.Address)
	}
	if cfg.Target.Address == "" {
		t.Errorf("target address empty")
	}
	if len(mounts) != 0 {
		t.Errorf("expected no secret mounts for non-mTLS, got %d", len(mounts))
	}
}

func TestRenderProxyConfigCutoverWithSourceTLS(t *testing.T) {
	m := &temporalv1alpha1.TemporalMigration{}
	m.Name = "mig"
	m.Namespace = "temporal-system"
	m.Spec.Source.Address = "old:7233"
	m.Spec.Source.TLS = &temporalv1alpha1.SourceTLSSpec{
		Enabled:   true,
		SecretRef: &corev1.LocalObjectReference{Name: "old-tls"},
	}
	m.Spec.Cutover = true
	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "newcluster"
	cluster.Namespace = "temporal-system"

	cfg, mounts, err := renderProxyConfig(m, cluster)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != proxy.ModeCutover {
		t.Errorf("mode = %q, want cutover", cfg.Mode)
	}
	if cfg.Source.TLS == nil || cfg.Source.TLS.CAFile == "" {
		t.Errorf("source TLS not rendered: %+v", cfg.Source.TLS)
	}
	if len(mounts) != 1 || mounts[0].SecretName != "old-tls" {
		t.Errorf("expected one source-tls mount, got %+v", mounts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/controller/ -run TestRenderProxyConfig`
Expected: FAIL — `renderProxyConfig`/`secretMount` undefined.

- [ ] **Step 3: Implement the renderer**

Create `internal/controller/temporalmigration_config.go` (Apache header, package `controller`):

```go
package controller

import (
	"fmt"

	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/proxy"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// secretMount describes a Secret the proxy pod must mount.
type secretMount struct {
	SecretName string
	MountPath  string
}

const (
	sourceTLSMountPath = "/etc/migration-proxy/source-tls"
	targetTLSMountPath = "/etc/migration-proxy/target-tls"
)

// renderProxyConfig builds the proxy config and required secret mounts.
func renderProxyConfig(m *temporalv1alpha1.TemporalMigration, cluster *temporalv1alpha1.TemporalCluster) (*proxy.Config, []secretMount, error) {
	mode := proxy.ModePassthrough
	if m.Spec.Cutover {
		mode = proxy.ModeCutover
	}

	cfg := &proxy.Config{
		Mode:   mode,
		Listen: ":7233",
		Source: proxy.BackendConfig{Address: m.Spec.Source.Address},
		Target: proxy.BackendConfig{Address: frontendAddress(cluster)},
	}

	var mounts []secretMount

	// Source TLS from spec.source.tls.
	if t := m.Spec.Source.TLS; t != nil && t.Enabled {
		if t.SecretRef == nil {
			return nil, nil, fmt.Errorf("source.tls.enabled requires source.tls.secretRef")
		}
		cfg.Source.TLS = &proxy.BackendTLS{
			CAFile:     sourceTLSMountPath + "/ca.crt",
			CertFile:   sourceTLSMountPath + "/tls.crt",
			KeyFile:    sourceTLSMountPath + "/tls.key",
			ServerName: t.ServerName,
		}
		mounts = append(mounts, secretMount{SecretName: t.SecretRef.Name, MountPath: sourceTLSMountPath})
	}

	// Target TLS: reuse the cluster's internode cert when mTLS is enabled.
	if cluster.Spec.MTLS != nil {
		cfg.Target.TLS = &proxy.BackendTLS{
			CAFile:     targetTLSMountPath + "/ca.crt",
			CertFile:   targetTLSMountPath + "/tls.crt",
			KeyFile:    targetTLSMountPath + "/tls.key",
			ServerName: fmt.Sprintf("%s.%s.svc.cluster.local", resources.FrontendServiceName(cluster.Name), cluster.Namespace),
		}
		mounts = append(mounts, secretMount{SecretName: resources.InternodeCertName(cluster.Name), MountPath: targetTLSMountPath})
	}

	return cfg, mounts, nil
}

// marshalProxyConfig renders the config to YAML for the ConfigMap.
func marshalProxyConfig(cfg *proxy.Config) (string, error) {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
```

- [ ] **Step 4: Run test + verify**

Run: `go test ./internal/controller/ -run TestRenderProxyConfig`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/temporalmigration_config.go internal/controller/temporalmigration_config_test.go
git commit -s -m "feat(controller): render migration proxy config"
```

---

## Task 12: TemporalMigration controller

Reconciles the CR: provisions ConfigMap/Service/Deployment (owner refs → GC), drives the phase machine, and performs drain detection via injected `MigrationClientFactory`. Mirrors `temporalschedule_controller.go` conventions (status conditions via `meta.SetStatusCondition`, `serverSideApply`, injectable factory for tests).

**Files:**
- Create: `internal/controller/temporalmigration_controller.go`
- Create: `internal/controller/temporalmigration_controller_test.go`
- Modify: `internal/controller/suite_test.go` (register the new CRD path is already covered; only add the reconciler if the suite wires reconcilers — otherwise tests instantiate the reconciler directly)

- [ ] **Step 1: Write the failing envtest (provisioning + phases)**

Create `internal/controller/temporalmigration_controller_test.go` (Apache header, package `controller`). Use the existing envtest harness (`k8sClient`, `ctx` from `suite_test.go`). A fake `MigrationClientFactory` returns configurable running counts:

```go
package controller

import (
	"context"
	"crypto/tls"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

type fakeMigClient struct {
	namespaces []string
	running    map[string]int64
}

func (f *fakeMigClient) ListNamespaces(context.Context) ([]string, error) { return f.namespaces, nil }
func (f *fakeMigClient) CountRunningWorkflows(_ context.Context, ns string) (int64, error) {
	return f.running[ns], nil
}
func (f *fakeMigClient) Close() error { return nil }

var _ = Describe("TemporalMigration controller", func() {
	It("provisions the proxy and reports Passthrough", func() {
		ns := "temporal-system"
		cluster := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "newc", Namespace: ns},
			Spec: temporalv1alpha1.TemporalClusterSpec{
				Version:          "1.31.1",
				NumHistoryShards: 512,
				Persistence:      minimalPersistence(),
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		mig := &temporalv1alpha1.TemporalMigration{
			ObjectMeta: metav1.ObjectMeta{Name: "mig", Namespace: ns},
			Spec: temporalv1alpha1.TemporalMigrationSpec{
				Source:    temporalv1alpha1.SourceClusterSpec{Address: "old:7233"},
				TargetRef: corev1.LocalObjectReference{Name: "newc"},
			},
		}
		Expect(k8sClient.Create(ctx, mig)).To(Succeed())

		r := &TemporalMigrationReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			MigrationClientFactory: func(context.Context, string, *tls.Config) (temporal.MigrationClient, error) {
				return &fakeMigClient{namespaces: []string{"orders"}, running: map[string]int64{"orders": 5}}, nil
			},
		}
		_, err := r.Reconcile(ctx, ctrlRequest("mig", ns))
		Expect(err).NotTo(HaveOccurred())

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mig-proxy", Namespace: ns}, &dep)).To(Succeed())
		Expect(dep.OwnerReferences).NotTo(BeEmpty())

		var got temporalv1alpha1.TemporalMigration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mig", Namespace: ns}, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(temporalv1alpha1.MigrationPhasePassthrough))
	})

	It("enters Cutover then Complete when source has drained", func() {
		ns := "temporal-system"
		cluster := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "newc2", Namespace: ns},
			Spec: temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512, Persistence: minimalPersistence()},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		mig := &temporalv1alpha1.TemporalMigration{
			ObjectMeta: metav1.ObjectMeta{Name: "mig2", Namespace: ns},
			Spec: temporalv1alpha1.TemporalMigrationSpec{
				Source:    temporalv1alpha1.SourceClusterSpec{Address: "old:7233"},
				TargetRef: corev1.LocalObjectReference{Name: "newc2"},
				Cutover:   true,
			},
		}
		Expect(k8sClient.Create(ctx, mig)).To(Succeed())

		drained := &fakeMigClient{namespaces: []string{"orders"}, running: map[string]int64{"orders": 0}}
		r := &TemporalMigrationReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			DrainStableThreshold: 1,
			MigrationClientFactory: func(context.Context, string, *tls.Config) (temporal.MigrationClient, error) {
				return drained, nil
			},
		}
		// Reconcile enough times to satisfy the stable threshold.
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, ctrlRequest("mig2", ns))
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(10 * time.Millisecond)
		}
		var got temporalv1alpha1.TemporalMigration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mig2", Namespace: ns}, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(temporalv1alpha1.MigrationPhaseComplete))
		Expect(got.Status.CutoverTime).NotTo(BeNil())
	})
})
```

> **Helper note:** `ctrlRequest` and `minimalPersistence` may already exist in the suite. If not, add them to the test file: `ctrlRequest` returns `ctrl.Request{NamespacedName: types.NamespacedName{Name, Namespace}}`; `minimalPersistence` returns a `PersistenceSpec` valid for the CRD schema (copy from an existing controller test such as `temporalcluster_persistence_test.go`).

- [ ] **Step 2: Run test to verify it fails**

Run: `make test` (envtest)
Expected: FAIL — `TemporalMigrationReconciler` undefined.

- [ ] **Step 3: Implement the controller**

Create `internal/controller/temporalmigration_controller.go` (Apache header, package `controller`):

```go
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const migrationProxyImageEnv = "MIGRATION_PROXY_IMAGE"

// migrationRequeue is the steady-state reconcile cadence.
const migrationRequeue = 30 * time.Second

// TemporalMigrationReconciler reconciles TemporalMigration objects.
type TemporalMigrationReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ProxyImage is the image used for the proxy Deployment (operator image).
	ProxyImage string
	// MigrationClientFactory builds visibility clients; injectable for tests.
	MigrationClientFactory temporal.MigrationClientFactory
	// DrainStableThreshold is the number of consecutive all-zero observations
	// required before declaring Complete. Defaults to 3.
	DrainStableThreshold int

	zeroStreak map[types.NamespacedName]int
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalmigrations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalmigrations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalmigrations/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile provisions the proxy and advances the migration phase machine.
func (r *TemporalMigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var mig temporalv1alpha1.TemporalMigration
	if err := r.Get(ctx, req.NamespacedName, &mig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var cluster temporalv1alpha1.TemporalCluster
	clusterKey := types.NamespacedName{Namespace: mig.Namespace, Name: mig.Spec.TargetRef.Name}
	if err := r.Get(ctx, clusterKey, &cluster); err != nil {
		meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionTargetReachable, Status: metav1.ConditionFalse,
			Reason: "ClusterNotFound", Message: "referenced TemporalCluster not found",
		})
		mig.Status.Phase = temporalv1alpha1.MigrationPhasePending
		return ctrl.Result{RequeueAfter: migrationRequeue}, r.statusUpdate(ctx, &mig)
	}

	// Provision proxy resources (ConfigMap, Service, Deployment).
	if err := r.provisionProxy(ctx, &mig, &cluster); err != nil {
		return ctrl.Result{}, err
	}
	mig.Status.ProxyEndpoint = fmt.Sprintf("%s.%s.svc.cluster.local:7233", resources.MigrationProxyName(&mig), mig.Namespace)

	// Phase machine.
	if !mig.Spec.Cutover {
		mig.Status.Phase = r.passthroughPhase(ctx, &mig)
		meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionMigrationComplete, Status: metav1.ConditionFalse,
			Reason: temporalv1alpha1.ReasonPassthrough, Message: "proxy forwarding all traffic to source",
		})
		mig.Status.ObservedGeneration = mig.Generation
		return ctrl.Result{RequeueAfter: migrationRequeue}, r.statusUpdate(ctx, &mig)
	}

	if mig.Status.CutoverTime == nil {
		now := metav1.Now()
		mig.Status.CutoverTime = &now
		log.Info("cutover enabled", "migration", req.NamespacedName)
	}

	complete, err := r.reconcileDrain(ctx, &mig, &cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	if complete {
		mig.Status.Phase = temporalv1alpha1.MigrationPhaseComplete
	} else {
		mig.Status.Phase = temporalv1alpha1.MigrationPhaseDraining
	}
	mig.Status.ObservedGeneration = mig.Generation
	return ctrl.Result{RequeueAfter: migrationRequeue}, r.statusUpdate(ctx, &mig)
}

// passthroughPhase returns Pending until the proxy has an available replica.
func (r *TemporalMigrationReconciler) passthroughPhase(ctx context.Context, mig *temporalv1alpha1.TemporalMigration) string {
	var dep appsv1.Deployment
	key := types.NamespacedName{Namespace: mig.Namespace, Name: resources.MigrationProxyName(mig)}
	if err := r.Get(ctx, key, &dep); err != nil || dep.Status.AvailableReplicas == 0 {
		return temporalv1alpha1.MigrationPhasePending
	}
	meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionProxyReady, Status: metav1.ConditionTrue,
		Reason: "Available", Message: "proxy is serving",
	})
	return temporalv1alpha1.MigrationPhasePassthrough
}

// provisionProxy renders config and applies the ConfigMap, Service, Deployment.
func (r *TemporalMigrationReconciler) provisionProxy(ctx context.Context, mig *temporalv1alpha1.TemporalMigration, cluster *temporalv1alpha1.TemporalCluster) error {
	cfg, mounts, err := renderProxyConfig(mig, cluster)
	if err != nil {
		return err
	}
	rendered, err := marshalProxyConfig(cfg)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(rendered))
	configHash := hex.EncodeToString(sum[:])

	cm := resources.BuildMigrationProxyConfigMap(mig, rendered)
	svc := resources.BuildMigrationProxyService(mig)
	dep := resources.BuildMigrationProxyDeployment(mig, r.proxyImage(), configHash)
	applyProxyMounts(dep, mounts)

	for _, obj := range []client.Object{cm, svc, dep} {
		if err := controllerutil.SetControllerReference(mig, obj, r.Scheme); err != nil {
			return err
		}
		if err := serverSideApply(ctx, r.Client, r.Scheme, obj, client.FieldOwner("temporal-operator")); err != nil {
			return fmt.Errorf("applying %T: %w", obj, err)
		}
	}
	return nil
}

// applyProxyMounts appends secret volumes/mounts to the proxy Deployment.
func applyProxyMounts(dep *appsv1.Deployment, mounts []secretMount) {
	c := &dep.Spec.Template.Spec.Containers[0]
	for i, m := range mounts {
		volName := fmt.Sprintf("tls-%d", i)
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name:         volName,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: m.SecretName}},
		})
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name: volName, MountPath: m.MountPath, ReadOnly: true,
		})
	}
}

// reconcileDrain queries the source running-workflow counts and reports when
// the source has fully drained for DrainStableThreshold consecutive checks.
func (r *TemporalMigrationReconciler) reconcileDrain(ctx context.Context, mig *temporalv1alpha1.TemporalMigration, _ *temporalv1alpha1.TemporalCluster) (bool, error) {
	srcTLS, err := r.sourceTLS(ctx, mig)
	if err != nil {
		return false, err
	}
	mc, err := r.MigrationClientFactory(ctx, mig.Spec.Source.Address, srcTLS)
	if err != nil {
		meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionSourceReachable, Status: metav1.ConditionFalse,
			Reason: temporalv1alpha1.ReasonUnreachable, Message: err.Error(),
		})
		return false, nil
	}
	defer func() { _ = mc.Close() }()
	meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionSourceReachable, Status: metav1.ConditionTrue,
		Reason: "Reachable", Message: "source frontend reachable",
	})

	namespaces, err := r.migratedNamespaces(ctx, mig, mc)
	if err != nil {
		return false, err
	}

	var total int64
	status := make([]temporalv1alpha1.NamespaceDrainStatus, 0, len(namespaces))
	for _, ns := range namespaces {
		count, err := mc.CountRunningWorkflows(ctx, ns)
		if err != nil {
			return false, err
		}
		total += count
		status = append(status, temporalv1alpha1.NamespaceDrainStatus{
			Namespace: ns, SourceRunningWorkflows: count, Drained: count == 0,
		})
	}
	mig.Status.Draining = status

	key := types.NamespacedName{Namespace: mig.Namespace, Name: mig.Name}
	if r.zeroStreak == nil {
		r.zeroStreak = map[types.NamespacedName]int{}
	}
	if total == 0 {
		r.zeroStreak[key]++
	} else {
		r.zeroStreak[key] = 0
	}

	threshold := r.DrainStableThreshold
	if threshold <= 0 {
		threshold = 3
	}
	complete := total == 0 && r.zeroStreak[key] >= threshold

	condStatus := metav1.ConditionFalse
	reason := temporalv1alpha1.ReasonDraining
	if complete {
		condStatus = metav1.ConditionTrue
		reason = temporalv1alpha1.ReasonDrained
	}
	meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionMigrationComplete, Status: condStatus,
		Reason: reason, Message: fmt.Sprintf("%d running workflows on source", total),
	})
	return complete, nil
}

// migratedNamespaces returns the configured namespaces or all source namespaces.
func (r *TemporalMigrationReconciler) migratedNamespaces(ctx context.Context, mig *temporalv1alpha1.TemporalMigration, mc temporal.MigrationClient) ([]string, error) {
	if len(mig.Spec.Namespaces) > 0 {
		out := make([]string, 0, len(mig.Spec.Namespaces))
		for _, m := range mig.Spec.Namespaces {
			out = append(out, m.Source)
		}
		return out, nil
	}
	return mc.ListNamespaces(ctx)
}

// sourceTLS builds the control-plane TLS config for the source frontend.
func (r *TemporalMigrationReconciler) sourceTLS(ctx context.Context, mig *temporalv1alpha1.TemporalMigration) (*tlsConfig, error) {
	t := mig.Spec.Source.TLS
	if t == nil || !t.Enabled || t.SecretRef == nil {
		return nil, nil
	}
	return buildSourceTLSConfig(ctx, r.Client, mig.Namespace, t)
}

func (r *TemporalMigrationReconciler) proxyImage() string {
	if r.ProxyImage != "" {
		return r.ProxyImage
	}
	return defaultProxyImage()
}

func (r *TemporalMigrationReconciler) statusUpdate(ctx context.Context, mig *temporalv1alpha1.TemporalMigration) error {
	return r.Status().Update(ctx, mig)
}

// SetupWithManager registers the controller and owned resources.
func (r *TemporalMigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.MigrationClientFactory == nil {
		r.MigrationClientFactory = temporal.NewMigrationClient
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalMigration{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Named("temporalmigration").
		Complete(r)
}
```

- [ ] **Step 4: Add the TLS + image helpers used above**

Create `internal/controller/temporalmigration_tls.go` (Apache header, package `controller`). `tlsConfig` is an alias to `crypto/tls`.Config; the helper reads the source secret like `clusterTLSConfig` does:

```go
package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

type tlsConfig = tls.Config

// defaultProxyImage resolves the proxy image from the environment, defaulting
// to the operator image at a well-known tag.
func defaultProxyImage() string {
	if v := os.Getenv(migrationProxyImageEnv); v != "" {
		return v
	}
	return "ghcr.io/bmorton/temporal-operator:latest"
}

// buildSourceTLSConfig builds a *tls.Config from the source TLS secret.
func buildSourceTLSConfig(ctx context.Context, c client.Client, namespace string, t *temporalv1alpha1.SourceTLSSpec) (*tls.Config, error) {
	var secret corev1.Secret
	key := types.NamespacedName{Namespace: namespace, Name: t.SecretRef.Name}
	if err := c.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("reading source tls secret %s: %w", key, err)
	}
	cfg := &tls.Config{ServerName: t.ServerName, MinVersion: tls.VersionTLS12}
	if ca := secret.Data["ca.crt"]; len(ca) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("invalid ca.crt in %s", key)
		}
		cfg.RootCAs = pool
	}
	if crt, k := secret.Data["tls.crt"], secret.Data["tls.key"]; len(crt) > 0 && len(k) > 0 {
		cert, err := tls.X509KeyPair(crt, k)
		if err != nil {
			return nil, fmt.Errorf("parsing source client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
```

- [ ] **Step 5: Run the envtest**

Run: `make test`
Expected: both `TemporalMigration controller` specs PASS.

- [ ] **Step 6: Lint**

Run: `make lint`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/temporalmigration_controller.go internal/controller/temporalmigration_tls.go internal/controller/temporalmigration_controller_test.go
git commit -s -m "feat(controller): add TemporalMigration reconciler"
```

---

## Task 13: Register the controller + RBAC manifests

**Files:**
- Modify: `cmd/main.go`
- Regenerate: `config/rbac/role.yaml`

- [ ] **Step 1: Register the reconciler in `cmd/main.go`**

After the `TemporalScheduleReconciler` block (currently ending at the `os.Exit(1)` / `}` around line 220), add:

```go
	if err := (&controller.TemporalMigrationReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		ProxyImage: os.Getenv("MIGRATION_PROXY_IMAGE"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TemporalMigration")
		os.Exit(1)
	}
```

- [ ] **Step 2: Regenerate RBAC + manifests**

Run: `make manifests`
Expected: `config/rbac/role.yaml` gains `temporalmigrations`, `deployments`, `services`, `configmaps` rules (from the `+kubebuilder:rbac` markers in Task 12).

- [ ] **Step 3: Build**

Run: `make build`
Expected: compiles clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/main.go config/rbac/role.yaml
git commit -s -m "feat: register TemporalMigration controller and RBAC"
```

---

## Task 14: Ship the `migration-proxy` binary in the image

The operator image must contain both `/manager` and `/migration-proxy` so the controller can run the proxy with the same image (`MIGRATION_PROXY_IMAGE` defaults to the operator image).

**Files:**
- Modify: `Dockerfile`
- Modify: `Makefile`

- [ ] **Step 1: Build both binaries in the Dockerfile**

In `Dockerfile`, replace the single build line:

```dockerfile
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go
```

with:

```dockerfile
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o migration-proxy ./cmd/migration-proxy
```

And in the final distroless stage, after `COPY --from=builder /workspace/manager .`, add:

```dockerfile
COPY --from=builder /workspace/migration-proxy .
```

- [ ] **Step 2: Build both binaries in the Makefile `build` target**

In `Makefile`, change the `build` recipe from:

```make
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go
```

to:

```make
build: manifests generate fmt vet ## Build manager and migration-proxy binaries.
	go build -o bin/manager cmd/main.go
	go build -o bin/migration-proxy ./cmd/migration-proxy
```

- [ ] **Step 3: Verify both binaries build**

Run: `make build && ls bin/manager bin/migration-proxy`
Expected: both binaries exist.

- [ ] **Step 4: Verify the image builds**

Run: `make docker-build IMG=temporal-operator:dev`
Expected: image builds with both binaries (if Docker is unavailable in the environment, skip and note it — the Dockerfile change is still required).

- [ ] **Step 5: Commit**

```bash
git add Dockerfile Makefile
git commit -s -m "build: ship migration-proxy binary in the operator image"
```

---

## Task 15: Sample CR, Helm chart, docs, and final verification

**Files:**
- Create: `config/samples/temporal_v1alpha1_temporalmigration.yaml`
- Create: `examples/migration/01-temporalmigration.yaml`
- Modify: `config/samples/kustomization.yaml`
- Modify: `dist/chart/...` (CRD + RBAC) — **by hand**, per repo convention (do NOT run `make helm-chart`)
- Create: `docs/migration.md` (or the docs-site equivalent)
- Modify: `README.md` (add `TemporalMigration` / `tm` to the CR table)

> **Repo convention (memory):** `make helm-chart` regenerates the chart with an incompatible values contract and recreates a deleted workflow. Add the new CRD + RBAC entries to `dist/chart/` **manually**, mirroring how the existing CRDs appear there.

- [ ] **Step 1: Write the sample CR**

Create `config/samples/temporal_v1alpha1_temporalmigration.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalMigration
metadata:
  name: orders-migration
  namespace: temporal-system
spec:
  source:
    address: old-temporal-frontend.legacy.svc.cluster.local:7233
  targetRef:
    name: orders
  # Flip to true to begin routing new workflows to the target cluster.
  cutover: false
```

Add it to `config/samples/kustomization.yaml` under `resources:`.

- [ ] **Step 2: Write a runnable example**

Create `examples/migration/01-temporalmigration.yaml` with the same content plus an inline comment block describing the workflow: (1) apply with `cutover: false`, (2) repoint clients to `status.proxyEndpoint`, (3) `kubectl patch ... cutover=true`, (4) watch `status.draining`, (5) on `Complete`, repoint clients to the target frontend and `kubectl delete` the migration.

- [ ] **Step 3: Add the CRD + RBAC to the Helm chart by hand**

Copy the generated `config/crd/bases/temporal.bmor10.com_temporalmigrations.yaml` into the chart's CRD directory following the existing pattern (e.g. `dist/chart/templates/crd/` or wherever the other CRDs live — match exactly). Add the `temporalmigrations`, `deployments`, `services`, `configmaps` RBAC rules to the chart's manager ClusterRole template, matching the existing style (`{{- if .Values.crd.enable }}` guards, etc.).

- [ ] **Step 4: Verify the chart still renders**

Run: `helm template dist/chart >/dev/null && echo OK`
Expected: `OK` (no template errors).

- [ ] **Step 5: Update README + docs**

Add a row to the README CR table:

```markdown
| `TemporalMigration` | `tm` | Gradually migrate traffic from an external Temporal cluster to a managed one. |
```

Create `docs/migration.md` documenting: the drain-based model, the passthrough→cutover→drain→complete flow, the **"run workers against both clusters during migration"** requirement, mTLS-to-source config, and the manual teardown steps. Call out the visibility-lag/long-running-workflow caveat.

- [ ] **Step 6: Full verification suite**

Run each and confirm:
- `make generate manifests` — no uncommitted diffs afterward (run `git status --short`).
- `go test ./internal/proxy/...` — PASS.
- `make build` — both binaries compile.
- `make test` — envtest suites PASS (including the new controller specs).
- `make lint` — clean.
- `helm template dist/chart >/dev/null` — renders.

- [ ] **Step 7: Commit**

```bash
git add config/samples examples/migration dist/chart docs/migration.md README.md
git commit -s -m "docs: add TemporalMigration sample, chart entries, and migration guide"
```

---

## Out of scope (do NOT implement — documented future work)

- Alternate/round-robin poll proxying (the `ClassPoll` seam in `handler.Stream` is where it lands).
- Routing-hint / Bloom-filter cache.
- Weighted/canary start splitting.
- Full typed `WorkflowService` implementation (Approach 3) and Temporal XDC/replication (semantic B).
- Auto-teardown of the proxy on Complete.
- Live config reload (the alpha rolls the proxy via the `config-hash` annotation when cutover flips).

## Self-review notes (verified while writing)

- **Spec coverage:** CRD (Task 1–2), director proxy with try-new-then-fallback (Task 3–6), passthrough/cutover gate (Director + config render, Task 5/11), SDK drain detection + per-namespace status (Task 10/12), mTLS to source (Task 11/12) and target (Task 11), manual teardown via owner-ref GC (Task 12 `SetControllerReference`), worker "run against both" caveat (Task 15 docs). All in-scope spec items map to a task.
- **Type consistency:** `Frame`/`RawCodec`/`CodecName`, `MethodClass`/`Classify`, `Mode`/`Backend`/`Director.Route`, `Handler.Stream`/`SetMode`, `Config`/`BackendConfig`/`BackendTLS`/`LoadConfig`/`NewServer`, `BuildMigrationProxy{ConfigMap,Service,Deployment}`/`MigrationProxyName`, `MigrationClient`/`MigrationClientFactory`/`NewMigrationClient`/`RunningWorkflowsQuery`, `renderProxyConfig`/`marshalProxyConfig`/`secretMount`, `TemporalMigrationReconciler` fields used in tests (`MigrationClientFactory`, `DrainStableThreshold`, `Scheme`, `Client`) — names are consistent across tasks.
- **Phase names** match the API constants (`MigrationPhasePending/Passthrough/Cutover/Draining/Complete`).

