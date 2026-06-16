# Azure-friendly operator (phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire up the existing `podTemplate` override in generated service Deployments, then add Azure-specific examples (Flexible Server password auth, AGIC ingress, Workload Identity preview) and documentation.

**Architecture:** Apply `PodTemplateOverride` (labels/annotations merge + strategic-merge pod `spec`) to each service Deployment's pod template in `internal/resources/deployment.go`, layering the shared `Services.Overrides.PodTemplate` first and the per-service `ServiceSpec.PodTemplate` on top, re-asserting selector labels after each merge. `BuildDeployment` gains an `error` return so strategic-merge failures propagate. Examples and docs are pure YAML/markdown additions. A follow-up GitHub issue tracks deferred passwordless work (schema Job + operator probe).

**Tech Stack:** Go, kubebuilder/controller-runtime, `k8s.io/apimachinery/pkg/util/strategicpatch`, Kubernetes YAML, Hugo docs, `gh` CLI.

---

## File structure

- **Modify** `internal/resources/deployment.go` — add `applyPodTemplate` helper; build the pod template into a variable; apply shared + per-service overrides; change `BuildDeployment` to return `(*appsv1.Deployment, error)`.
- **Modify** `internal/controller/temporalcluster_services.go:66` — handle the new error from `BuildDeployment`.
- **Modify** `internal/resources/builders_test.go` — update the 3 existing `BuildDeployment` call sites for the new signature; add `podTemplate` unit tests.
- **Create** `examples/cluster-azure-postgres-flexible/temporalcluster.yaml`, `examples/cluster-azure-postgres-flexible/README.md`.
- **Create** `examples/cluster-azure-aks-ingress/temporalcluster.yaml`, `examples/cluster-azure-aks-ingress/README.md`.
- **Create** `examples/cluster-azure-workload-identity/serviceaccount.yaml`, `examples/cluster-azure-workload-identity/temporalcluster.yaml`, `examples/cluster-azure-workload-identity/README.md`.
- **Modify** `examples/README.md` — add three rows.
- **Create** `docs/content/installation/azure.md`.
- **External:** open a GitHub issue with `gh` and capture its URL for cross-linking.

---

## Task 1: Add the `applyPodTemplate` helper (TDD)

**Files:**
- Modify: `internal/resources/deployment.go`
- Test: `internal/resources/builders_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/resources/builders_test.go`. The helper is unexported, so the test lives in the same `resources` package (it already does).

```go
func TestApplyPodTemplateLabelsAnnotationsAndSpec(t *testing.T) {
	base := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{"app.kubernetes.io/component": "frontend"},
			Annotations: map[string]string{"existing": "keep"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "temporal", Image: "temporalio/server:1.31.1"}},
			Volumes:    []corev1.Volume{{Name: "config"}},
		},
	}
	selector := map[string]string{"app.kubernetes.io/component": "frontend"}
	override := &temporalv1alpha1.PodTemplateOverride{
		Labels:      map[string]string{"azure.workload.identity/use": "true"},
		Annotations: map[string]string{"added": "yes"},
		Spec: &runtime.RawExtension{Raw: []byte(`{
			"serviceAccountName": "temporal-azure",
			"containers": [
				{"name": "temporal", "volumeMounts": [{"name": "azure-token", "mountPath": "/azure"}]},
				{"name": "sidecar", "image": "mcr.microsoft.com/azure-cli:latest"}
			],
			"volumes": [{"name": "azure-token", "emptyDir": {}}]
		}`)},
	}

	got, err := applyPodTemplate(base, override, selector)
	if err != nil {
		t.Fatalf("applyPodTemplate returned error: %v", err)
	}

	if got.Labels["azure.workload.identity/use"] != "true" {
		t.Errorf("override label missing: %v", got.Labels)
	}
	if got.Labels["app.kubernetes.io/component"] != "frontend" {
		t.Errorf("selector label must be preserved: %v", got.Labels)
	}
	if got.Annotations["existing"] != "keep" || got.Annotations["added"] != "yes" {
		t.Errorf("annotations not merged: %v", got.Annotations)
	}
	if got.Spec.ServiceAccountName != "temporal-azure" {
		t.Errorf("serviceAccountName not set: %q", got.Spec.ServiceAccountName)
	}
	if len(got.Spec.Containers) != 2 {
		t.Fatalf("expected sidecar appended, got %d containers", len(got.Spec.Containers))
	}
	var temporal *corev1.Container
	for i := range got.Spec.Containers {
		if got.Spec.Containers[i].Name == "temporal" {
			temporal = &got.Spec.Containers[i]
		}
	}
	if temporal == nil || len(temporal.VolumeMounts) != 1 || temporal.VolumeMounts[0].Name != "azure-token" {
		t.Errorf("temporal container volumeMount not merged: %+v", temporal)
	}
	if len(got.Spec.Volumes) != 2 {
		t.Errorf("expected azure-token volume appended, got %d", len(got.Spec.Volumes))
	}
}

func TestApplyPodTemplateNilIsNoop(t *testing.T) {
	base := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "b"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "temporal"}}},
	}
	got, err := applyPodTemplate(base, nil, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Spec.Containers) != 1 || got.Labels["a"] != "b" {
		t.Errorf("nil override must be a no-op, got %+v", got)
	}
}

func TestApplyPodTemplateOverrideCannotDropSelectorLabel(t *testing.T) {
	base := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/component": "frontend"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "temporal"}}},
	}
	selector := map[string]string{"app.kubernetes.io/component": "frontend"}
	override := &temporalv1alpha1.PodTemplateOverride{
		Labels: map[string]string{"app.kubernetes.io/component": "evil"},
	}
	got, err := applyPodTemplate(base, override, selector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Labels["app.kubernetes.io/component"] != "frontend" {
		t.Errorf("selector label must win over override, got %q", got.Labels["app.kubernetes.io/component"])
	}
}
```

Add the import for `runtime` to the test file's import block:

```go
	"k8s.io/apimachinery/pkg/runtime"
```

(`corev1` and `metav1` are already imported in `builders_test.go`; if `corev1` is missing, add `corev1 "k8s.io/api/core/v1"`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/resources/ -run TestApplyPodTemplate -v`
Expected: compile failure — `undefined: applyPodTemplate`.

- [ ] **Step 3: Implement `applyPodTemplate`**

In `internal/resources/deployment.go`, add `encoding/json`, `fmt`, and the strategic-merge import to the import block:

```go
import (
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)
```

Add the helper near the bottom of the file (above `intstrFromInt`):

```go
// applyPodTemplate layers a PodTemplateOverride onto a generated pod template.
// Labels and annotations are merged (override wins), and the override's partial
// PodSpec is strategic-merge patched onto the generated PodSpec (containers and
// volumes merge by name). Selector labels are re-asserted afterward so an
// override can never drop a label the Deployment selector depends on. A nil
// override is a no-op.
func applyPodTemplate(tmpl corev1.PodTemplateSpec, override *temporalv1alpha1.PodTemplateOverride, selectorLabels map[string]string) (corev1.PodTemplateSpec, error) {
	if override == nil {
		return tmpl, nil
	}

	if len(override.Labels) > 0 && tmpl.Labels == nil {
		tmpl.Labels = map[string]string{}
	}
	for k, v := range override.Labels {
		tmpl.Labels[k] = v
	}

	if len(override.Annotations) > 0 && tmpl.Annotations == nil {
		tmpl.Annotations = map[string]string{}
	}
	for k, v := range override.Annotations {
		tmpl.Annotations[k] = v
	}

	if override.Spec != nil && len(override.Spec.Raw) > 0 {
		original, err := json.Marshal(tmpl.Spec)
		if err != nil {
			return tmpl, fmt.Errorf("marshaling generated pod spec: %w", err)
		}
		patched, err := strategicpatch.StrategicMergePatch(original, override.Spec.Raw, corev1.PodSpec{})
		if err != nil {
			return tmpl, fmt.Errorf("applying podTemplate spec patch: %w", err)
		}
		var merged corev1.PodSpec
		if err := json.Unmarshal(patched, &merged); err != nil {
			return tmpl, fmt.Errorf("unmarshaling patched pod spec: %w", err)
		}
		tmpl.Spec = merged
	}

	// Re-assert selector labels last so an override cannot drop one.
	if len(selectorLabels) > 0 && tmpl.Labels == nil {
		tmpl.Labels = map[string]string{}
	}
	for k, v := range selectorLabels {
		tmpl.Labels[k] = v
	}

	return tmpl, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/resources/ -run TestApplyPodTemplate -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/resources/deployment.go internal/resources/builders_test.go
git commit -s -m "feat(resources): add applyPodTemplate strategic-merge helper" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Apply `podTemplate` in `BuildDeployment` (TDD)

**Files:**
- Modify: `internal/resources/deployment.go:127` (`BuildDeployment` signature + body)
- Modify: `internal/controller/temporalcluster_services.go:66`
- Test: `internal/resources/builders_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/resources/builders_test.go`:

```go
func TestBuildDeploymentAppliesSharedAndPerServicePodTemplate(t *testing.T) {
	c := builderCluster()
	c.Spec.Services.Overrides = &temporalv1alpha1.ServiceOverrides{
		PodTemplate: &temporalv1alpha1.PodTemplateOverride{
			Labels: map[string]string{"shared": "yes"},
			Spec:   &runtime.RawExtension{Raw: []byte(`{"serviceAccountName": "shared-sa"}`)},
		},
	}
	c.Spec.Services.Frontend = &temporalv1alpha1.ServiceSpec{
		PodTemplate: &temporalv1alpha1.PodTemplateOverride{
			Labels: map[string]string{"perservice": "yes"},
			Spec:   &runtime.RawExtension{Raw: []byte(`{"serviceAccountName": "frontend-sa"}`)},
		},
	}

	var frontend ServiceInfo
	for _, s := range EnabledServices(c) {
		if s.Name == ServiceFrontend {
			frontend = s
		}
	}

	dep, err := BuildDeployment(c, frontend, "abc123", "", nil)
	if err != nil {
		t.Fatalf("BuildDeployment error: %v", err)
	}
	tmpl := dep.Spec.Template
	if tmpl.Labels["shared"] != "yes" || tmpl.Labels["perservice"] != "yes" {
		t.Errorf("expected both shared and per-service labels, got %v", tmpl.Labels)
	}
	// Per-service spec is applied after shared, so it wins.
	if tmpl.Spec.ServiceAccountName != "frontend-sa" {
		t.Errorf("expected per-service serviceAccountName to win, got %q", tmpl.Spec.ServiceAccountName)
	}
	// Selector labels survive.
	if tmpl.Labels[LabelComponent] != ServiceFrontend {
		t.Errorf("selector label dropped: %v", tmpl.Labels)
	}
	// Generated temporal container survives the merge.
	if len(tmpl.Spec.Containers) != 1 || tmpl.Spec.Containers[0].Name != "temporal" {
		t.Errorf("temporal container not preserved: %+v", tmpl.Spec.Containers)
	}
}

func TestBuildDeploymentInvalidPodTemplateSpecErrors(t *testing.T) {
	c := builderCluster()
	c.Spec.Services.Worker = &temporalv1alpha1.ServiceSpec{
		PodTemplate: &temporalv1alpha1.PodTemplateOverride{
			Spec: &runtime.RawExtension{Raw: []byte(`not-json`)},
		},
	}
	var worker ServiceInfo
	for _, s := range EnabledServices(c) {
		if s.Name == ServiceWorker {
			worker = s
		}
	}
	if _, err := BuildDeployment(c, worker, "abc123", "", nil); err == nil {
		t.Errorf("expected error for invalid podTemplate spec patch")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/resources/ -run TestBuildDeployment -v`
Expected: compile failure — `BuildDeployment` returns one value, tests expect two.

- [ ] **Step 3: Change `BuildDeployment` to apply overrides and return an error**

In `internal/resources/deployment.go`, change the signature:

```go
func BuildDeployment(cluster *temporalv1alpha1.TemporalCluster, svc ServiceInfo, configHash, version string, mtls *MTLSMounts) (*appsv1.Deployment, error) {
```

Replace the final `return &appsv1.Deployment{...}` block (currently lines ~251-277) with: build the pod template into a variable, apply overrides, then assemble and return. The new tail of the function:

```go
	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      podLabels,
			Annotations: podAnnotations,
		},
		Spec: corev1.PodSpec{
			ImagePullSecrets:          cluster.Spec.ImagePullSecrets,
			NodeSelector:              nodeSelector,
			Tolerations:               tolerations,
			Affinity:                  affinity,
			TopologySpreadConstraints: topologySpread,
			Containers:                []corev1.Container{container},
			Volumes:                   volumes,
		},
	}

	selector := SelectorLabels(cluster, svc.Name)
	var err error
	if cluster.Spec.Services.Overrides != nil {
		podTemplate, err = applyPodTemplate(podTemplate, cluster.Spec.Services.Overrides.PodTemplate, selector)
		if err != nil {
			return nil, fmt.Errorf("applying shared podTemplate for %s: %w", svc.Name, err)
		}
	}
	if svc.Spec != nil {
		podTemplate, err = applyPodTemplate(podTemplate, svc.Spec.PodTemplate, selector)
		if err != nil {
			return nil, fmt.Errorf("applying podTemplate for %s: %w", svc.Name, err)
		}
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(cluster.Name, svc.Name),
			Namespace: cluster.Namespace,
			Labels:    podLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(cluster, svc.Name)},
			Template: podTemplate,
		},
	}, nil
}
```

- [ ] **Step 4: Update the production caller**

In `internal/controller/temporalcluster_services.go`, replace line 66:

```go
		dep, err := resources.BuildDeployment(cluster, svc, configHash, version, mtls)
		if err != nil {
			return err
		}
		if err := r.apply(ctx, cluster, dep); err != nil {
			return err
		}
```

- [ ] **Step 5: Update the three existing test call sites**

In `internal/resources/builders_test.go`, update the existing `TestBuildDeployment` and related helpers (current lines ~67, ~112, ~126) to consume the new two-value return:

```go
	dep, err := BuildDeployment(c, svc, "abc123", "", nil)
	if err != nil {
		t.Fatalf("BuildDeployment error: %v", err)
	}
```

```go
	depWorker, err := BuildDeployment(c, worker, "abc123", "", nil)
	if err != nil {
		t.Fatalf("BuildDeployment error: %v", err)
	}
	ctr := depWorker.Spec.Template.Spec.Containers[0]
```

```go
	depMTLS, err := BuildDeployment(c, svc, "abc123", "", mtls)
	if err != nil {
		t.Fatalf("BuildDeployment error: %v", err)
	}
	ctr := depMTLS.Spec.Template.Spec.Containers[0]
```

- [ ] **Step 6: Run package tests + build**

Run: `go test ./internal/resources/... ./internal/controller/... && go build ./...`
Expected: PASS / clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/resources/deployment.go internal/resources/builders_test.go internal/controller/temporalcluster_services.go
git commit -s -m "feat(resources): apply podTemplate overrides to service Deployments" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Verify full build, lint, and manifests are unchanged

**Files:** none (verification only)

- [ ] **Step 1: Confirm no CRD drift**

Run: `make manifests && git status --porcelain config/`
Expected: no output (the `podTemplate` fields already existed in the CRD; nothing should change).

- [ ] **Step 2: Run the full test suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 3: Run the linter**

Run: `make lint`
Expected: no findings.

- [ ] **Step 4: Commit (only if `make manifests` changed anything unexpectedly)**

If `git status` is clean, skip. Otherwise inspect and commit:

```bash
git add config/
git commit -s -m "chore: regenerate manifests" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Open the follow-up GitHub issue

**Files:** none (uses `gh`)

- [ ] **Step 1: Create the issue and capture its URL**

Run:

```bash
gh issue create \
  --title "Full Azure Workload Identity passwordless auth (operator probe + schema Job)" \
  --body "$(cat <<'EOF'
Phase 1 (#PR) wired up `podTemplate` overrides so **server pods** can authenticate
to Azure Database for PostgreSQL Flexible Server passwordlessly via Workload
Identity + Entra tokens (see `examples/cluster-azure-workload-identity`).

Two actors still require static credentials, so a fully passwordless cluster is
not yet possible:

- [ ] **Schema Job** (`internal/resources/schemajob.go`): only reads a static
  `SQL_PASSWORD` env var. Needs `podTemplate` support on the Job and a way to
  populate `SQL_PASSWORD` from a `passwordCommand` / token file.
- [ ] **Operator probe + schema inspection**
  (`internal/persistence/sql.go`, `internal/controller/temporalcluster_persistence.go`):
  builds the DSN from `cred.Password` only and never executes the configured
  `passwordCommand`. Needs to execute `passwordCommand` for its own reachability
  probe and schema-version inspection (operator pod running with Workload
  Identity).

Design context: `docs/superpowers/specs/2026-06-16-azure-support-design.md`.
EOF
)"
```

Expected: prints the new issue URL (e.g. `https://github.com/bmorton/temporal-operator/issues/NN`). Record `NN` — it is referenced as `#NN` in Tasks 7 and 8.

---

## Task 5: Add the Flexible Server (password auth) example

**Files:**
- Create: `examples/cluster-azure-postgres-flexible/temporalcluster.yaml`
- Create: `examples/cluster-azure-postgres-flexible/README.md`

- [ ] **Step 1: Create `temporalcluster.yaml`**

```yaml
# Azure Database for PostgreSQL Flexible Server with password auth over TLS.
# Pre-create the `temporal` and `temporal_visibility` databases on the server
# before applying (the operator only runs setup-schema, it does not CREATE DATABASE).
apiVersion: v1
kind: Secret
metadata:
  name: temporal-flexible-store
type: Opaque
stringData:
  # Replace with the Flexible Server admin (or app) password.
  password: "REPLACE_ME"
---
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: azure
spec:
  version: "1.31.1"
  numHistoryShards: 512
  ui:
    enabled: true
  persistence:
    defaultStore:
      sql:
        pluginName: postgres12
        # Flexible Server endpoint, e.g. my-temporal.postgres.database.azure.com
        host: REPLACE_ME.postgres.database.azure.com
        port: 5432
        database: temporal
        user: temporaladmin
        passwordSecretRef: { name: temporal-flexible-store, key: password }
        # Flexible Server requires TLS.
        tls: { enabled: true }
    visibilityStore:
      sql:
        pluginName: postgres12
        host: REPLACE_ME.postgres.database.azure.com
        port: 5432
        database: temporal_visibility
        user: temporaladmin
        passwordSecretRef: { name: temporal-flexible-store, key: password }
        tls: { enabled: true }
```

- [ ] **Step 2: Create `README.md`**

```markdown
# Azure Database for PostgreSQL Flexible Server (password auth)

A `TemporalCluster` backed by Azure Database for PostgreSQL Flexible Server using
password authentication over TLS. This is the simplest Azure persistence option
and works with the operator today.

## Prerequisites

1. A Flexible Server instance reachable from your AKS cluster (public access with
   firewall rules, or private access / VNet integration).
2. Two databases created on the server **before** applying this manifest — the
   operator runs `setup-schema` but does not create databases:

   ```sql
   CREATE DATABASE temporal;
   CREATE DATABASE temporal_visibility;
   ```

3. `max_connections` raised on the server (Server parameters blade). Temporal
   opens several pools per pod; the small-SKU default can be exhausted. ~200 is a
   safe starting point.

## Apply

Edit `temporalcluster.yaml` to set the server FQDN, user, and password, then:

```sh
kubectl apply -f temporalcluster.yaml
```

TLS is required by Flexible Server; `tls.enabled: true` is set on both stores.
Azure's certificate chains to a public root, so no CA secret is needed.
```

- [ ] **Step 3: Validate the YAML parses**

Run: `kubectl apply --dry-run=client -f examples/cluster-azure-postgres-flexible/temporalcluster.yaml`
Expected: `secret/... created (dry run)` and `temporalcluster.../azure created (dry run)` with no schema errors. (If no cluster is reachable, use `kubectl apply --dry-run=client --validate=false`.)

- [ ] **Step 4: Commit**

```bash
git add examples/cluster-azure-postgres-flexible/
git commit -s -m "docs(examples): add Azure Flexible Server password-auth example" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Add the AKS / AGIC ingress example

**Files:**
- Create: `examples/cluster-azure-aks-ingress/temporalcluster.yaml`
- Create: `examples/cluster-azure-aks-ingress/README.md`

- [ ] **Step 1: Create `temporalcluster.yaml`**

```yaml
# Temporal UI exposed on AKS via the Application Gateway Ingress Controller (AGIC).
# Persistence is elided for brevity — combine with cluster-azure-postgres-flexible.
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: azure-ui
spec:
  version: "1.31.1"
  numHistoryShards: 512
  ui:
    enabled: true
    ingress:
      enabled: true
      ingressClassName: azure-application-gateway
      host: temporal.example.com
      annotations:
        appgw.ingress.kubernetes.io/backend-protocol: "http"
        appgw.ingress.kubernetes.io/request-timeout: "300"
  persistence:
    defaultStore:
      sql:
        pluginName: postgres12
        host: REPLACE_ME.postgres.database.azure.com
        port: 5432
        database: temporal
        user: temporaladmin
        passwordSecretRef: { name: temporal-flexible-store, key: password }
        tls: { enabled: true }
    visibilityStore:
      sql:
        pluginName: postgres12
        host: REPLACE_ME.postgres.database.azure.com
        port: 5432
        database: temporal_visibility
        user: temporaladmin
        passwordSecretRef: { name: temporal-flexible-store, key: password }
        tls: { enabled: true }
```

> The UI ingress schema (`api/v1alpha1/shared_types.go`, `UIIngressSpec`)
> supports `enabled`, `ingressClassName`, `host`, `annotations`, and
> `tlsSecretName` — the YAML above uses only those fields.

- [ ] **Step 2: Create `README.md`**

```markdown
# Temporal UI on AKS via Application Gateway (AGIC)

Exposes the Temporal Web UI through the Azure Application Gateway Ingress
Controller (AGIC).

## Prerequisites

- An AKS cluster with the AGIC add-on enabled (`az aks enable-addons --addons
  ingress-appgw ...`) or AGIC installed via Helm.
- A DNS record pointing `temporal.example.com` at the Application Gateway public
  IP.
- A working persistence backend — combine this with
  [`cluster-azure-postgres-flexible`](../cluster-azure-postgres-flexible).

## Apply

```sh
kubectl apply -f temporalcluster.yaml
```

The `ingressClassName: azure-application-gateway` selects AGIC; the
`appgw.ingress.kubernetes.io/*` annotations tune the Application Gateway backend.
Add TLS via an `appgw.ingress.kubernetes.io/appgw-ssl-certificate` annotation or
a standard `tls:` block once you have a certificate provisioned.
```

- [ ] **Step 3: Validate the YAML parses**

Run: `kubectl apply --dry-run=client --validate=false -f examples/cluster-azure-aks-ingress/temporalcluster.yaml`
Expected: `temporalcluster.../azure-ui created (dry run)`.

- [ ] **Step 4: Commit**

```bash
git add examples/cluster-azure-aks-ingress/
git commit -s -m "docs(examples): add AKS AGIC ingress example" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 7: Add the Workload Identity (passwordless preview) example

**Files:**
- Create: `examples/cluster-azure-workload-identity/serviceaccount.yaml`
- Create: `examples/cluster-azure-workload-identity/temporalcluster.yaml`
- Create: `examples/cluster-azure-workload-identity/README.md`

Replace `#NN` below with the issue number from Task 4.

- [ ] **Step 1: Create `serviceaccount.yaml`**

```yaml
# ServiceAccount federated with an Azure managed identity / app registration.
# The client-id annotation is what Azure Workload Identity uses to mint tokens.
apiVersion: v1
kind: ServiceAccount
metadata:
  name: temporal-azure
  annotations:
    azure.workload.identity/client-id: REPLACE_WITH_MANAGED_IDENTITY_CLIENT_ID
```

- [ ] **Step 2: Create `temporalcluster.yaml`**

```yaml
# PREVIEW: passwordless Flexible Server auth via Azure Workload Identity.
# This makes the Temporal SERVER pods passwordless. The operator's own
# reachability probe and the schema Job still need credentials today — see the
# README and the tracking issue.
apiVersion: v1
kind: Secret
metadata:
  name: temporal-azure-pwcmd
type: Opaque
stringData:
  # Temporal runs this command per connection to obtain the DB password.
  # It waits for the sidecar to write the first token, then prints it.
  command: "sh -c 'until [ -s /azure/pgpass ]; do sleep 1; done; cat /azure/pgpass'"
---
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: azure-wi
spec:
  version: "1.31.1"
  numHistoryShards: 512
  ui:
    enabled: true
  services:
    overrides:
      podTemplate:
        labels:
          azure.workload.identity/use: "true"
        spec:
          serviceAccountName: temporal-azure
          containers:
            # Patch the generated server container (merged by name) to mount the
            # shared token volume.
            - name: temporal
              volumeMounts:
                - name: azure-token
                  mountPath: /azure
            # Sidecar refreshes an Entra access token for Flexible Server.
            - name: azure-token-refresher
              image: mcr.microsoft.com/azure-cli:latest
              command: ["/bin/sh", "-c"]
              args:
                - |
                  set -e
                  az login --service-principal \
                    -u "$AZURE_CLIENT_ID" \
                    --tenant "$AZURE_TENANT_ID" \
                    --federated-token "$(cat "$AZURE_FEDERATED_TOKEN_FILE")" \
                    --allow-no-subscriptions
                  while true; do
                    az account get-access-token --resource-type oss-rdbms \
                      --query accessToken -o tsv > /azure/pgpass.tmp
                    mv /azure/pgpass.tmp /azure/pgpass
                    sleep 1800
                  done
              volumeMounts:
                - name: azure-token
                  mountPath: /azure
          volumes:
            - name: azure-token
              emptyDir: {}
  persistence:
    defaultStore:
      sql:
        pluginName: postgres12
        host: REPLACE_ME.postgres.database.azure.com
        port: 5432
        database: temporal
        # The Postgres role created for the managed identity via
        # pgaadauth_create_principal (usually the identity's display name).
        user: temporal-identity
        passwordCommandSecretRef: { name: temporal-azure-pwcmd, key: command }
        tls: { enabled: true }
    visibilityStore:
      sql:
        pluginName: postgres12
        host: REPLACE_ME.postgres.database.azure.com
        port: 5432
        database: temporal_visibility
        user: temporal-identity
        passwordCommandSecretRef: { name: temporal-azure-pwcmd, key: command }
        tls: { enabled: true }
```

- [ ] **Step 3: Create `README.md`** (replace `#NN`)

```markdown
# PREVIEW: passwordless Flexible Server via Azure Workload Identity

Demonstrates passwordless authentication to Azure Database for PostgreSQL
Flexible Server using [Azure Workload Identity](https://azure.github.io/azure-workload-identity/)
and Microsoft Entra access tokens. It exercises the operator's `podTemplate`
override to inject a `serviceAccountName`, the Workload Identity pod label, and a
token-refresher sidecar.

## Status: preview

This makes the **Temporal server pods** passwordless. Two actors are **not** yet
passwordless, so a cluster applied as-is will not reach Ready without extra
setup:

- The **operator's** reachability probe + schema inspection still build the DSN
  from a static password.
- The **schema Job** (`setup-schema`) still reads a static `SQL_PASSWORD`.

Tracking issue: #NN. Until it lands, bootstrap the schema with password auth
(apply `cluster-azure-postgres-flexible` once to create the schema), or grant
the operator/job identity temporary password access.

## How it works

1. The `ServiceAccount` (`serviceaccount.yaml`) is annotated with the managed
   identity's `client-id`.
2. `podTemplate.labels` sets `azure.workload.identity/use: "true"`, so the
   Workload Identity webhook injects `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`,
   `AZURE_FEDERATED_TOKEN_FILE`, and `AZURE_AUTHORITY_HOST` into every container.
3. `podTemplate.spec` adds the `azure-token-refresher` sidecar, which performs a
   federated `az login` and writes a fresh Entra access token to a shared
   `emptyDir` (`/azure/pgpass`) every 30 minutes.
4. `passwordCommandSecretRef` tells Temporal to read the token file per
   connection, so expiring tokens are picked up automatically.

## Prerequisites

- AKS with the OIDC issuer and Workload Identity enabled
  (`az aks update --enable-oidc-issuer --enable-workload-identity`).
- A managed identity (or app registration) with a federated credential bound to
  this ServiceAccount.
- The identity mapped to a Postgres role on the Flexible Server:
  `SELECT pgaadauth_create_principal('temporal-identity', false, false);`
- Microsoft Entra authentication enabled on the Flexible Server.

## Apply

```sh
kubectl apply -f serviceaccount.yaml
kubectl apply -f temporalcluster.yaml
```

The server may restart a few times on first boot until the sidecar writes the
first token; the `passwordCommand` waits for the token file to avoid most of
this.
```

- [ ] **Step 4: Validate the YAML parses**

Run: `kubectl apply --dry-run=client --validate=false -f examples/cluster-azure-workload-identity/`
Expected: dry-run `created` lines for the ServiceAccount, Secret, and TemporalCluster.

- [ ] **Step 5: Commit**

```bash
git add examples/cluster-azure-workload-identity/
git commit -s -m "docs(examples): add Azure Workload Identity passwordless preview" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 8: Add the Azure installation guide + examples index rows

**Files:**
- Create: `docs/content/installation/azure.md`
- Modify: `examples/README.md`

Replace `#NN` with the Task 4 issue number.

- [ ] **Step 1: Inspect an existing docs page for Hugo front-matter conventions**

Run: `head -20 docs/content/installation/verifying-releases.md`
Expected: shows the front-matter block (`+++` TOML or `---` YAML, `title`, `weight`). Match that style in the new page.

- [ ] **Step 2: Create `docs/content/installation/azure.md`**

Use the same front-matter delimiter and fields the existing page uses. Body:

```markdown
# Running on Azure

This guide covers running the Temporal operator on Azure: AKS, Azure Database for
PostgreSQL Flexible Server, Application Gateway ingress, and passwordless
Microsoft Entra authentication.

## AKS prerequisites

- An AKS cluster (`az aks create ...`).
- The operator installed (see the [installation guide](./_index.md)).

## Persistence: Flexible Server

Azure Database for PostgreSQL Flexible Server is the recommended SQL backend.

- Create the `temporal` and `temporal_visibility` databases up front — the
  operator runs `setup-schema` but does not create databases.
- Raise `max_connections` (~200) to avoid pool exhaustion on smaller SKUs.
- TLS is required; set `tls.enabled: true` on each store. Azure chains to a
  public root, so no CA secret is needed.

Example: [`examples/cluster-azure-postgres-flexible`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-azure-postgres-flexible).

## Exposing the UI with Application Gateway (AGIC)

Set `ui.ingress.ingressClassName: azure-application-gateway` and AGIC
annotations.

Example: [`examples/cluster-azure-aks-ingress`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-azure-aks-ingress).

## Passwordless auth with Microsoft Entra + Workload Identity (preview)

The operator can drive the Temporal **server** pods passwordlessly using Azure
Workload Identity and Entra access tokens, via the `podTemplate` override
(`serviceAccountName`, the `azure.workload.identity/use` label, and a
token-refresher sidecar).

1. Enable the OIDC issuer and Workload Identity on AKS:
   `az aks update -g <rg> -n <cluster> --enable-oidc-issuer --enable-workload-identity`.
2. Create a managed identity and a federated credential bound to the
   `temporal-azure` ServiceAccount.
3. Enable Entra auth on the Flexible Server and map the identity to a Postgres
   role with `pgaadauth_create_principal`.

**Current limitation:** only the server pods are passwordless today. The
operator's reachability probe and the schema Job still need a password. Bootstrap
the schema with password auth first, or grant those identities temporary access.
Tracking issue: [#NN](https://github.com/bmorton/temporal-operator/issues/NN).

Example: [`examples/cluster-azure-workload-identity`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-azure-workload-identity).
```

- [ ] **Step 3: Add rows to `examples/README.md`**

Insert into the table (after the `cluster-cnpg-integrated` row):

```markdown
| [`cluster-azure-postgres-flexible`](./cluster-azure-postgres-flexible) | Azure Database for PostgreSQL Flexible Server (password auth). |
| [`cluster-azure-aks-ingress`](./cluster-azure-aks-ingress) | Temporal UI on AKS via Application Gateway (AGIC). |
| [`cluster-azure-workload-identity`](./cluster-azure-workload-identity) | **Preview:** passwordless Flexible Server via Azure Workload Identity. |
```

- [ ] **Step 4: Lint the markdown**

Run: `npx --yes markdownlint-cli2 "docs/content/installation/azure.md" "examples/README.md" "examples/cluster-azure-*/README.md"`
Expected: no errors. Fix any reported issues (the repo uses `.markdownlint.yaml`).

- [ ] **Step 5: Commit**

```bash
git add docs/content/installation/azure.md examples/README.md
git commit -s -m "docs: add Azure installation guide and example index rows" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 9: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full build, test, lint**

Run: `make build && make test && make lint`
Expected: all pass.

- [ ] **Step 2: Confirm no unintended diffs**

Run: `git status --porcelain`
Expected: clean (everything committed).

- [ ] **Step 3: Review the commit series**

Run: `git log --oneline -9`
Expected: the design-spec commit plus the feature/docs/example commits from this plan, in order.
