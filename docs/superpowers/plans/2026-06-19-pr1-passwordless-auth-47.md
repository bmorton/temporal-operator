# PR1: Full passwordless auth (close #47) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the operator's own DB probe/schema-inspection and the schema Job authenticate to Postgres via the configured `passwordCommand`, so an Azure Workload Identity cluster reaches `Ready` with zero static passwords.

**Architecture:** When a SQL datastore credential carries a `PasswordCommand` (from `passwordCommandSecretRef`) instead of a static `Password`, (a) the operator's `sqlBackend` executes that command via an injectable `CommandRunner` to obtain a fresh token on every probe/inspection, and (b) the schema Job wraps `temporal-sql-tool` in `sh -c` that exports `SQL_PASSWORD` from the command at start. A new `spec.persistence.schemaJob.podTemplate` field and hand-edited operator Helm values let users attach the Workload Identity ServiceAccount, pod label, and token sidecar/initContainer needed for the command to work in-pod.

**Tech Stack:** Go 1.26.4, controller-runtime, kubebuilder/controller-gen, Helm (dist/chart, hand-edited), envtest.

**Branch:** `feat/azure-passwordless-47` (already created; the design doc is already committed here).

**Reference spec:** `docs/superpowers/specs/2026-06-19-aks-passwordless-e2e-design.md`

**Repo conventions (do not violate):**
- Sign off every commit: `git commit -s`.
- Conventional Commit messages (`feat:`, `fix:`, `test:`, `docs:`, `chore:`).
- Build/test/lint: `make build`, `make test` (uses envtest; do NOT use `go test ./...` directly for envtest packages), `make lint`. Regenerate after API changes: `make generate manifests`.
- Edit `dist/chart` BY HAND. NEVER run `make helm-chart`.
- Include the trailer `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>` in commit messages.

---

## Task 1: Add an injectable CommandRunner to the persistence package

**Files:**
- Create: `internal/persistence/command.go`
- Test: `internal/persistence/command_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/persistence/command_test.go
package persistence

import (
	"context"
	"testing"
)

func TestDefaultCommandRunnerTrimsOutput(t *testing.T) {
	out, err := DefaultCommandRunner(context.Background(), "printf 'tok123\\n'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "tok123" {
		t.Errorf("expected trimmed %q, got %q", "tok123", out)
	}
}

func TestDefaultCommandRunnerError(t *testing.T) {
	if _, err := DefaultCommandRunner(context.Background(), "exit 3"); err == nil {
		t.Fatal("expected error from failing command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/persistence/ -run TestDefaultCommandRunner -v`
Expected: FAIL — `undefined: DefaultCommandRunner`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/persistence/command.go
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

package persistence

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner executes a shell command and returns its trimmed stdout. It is
// used to resolve short-lived datastore credentials emitted by a user-supplied
// passwordCommand (e.g. an Entra access token).
type CommandRunner func(ctx context.Context, command string) (string, error)

// DefaultCommandRunner runs the command with "sh -c" and returns trimmed stdout.
func DefaultCommandRunner(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("executing password command: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/persistence/ -run TestDefaultCommandRunner -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/persistence/command.go internal/persistence/command_test.go
git commit -s -m "feat(persistence): add injectable CommandRunner for passwordCommand

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Resolve the password from the command in sqlBackend probe/inspection

**Files:**
- Modify: `internal/persistence/sql.go` (struct `sqlBackend`, methods `dsn`, `Probe`, `SchemaVersion`)
- Test: `internal/persistence/sql_test.go` (new file)

Context: today `sqlBackend.dsn()` builds the DSN from `b.cred.Password`. We add a
`runner CommandRunner` field and a `resolvePassword(ctx)` method that runs
`b.cred.PasswordCommand` when set (fresh each call), else returns
`b.cred.Password`. `dsn` takes the resolved password as an argument.

- [ ] **Step 1: Write the failing test**

```go
// internal/persistence/sql_test.go
package persistence

import (
	"context"
	"errors"
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func sqlBackendSpec() *temporalv1alpha1.SQLDatastoreSpec {
	return &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12",
		Host:       "pg.example.com",
		Port:       5432,
		Database:   "temporal",
		User:       "temporal",
		TLS:        &temporalv1alpha1.DatastoreTLSSpec{Enabled: true},
	}
}

func TestResolvePasswordStatic(t *testing.T) {
	b := &sqlBackend{spec: sqlBackendSpec(), cred: ResolvedCredential{Password: "static"}, dbName: "temporal"}
	got, err := b.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "static" {
		t.Errorf("expected static password, got %q", got)
	}
}

func TestResolvePasswordRunsCommandFresh(t *testing.T) {
	calls := 0
	b := &sqlBackend{
		spec:   sqlBackendSpec(),
		cred:   ResolvedCredential{PasswordCommand: "get-token"},
		dbName: "temporal",
		runner: func(_ context.Context, cmd string) (string, error) {
			calls++
			if cmd != "get-token" {
				t.Errorf("unexpected command %q", cmd)
			}
			return "token-fresh", nil
		},
	}
	for i := 0; i < 2; i++ {
		got, err := b.resolvePassword(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "token-fresh" {
			t.Errorf("expected token, got %q", got)
		}
	}
	if calls != 2 {
		t.Errorf("expected command run fresh per call (2), got %d", calls)
	}
}

func TestResolvePasswordCommandError(t *testing.T) {
	b := &sqlBackend{
		spec:   sqlBackendSpec(),
		cred:   ResolvedCredential{PasswordCommand: "boom"},
		dbName: "temporal",
		runner: func(_ context.Context, _ string) (string, error) { return "", errors.New("boom") },
	}
	if _, err := b.resolvePassword(context.Background()); err == nil {
		t.Fatal("expected error from failing command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/persistence/ -run TestResolvePassword -v`
Expected: FAIL — `sqlBackend has no field or method resolvePassword` / unknown field `runner`.

- [ ] **Step 3: Write minimal implementation**

In `internal/persistence/sql.go`, change the `sqlBackend` struct and methods:

```go
// sqlBackend adapts the SQL prober to the Backend interface.
type sqlBackend struct {
	spec   *temporalv1alpha1.SQLDatastoreSpec
	cred   ResolvedCredential
	dbName string
	// runner resolves a passwordCommand credential. Defaults to
	// DefaultCommandRunner when nil.
	runner CommandRunner
}

// resolvePassword returns the static password, or the fresh output of the
// configured passwordCommand when one is set (re-run on every call so an
// expiring token is always current).
func (b *sqlBackend) resolvePassword(ctx context.Context) (string, error) {
	if b.cred.PasswordCommand == "" {
		return b.cred.Password, nil
	}
	run := b.runner
	if run == nil {
		run = DefaultCommandRunner
	}
	return run(ctx, b.cred.PasswordCommand)
}

func (b *sqlBackend) dsn(password string) string {
	return BuildPostgresDSN(b.spec, password, b.dbName)
}

func (b *sqlBackend) Probe(ctx context.Context) error {
	password, err := b.resolvePassword(ctx)
	if err != nil {
		return err
	}
	return SQLProber{}.Probe(ctx, b.dsn(password))
}

func (b *sqlBackend) SchemaVersion(ctx context.Context) (string, error) {
	password, err := b.resolvePassword(ctx)
	if err != nil {
		return "", err
	}
	return SQLProber{}.CurrentSchemaVersion(ctx, b.dsn(password), b.dbName)
}
```

Add `"context"` to the imports if not already present (it is). Remove the old
parameterless `dsn()` method (replaced above).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/persistence/ -run TestResolvePassword -v`
Expected: PASS (all three subtests).

- [ ] **Step 5: Run the whole persistence package to catch regressions**

Run: `go test ./internal/persistence/ -v`
Expected: PASS (existing backend/schema/secrets tests still pass).

- [ ] **Step 6: Commit**

```bash
git add internal/persistence/sql.go internal/persistence/sql_test.go
git commit -s -m "feat(persistence): execute passwordCommand for operator probe and schema inspection

Closes part of #47: the operator now resolves a passwordCommand-based
credential (e.g. an Entra access token) on every probe and schema-version
inspection instead of only reading a static password.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Add the `spec.persistence.schemaJob.podTemplate` API field

**Files:**
- Modify: `api/v1alpha1/persistence_types.go`
- Regenerate: `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/...`, `dist/chart/templates/crd/...`

- [ ] **Step 1: Add the type and field**

In `api/v1alpha1/persistence_types.go`, add a `SchemaJob` field to
`PersistenceSpec` and a new `SchemaJobSpec` type:

```go
// PersistenceSpec configures the default and visibility datastores.
type PersistenceSpec struct {
	// DefaultStore holds workflow execution state. Exactly one of sql or
	// cassandra must be set.
	// +kubebuilder:validation:XValidation:rule="has(self.sql) != has(self.cassandra)",message="exactly one of sql or cassandra must be set for defaultStore"
	DefaultStore DatastoreSpec `json:"defaultStore"`

	// VisibilityStore holds visibility records. One of sql, cassandra, or
	// elasticsearch must be set.
	VisibilityStore DatastoreSpec `json:"visibilityStore"`

	// SchemaJob customizes the schema setup/update Jobs the operator runs.
	// +optional
	SchemaJob *SchemaJobSpec `json:"schemaJob,omitempty"`
}

// SchemaJobSpec customizes the schema management Jobs (setup-schema /
// update-schema) the operator runs against SQL and Cassandra datastores.
type SchemaJobSpec struct {
	// PodTemplate overrides metadata and the pod spec of the schema Job pods.
	// Use it to attach a ServiceAccount, pod labels (e.g. Azure Workload
	// Identity), and a token initContainer so the Job can authenticate with a
	// passwordCommand instead of a static password.
	// +optional
	PodTemplate *PodTemplateOverride `json:"podTemplate,omitempty"`
}
```

- [ ] **Step 2: Regenerate deepcopy + manifests**

Run: `make generate manifests`
Expected: `api/v1alpha1/zz_generated.deepcopy.go` gains `SchemaJobSpec` deepcopy
funcs and `PersistenceSpec.DeepCopyInto` copies `SchemaJob`; the CRD under
`config/crd/bases/` gains `spec.persistence.schemaJob.podTemplate`.

- [ ] **Step 3: Propagate the CRD change into the Helm chart by hand**

The e2e installs CRDs from `dist/chart`, not `config/crd`. Copy the regenerated
`spec.persistence.schemaJob` block from
`config/crd/bases/temporal.bmor10.com_temporalclusters.yaml` into the matching
CRD template under `dist/chart/templates/crd/` (same file, same `persistence`
properties location). Do NOT run `make helm-chart`.

Verify the chart still renders:

Run: `helm template dist/chart >/dev/null && echo OK`
Expected: `OK`.

- [ ] **Step 4: Build to confirm the API compiles**

Run: `make build`
Expected: compiles clean.

- [ ] **Step 5: Commit**

```bash
git add api/v1alpha1/persistence_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd dist/chart
git commit -s -m "feat(api): add persistence.schemaJob.podTemplate

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Make the schema Job use the passwordCommand and apply its podTemplate

**Files:**
- Modify: `internal/resources/schemajob.go` (`SchemaJobParams`, `passwordEnv`, container command/args, `BuildSchemaJob`)
- Test: `internal/resources/schemajob_test.go`

Context: `BuildSchemaJob` currently always runs `temporal-sql-tool` directly with
a static `SQL_PASSWORD` env. We add `PasswordCommand` and `PodTemplate` to
`SchemaJobParams`. When `PasswordCommand` is set, wrap the command in `sh -c`
that exports `SQL_PASSWORD` from the command before exec'ing the tool, and skip
the static env. The `PodTemplate` is applied with the existing `applyPodTemplate`
helper (passing the Job's pod labels as the labels to re-assert).

- [ ] **Step 1: Write the failing tests**

Add to `internal/resources/schemajob_test.go`:

```go
func TestBuildSchemaJobPasswordCommand(t *testing.T) {
	spec := sqlSpec()
	spec.PasswordSecretRef = nil
	job := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          spec,
		PasswordCommand:  "cat /azure/pgpass",
		Store:            StoreDefault,
		Action:           ActionSetup,
		SchemaVersionDir: "v12",
	})
	c := job.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 2 || c.Command[0] != "sh" || c.Command[1] != "-c" {
		t.Fatalf("expected sh -c wrapper, got command %v", c.Command)
	}
	if len(c.Args) != 1 || !strings.Contains(c.Args[0], `SQL_PASSWORD="$(cat /azure/pgpass)"`) {
		t.Errorf("expected SQL_PASSWORD exported from command, got args %v", c.Args)
	}
	if !strings.Contains(c.Args[0], "exec temporal-sql-tool") {
		t.Errorf("expected exec of temporal-sql-tool, got %v", c.Args)
	}
	for _, e := range c.Env {
		if e.Name == "SQL_PASSWORD" {
			t.Errorf("did not expect static SQL_PASSWORD env, got %+v", c.Env)
		}
	}
}

func TestBuildSchemaJobPodTemplate(t *testing.T) {
	raw := []byte(`{"serviceAccountName":"temporal-wi","initContainers":[{"name":"token","image":"mcr.microsoft.com/azure-cli"}]}`)
	job := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          sqlSpec(),
		Store:            StoreDefault,
		Action:           ActionSetup,
		SchemaVersionDir: "v12",
		PodTemplate: &temporalv1alpha1.PodTemplateOverride{
			Labels: map[string]string{"azure.workload.identity/use": "true"},
			Spec:   &runtime.RawExtension{Raw: raw},
		},
	})
	tpl := job.Spec.Template
	if tpl.Labels["azure.workload.identity/use"] != "true" {
		t.Errorf("expected WI label, got %v", tpl.Labels)
	}
	if tpl.Spec.ServiceAccountName != "temporal-wi" {
		t.Errorf("expected serviceAccountName from podTemplate, got %q", tpl.Spec.ServiceAccountName)
	}
	if len(tpl.Spec.InitContainers) != 1 || tpl.Spec.InitContainers[0].Name != "token" {
		t.Errorf("expected token initContainer, got %v", tpl.Spec.InitContainers)
	}
	if len(tpl.Spec.Containers) != 1 || tpl.Spec.Containers[0].Name != "schema" {
		t.Errorf("expected generated schema container preserved, got %v", tpl.Spec.Containers)
	}
}
```

Add the imports `"strings"` and
`"k8s.io/apimachinery/pkg/runtime"` to the test file's import block if missing.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/resources/ -run 'TestBuildSchemaJobPasswordCommand|TestBuildSchemaJobPodTemplate' -v`
Expected: FAIL — unknown fields `PasswordCommand` / `PodTemplate` on `SchemaJobParams`.

- [ ] **Step 3: Implement the params, command wrapper, and podTemplate apply**

In `internal/resources/schemajob.go`:

Add fields to `SchemaJobParams`:

```go
	// Store and Action select the operation.
	Store  SchemaStore
	Action SchemaAction
	// SchemaVersionDir is the on-image schema version directory, e.g. "v12".
	SchemaVersionDir string
	// PasswordCommand, when set, is a shell command whose stdout is exported as
	// SQL_PASSWORD before the schema tool runs (passwordless / token auth).
	PasswordCommand string
	// PodTemplate overrides the Job pod template (ServiceAccount, labels, token
	// initContainer, volumes). Nil leaves the generated pod unchanged.
	PodTemplate *temporalv1alpha1.PodTemplateOverride
}
```

Add a helper that builds the container `Command`/`Args`/`Env`:

```go
// schemaContainerExec returns the container Command, Args, and Env for the
// schema tool. When p.PasswordCommand is set, the tool is wrapped in "sh -c"
// that exports SQL_PASSWORD from the command output (no static env); otherwise
// the tool is invoked directly with the static SQL_PASSWORD env.
func schemaContainerExec(p SchemaJobParams) (command []string, args []string, env []corev1.EnvVar) {
	tool := schemaCommand(p)
	toolArgs := schemaToolArgs(p)
	if p.PasswordCommand != "" && !p.isCassandra() {
		quoted := make([]string, 0, len(toolArgs))
		for _, a := range toolArgs {
			quoted = append(quoted, shellQuote(a))
		}
		script := fmt.Sprintf("export SQL_PASSWORD=\"$(%s)\"; exec %s %s",
			p.PasswordCommand, tool, strings.Join(quoted, " "))
		return []string{"sh", "-c"}, []string{script}, nil
	}
	return []string{tool}, toolArgs, passwordEnv(p)
}

// shellQuote single-quotes an argument for safe use inside an sh -c script.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
```

Add `"strings"` to the imports.

In `BuildSchemaJob`, replace the inline `Command`/`Args`/`Env` and apply the
podTemplate before returning:

```go
func BuildSchemaJob(p SchemaJobParams) *batchv1.Job {
	name := SchemaJobName(p.Cluster.Name, p.Store, p.Action)
	backoff := schemaJobBackoffLimit
	ttl := schemaJobTTLAfterFinish

	labels := map[string]string{
		"app.kubernetes.io/name":       "temporal",
		"app.kubernetes.io/instance":   p.Cluster.Name,
		"app.kubernetes.io/component":  "schema",
		"app.kubernetes.io/managed-by": "temporal-operator",
		"temporal.bmor10.com/store":    string(p.Store),
		"temporal.bmor10.com/action":   string(p.Action),
	}

	command, args, env := schemaContainerExec(p)

	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec: corev1.PodSpec{
			RestartPolicy:    corev1.RestartPolicyNever,
			ImagePullSecrets: p.Cluster.Spec.ImagePullSecrets,
			Containers: []corev1.Container{
				{
					Name:    "schema",
					Image:   temporal.AdminToolsImage(p.Cluster.Spec.Version),
					Command: command,
					Args:    args,
					Env:     env,
				},
			},
		},
	}

	if merged, err := applyPodTemplate(template, p.PodTemplate, labels); err == nil {
		template = merged
	}

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.Cluster.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template:                template,
		},
	}
}
```

Note: `applyPodTemplate` (defined in `deployment.go`, same package) re-asserts
the passed labels after the strategic merge, so the Job's identifying labels are
preserved even if an override omits them. A nil `PodTemplate` is a no-op.

- [ ] **Step 4: Run the new tests**

Run: `go test ./internal/resources/ -run 'TestBuildSchemaJobPasswordCommand|TestBuildSchemaJobPodTemplate' -v`
Expected: PASS.

- [ ] **Step 5: Run the whole resources package**

Run: `go test ./internal/resources/ -v`
Expected: PASS — existing `TestBuildSchemaJobSetup` and others unchanged (static
path still uses `temporal-sql-tool` directly with `SQL_PASSWORD` env).

- [ ] **Step 6: Commit**

```bash
git add internal/resources/schemajob.go internal/resources/schemajob_test.go
git commit -s -m "feat(resources): schema Job passwordCommand + podTemplate support

Closes part of #47: the schema Job can now export SQL_PASSWORD from a
passwordCommand and attach a Workload Identity ServiceAccount, pod label,
and token initContainer via persistence.schemaJob.podTemplate.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Thread the resolved credential and podTemplate into the schema Job from the controller

**Files:**
- Modify: `internal/controller/temporalcluster_persistence.go` (`schemaTarget`, `buildSchemaTargets`, `ensureSchemaJob`)
- Test: covered by envtest suite `internal/controller/` (run full `make test`)

Context: `ensureSchemaJob` builds `SchemaJobParams` from `t.spec.SQL` only. It
must also pass the resolved `PasswordCommand` (already resolved in
`buildSchemaTargets`) and the cluster's `persistence.schemaJob.podTemplate`.

- [ ] **Step 1: Carry the credential on schemaTarget**

In `internal/controller/temporalcluster_persistence.go`, add a field to
`schemaTarget` and set it in `buildSchemaTargets`:

```go
type schemaTarget struct {
	store   resources.SchemaStore
	spec    temporalv1alpha1.DatastoreSpec
	backend persistence.Backend
	cred    persistence.ResolvedCredential
}
```

In `buildSchemaTargets`, the inner `build` closure already resolves `cred`;
include it in the returned target:

```go
		return schemaTarget{store: name, spec: store, backend: backend, cred: cred}, nil
```

- [ ] **Step 2: Pass PasswordCommand + PodTemplate into BuildSchemaJob**

In `ensureSchemaJob`, extend the `resources.BuildSchemaJob` params:

```go
		built := resources.BuildSchemaJob(resources.SchemaJobParams{
			Cluster:          cluster,
			SQLSpec:          t.spec.SQL,
			CassandraSpec:    t.spec.Cassandra,
			Store:            t.store,
			Action:           action,
			SchemaVersionDir: resources.PostgresSchemaDir,
			PasswordCommand:  t.cred.PasswordCommand,
			PodTemplate:      schemaJobPodTemplate(cluster),
		})
```

Add a small helper near the bottom of the file:

```go
// schemaJobPodTemplate returns the configured schema Job podTemplate override,
// or nil when none is set.
func schemaJobPodTemplate(cluster *temporalv1alpha1.TemporalCluster) *temporalv1alpha1.PodTemplateOverride {
	if cluster.Spec.Persistence.SchemaJob == nil {
		return nil
	}
	return cluster.Spec.Persistence.SchemaJob.PodTemplate
}
```

- [ ] **Step 3: Build to confirm it compiles**

Run: `make build`
Expected: compiles clean.

- [ ] **Step 4: Run the full test suite (envtest)**

Run: `make test`
Expected: PASS — controller envtest suites still green; persistence and
resources unit tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/temporalcluster_persistence.go
git commit -s -m "feat(controller): wire passwordCommand and schemaJob podTemplate into schema Jobs

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Add opt-in Workload Identity values to the operator Helm chart

**Files:**
- Modify: `dist/chart/values.yaml`, `dist/chart/templates/manager/manager.yaml` (operator Deployment + ServiceAccount), edited BY HAND.

Context: for the operator pod's `cat /azure/pgpass` passwordCommand to work, the
operator Deployment needs (opt-in) a WI ServiceAccount annotation, the
`azure.workload.identity/use: "true"` pod label, and a token-refresher sidecar
writing to a shared `emptyDir`. All default OFF.

- [ ] **Step 1: Inspect the current chart manager template + values**

Run: `sed -n '1,80p' dist/chart/templates/manager/manager.yaml && echo --- && sed -n '1,80p' dist/chart/values.yaml`
Expected: see the operator Deployment pod template, ServiceAccount template, and
the flat `values.yaml` contract (e.g. `rbacHelpers.enable`).

- [ ] **Step 2: Add values block**

Append to `dist/chart/values.yaml` (match existing flat style):

```yaml
# workloadIdentity wires Azure Workload Identity onto the operator pod so its
# passwordCommand (e.g. "cat /azure/pgpass") can read an Entra token written by
# a sidecar. Disabled by default; non-Azure installs are unaffected.
workloadIdentity:
  enable: false
  # clientId is the managed identity client ID set as the ServiceAccount
  # annotation azure.workload.identity/client-id.
  clientId: ""
  # tokenSidecar runs a federated az login and refreshes the token file.
  tokenSidecar:
    image: mcr.microsoft.com/azure-cli:latest
    # tokenPath is the shared file the sidecar writes and the operator reads.
    tokenPath: /azure/pgpass
    # refreshSeconds is how often the sidecar rewrites the token.
    refreshSeconds: 1800
```

- [ ] **Step 3: Add the conditional pod label, SA annotation, sidecar, and volume**

In `dist/chart/templates/manager/manager.yaml`, within the operator Deployment
pod template metadata add (guarded so it is absent when disabled):

```yaml
      labels:
        {{- include "chart.labels" . | nindent 8 }}
        control-plane: controller-manager
        {{- if .Values.workloadIdentity.enable }}
        azure.workload.identity/use: "true"
        {{- end }}
```

Add the shared volume to the pod `spec`:

```yaml
      {{- if .Values.workloadIdentity.enable }}
      volumes:
        - name: azure-token
          emptyDir: {}
      {{- end }}
```

Add the token-refresher sidecar to `containers` (after the manager container):

```yaml
        {{- if .Values.workloadIdentity.enable }}
        - name: azure-token-refresher
          image: {{ .Values.workloadIdentity.tokenSidecar.image }}
          command: ["/bin/sh", "-c"]
          args:
            - |
              set -e
              az login --federated-token "$(cat $AZURE_FEDERATED_TOKEN_FILE)" \
                --service-principal -u "$AZURE_CLIENT_ID" -t "$AZURE_TENANT_ID" >/dev/null
              while true; do
                az account get-access-token --resource-type oss-rdbms \
                  --query accessToken -o tsv > {{ .Values.workloadIdentity.tokenSidecar.tokenPath }}
                sleep {{ .Values.workloadIdentity.tokenSidecar.refreshSeconds }}
              done
          volumeMounts:
            - name: azure-token
              mountPath: {{ dir .Values.workloadIdentity.tokenSidecar.tokenPath }}
        {{- end }}
```

Mount the shared volume into the manager container as well (so its
passwordCommand can read the token file). Add to the manager container's
`volumeMounts`:

```yaml
          {{- if .Values.workloadIdentity.enable }}
          volumeMounts:
            - name: azure-token
              mountPath: {{ dir .Values.workloadIdentity.tokenSidecar.tokenPath }}
          {{- end }}
```

In the operator ServiceAccount template, add the WI annotation:

```yaml
  annotations:
    {{- if .Values.workloadIdentity.enable }}
    azure.workload.identity/client-id: {{ .Values.workloadIdentity.clientId | quote }}
    {{- end }}
```

(If a `volumeMounts:` key already exists on the manager container, append the
item under it instead of adding a second key.)

- [ ] **Step 4: Verify the chart renders both ways**

Run:
```bash
helm template dist/chart >/dev/null && echo "default OK"
helm template dist/chart --set workloadIdentity.enable=true --set workloadIdentity.clientId=abc123 \
  | grep -q 'azure.workload.identity/use' && echo "WI OK"
```
Expected: `default OK` and `WI OK`.

- [ ] **Step 5: Commit**

```bash
git add dist/chart/values.yaml dist/chart/templates
git commit -s -m "feat(chart): opt-in Azure Workload Identity wiring for the operator pod

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 7: Update the Workload Identity example and Azure docs

**Files:**
- Modify: `examples/cluster-azure-workload-identity/README.md`, `examples/cluster-azure-workload-identity/temporalcluster.yaml`
- Modify: `docs/content/installation/azure.md`

- [ ] **Step 1: Add schemaJob podTemplate to the example cluster**

In `examples/cluster-azure-workload-identity/temporalcluster.yaml`, add a
`persistence.schemaJob.podTemplate` that attaches the WI ServiceAccount, the
`azure.workload.identity/use: "true"` label, and a one-shot token initContainer
writing `/azure/pgpass`, mirroring the server pods' sidecar but as an
initContainer (the Job is short-lived). Keep `passwordCommandSecretRef` pointing
at the `cat /azure/pgpass` Secret.

```yaml
  persistence:
    schemaJob:
      podTemplate:
        labels:
          azure.workload.identity/use: "true"
        spec:
          serviceAccountName: temporal-workload-identity
          initContainers:
            - name: azure-token
              image: mcr.microsoft.com/azure-cli:latest
              command: ["/bin/sh", "-c"]
              args:
                - |
                  az login --federated-token "$(cat $AZURE_FEDERATED_TOKEN_FILE)" \
                    --service-principal -u "$AZURE_CLIENT_ID" -t "$AZURE_TENANT_ID" >/dev/null
                  az account get-access-token --resource-type oss-rdbms \
                    --query accessToken -o tsv > /azure/pgpass
              volumeMounts:
                - name: azure-token
                  mountPath: /azure
          volumes:
            - name: azure-token
              emptyDir: {}
```

- [ ] **Step 2: Rewrite the README status + bootstrap section**

In `examples/cluster-azure-workload-identity/README.md`, remove the "Two actors
are not yet passwordless" caveat and the bootstrap workaround. Replace the
"Status: preview" section with a statement that full passwordless now works
(operator probe, schema Job, and server pods all use the token), and document
installing the operator with the new Helm values:

```sh
helm install temporal-operator oci://ghcr.io/bmorton/charts/temporal-operator \
  --namespace temporal-system --create-namespace \
  --set workloadIdentity.enable=true \
  --set workloadIdentity.clientId=<operator-identity-client-id>
```

Keep the issue reference but note it is now resolved by the PR.

- [ ] **Step 3: Update the Azure docs page**

In `docs/content/installation/azure.md`, update the Microsoft Entra / Workload
Identity section: state full passwordless is supported, document the operator
chart `workloadIdentity.*` values and the `persistence.schemaJob.podTemplate`,
and remove the "current limitations" wording that referenced #47 as unresolved.

- [ ] **Step 4: Sanity-check the example YAML parses**

Run: `kubectl apply --dry-run=client -f examples/cluster-azure-workload-identity/temporalcluster.yaml -f examples/cluster-azure-workload-identity/serviceaccount.yaml 2>/dev/null || echo "needs CRD (ok offline)"`
Expected: either dry-run validation passes, or the offline `needs CRD` notice
(acceptable without a cluster). The YAML must at least be syntactically valid —
confirm with `python -c "import yaml,sys; list(yaml.safe_load_all(open('examples/cluster-azure-workload-identity/temporalcluster.yaml')))" && echo YAML-OK`.

- [ ] **Step 5: Commit**

```bash
git add examples/cluster-azure-workload-identity docs/content/installation/azure.md
git commit -s -m "docs: full passwordless Workload Identity example and Azure guide

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 8: Final verification and PR

- [ ] **Step 1: Generate/manifests are up to date**

Run: `make generate manifests && git diff --exit-code` 
Expected: no diff (generated artifacts already committed).

- [ ] **Step 2: Full build, test, lint**

Run: `make build && make test && make lint`
Expected: all succeed.

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin feat/azure-passwordless-47
gh pr create --fill --title "feat: full Azure Workload Identity passwordless auth (#47)" \
  --body "Closes #47. Operator probe + schema inspection execute the configured passwordCommand; schema Job exports SQL_PASSWORD from the command and supports persistence.schemaJob.podTemplate; opt-in operator Helm Workload Identity wiring. See docs/superpowers/specs/2026-06-19-aks-passwordless-e2e-design.md."
```

Expected: PR created. PR2 (AKS e2e) follows on its own branch after this merges.
