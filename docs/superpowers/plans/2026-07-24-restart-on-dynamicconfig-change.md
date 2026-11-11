# Restart Temporal services on dynamicConfig change — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a change to `spec.dynamicConfig` automatically trigger a rolling restart of the affected `TemporalCluster` service pods.

**Architecture:** Stamp a new `temporal.bmor10.com/dynamicconfig-hash` annotation onto each service pod template, computed from the rendered dynamic config via the existing `resources.ConfigHash` helper. Thread the hash through the pure `plan.ServicesInput` path, mirroring the existing `config-hash` and `cert-hash` patterns. When the annotation value changes, the pod template mutates and Kubernetes performs a rolling update.

**Tech Stack:** Go, controller-runtime, kubebuilder. Design doc: `docs/superpowers/specs/2026-07-24-restart-on-dynamicconfig-change-design.md`.

## Global Constraints

- Pre-1.0 project; keep changes focused, no unrequested breaking changes.
- Conventional Commits; sign off every commit with `git commit -s`.
- No CRD/API type change → do **not** run `make generate`, `make manifests`, or `make helm-chart`.
- `api/v1alpha1` must stay WASM-safe (not touched by this plan).
- Reuse the existing `resources.ConfigHash(content string) string` helper (sha256 → 16 hex chars). Do not add a new hashing helper.
- Annotation constant name/value: `DynamicConfigHashAnnotation = "temporal.bmor10.com/dynamicconfig-hash"`.

---

### Task 1: Add the annotation constant and stamp it in BuildDeployment

**Files:**
- Modify: `internal/resources/labels.go` (annotation constant block, ~lines 32-37)
- Modify: `internal/resources/deployment.go` (`BuildDeployment` signature ~line 131; `podAnnotations` ~line 227)
- Modify: `internal/resources/builders_test.go` (all 6 `BuildDeployment(...)` calls; add annotation assertion near line 96)

**Interfaces:**
- Consumes: existing `resources.ConfigHash(content string) string`.
- Produces:
  - Constant `resources.DynamicConfigHashAnnotation = "temporal.bmor10.com/dynamicconfig-hash"`.
  - New signature: `BuildDeployment(cluster *temporalv1alpha1.TemporalCluster, svc ServiceInfo, configHash, dynamicConfigHash, version string, mtls *MTLSMounts) (*appsv1.Deployment, error)` — `dynamicConfigHash` inserted immediately after `configHash`.

- [ ] **Step 1: Update the failing test first (assertion + call sites)**

In `internal/resources/builders_test.go`, add the new argument (empty string `""` unless noted) to every `BuildDeployment` call. The 6 call sites currently are:

```go
// line 75
dep, err := BuildDeployment(c, svc, "abc123", "dyn456", "", nil)
// line 133
dep, err := BuildDeployment(c, frontend, "abc123", "dyn456", "", nil)
// line 173
dep, err := BuildDeployment(c, frontend, "abc123", "dyn456", "", nil)
// line 242
if _, err := BuildDeployment(c, worker, "abc123", "dyn456", "", nil); err == nil {
// line 259
dep, err := BuildDeployment(c, worker, "abc123", "dyn456", "", nil)
// line 277
dep, err := BuildDeployment(c, svc, "abc123", "dyn456", "", mtls)
```

Then add an assertion in `TestBuildDeployment` right after the existing config-hash check (line 96-98):

```go
	if dep.Spec.Template.Annotations[DynamicConfigHashAnnotation] != "dyn456" {
		t.Errorf("expected dynamicconfig-hash annotation")
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resources/ -run TestBuildDeployment -v`
Expected: FAIL — compile error `too many arguments in call to BuildDeployment` / `undefined: DynamicConfigHashAnnotation`.

- [ ] **Step 3: Add the annotation constant**

In `internal/resources/labels.go`, add after the `CertHashAnnotation` declaration (line 37):

```go
	// DynamicConfigHashAnnotation stamps the rendered dynamic-config hash onto
	// pods so dynamic-config changes trigger a rollout.
	DynamicConfigHashAnnotation = "temporal.bmor10.com/dynamicconfig-hash"
```

- [ ] **Step 4: Update BuildDeployment**

In `internal/resources/deployment.go`, change the signature (line 131):

```go
func BuildDeployment(cluster *temporalv1alpha1.TemporalCluster, svc ServiceInfo, configHash, dynamicConfigHash, version string, mtls *MTLSMounts) (*appsv1.Deployment, error) {
```

And change the `podAnnotations` initialization (line 227):

```go
	podAnnotations := map[string]string{
		ConfigHashAnnotation:        configHash,
		DynamicConfigHashAnnotation: dynamicConfigHash,
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/resources/ -run TestBuildDeployment -v`
Expected: PASS.

- [ ] **Step 6: Run the full resources package to catch other call sites**

Run: `go test ./internal/resources/`
Expected: PASS (all 6 call sites compile).

- [ ] **Step 7: Commit**

```bash
git add internal/resources/labels.go internal/resources/deployment.go internal/resources/builders_test.go
git commit -s -m "feat(resources): stamp dynamicconfig-hash annotation on service pods"
```

---

### Task 2: Thread DynamicConfigHash through the plan path

**Files:**
- Modify: `internal/plan/services.go` (`ServicesInput` struct ~line 30; `BuildDeployment` call ~line 49)
- Modify: `internal/plan/plan.go` (`servicesInput` literal ~line 88)
- Modify: `internal/plan/services_test.go` (`ServicesInput` literal ~line 27; add assertion)

**Interfaces:**
- Consumes: `resources.ConfigHash`, `resources.DynamicConfigHashAnnotation`, new `BuildDeployment` signature from Task 1.
- Produces: `plan.ServicesInput.DynamicConfigHash string` field, populated by callers and passed to `BuildDeployment`.

- [ ] **Step 1: Write the failing test**

In `internal/plan/services_test.go`, update the `ServicesInput` literal in `TestPlanServicesObjects` (line 27) to set the new field:

```go
	in := ServicesInput{
		RenderedConfig:        "config: yes",
		RenderedDynamicConfig: "{}\n",
		ConfigHash:            "deadbeef",
		DynamicConfigHash:     "cafef00d",
		ServiceVersions:       nil,
		MTLS:                  nil,
	}
```

Add a new test asserting the hash reaches the Deployment annotation. Append to `internal/plan/services_test.go`:

```go
func TestPlanServicesStampsDynamicConfigHash(t *testing.T) {
	c := testCluster()
	in := ServicesInput{
		RenderedConfig:        "config: yes",
		RenderedDynamicConfig: "{}\n",
		ConfigHash:            "deadbeef",
		DynamicConfigHash:     "cafef00d",
	}
	got, err := PlanServices(c, in)
	if err != nil {
		t.Fatalf("PlanServices error: %v", err)
	}
	found := false
	for _, o := range got {
		dep, ok := o.Object.(*appsv1.Deployment)
		if !ok {
			continue
		}
		found = true
		if dep.Spec.Template.Annotations[resources.DynamicConfigHashAnnotation] != "cafef00d" {
			t.Errorf("expected dynamicconfig-hash annotation on %s, got %q",
				dep.Name, dep.Spec.Template.Annotations[resources.DynamicConfigHashAnnotation])
		}
	}
	if !found {
		t.Fatalf("no Deployment found in planned objects")
	}
}
```

Ensure the test file imports `appsv1 "k8s.io/api/apps/v1"` and `"github.com/bmorton/temporal-operator/internal/resources"` (add if missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/plan/ -run TestPlanServices -v`
Expected: FAIL — `unknown field DynamicConfigHash in struct literal`.

- [ ] **Step 3: Add the struct field**

In `internal/plan/services.go`, add to `ServicesInput` (after `ConfigHash`, line 33):

```go
	ConfigHash            string
	DynamicConfigHash     string
```

- [ ] **Step 4: Pass the field to BuildDeployment**

In `internal/plan/services.go` (line 49):

```go
		deployment, err := resources.BuildDeployment(cluster, svc, in.ConfigHash, in.DynamicConfigHash, version, in.MTLS)
```

- [ ] **Step 5: Populate it in the preview path**

In `internal/plan/plan.go`, update the `servicesInput` literal (line 88):

```go
	servicesInput := ServicesInput{
		RenderedConfig:        renderedConfig,
		RenderedDynamicConfig: renderedDynamic,
		ConfigHash:            resources.ConfigHash(renderedConfig),
		DynamicConfigHash:     resources.ConfigHash(renderedDynamic),
		MTLS:                  mtls,
	}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/plan/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/plan/services.go internal/plan/plan.go internal/plan/services_test.go
git commit -s -m "feat(plan): thread dynamicconfig hash to service deployments"
```

---

### Task 3: Populate DynamicConfigHash in the controller

**Files:**
- Modify: `internal/controller/temporalcluster_services.go` (`plan.PlanServices` input ~lines 53-59)

**Interfaces:**
- Consumes: `plan.ServicesInput.DynamicConfigHash` (Task 2), `resources.ConfigHash`, `rendered.dynamicConfig` (already available from `renderConfig`).
- Produces: reconciled Deployments carrying the dynamicconfig-hash annotation.

- [ ] **Step 1: Set the field in reconcileServices**

In `internal/controller/temporalcluster_services.go`, update the `plan.PlanServices` input literal (lines 53-59):

```go
	planned, err := plan.PlanServices(cluster, plan.ServicesInput{
		RenderedConfig:        rendered.config,
		RenderedDynamicConfig: rendered.dynamicConfig,
		ConfigHash:            resources.ConfigHash(rendered.config),
		DynamicConfigHash:     resources.ConfigHash(rendered.dynamicConfig),
		ServiceVersions:       serviceVersions,
		MTLS:                  r.mtlsMounts(ctx, cluster),
	})
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./...`
Expected: success, no output.

- [ ] **Step 3: Run the controller envtest suite**

Run: `KUBEBUILDER_ASSETS="$(bin/setup-envtest use -p path)" go test ./internal/controller/...`
Expected: PASS (`ok ... internal/controller`).

- [ ] **Step 4: Commit**

```bash
git add internal/controller/temporalcluster_services.go
git commit -s -m "feat(controller): roll services when dynamicConfig changes"
```

---

### Task 4: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Build**

Run: `make build`
Expected: builds `bin/manager` with no errors.

- [ ] **Step 2: Lint**

Run: `make lint`
Expected: `0 issues.`

- [ ] **Step 3: Full unit + envtest suites**

Run: `make test`
Expected: all packages `ok`.

- [ ] **Step 4: Confirm no generated-file drift**

Run: `git status --short`
Expected: only the source files changed in Tasks 1-3; no CRD/Helm/doc changes. If anything else appears, investigate before proceeding.

---

## Self-Review

- **Spec coverage:** annotation constant (Task 1), `ConfigHash` reuse (Tasks 1-3), thread through `ServicesInput`/`plan.go`/controller (Tasks 2-3), `BuildDeployment` param + stamp (Task 1), empty-dynamicConfig stable hash (covered — `ConfigHash("")` is deterministic and asserted implicitly by existing golden/plan tests), tests at resources/plan/controller layers (Tasks 1-3), out-of-scope items untouched. All covered.
- **Placeholder scan:** none — every code step shows exact content.
- **Type consistency:** `DynamicConfigHashAnnotation`, `DynamicConfigHash`, and the `BuildDeployment(cluster, svc, configHash, dynamicConfigHash, version, mtls)` signature are used identically across Tasks 1-3.
