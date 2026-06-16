# WASM Resource Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an in-browser, GitHub Pages tool that takes a pasted `TemporalCluster` CR and shows every Kubernetes object the operator would create, on a tabbed page — reusing the operator's own code with no manual syncing.

**Architecture:** Extract the operator's object-assembly decisions into a new pure `internal/plan` package consumed by *both* the controller and a new `cmd/preview-wasm` WebAssembly shim. The controller keeps its existing phase gating; the WASM shim runs the real webhook defaulter/validator then the planner with placeholder credentials. A Hugo page loads the wasm and renders objects in tabs by Kind.

**Tech Stack:** Go (`GOOS=js GOARCH=wasm`), `sigs.k8s.io/yaml`, controller-runtime, Hugo (hugo-book theme), vanilla JS, GitHub Pages.

**Spec:** `docs/superpowers/specs/2026-06-16-wasm-resource-preview-design.md`

**Quality bar:** The tool may be alpha. The operator must stay high quality: every controller change is behavior-preserving and proven by the existing controller/envtest suite. Run `make lint` and `make test` before each commit that touches Go. Sign off every commit (`git commit -s`) per the repo DCO requirement.

---

## File Structure

**New Go files**
- `internal/plan/plan.go` — `Phase`, `PlannedObject`, and the `PlanAll`/`PlanFromSpec` orchestrators.
- `internal/plan/services.go` — `PlanServices` (config secret, dynamic config, per-service Deployment/Service/PDB).
- `internal/plan/mtls.go` — `PlanMTLS` (internode + frontend certs).
- `internal/plan/ui.go` — `PlanUI` (UI cert/deployment/service/ingress).
- `internal/plan/monitoring.go` — `PlanMonitoring` (ServiceMonitor).
- `internal/plan/schema.go` — `PlanSchemaJobs` (setup jobs for both stores).
- `internal/plan/*_test.go` — pure table tests.
- `cmd/preview-wasm/main.go` — js/wasm shim exposing `temporalPreview`.
- `cmd/preview-wasm/wasm_build_test.go` — CI compile guard.

**Modified Go files**
- `internal/resources/certificates.go` — add exported `MTLSEnabled` helper.
- `internal/controller/temporalcluster_mtls.go` — `mTLSEnabled` delegates to `resources.MTLSEnabled`.
- `internal/controller/temporalcluster_services.go` — `reconcileServices` calls `plan.PlanServices`.
- `internal/controller/temporalcluster_ui.go` — `reconcileUI` calls `plan.PlanUI`.
- `internal/controller/temporalcluster_monitoring.go` — `reconcileMonitoring` calls `plan.PlanMonitoring`.

**New web / build files**
- `docs/static/preview/index.html` — the tool page (committed).
- `docs/static/preview/app.js` — UI logic (committed).
- `docs/content/tools/_index.md` — Hugo section landing page (committed).
- `docs/content/tools/resource-preview.md` — links to the tool (committed).
- `hack/build-preview.sh` — builds wasm, copies `wasm_exec.js`, copies example CRs + manifest (committed).

**Modified web / build files**
- `.gitignore` — ignore generated wasm + copied assets.
- `Makefile` — add `preview-wasm` target.
- `.github/workflows/docs.yml` — build wasm before Hugo; add a wasm compile guard.

**Generated (never committed)**
- `docs/static/preview/temporal-operator-preview.wasm`
- `docs/static/preview/wasm_exec.js`
- `docs/static/preview/examples/*.yaml` + `docs/static/preview/examples/index.json`

---

## Task 1: Export `MTLSEnabled` from resources

The planner and controller both need the mTLS-enabled check. Today it is the unexported `mTLSEnabled` in the controller package. Move the predicate to `internal/resources` so the pure planner can use it without importing the controller.

**Files:**
- Modify: `internal/resources/certificates.go`
- Modify: `internal/controller/temporalcluster_mtls.go:37-39`
- Test: `internal/resources/certificates_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/resources/certificates_test.go`:

```go
func TestMTLSEnabled(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{}
	if MTLSEnabled(c) {
		t.Errorf("expected mTLS disabled when spec.MTLS is nil")
	}
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager"}
	if !MTLSEnabled(c) {
		t.Errorf("expected mTLS enabled for cert-manager provider")
	}
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "other"}
	if MTLSEnabled(c) {
		t.Errorf("expected mTLS disabled for non cert-manager provider")
	}
}
```

If `certificates_test.go` does not import the API package, add `temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"` to its imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resources/ -run TestMTLSEnabled`
Expected: FAIL — `undefined: MTLSEnabled`.

- [ ] **Step 3: Implement the helper**

Add to `internal/resources/certificates.go` (near the other exported helpers):

```go
// MTLSEnabled reports whether the cluster requests cert-manager-issued mTLS.
func MTLSEnabled(cluster *temporalv1alpha1.TemporalCluster) bool {
	return cluster.Spec.MTLS != nil && cluster.Spec.MTLS.Provider == "cert-manager"
}
```

- [ ] **Step 4: Delegate the controller predicate**

Replace the body of `mTLSEnabled` in `internal/controller/temporalcluster_mtls.go`:

```go
func mTLSEnabled(cluster *temporalv1alpha1.TemporalCluster) bool {
	return resources.MTLSEnabled(cluster)
}
```

Confirm `internal/controller/temporalcluster_mtls.go` already imports `"github.com/bmorton/temporal-operator/internal/resources"` (it does — it calls `resources.BuildInternodeCertificate`).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/resources/ ./internal/controller/ -run 'TestMTLSEnabled|MTLS'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/resources/certificates.go internal/resources/certificates_test.go internal/controller/temporalcluster_mtls.go
git commit -s -m "refactor(resources): export MTLSEnabled helper"
```

---

## Task 2: Create the `internal/plan` package skeleton + spec-pure planners

Create the planner package with the object/phase types and the three planners whose inputs are purely the cluster spec: mTLS certs, UI, and ServiceMonitor.

**Files:**
- Create: `internal/plan/plan.go`
- Create: `internal/plan/mtls.go`
- Create: `internal/plan/ui.go`
- Create: `internal/plan/monitoring.go`
- Test: `internal/plan/plan_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/plan/plan_test.go`:

```go
package plan

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func testCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "ns"},
		Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1"},
	}
}

func kinds(objs []PlannedObject) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.Object.GetObjectKind().GroupVersionKind().Kind)
	}
	return out
}

func TestPlanMTLSDisabled(t *testing.T) {
	if got := PlanMTLS(testCluster()); len(got) != 0 {
		t.Errorf("expected no mTLS objects when disabled, got %v", kinds(got))
	}
}

func TestPlanMTLSEnabled(t *testing.T) {
	c := testCluster()
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager", IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"}}
	got := PlanMTLS(c)
	if len(got) != 2 {
		t.Fatalf("expected internode + frontend certs, got %d", len(got))
	}
	for _, o := range got {
		if o.Phase != PhaseMTLS {
			t.Errorf("expected PhaseMTLS, got %s", o.Phase)
		}
	}
}

func TestPlanUIDisabledAndEnabled(t *testing.T) {
	c := testCluster()
	if got := PlanUI(c); len(got) != 0 {
		t.Errorf("expected no UI objects when disabled, got %v", kinds(got))
	}
	c.Spec.UI = &temporalv1alpha1.UISpec{Enabled: true, Version: "2.34.0"}
	got := PlanUI(c)
	// Deployment + Service, no cert (mTLS off), no ingress (off).
	if len(got) != 2 {
		t.Fatalf("expected deployment + service, got %d (%v)", len(got), kinds(got))
	}
	c.Spec.UI.Ingress = &temporalv1alpha1.UIIngressSpec{Enabled: true, Host: "ui.example.com"}
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager", IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"}}
	got = PlanUI(c)
	// client cert + deployment + service + ingress.
	if len(got) != 4 {
		t.Fatalf("expected cert+deployment+service+ingress, got %d (%v)", len(got), kinds(got))
	}
}

func TestPlanMonitoring(t *testing.T) {
	c := testCluster()
	if got := PlanMonitoring(c); len(got) != 0 {
		t.Errorf("expected no ServiceMonitor when disabled")
	}
	c.Spec.Metrics = &temporalv1alpha1.MetricsSpec{
		Enabled:        true,
		ServiceMonitor: &temporalv1alpha1.ServiceMonitorSpec{Enabled: true},
	}
	if got := PlanMonitoring(c); len(got) != 1 {
		t.Errorf("expected one ServiceMonitor, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/plan/`
Expected: FAIL — package/types do not exist.

- [ ] **Step 3: Implement `plan.go` (types + helpers)**

Create `internal/plan/plan.go`:

```go
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

// Package plan computes the desired set of Kubernetes objects for a Temporal
// custom resource. It is pure (no client, no IO) so it can be shared by the
// operator's controllers and by the WebAssembly preview tool.
package plan

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Phase labels which operator concern produces an object. It is surfaced in the
// preview UI as a badge and documents why each object exists.
type Phase string

const (
	PhasePersistenceSchema Phase = "Persistence & Schema"
	PhaseCoreServices      Phase = "Core Services"
	PhaseMTLS              Phase = "mTLS"
	PhaseUI                Phase = "UI"
	PhaseMonitoring        Phase = "Monitoring"
)

// PlannedObject is a desired object tagged with the phase that produces it.
type PlannedObject struct {
	Object client.Object
	Phase  Phase
}

func tag(phase Phase, objs ...client.Object) []PlannedObject {
	out := make([]PlannedObject, 0, len(objs))
	for _, o := range objs {
		if o == nil {
			continue
		}
		out = append(out, PlannedObject{Object: o, Phase: phase})
	}
	return out
}
```

- [ ] **Step 4: Implement `mtls.go`**

Create `internal/plan/mtls.go`:

```go
package plan

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// PlanMTLS returns the internode and frontend Certificates when cert-manager
// mTLS is enabled, and nothing otherwise.
func PlanMTLS(cluster *temporalv1alpha1.TemporalCluster) []PlannedObject {
	if !resources.MTLSEnabled(cluster) {
		return nil
	}
	return tag(PhaseMTLS,
		client.Object(resources.BuildInternodeCertificate(cluster)),
		client.Object(resources.BuildFrontendCertificate(cluster)),
	)
}
```

- [ ] **Step 5: Implement `ui.go`**

Create `internal/plan/ui.go`:

```go
package plan

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// PlanUI returns the temporal-ui objects when the UI is enabled: an optional
// client certificate (under mTLS), the Deployment, the Service, and an optional
// Ingress. It mirrors the controller's reconcileUI ordering.
func PlanUI(cluster *temporalv1alpha1.TemporalCluster) []PlannedObject {
	if cluster.Spec.UI == nil || !cluster.Spec.UI.Enabled {
		return nil
	}
	objs := make([]client.Object, 0, 4)
	if resources.MTLSEnabled(cluster) {
		objs = append(objs, resources.BuildUIClientCertificate(cluster))
	}
	objs = append(objs, resources.BuildUIDeployment(cluster), resources.BuildUIService(cluster))
	if ingress := resources.BuildUIIngress(cluster); ingress != nil {
		objs = append(objs, ingress)
	}
	return tag(PhaseUI, objs...)
}
```

- [ ] **Step 6: Implement `monitoring.go`**

Create `internal/plan/monitoring.go`:

```go
package plan

import (
	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// PlanMonitoring returns the ServiceMonitor when it is enabled in the spec. The
// controller additionally gates creation on the ServiceMonitor CRD being
// installed; that is a runtime concern and stays in the controller.
func PlanMonitoring(cluster *temporalv1alpha1.TemporalCluster) []PlannedObject {
	if cluster.Spec.Metrics == nil || cluster.Spec.Metrics.ServiceMonitor == nil || !cluster.Spec.Metrics.ServiceMonitor.Enabled {
		return nil
	}
	return tag(PhaseMonitoring, resources.BuildServiceMonitor(cluster))
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/plan/`
Expected: PASS.

- [ ] **Step 8: Lint and commit**

```bash
make lint
git add internal/plan/
git commit -s -m "feat(plan): add pure planner package with mTLS, UI, monitoring"
```

---

## Task 3: Add `PlanServices`, `PlanSchemaJobs`, and the `PlanFromSpec` orchestrator

`PlanServices` takes the already-rendered config (the operator renders it with real credentials; the preview renders it with placeholders) plus per-service inputs. `PlanSchemaJobs` produces the initial setup jobs for both stores. `PlanFromSpec` is the no-IO orchestrator used by the preview.

**Files:**
- Create: `internal/plan/services.go`
- Create: `internal/plan/schema.go`
- Modify: `internal/plan/plan.go` (add `PlanFromSpec`)
- Test: `internal/plan/services_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/plan/services_test.go`:

```go
package plan

import (
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func TestPlanServicesObjects(t *testing.T) {
	c := testCluster()
	in := ServicesInput{
		RenderedConfig:        "config: yes",
		RenderedDynamicConfig: "{}\n",
		ConfigHash:            "deadbeef",
		ServiceVersions:       nil,
		MTLS:                  nil,
	}
	got := PlanServices(c, in)
	// config Secret + dynamic ConfigMap + 4 services * (Deployment+Service+PDB)
	// + 1 frontend Service = 2 + 12 + 1 = 15.
	if len(got) != 15 {
		t.Fatalf("expected 15 objects, got %d (%v)", len(got), kinds(got))
	}
	for _, o := range got {
		if o.Phase != PhaseCoreServices {
			t.Errorf("expected PhaseCoreServices, got %s", o.Phase)
		}
	}
}

func TestPlanSchemaJobs(t *testing.T) {
	c := testCluster()
	c.Spec.Persistence = temporalv1alpha1.PersistenceSpec{
		DefaultStore:    temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{PluginName: "postgres12"}},
		VisibilityStore: temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{PluginName: "postgres12"}},
	}
	got := PlanSchemaJobs(c)
	if len(got) != 2 {
		t.Fatalf("expected one setup Job per store, got %d (%v)", len(got), kinds(got))
	}
	for _, o := range got {
		if o.Phase != PhasePersistenceSchema {
			t.Errorf("expected PhasePersistenceSchema, got %s", o.Phase)
		}
	}
}

func TestPlanFromSpecCoversAllPhases(t *testing.T) {
	c := testCluster()
	c.Spec.Persistence = temporalv1alpha1.PersistenceSpec{
		DefaultStore:    temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{PluginName: "postgres12"}},
		VisibilityStore: temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{PluginName: "postgres12"}},
	}
	c.Spec.UI = &temporalv1alpha1.UISpec{Enabled: true, Version: "2.34.0"}
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager", IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"}}

	got, err := PlanFromSpec(c)
	if err != nil {
		t.Fatalf("PlanFromSpec error: %v", err)
	}
	seen := map[Phase]bool{}
	for _, o := range got {
		seen[o.Phase] = true
	}
	for _, p := range []Phase{PhasePersistenceSchema, PhaseCoreServices, PhaseMTLS, PhaseUI} {
		if !seen[p] {
			t.Errorf("expected objects for phase %s", p)
		}
	}
}
```

If `SQLDatastoreSpec` does not have a `PluginName` field, replace it with the minimal valid SQL spec the codebase uses (check `api/v1alpha1/persistence_types.go`) so `temporal.RenderClusterConfig` and `BuildSchemaJob` succeed. The assertion that matters is the phase coverage, not the exact field.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/plan/ -run 'PlanServices|PlanSchema|PlanFromSpec'`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Implement `services.go`**

Create `internal/plan/services.go`:

```go
package plan

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// ServicesInput carries the inputs the controller derives from IO (rendered
// config, config hash, per-service image versions, and mTLS mounts) so that
// PlanServices itself stays pure. The preview supplies placeholder-credentialed
// renders and an empty cert hash.
type ServicesInput struct {
	RenderedConfig        string
	RenderedDynamicConfig string
	ConfigHash            string
	ServiceVersions       map[string]string
	MTLS                  *resources.MTLSMounts
}

// PlanServices returns the config Secret, dynamic-config ConfigMap, and the
// Deployment/headless Service/PodDisruptionBudget for each enabled service
// (plus the frontend Service). Ordering matches the controller's
// reconcileServices so golden comparisons stay stable.
func PlanServices(cluster *temporalv1alpha1.TemporalCluster, in ServicesInput) []PlannedObject {
	objs := []client.Object{
		resources.BuildConfigSecret(cluster, in.RenderedConfig),
		resources.BuildDynamicConfigMap(cluster, in.RenderedDynamicConfig),
	}
	for _, svc := range resources.EnabledServices(cluster) {
		version := in.ServiceVersions[svc.Name]
		objs = append(objs,
			resources.BuildDeployment(cluster, svc, in.ConfigHash, version, in.MTLS),
			resources.BuildHeadlessService(cluster, svc),
			resources.BuildPodDisruptionBudget(cluster, svc),
		)
		if svc.Name == resources.ServiceFrontend {
			objs = append(objs, resources.BuildFrontendService(cluster, svc))
		}
	}
	return tag(PhaseCoreServices, objs...)
}
```

- [ ] **Step 4: Implement `schema.go`**

Create `internal/plan/schema.go`:

```go
package plan

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// postgresSchemaDir mirrors the controller's on-image schema directory for the
// postgres12 plugin.
const postgresSchemaDir = "v12"

// PlanSchemaJobs returns the initial setup-schema Job for the default and
// visibility stores. The operator additionally runs update-schema Jobs based on
// live schema introspection; the preview shows the from-scratch setup case,
// which is the representative "what gets created" view.
func PlanSchemaJobs(cluster *temporalv1alpha1.TemporalCluster) []PlannedObject {
	stores := []struct {
		name  resources.SchemaStore
		store temporalv1alpha1.DatastoreSpec
	}{
		{resources.StoreDefault, cluster.Spec.Persistence.DefaultStore},
		{resources.StoreVisibility, cluster.Spec.Persistence.VisibilityStore},
	}
	objs := make([]client.Object, 0, len(stores))
	for _, s := range stores {
		objs = append(objs, resources.BuildSchemaJob(resources.SchemaJobParams{
			Cluster:          cluster,
			SQLSpec:          s.store.SQL,
			CassandraSpec:    s.store.Cassandra,
			Store:            s.name,
			Action:           resources.ActionSetup,
			SchemaVersionDir: postgresSchemaDir,
		}))
	}
	return tag(PhasePersistenceSchema, objs...)
}
```

The constants `resources.StoreDefault` and `resources.StoreVisibility` (type `resources.SchemaStore`) are defined in `internal/resources/schemajob.go` — confirmed present.

- [ ] **Step 5: Implement `PlanFromSpec` in `plan.go`**

Append to `internal/plan/plan.go`:

```go
import (
	"fmt"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// PlanFromSpec computes the full desired object set for a cluster using only its
// spec, with placeholder credentials. It is the entry point for the WebAssembly
// preview. The cluster is expected to already be defaulted by the caller.
func PlanFromSpec(cluster *temporalv1alpha1.TemporalCluster) ([]PlannedObject, error) {
	opts := temporal.BuildOptions{
		PublicClientHostPort: fmt.Sprintf("%s.%s.svc:%d",
			resources.FrontendServiceName(cluster.Name), cluster.Namespace,
			temporal.DefaultServicePorts()["frontend"].GRPCPort),
	}
	renderedConfig, err := temporal.RenderClusterConfig(cluster, opts)
	if err != nil {
		return nil, fmt.Errorf("rendering config: %w", err)
	}
	renderedDynamic, _, err := temporal.RenderDynamicConfig(cluster.Spec.DynamicConfig, cluster.Spec.Version)
	if err != nil {
		return nil, fmt.Errorf("rendering dynamic config: %w", err)
	}

	var mtls *resources.MTLSMounts
	if resources.MTLSEnabled(cluster) {
		mtls = &resources.MTLSMounts{
			Enabled:         true,
			InternodeSecret: resources.InternodeCertName(cluster.Name),
			FrontendSecret:  resources.FrontendCertName(cluster.Name),
		}
	}

	servicesInput := ServicesInput{
		RenderedConfig:        renderedConfig,
		RenderedDynamicConfig: renderedDynamic,
		ConfigHash:            resources.ConfigHash(renderedConfig),
		MTLS:                  mtls,
	}

	var out []PlannedObject
	out = append(out, PlanSchemaJobs(cluster)...)
	out = append(out, PlanMTLS(cluster)...)
	out = append(out, PlanServices(cluster, servicesInput)...)
	out = append(out, PlanUI(cluster)...)
	out = append(out, PlanMonitoring(cluster)...)
	return out, nil
}
```

Merge the new imports into the existing single `import (...)` block in `plan.go` (do not create a second import block).

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/plan/`
Expected: PASS.

- [ ] **Step 7: Verify the package still builds for wasm**

Run: `GOOS=js GOARCH=wasm go build ./internal/plan/`
Expected: no output, exit 0.

- [ ] **Step 8: Lint and commit**

```bash
make lint
git add internal/plan/
git commit -s -m "feat(plan): add services, schema-job, and PlanFromSpec planners"
```

---

## Task 4: Route the controller through the planner (behavior-preserving)

Make the controller's reconcile functions construct objects via the planner instead of inline `Build*` calls. Phase gating, status logic, and the ServiceMonitor-CRD check stay exactly where they are. The existing controller/envtest suite is the safety net — it must pass unchanged.

**Files:**
- Modify: `internal/controller/temporalcluster_services.go:54-80`
- Modify: `internal/controller/temporalcluster_mtls.go:48-58`
- Modify: `internal/controller/temporalcluster_ui.go:32-48`
- Modify: `internal/controller/temporalcluster_monitoring.go:42`
- Test: existing `internal/controller/*_test.go` (no new tests; this is a refactor)

- [ ] **Step 1: Capture the baseline**

Run: `make test`
Expected: PASS. Record that the suite is green before refactoring.

- [ ] **Step 2: Refactor `reconcileServices`**

In `internal/controller/temporalcluster_services.go`, replace the object-building block (the part from `dynamicCM := ...` through the end of the `for _, svc := range services` loop) with planner-driven application. The function becomes:

```go
func (r *TemporalClusterReconciler) reconcileServices(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, serviceVersions map[string]string) error {
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionSchemaReady) {
		return nil
	}

	rendered, err := r.renderConfig(ctx, cluster)
	if err != nil {
		return err
	}

	planned := plan.PlanServices(cluster, plan.ServicesInput{
		RenderedConfig:        rendered.config,
		RenderedDynamicConfig: rendered.dynamicConfig,
		ConfigHash:            resources.ConfigHash(rendered.config),
		ServiceVersions:       serviceVersions,
		MTLS:                  r.mtlsMounts(ctx, cluster),
	})
	for _, p := range planned {
		if err := r.apply(ctx, cluster, p.Object); err != nil {
			return err
		}
	}

	return r.rollupServiceStatus(ctx, cluster, resources.EnabledServices(cluster))
}
```

Add `"github.com/bmorton/temporal-operator/internal/plan"` to the file's imports.

- [ ] **Step 3: Refactor `reconcileMTLS`**

In `internal/controller/temporalcluster_mtls.go`, replace the two `resources.Build*Certificate` + apply lines with:

```go
	for _, p := range plan.PlanMTLS(cluster) {
		if err := r.apply(ctx, cluster, p.Object); err != nil {
			return err
		}
	}
```

Keep the subsequent status block, but source the certificate names from the helpers instead of local variables:

```go
	ready, failed := r.certificatesStatus(ctx, cluster,
		resources.InternodeCertName(cluster.Name), resources.FrontendCertName(cluster.Name))
```

Add the `plan` import. Remove the now-unused `internode`/`frontend` locals.

- [ ] **Step 4: Refactor `reconcileUI`**

In `internal/controller/temporalcluster_ui.go`, replace the body after the enabled-guard with:

```go
	for _, p := range plan.PlanUI(cluster) {
		if err := r.apply(ctx, cluster, p.Object); err != nil {
			return err
		}
	}
	return nil
```

Add the `plan` import; remove the now-unused `resources` references if that import becomes unused (keep it if other functions in the file still use it).

- [ ] **Step 5: Refactor `reconcileMonitoring`**

In `internal/controller/temporalcluster_monitoring.go`, keep the spec-enabled and CRD-installed guards. Replace the final apply line:

```go
	planned := plan.PlanMonitoring(cluster)
	for _, p := range planned {
		if err := r.apply(ctx, cluster, p.Object); err != nil {
			return err
		}
	}
	return nil
```

Add the `plan` import.

- [ ] **Step 6: Run the full suite to prove behavior is unchanged**

Run: `make test`
Expected: PASS — identical to the Step 1 baseline.

- [ ] **Step 7: Lint and commit**

```bash
make lint
git add internal/controller/
git commit -s -m "refactor(controller): build objects via internal/plan planner"
```

---

## Task 5: Add the `cmd/preview-wasm` shim

A js/wasm program exposing `temporalPreview(kind, yaml)` to the browser: parse → default → validate → plan → marshal to YAML (decoding Secret data).

**Files:**
- Create: `cmd/preview-wasm/main.go`
- Create: `cmd/preview-wasm/wasm_build_test.go`

- [ ] **Step 1: Write the compile-guard test**

Create `cmd/preview-wasm/wasm_build_test.go`:

```go
//go:build !js

package main

import (
	"os/exec"
	"testing"
)

// TestWASMCompiles guards against changes that break the js/wasm build of the
// preview shim, which would silently break the docs site.
func TestWASMCompiles(t *testing.T) {
	cmd := exec.Command("go", "build", "-o", t.TempDir()+"/preview.wasm", ".")
	cmd.Env = append(cmd.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("js/wasm build failed: %v\n%s", err, out)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/preview-wasm/`
Expected: FAIL — build fails because `main.go` does not exist yet.

- [ ] **Step 3: Implement `main.go`**

Create `cmd/preview-wasm/main.go`:

```go
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

//go:build js && wasm

// Command preview-wasm exposes the operator's object planner to the browser. It
// registers a global temporalPreview(kind, yaml) function that returns a JSON
// string describing every object the operator would create.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall/js"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/plan"
	webhookv1alpha1 "github.com/bmorton/temporal-operator/internal/webhook/v1alpha1"
)

type previewObject struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Phase string `json:"phase"`
	YAML  string `json:"yaml"`
}

type previewResult struct {
	Objects []previewObject `json:"objects"`
	Errors  []string        `json:"errors"`
}

func result(objs []previewObject, errs ...string) string {
	if objs == nil {
		objs = []previewObject{}
	}
	if errs == nil {
		errs = []string{}
	}
	b, _ := json.Marshal(previewResult{Objects: objs, Errors: errs})
	return string(b)
}

// previewTemporalCluster handles the one fully-wired kind. Additional kinds are
// added by extending the switch in temporalPreview.
func previewTemporalCluster(yamlSrc string) string {
	var cluster temporalv1alpha1.TemporalCluster
	if err := yaml.Unmarshal([]byte(yamlSrc), &cluster); err != nil {
		return result(nil, fmt.Sprintf("invalid YAML: %v", err))
	}
	if cluster.Namespace == "" {
		cluster.Namespace = "default"
	}

	ctx := context.Background()
	defaulter := &webhookv1alpha1.TemporalClusterCustomDefaulter{}
	if err := defaulter.Default(ctx, &cluster); err != nil {
		return result(nil, fmt.Sprintf("defaulting failed: %v", err))
	}

	validator := &webhookv1alpha1.TemporalClusterCustomValidator{}
	if _, err := validator.ValidateCreate(ctx, &cluster); err != nil {
		return result(nil, fmt.Sprintf("validation failed: %v", err))
	}

	planned, err := plan.PlanFromSpec(&cluster)
	if err != nil {
		return result(nil, err.Error())
	}

	objs := make([]previewObject, 0, len(planned))
	for _, p := range planned {
		rendered, err := renderObject(p.Object)
		if err != nil {
			return result(nil, fmt.Sprintf("rendering %s: %v", p.Object.GetName(), err))
		}
		objs = append(objs, previewObject{
			Kind:  p.Object.GetObjectKind().GroupVersionKind().Kind,
			Name:  p.Object.GetName(),
			Phase: string(p.Phase),
			YAML:  rendered,
		})
	}
	return result(objs)
}

// renderObject marshals an object to YAML, decoding Secret data to readable text
// so the rendered Temporal config is visible instead of base64.
func renderObject(obj client.Object) (string, error) {
	if secret, ok := obj.(*corev1.Secret); ok && len(secret.Data) > 0 {
		if secret.StringData == nil {
			secret.StringData = map[string]string{}
		}
		for k, v := range secret.Data {
			secret.StringData[k] = string(v)
		}
		secret.Data = nil
	}
	b, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func temporalPreview(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return result(nil, "temporalPreview(kind, yaml) requires two arguments")
	}
	kind := args[0].String()
	src := args[1].String()
	switch kind {
	case "TemporalCluster":
		return previewTemporalCluster(src)
	default:
		return result(nil, fmt.Sprintf("kind %q is not supported yet", kind))
	}
}

func main() {
	js.Global().Set("temporalPreview", js.FuncOf(temporalPreview))
	select {} // keep the Go runtime alive for callbacks
}
```

Before building, verify the exact exported names by reading `internal/webhook/v1alpha1/temporalcluster_webhook.go`: `TemporalClusterCustomDefaulter.Default(ctx, runtime.Object)` and `TemporalClusterCustomValidator.ValidateCreate(ctx, runtime.Object) (admission.Warnings, error)` (both confirmed present). If the defaulter is hidden behind a constructor, use it instead of a bare struct literal.

- [ ] **Step 4: Run the compile guard to verify it passes**

Run: `go test ./cmd/preview-wasm/`
Expected: PASS.

- [ ] **Step 5: Smoke-test the wasm output exists**

Run: `GOOS=js GOARCH=wasm go build -o /tmp/preview.wasm ./cmd/preview-wasm/ && ls -lh /tmp/preview.wasm && rm /tmp/preview.wasm`
Expected: a multi-MB `.wasm` file is produced.

- [ ] **Step 6: Commit**

```bash
git add cmd/preview-wasm/
git commit -s -m "feat(preview-wasm): add browser shim exposing temporalPreview"
```

---

## Task 6: Build script, Makefile target, and .gitignore

Reproducibly build the wasm, copy Go's `wasm_exec.js`, and copy example CRs into the docs static dir.

**Files:**
- Create: `hack/build-preview.sh`
- Modify: `Makefile`
- Modify: `.gitignore`

- [ ] **Step 1: Implement the build script**

Create `hack/build-preview.sh`:

```bash
#!/usr/bin/env bash
# Builds the WebAssembly resource-preview tool and stages its static assets
# under docs/static/preview. Generated files are git-ignored and rebuilt on
# every docs deploy, so the tool can never drift from the operator's code.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="${repo_root}/docs/static/preview"
examples_dir="${out_dir}/examples"

mkdir -p "${examples_dir}"

echo "Building preview.wasm..."
GOOS=js GOARCH=wasm go build -ldflags="-s -w" \
  -o "${out_dir}/temporal-operator-preview.wasm" \
  "${repo_root}/cmd/preview-wasm"

echo "Copying wasm_exec.js..."
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "${out_dir}/wasm_exec.js"

echo "Staging example TemporalCluster manifests..."
{
  printf '['
  first=true
  while IFS= read -r file; do
    rel="${file#"${repo_root}/examples/"}"
    name="${rel%.yaml}"
    name="${name//\//-}"        # e.g. cluster-upgrade/02-temporalcluster -> cluster-upgrade-02-temporalcluster
    cp "${file}" "${examples_dir}/${name}.yaml"
    if [ "${first}" = true ]; then first=false; else printf ','; fi
    printf '{"name":"%s","file":"examples/%s.yaml"}' "${name}" "${name}"
  done < <(grep -rl --include='*.yaml' '^kind: TemporalCluster' "${repo_root}/examples" | sort)
  printf ']'
} > "${examples_dir}/index.json"

echo "Preview assets written to ${out_dir}"
```

The `grep -rl '^kind: TemporalCluster'` selects every example file that declares a TemporalCluster (verified: `examples/*/temporalcluster.yaml`, plus the numbered upgrade/cnpg variants). Multi-document files such as `examples/cluster-with-namespaces-and-search-attributes/resources.yaml` will also match; the wasm shim parses only the first YAML document, which is acceptable for an example preset. If you prefer to exclude multi-doc files, add `| while read f; do [ "$(grep -c '^kind:' "$f")" = 1 ] && echo "$f"; done` to the pipeline.

- [ ] **Step 2: Make the script executable and test it**

Run:
```bash
chmod +x hack/build-preview.sh && ./hack/build-preview.sh && ls docs/static/preview/ && cat docs/static/preview/examples/index.json
```
Expected: `temporal-operator-preview.wasm`, `wasm_exec.js`, and `examples/index.json` (a non-empty JSON array) are produced.

- [ ] **Step 3: Add the Makefile target**

Add to `Makefile` (in the build section, after the `build` target):

```makefile
.PHONY: preview-wasm
preview-wasm: ## Build the WebAssembly resource-preview tool into docs/static/preview.
	./hack/build-preview.sh
```

- [ ] **Step 4: Verify the target**

Run: `make preview-wasm`
Expected: rebuilds the assets without error.

- [ ] **Step 5: Ignore generated assets**

Append to `.gitignore`:

```gitignore
# Generated WebAssembly resource-preview assets (rebuilt on every docs deploy)
docs/static/preview/temporal-operator-preview.wasm
docs/static/preview/wasm_exec.js
docs/static/preview/examples/
```

- [ ] **Step 6: Confirm generated files are ignored**

Run: `git status --porcelain docs/static/preview/`
Expected: no `temporal-operator-preview.wasm`, `wasm_exec.js`, or `examples/` entries appear.

- [ ] **Step 7: Commit**

```bash
git add hack/build-preview.sh Makefile .gitignore
git commit -s -m "build(preview): add build script, make target, and gitignore"
```

---

## Task 7: The web page (HTML + JS)

The committed UI: paste box + example dropdown on the left, tabs by Kind on the right.

**Files:**
- Create: `docs/static/preview/index.html`
- Create: `docs/static/preview/app.js`

- [ ] **Step 1: Create `index.html`**

Create `docs/static/preview/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>temporal-operator — Resource Preview</title>
  <style>
    :root { --border: #d0d7de; --bg: #f6f8fa; --accent: #0969da; }
    * { box-sizing: border-box; }
    body { font-family: system-ui, sans-serif; margin: 0; color: #1f2328; }
    header { padding: 12px 20px; border-bottom: 1px solid var(--border); }
    header h1 { font-size: 18px; margin: 0; }
    header p { margin: 4px 0 0; color: #57606a; font-size: 13px; }
    main { display: grid; grid-template-columns: 380px 1fr; gap: 16px; padding: 16px; height: calc(100vh - 70px); }
    .pane { display: flex; flex-direction: column; min-height: 0; }
    textarea { flex: 1; width: 100%; font-family: ui-monospace, monospace; font-size: 12px; padding: 8px; border: 1px solid var(--border); border-radius: 6px; resize: none; }
    .controls { display: flex; gap: 8px; margin-bottom: 8px; align-items: center; }
    select, button { font-size: 13px; padding: 6px 10px; border: 1px solid var(--border); border-radius: 6px; background: #fff; cursor: pointer; }
    button.primary { background: var(--accent); color: #fff; border-color: var(--accent); }
    #status { font-size: 12px; color: #57606a; margin-top: 8px; }
    #errors { color: #cf222e; font-size: 13px; white-space: pre-wrap; margin: 8px 0; }
    .tabs { display: flex; flex-wrap: wrap; gap: 4px; border-bottom: 1px solid var(--border); margin-bottom: 8px; }
    .tab { padding: 6px 10px; border: 1px solid var(--border); border-bottom: none; border-radius: 6px 6px 0 0; background: var(--bg); cursor: pointer; font-size: 13px; }
    .tab.active { background: #fff; font-weight: 600; }
    .objects { overflow: auto; min-height: 0; flex: 1; }
    .object { border: 1px solid var(--border); border-radius: 6px; margin-bottom: 8px; }
    .object summary { padding: 8px 10px; cursor: pointer; display: flex; align-items: center; gap: 8px; }
    .badge { font-size: 11px; background: var(--bg); border: 1px solid var(--border); border-radius: 10px; padding: 1px 8px; color: #57606a; }
    .object pre { margin: 0; padding: 10px; background: var(--bg); overflow: auto; font-size: 12px; border-top: 1px solid var(--border); }
    .copy { margin-left: auto; }
  </style>
</head>
<body>
  <header>
    <h1>Resource Preview <span class="badge">alpha</span></h1>
    <p>Paste a TemporalCluster and see every Kubernetes object the operator would create. Runs entirely in your browser.</p>
  </header>
  <main>
    <section class="pane">
      <div class="controls">
        <select id="example"><option value="">Load example…</option></select>
        <button id="render" class="primary">Preview &rarr;</button>
      </div>
      <textarea id="input" spellcheck="false" placeholder="apiVersion: temporal.bmor10.com/v1alpha1&#10;kind: TemporalCluster&#10;..."></textarea>
      <div id="status">Loading WebAssembly…</div>
    </section>
    <section class="pane">
      <div id="errors"></div>
      <div class="tabs" id="tabs"></div>
      <div class="objects" id="objects"></div>
    </section>
  </main>
  <script src="wasm_exec.js"></script>
  <script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Create `app.js`**

Create `docs/static/preview/app.js`:

```javascript
// Loads the operator's WebAssembly planner and renders its output as tabs by Kind.
const els = {
  input: document.getElementById("input"),
  status: document.getElementById("status"),
  errors: document.getElementById("errors"),
  tabs: document.getElementById("tabs"),
  objects: document.getElementById("objects"),
  example: document.getElementById("example"),
  render: document.getElementById("render"),
};

let ready = false;
let current = { objects: [], activeKind: null };

async function initWasm() {
  const go = new Go();
  const resp = await fetch("temporal-operator-preview.wasm");
  const { instance } = await WebAssembly.instantiateStreaming(resp, go.importObject);
  go.run(instance); // registers window.temporalPreview, then blocks on select{}
  ready = true;
  els.status.textContent = "Ready. Paste a TemporalCluster and click Preview.";
}

async function loadExamples() {
  try {
    const list = await (await fetch("examples/index.json")).json();
    for (const ex of list) {
      const opt = document.createElement("option");
      opt.value = ex.file;
      opt.textContent = ex.name;
      els.example.appendChild(opt);
    }
  } catch (_) { /* examples are optional */ }
}

els.example.addEventListener("change", async () => {
  if (!els.example.value) return;
  els.input.value = await (await fetch(els.example.value)).text();
});

els.render.addEventListener("click", () => {
  if (!ready) { els.status.textContent = "Still loading WebAssembly…"; return; }
  const raw = window.temporalPreview("TemporalCluster", els.input.value);
  const res = JSON.parse(raw);
  els.errors.textContent = (res.errors || []).join("\n");
  current.objects = res.objects || [];
  current.activeKind = null;
  renderTabs();
});

function renderTabs() {
  const kinds = [...new Set(current.objects.map((o) => o.kind))].sort();
  els.tabs.innerHTML = "";
  if (!current.activeKind && kinds.length) current.activeKind = kinds[0];
  for (const kind of kinds) {
    const count = current.objects.filter((o) => o.kind === kind).length;
    const tab = document.createElement("div");
    tab.className = "tab" + (kind === current.activeKind ? " active" : "");
    tab.textContent = `${kind} (${count})`;
    tab.addEventListener("click", () => { current.activeKind = kind; renderTabs(); });
    els.tabs.appendChild(tab);
  }
  renderObjects();
}

function renderObjects() {
  els.objects.innerHTML = "";
  for (const obj of current.objects.filter((o) => o.kind === current.activeKind)) {
    const d = document.createElement("details");
    d.className = "object";
    const s = document.createElement("summary");
    s.innerHTML = `<strong>${obj.name}</strong><span class="badge">${obj.phase}</span>`;
    const copy = document.createElement("button");
    copy.className = "copy";
    copy.textContent = "Copy";
    copy.addEventListener("click", (e) => { e.preventDefault(); navigator.clipboard.writeText(obj.yaml); });
    s.appendChild(copy);
    const pre = document.createElement("pre");
    pre.textContent = obj.yaml;
    d.appendChild(s);
    d.appendChild(pre);
    els.objects.appendChild(d);
  }
}

loadExamples();
initWasm().catch((e) => { els.status.textContent = "Failed to load WebAssembly: " + e; });
```

- [ ] **Step 3: Manually verify the page locally**

Run:
```bash
make preview-wasm
cd docs/static/preview && python3 -m http.server 8087 &
sleep 1 && curl -sf http://localhost:8087/index.html >/dev/null && echo "page served" && curl -sf http://localhost:8087/examples/index.json | head -c 200
```
Then open `http://localhost:8087/` in a browser, pick an example, click **Preview**, and confirm tabs render objects. Stop the server with `kill %1` when done.
Expected: `page served` prints and the examples JSON is non-empty. (Manual browser check confirms tabs + YAML render.)

- [ ] **Step 4: Commit**

```bash
git add docs/static/preview/index.html docs/static/preview/app.js
git commit -s -m "feat(docs): add resource-preview web page"
```

---

## Task 8: Hugo content pages linking the tool

Add a "Tools" section to the docs nav that links to the standalone preview page.

**Files:**
- Create: `docs/content/tools/_index.md`
- Create: `docs/content/tools/resource-preview.md`

- [ ] **Step 1: Create the section index**

Create `docs/content/tools/_index.md`:

```markdown
---
title: Tools
weight: 80
bookCollapseSection: true
---

# Tools

Browser-based tools for working with temporal-operator.

- [Resource Preview]({{< relref "resource-preview" >}}) — paste a `TemporalCluster`
  and see every Kubernetes object the operator would create.
```

- [ ] **Step 2: Create the tool page**

Create `docs/content/tools/resource-preview.md`:

```markdown
---
title: Resource Preview
weight: 10
---

# Resource Preview

The Resource Preview tool runs the operator's own object planner — compiled to
WebAssembly — entirely in your browser. Paste a `TemporalCluster` custom
resource and it shows every Kubernetes object the operator would create, grouped
into tabs by kind, after applying the same defaulting and validation the
operator's admission webhooks perform.

Because the tool is built from the operator's source on every docs deploy, the
preview stays in lockstep with the operator and never drifts.

{{< button href="/temporal-operator/preview/" >}}Open Resource Preview{{< /button >}}

> **Alpha.** The tool currently supports `TemporalCluster`. It uses placeholder
> credentials when rendering configuration, so secret values shown are not real.
```

The hugo-book theme provides a `button` shortcode (`docs/themes/hugo-book/layouts/shortcodes/button.html`), so the button line works. The shortcode also accepts `relref="tools/resource-preview"` for internal links; the absolute `href` used here points at the standalone static page under `/preview/`.

- [ ] **Step 3: Build the site locally to verify it renders**

Run:
```bash
make preview-wasm
hugo --source docs --minify --baseURL "http://localhost/temporal-operator/" 2>&1 | tail -5
ls docs/public/preview/ | head
```
Expected: Hugo build succeeds and `docs/public/preview/` contains `index.html`, `app.js`, `wasm_exec.js`, and the wasm file (Hugo copies `static/` verbatim).

- [ ] **Step 4: Commit**

```bash
git add docs/content/tools/
git commit -s -m "docs: link the resource-preview tool from the site nav"
```

---

## Task 9: Wire the wasm build into the docs CI workflow

Build the wasm before Hugo on deploy, and add a compile guard on PRs touching the relevant Go packages.

**Files:**
- Modify: `.github/workflows/docs.yml`

- [ ] **Step 1: Add Go setup + wasm build to the build-deploy job**

In `.github/workflows/docs.yml`, in the `build-deploy` job, after the `actions/checkout` step and before `Setup Hugo`, insert:

```yaml
      - name: Setup Go
        uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0
        with:
          go-version: "1.26.x"
      - name: Build preview WebAssembly
        run: make preview-wasm
```

Use the exact pinned SHA for `actions/setup-go` already used in `.github/workflows/ci.yml` (`actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0`) so the repo's zizmor hash-pinning convention is satisfied.

- [ ] **Step 2: Add a compile-guard job for PRs**

Add a new job to `.github/workflows/docs.yml` that builds the wasm on pull requests (so drift fails fast even though the deploy job is push-only):

```yaml
  preview-wasm:
    name: Preview wasm compiles
    runs-on: namespace-profile-temporal-operator
    steps:
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6
        with:
          persist-credentials: false
      - name: Setup Go
        uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0
        with:
          go-version: "1.26.x"
      - name: Build preview WebAssembly
        run: make preview-wasm
```

Also broaden the workflow's `on.push.paths` and `on.pull_request.paths` to include the Go sources the wasm depends on, so the workflow runs when they change:

```yaml
    paths:
      - "docs/**"
      - "examples/**"
      - "internal/plan/**"
      - "internal/resources/**"
      - "internal/temporal/**"
      - "internal/webhook/**"
      - "cmd/preview-wasm/**"
      - "api/**"
```

Apply that `paths` list to both the `push` and `pull_request` triggers.

- [ ] **Step 3: Validate the workflow YAML**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/docs.yml'))" && echo "valid yaml"`
Expected: `valid yaml`.

- [ ] **Step 4: Run zizmor locally if available, else rely on CI**

Run: `command -v zizmor >/dev/null && zizmor .github/workflows/docs.yml || echo "zizmor not installed locally; CI will check"`
Expected: no findings, or the skip message.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/docs.yml
git commit -s -m "ci(docs): build preview wasm and guard its compilation"
```

---

## Task 10: Final verification and README mention

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Run the full Go suite and lint**

Run: `make test && make lint`
Expected: both PASS.

- [ ] **Step 2: Rebuild preview and the site end-to-end**

Run:
```bash
make preview-wasm && hugo --source docs --minify --baseURL "http://localhost/temporal-operator/" 2>&1 | tail -3
```
Expected: Hugo build succeeds with the preview assets present in `docs/public/preview/`.

- [ ] **Step 3: Add a short README pointer**

Add a bullet to the README's features or docs section (find the existing docs link and place it nearby):

```markdown
- **Resource Preview** — a browser tool (no install) that shows every Kubernetes
  object the operator would create for a pasted `TemporalCluster`. See the
  [Resource Preview](https://bmorton.github.io/temporal-operator/tools/resource-preview/)
  page.
```

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -s -m "docs: mention the resource-preview tool in the README"
```

- [ ] **Step 5: Final review of the whole change**

Run: `git log --oneline origin/main..HEAD`
Expected: a clean sequence of the commits above, each signed off.

---

## Self-Review Notes

- **Spec coverage:** planner extraction (Tasks 1–4), defaulting+validation in the shim (Task 5), tabs-by-Kind page with phase badges + copy + examples (Tasks 6–8), CI-built never-committed wasm with a drift guard (Tasks 6, 9), extensible `kind` switch (Task 5), other CRDs intentionally stubbed (Task 5 `default:` branch). All spec sections map to a task.
- **Type consistency:** `PlannedObject`, `ServicesInput`, `PlanMTLS/PlanUI/PlanMonitoring/PlanServices/PlanSchemaJobs/PlanFromSpec`, and `Phase*` constants are defined in Task 2–3 and used consistently in Tasks 4–5. The shim uses `temporalPreview` everywhere (HTML/JS/Go agree).
- **Verification gaps confirmed during planning:** `resources.StoreDefault`/`StoreVisibility` constant names, example-manifest selection via `grep -rl '^kind: TemporalCluster'`, the hugo-book `button` shortcode, and the pinned `actions/setup-go@4a36011…# v6.4.0` SHA are all verified against the codebase. Remaining items to confirm at implementation time: the minimal valid `SQLDatastoreSpec` fields for the `PlanFromSpec` test (Task 3 Step 1 notes how), and that `temporal.RenderClusterConfig` succeeds for a spec with placeholder credentials (it does in `configtemplate_test.go`).
