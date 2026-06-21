# Cluster-scoped passwordless identity Implementation Plan

> **Status: shipped in PR #85 (`feat/azure-passwordless-47`).** This is the
> as-built record. It incorporates the foundational `passwordCommand` server-pod
> and schema-Job wiring that closes #47 (originally scoped as a separate PR) as
> well as the cluster-scoped, operator-credential-free refactor that replaced the
> earlier in-process Entra approach. See the design at
> `docs/superpowers/specs/2026-06-20-cluster-scoped-passwordless-identity-design.md`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a `TemporalCluster` reach `Ready` passwordlessly via a single
cluster-level `spec.persistence.azureWorkloadIdentity` field, with the operator
holding no database credential — all DB access happens in the cluster's namespace
under the cluster's identity.

**Architecture:** Move Azure identity from per-store to a cluster-level field that
the operator expands into ServiceAccount + token sidecars/initContainers +
`passwordCommand` wiring (server pods, schema Jobs, inspector Jobs). The operator
deletes its in-process Entra token path; for the Workload-Identity path it learns
reachability + schema version by running a short-lived **inspector Job** that runs
the operator's own image with an `inspect` subcommand (reads the token file,
reuses `SQLProber`, emits JSON via the pod termination message). Static-password
and Cassandra paths are unchanged.

**Tech Stack:** Go 1.26.4, controller-runtime v0.23, Kubebuilder CRDs, Helm
(`dist/chart`), Chainsaw e2e, Azure CLI / AKS / Flexible Server.

**Reference spec:** `docs/superpowers/specs/2026-06-20-cluster-scoped-passwordless-identity-design.md`

**Branch:** `feat/azure-passwordless-47` (continue PR #85; do not branch).

## Global Constraints

- Go module `github.com/bmorton/temporal-operator`; CRD group `temporal.bmor10.com`; copyright owner "Brian Morton" (use the existing Apache header from any file).
- Go 1.26.4 (`go.mod`).
- Sign off every commit: `git commit -s`. Conventional Commit messages. Append trailer `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>`.
- After API type changes, run `make generate manifests`; then **hand-edit** `dist/chart/templates/crd/*.yaml` to match (NEVER run `make helm-chart` — it is destructive; tracked in issue #82).
- Edit `dist/chart` by hand. Do not modify the kind (`.github/workflows/e2e.yml`) or nsc (`hack/nsc-e2e.sh`) flows.
- Validate locally with `make build`, `make test`, `make lint` (these set up envtest). Run targeted `go test ./internal/...` during TDD.
- Image pins must be real published tags (server/admin-tools `1.31.1`; azure-cli `2.87.0`).
- Inspector image = the operator's own running image, discovered via the downward API; do not introduce `psql` or a third image.

---

## File Structure

**API**
- Modify `api/v1alpha1/persistence_types.go` — remove `SQLDatastoreSpec.AzureWorkloadIdentity`; add `PersistenceSpec.AzureWorkloadIdentity *AzureWorkloadIdentitySpec`; extend `AzureWorkloadIdentitySpec` with `ClientID`, `ServiceAccountName`, `Image`, `RefreshInterval` (keep `Scope`).
- Regenerate `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/bases/*temporalclusters.yaml`; hand-edit `dist/chart/templates/crd/*temporalclusters.yaml`.

**Persistence package** (`internal/persistence/`)
- Delete `azure.go`.
- Modify `secrets.go` — drop `AzureWorkloadIdentity` from `ResolvedCredential` and `AzureWorkloadIdentityCredential` type; simplify `ResolveSQL`.
- Modify `sql.go` — drop `tokens`/`resolvePassword` Azure branch; `sqlBackend` uses static `cred.Password` only.
- Create `inspect.go` — `InspectResult` struct + `InspectSQL(ctx, spec, password, dbName) InspectResult` reusing `SQLProber`, plus JSON marshal helpers.
- Create `jobinspector.go` — `JobInspectorBackend` implementing `Backend` by creating/reading an inspector Job and parsing its termination message.

**Resources** (`internal/resources/`)
- Create `azureidentity.go` — `AzureWorkloadIdentityEnabled(cluster)`, `AzureServiceAccountName(cluster)`, `BuildAzureServiceAccount(cluster)`, `ApplyAzureWorkloadIdentity(podSpec, meta, cluster, containerName)` (injects SA, label, token sidecar/init, volume, mount), `AzurePasswordCommand()`, `BuildInspectorJob(params)`.
- Modify `schemajob.go` — when Azure WI is enabled, the schema Job gets the Azure init-container wiring + injected `passwordCommand` from the builder (not a user podTemplate).

**Controller** (`internal/controller/`)
- Modify `temporalcluster_persistence.go` — ensure the Azure SA exists; select `JobInspectorBackend` for SQL stores when WI enabled; generate schema-Job wiring; pass operator image.
- Modify `temporalcluster_services.go` — inject Azure server-pod wiring when WI enabled.
- Add RBAC marker for `serviceaccounts` create/get if not present.

**cmd**
- Create `cmd/inspect.go` — `runInspect(args)` subcommand.
- Modify `cmd/main.go` — dispatch `inspect` before manager setup; expose `OPERATOR_IMAGE` discovery via downward API env.

**Chart / example / e2e**
- Modify `dist/chart/values.yaml`, `dist/chart/templates/manager/manager.yaml`, `dist/chart/templates/rbac/controller-manager.yaml` — remove operator `workloadIdentity.*`; add SA RBAC; add `OPERATOR_IMAGE` env via downward API (image) — see Task 8.
- Simplify `examples/cluster-azure-workload-identity/temporalcluster.yaml`; delete `serviceaccount.yaml`.
- Simplify `test/e2e/azure/03-temporalcluster.yaml`; delete `01-serviceaccount.yaml`, `02-secret.yaml`; update `chainsaw-test.yaml`.
- Modify `hack/azure-e2e.sh` — federated credential → cluster SA in cluster namespace; remove operator federated credential + operator `workloadIdentity.*` Helm flags.

---

## Task 1: Move `azureWorkloadIdentity` to the cluster level (API)

**Files:**
- Modify: `api/v1alpha1/persistence_types.go`
- Regen: `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/bases/temporal.bmor10.com_temporalclusters.yaml`
- Hand-edit: `dist/chart/templates/crd/temporal.bmor10.com_temporalclusters.yaml`

**Interfaces:**
- Produces: `PersistenceSpec.AzureWorkloadIdentity *AzureWorkloadIdentitySpec`; `AzureWorkloadIdentitySpec{ ClientID string; Scope string; ServiceAccountName string; Image string; RefreshInterval *metav1.Duration }`.

- [ ] **Step 1: Remove the per-store field.** In `SQLDatastoreSpec` delete the `AzureWorkloadIdentity *AzureWorkloadIdentitySpec` field and its doc comment (currently `persistence_types.go:94-101`).

- [ ] **Step 2: Add the cluster-level field.** In `PersistenceSpec` add:

```go
	// AzureWorkloadIdentity, when set, makes this cluster authenticate to its
	// SQL datastore(s) passwordlessly using Azure Workload Identity. The operator
	// generates a ServiceAccount, token sidecar/initContainers, and the
	// passwordCommand wiring in the cluster's namespace; the operator itself
	// holds no database credential. SQL stores only.
	// +optional
	AzureWorkloadIdentity *AzureWorkloadIdentitySpec `json:"azureWorkloadIdentity,omitempty"`
```

- [ ] **Step 3: Extend the spec type.** Replace `AzureWorkloadIdentitySpec` with:

```go
// AzureWorkloadIdentitySpec configures passwordless Microsoft Entra auth for a
// cluster's SQL datastores via Azure Workload Identity. The operator expands it
// into a ServiceAccount, token sidecar/initContainers, and passwordCommand
// wiring in the cluster's namespace.
type AzureWorkloadIdentitySpec struct {
	// ClientID is the Azure managed-identity / app-registration client ID used
	// for the ServiceAccount's azure.workload.identity/client-id annotation.
	ClientID string `json:"clientId"`

	// Scope is the Entra token scope requested for the database. Defaults to
	// "https://ossrdbms-aad.database.windows.net/.default".
	// +optional
	Scope string `json:"scope,omitempty"`

	// ServiceAccountName overrides the generated ServiceAccount name
	// (default "<cluster>-azure").
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Image overrides the azure-cli image used by the token sidecar /
	// initContainers (default "mcr.microsoft.com/azure-cli:2.87.0").
	// +optional
	Image string `json:"image,omitempty"`

	// RefreshInterval is how often the server-pod sidecar refreshes the token
	// (default 30m).
	// +optional
	RefreshInterval *metav1.Duration `json:"refreshInterval,omitempty"`
}
```

- [ ] **Step 4: Regenerate.** Run `make generate manifests`. Expected: `zz_generated.deepcopy.go` and `config/crd/bases/...temporalclusters.yaml` update; no errors.

- [ ] **Step 5: Propagate the CRD to the chart by hand.** Diff the regenerated `config/crd/bases/temporal.bmor10.com_temporalclusters.yaml` against `dist/chart/templates/crd/temporal.bmor10.com_temporalclusters.yaml` and apply the same schema change (the `azureWorkloadIdentity` block moves from under `spec.persistence.{defaultStore,visibilityStore}.sql` to `spec.persistence`, and gains `clientId`/`serviceAccountName`/`image`/`refreshInterval`). Preserve the chart file's existing indentation. Verify: `diff <(sed -n '/azureWorkloadIdentity:/,/refreshInterval/p' config/crd/bases/temporal.bmor10.com_temporalclusters.yaml) ...` shows the block present in both at the persistence level.

- [ ] **Step 6: Build + commit.**

```bash
go build ./... && git add api/ config/crd dist/chart/templates/crd
git commit -s -m "feat(api): move azureWorkloadIdentity to cluster persistence level

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Strip the operator's in-process Entra path (persistence)

**Files:**
- Delete: `internal/persistence/azure.go`, `internal/persistence/azure_test.go` (if present)
- Modify: `internal/persistence/secrets.go`, `internal/persistence/sql.go`
- Test: `internal/persistence/sql_test.go`, `internal/persistence/secrets_test.go`

**Interfaces:**
- Produces: `ResolvedCredential{ Password string; PasswordCommand string }` (no Azure field). `sqlBackend` connects with `cred.Password` only.

- [ ] **Step 1: Update the failing tests first.** In `sql_test.go`, delete tests that inject a fake `tokenProvider` / assert Entra-token DSNs. Keep/confirm a test asserting the static-password DSN:

```go
func TestSQLBackendProbeUsesStaticPassword(t *testing.T) {
	b := &sqlBackend{spec: &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12", Host: "h", Port: 5432, User: "u",
	}, cred: ResolvedCredential{Password: "secret"}, dbName: "temporal"}
	got := b.dsn(b.cred.Password)
	if !strings.Contains(got, "u:secret@") {
		t.Fatalf("dsn = %q, want static password", got)
	}
}
```

In `secrets_test.go`, drop assertions on `cred.AzureWorkloadIdentity`.

- [ ] **Step 2: Run tests to verify they fail to compile.** Run: `go test ./internal/persistence/... 2>&1 | head`. Expected: compile errors referencing removed `tokens`/`AzureWorkloadIdentity` (proves the old code is still there).

- [ ] **Step 3: Delete `azure.go`** (and `azure_test.go` if it exists): `git rm internal/persistence/azure.go`.

- [ ] **Step 4: Simplify `secrets.go`.** Remove the `AzureWorkloadIdentity` field from `ResolvedCredential`, delete the `AzureWorkloadIdentityCredential` type, and remove the `if spec.AzureWorkloadIdentity != nil {...}` block from `ResolveSQL` (keep `DefaultAzureOSSRDBMSScope` — it moves to `resources` in Task 4; for now keep it here or move it). `ResolveSQL` becomes just the `switch` on `PasswordCommandSecretRef` / `PasswordSecretRef`.

- [ ] **Step 5: Simplify `sql.go`.** Remove the `tokens tokenProvider` field, the `resolvePassword` method's Azure branch and the `azureTokenTimeout` const; `Probe`/`SchemaVersion` use `b.cred.Password` directly:

```go
func (b *sqlBackend) Probe(ctx context.Context) error {
	return SQLProber{}.Probe(ctx, b.dsn(b.cred.Password))
}
func (b *sqlBackend) SchemaVersion(ctx context.Context) (string, error) {
	return SQLProber{}.CurrentSchemaVersion(ctx, b.dsn(b.cred.Password), b.dbName)
}
```

- [ ] **Step 6: Run tests + build.** Run: `go test ./internal/persistence/... && go build ./...`. Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add -A internal/persistence
git commit -s -m "refactor(persistence): remove operator in-process Entra token path

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: `inspect` subcommand + `InspectSQL` (operator image)

**Files:**
- Create: `internal/persistence/inspect.go`, `internal/persistence/inspect_test.go`
- Create: `cmd/inspect.go`
- Modify: `cmd/main.go`

**Interfaces:**
- Produces: `persistence.InspectResult{ Reachable bool `json:"reachable"`; Version string `json:"version,omitempty"`; Error string `json:"error,omitempty"` }`; `persistence.InspectSQL(ctx, spec *temporalv1alpha1.SQLDatastoreSpec, password, dbName string) InspectResult`; `(InspectResult).JSON() string`.
- Consumes: `SQLProber.Probe`, `SQLProber.CurrentSchemaVersion`, `BuildPostgresDSN` (from `sql.go`).

- [ ] **Step 1: Write the failing test** (`inspect_test.go`):

```go
func TestInspectResultJSONRoundTrip(t *testing.T) {
	r := InspectResult{Reachable: true, Version: "1.13"}
	var got InspectResult
	if err := json.Unmarshal([]byte(r.JSON()), &got); err != nil {
		t.Fatal(err)
	}
	if got != r {
		t.Fatalf("round-trip = %+v, want %+v", got, r)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails.** Run: `go test ./internal/persistence/ -run TestInspectResultJSON`. Expected: FAIL (undefined `InspectResult`).

- [ ] **Step 3: Implement `inspect.go`:**

```go
package persistence

import (
	"context"
	"encoding/json"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// InspectResult is the JSON the inspector subcommand writes to the pod
// termination message: reachability + current schema version for one store.
type InspectResult struct {
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (r InspectResult) JSON() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// InspectSQL probes a SQL store with the given password and reads its schema
// version, returning a structured result (never an error — failures are encoded
// in the result so the subcommand can emit them).
func InspectSQL(ctx context.Context, spec *temporalv1alpha1.SQLDatastoreSpec, password, dbName string) InspectResult {
	dsn := BuildPostgresDSN(spec, password, dbName)
	if err := (SQLProber{}).Probe(ctx, dsn); err != nil {
		return InspectResult{Reachable: false, Error: err.Error()}
	}
	version, err := (SQLProber{}).CurrentSchemaVersion(ctx, dsn, dbName)
	if err != nil {
		return InspectResult{Reachable: true, Error: err.Error()}
	}
	return InspectResult{Reachable: true, Version: version}
}
```

- [ ] **Step 4: Run the test.** Run: `go test ./internal/persistence/ -run TestInspectResult`. Expected: PASS.

- [ ] **Step 5: Implement `cmd/inspect.go`.** The subcommand reads connection params from flags and the password from a file, calls `InspectSQL`, and prints the JSON to stdout AND to the termination-message path:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/persistence"
)

// runInspect implements: manager inspect --host H --port P --db D --user U
//   --plugin postgres12 --tls --password-file /azure/pgpass
//   [--termination-path /dev/termination-log]
func runInspect(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	host := fs.String("host", "", "")
	port := fs.Int("port", 5432, "")
	db := fs.String("db", "", "")
	user := fs.String("user", "", "")
	plugin := fs.String("plugin", "postgres12", "")
	tls := fs.Bool("tls", false, "")
	pwFile := fs.String("password-file", "", "file holding the DB password/token")
	termPath := fs.String("termination-path", "/dev/termination-log", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pw, err := os.ReadFile(*pwFile)
	if err != nil {
		emit(*termPath, persistence.InspectResult{Reachable: false, Error: fmt.Sprintf("reading password file: %v", err)})
		return 1
	}
	spec := &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: *plugin, Host: *host, Port: int32(*port), User: *user, Database: *db,
	}
	if *tls {
		spec.TLS = &temporalv1alpha1.DatastoreTLSSpec{Enabled: true}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res := persistence.InspectSQL(ctx, spec, string(pw), *db)
	emit(*termPath, res)
	return 0 // always 0: the result (not the exit code) carries the outcome
}

func emit(termPath string, res persistence.InspectResult) {
	out := res.JSON()
	fmt.Println(out)
	_ = os.WriteFile(termPath, []byte(out), 0o644)
}
```

(Trim the password of a trailing newline — `strings.TrimSpace(string(pw))` — since the sidecar writes a token followed by a newline.)

- [ ] **Step 6: Dispatch in `cmd/main.go`.** At the very top of `func main()`, before any flag parsing:

```go
	if len(os.Args) > 1 && os.Args[1] == "inspect" {
		os.Exit(runInspect(os.Args[2:]))
	}
```

- [ ] **Step 7: Build + a manual smoke (optional).** Run: `go build ./... && ./bin/manager inspect --help 2>&1 | head` (after `make build`). Expected: builds; the subcommand parses without starting the manager.

- [ ] **Step 8: Commit.**

```bash
git add internal/persistence/inspect.go internal/persistence/inspect_test.go cmd/inspect.go cmd/main.go
git commit -s -m "feat(cmd): add inspect subcommand for in-namespace schema inspection

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Azure identity resource builders

**Files:**
- Create: `internal/resources/azureidentity.go`, `internal/resources/azureidentity_test.go`

**Interfaces:**
- Consumes: `*temporalv1alpha1.TemporalCluster`, `corev1.PodSpec`, `metav1.ObjectMeta`.
- Produces:
  - `const AzureTokenVolumeName = "azure-token"`, `const AzureTokenMountPath = "/azure"`, `const AzureTokenFile = "/azure/pgpass"`, `const DefaultAzureCLIImage = "mcr.microsoft.com/azure-cli:2.87.0"`, `const DefaultAzureScope = "https://ossrdbms-aad.database.windows.net/.default"`, `const AzureWILabel = "azure.workload.identity/use"`.
  - `AzureWorkloadIdentityEnabled(cluster) bool`
  - `AzureServiceAccountName(cluster) string` (spec override or `<cluster>-azure`)
  - `AzurePasswordCommand() string` → `sh -c 'until [ -s /azure/pgpass ]; do sleep 1; done; cat /azure/pgpass'`
  - `BuildAzureServiceAccount(cluster) *corev1.ServiceAccount`
  - `AzureTokenInitContainer(cluster) corev1.Container` (one-shot: az login + write `/azure/pgpass`)
  - `AzureTokenRefresherSidecar(cluster) corev1.Container` (loop: refresh every `RefreshInterval`)
  - `ApplyAzureServerWorkloadIdentity(podMeta *metav1.ObjectMeta, podSpec *corev1.PodSpec, cluster, mainContainerName string)` (SA + label + volume + main mount + sidecar)
  - `ApplyAzureSchemaWorkloadIdentity(podMeta, podSpec, cluster, mainContainerName string)` (SA + label + volume + main mount + initContainer)

- [ ] **Step 1: Write failing builder tests** (`azureidentity_test.go`) asserting:
  - `AzureServiceAccountName` returns `"<cluster>-azure"` by default and the override when set.
  - `BuildAzureServiceAccount` sets annotation `azure.workload.identity/client-id` = `clientId` and namespace = cluster namespace.
  - `ApplyAzureServerWorkloadIdentity` adds the `azure-token` emptyDir volume, the WI pod label `azure.workload.identity/use: "true"`, mounts `/azure` on the named main container, and appends a sidecar named `azure-token-refresher` using the default image.
  - `ApplyAzureSchemaWorkloadIdentity` adds an initContainer named `azure-token` and the `/azure` mount on the named main container.

```go
func TestApplyAzureServerWorkloadIdentity(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "azure-e2e", Namespace: "ns"},
		Spec: temporalv1alpha1.TemporalClusterSpec{Persistence: temporalv1alpha1.PersistenceSpec{
			AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{ClientID: "cid"},
		}},
	}
	var meta metav1.ObjectMeta
	spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "temporal"}}}
	ApplyAzureServerWorkloadIdentity(&meta, &spec, c, "temporal")
	if meta.Labels[AzureWILabel] != "true" { t.Fatal("missing WI label") }
	if spec.ServiceAccountName != "azure-e2e-azure" { t.Fatalf("sa=%q", spec.ServiceAccountName) }
	// volume, mount on temporal container, sidecar present
	// ...assert as above...
}
```

- [ ] **Step 2: Run to confirm failure.** Run: `go test ./internal/resources/ -run Azure`. Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement `azureidentity.go`.** Build the constants, `BuildAzureServiceAccount`, the init/sidecar containers (mirror the **validated** scripts: `az login --service-principal -u "$AZURE_CLIENT_ID" --tenant "$AZURE_TENANT_ID" --federated-token "$(cat "$AZURE_FEDERATED_TOKEN_FILE")" --allow-no-subscriptions` then `az account get-access-token --resource-type oss-rdbms --query accessToken -o tsv > /azure/pgpass`; sidecar loops with `mv` + `sleep <refreshInterval seconds>`), and the two `Apply*` helpers (merge by container name; append volume/initContainer/sidecar idempotently). Default image/scope/interval from the spec or the constants.

- [ ] **Step 4: Run tests.** Run: `go test ./internal/resources/ -run Azure`. Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/resources/azureidentity.go internal/resources/azureidentity_test.go
git commit -s -m "feat(resources): Azure Workload Identity builders (SA, sidecar, init)

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Inspector Job builder

**Files:**
- Create: `internal/resources/inspectorjob.go`, `internal/resources/inspectorjob_test.go`

**Interfaces:**
- Consumes: Task 4 helpers; `SchemaStore` (`StoreDefault`/`StoreVisibility`); `*temporalv1alpha1.SQLDatastoreSpec`.
- Produces:
  - `InspectorJobName(clusterName string, store SchemaStore) string` → `<cluster>-inspect-<store>`
  - `type InspectorJobParams struct { Cluster *temporalv1alpha1.TemporalCluster; Store SchemaStore; SQLSpec *temporalv1alpha1.SQLDatastoreSpec; OperatorImage string }`
  - `BuildInspectorJob(params InspectorJobParams) *batchv1.Job`

- [ ] **Step 1: Write the failing test.** Assert the Job: name `<cluster>-inspect-default`; namespace = cluster namespace; `BackoffLimit=0`, `TTLSecondsAfterFinished≈300`, `RestartPolicy=Never`; SA = `AzureServiceAccountName`; WI pod label; an `azure-token` initContainer + `/azure` volume; one main container using `OperatorImage` whose `args` start with `inspect` and include `--host`, `--db`, `--user`, `--password-file /azure/pgpass`, `--tls` (when `spec.TLS.Enabled`), and `terminationMessagePolicy: FallbackToLogsOnError`.

- [ ] **Step 2: Run to confirm failure.** Run: `go test ./internal/resources/ -run Inspector`. Expected: FAIL.

- [ ] **Step 3: Implement `inspectorjob.go`** using Task 4's `ApplyAzureSchemaWorkloadIdentity` for the token wiring and a main container:

```go
Container{
	Name:  "inspect",
	Image: params.OperatorImage,
	Args: []string{"inspect",
		"--host", spec.Host,
		"--port", strconv.Itoa(int(spec.Port)),
		"--db", spec.Database,
		"--user", spec.User,
		"--plugin", spec.PluginName,
		"--password-file", AzureTokenFile,
	}, // append "--tls" when spec.TLS != nil && spec.TLS.Enabled
	TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
}
```

- [ ] **Step 4: Run tests.** Run: `go test ./internal/resources/ -run Inspector`. Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/resources/inspectorjob.go internal/resources/inspectorjob_test.go
git commit -s -m "feat(resources): inspector Job builder (operator image)

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: `JobInspectorBackend`

**Files:**
- Create: `internal/persistence/jobinspector.go`, `internal/persistence/jobinspector_test.go`

**Interfaces:**
- Consumes: `Backend` interface; `BuildInspectorJob` (via a small injected builder/result to avoid an import cycle — see note); controller-runtime `client.Client`.
- Produces: `NewJobInspectorBackend(c client.Client, scheme, cluster, store, sqlSpec, dbName, operatorImage) *JobInspectorBackend` implementing `Backend`.

> **Import-cycle note:** `internal/resources` imports `api`, and `internal/persistence` imports `api`; `resources` does NOT import `persistence`. To keep it that way, `JobInspectorBackend` should build the Job via a function value injected by the controller (the controller imports both packages), OR move `InspectorJobName`/`BuildInspectorJob` calls into the controller and have the backend receive a "ensure job, return *batchv1.Job" closure. Choose the closure approach: `NewJobInspectorBackend(..., ensureJob func(ctx) (*batchv1.Job, error))`.

- [ ] **Step 1: Write failing tests** with a fake client (`sigs.k8s.io/controller-runtime/pkg/client/fake`):
  - `Probe` returns a sentinel `ErrInspecting` when the Job has no completed pod yet.
  - With a Job whose pod terminated with message `{"reachable":true,"version":"1.13"}`, `Probe` returns nil and `SchemaVersion` returns `"1.13"` (and only one ensure call — caching).
  - With message `{"reachable":false,"error":"timeout"}`, `Probe` returns an error containing `timeout`.
  - `Kind()` returns `"sql"`.

- [ ] **Step 2: Run to confirm failure.** Run: `go test ./internal/persistence/ -run JobInspector`. Expected: FAIL.

- [ ] **Step 3: Implement `jobinspector.go`.** `inspectOnce` ensures the Job (via the injected closure), then finds its pod (`label job-name=<job>`), reads `pod.Status.ContainerStatuses[].State.Terminated.Message`, unmarshals `InspectResult`, caches it. `Probe` runs `inspectOnce`; pending → `ErrInspecting`; `!reachable` → error; reachable → nil. `SchemaVersion` runs `inspectOnce` and returns the cached `Version`. `EnsureSchema` returns `(false, nil)`. Export `var ErrInspecting = errors.New("schema inspection in progress")`.

- [ ] **Step 4: Run tests.** Run: `go test ./internal/persistence/ -run JobInspector`. Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/persistence/jobinspector.go internal/persistence/jobinspector_test.go
git commit -s -m "feat(persistence): JobInspectorBackend reads reachability+version from Jobs

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 7: Wire the abstraction into the controller

**Files:**
- Modify: `internal/controller/temporalcluster_persistence.go`, `internal/controller/temporalcluster_services.go`
- Modify: `internal/controller/temporalcluster_controller.go` (RBAC markers, if SA create not yet allowed)
- Test: `internal/controller/temporalcluster_persistence_test.go`

**Interfaces:**
- Consumes: Tasks 4–6. The reconciler needs its own image: add field `OperatorImage string` to `TemporalClusterReconciler`, populated in `cmd/main.go` from the `OPERATOR_IMAGE` env (Task 8).

- [ ] **Step 1: Ensure the Azure SA exists.** In `reconcilePersistence`, before `buildSchemaTargets`, when `resources.AzureWorkloadIdentityEnabled(cluster)`:

```go
	if resources.AzureWorkloadIdentityEnabled(cluster) {
		sa := resources.BuildAzureServiceAccount(cluster)
		if err := controllerutil.SetControllerReference(cluster, sa, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
	}
```

- [ ] **Step 2: Select the backend.** In `buildSchemaTargets`' `build` closure, when `resources.AzureWorkloadIdentityEnabled(cluster) && store.SQL != nil`, construct a `JobInspectorBackend` (with an `ensureJob` closure calling `r.ensureInspectorJob(ctx, cluster, store.SQL, name)`), else fall through to `factory(...)`. Add `ensureInspectorJob` mirroring `ensureSchemaJob` but using `resources.BuildInspectorJob` with `OperatorImage: r.OperatorImage`.

- [ ] **Step 3: Handle `ErrInspecting`.** In `reconcilePersistence`'s probe loop, treat `errors.Is(err, persistence.ErrInspecting)` as "not yet known": set a `PersistenceReachable=False` with reason `Inspecting` (add `ReasonInspecting` constant) and `return ctrl.Result{RequeueAfter: persistenceRequeueAfter}, nil` (don't log it as an error).

- [ ] **Step 4: Generate schema-Job wiring.** In `ensureSchemaJob`, when `resources.AzureWorkloadIdentityEnabled(cluster)`, pass `PasswordCommand: resources.AzurePasswordCommand()` and apply `resources.ApplyAzureSchemaWorkloadIdentity` to the built Job's pod template (instead of relying on `schemaJobPodTemplate`). Keep the user `schemaJobPodTemplate` path for non-Azure.

- [ ] **Step 5: Generate server-pod wiring.** In `temporalcluster_services.go` where the service Deployment pod template is built, when `resources.AzureWorkloadIdentityEnabled(cluster)`, call `resources.ApplyAzureServerWorkloadIdentity(&pod.ObjectMeta, &pod.Spec, cluster, <serverContainerName>)` and ensure the rendered Temporal config uses the injected `passwordCommand`. (The server container name is per `svc.Name`; confirm against the existing builder and pass it.)

- [ ] **Step 6: RBAC.** Ensure a kubebuilder marker exists for ServiceAccounts: `// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create`. Run `make manifests` and hand-propagate the rule into `dist/chart/templates/rbac/controller-manager.yaml`.

- [ ] **Step 7: Controller test.** With envtest + a faked backend factory/inspector, assert: a cluster with `azureWorkloadIdentity` creates the SA (`<cluster>-azure`) and an inspector Job, and surfaces `PersistenceReachable`/`SchemaReady` from a faked inspector result. Run: `make test`. Expected: PASS.

- [ ] **Step 8: Commit.**

```bash
git add internal/controller config/rbac dist/chart/templates/rbac
git commit -s -m "feat(controller): generate Azure WI wiring + Job-based inspection

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 8: Operator image discovery + chart cleanup

**Files:**
- Modify: `cmd/main.go`, `dist/chart/values.yaml`, `dist/chart/templates/manager/manager.yaml`, `dist/chart/templates/rbac/controller-manager.yaml`

- [ ] **Step 1: Discover the operator image.** In `cmd/main.go`, read `OPERATOR_IMAGE` from the environment and set `reconciler.OperatorImage`. In `dist/chart/templates/manager/manager.yaml`, add to the manager container env:

```yaml
            - name: OPERATOR_IMAGE
              value: "{{ .Values.manager.image.repository }}:{{ .Values.manager.image.tag | default .Chart.AppVersion }}"
```

(If `reconciler.OperatorImage == ""`, fall back to a build-time default constant so non-chart runs still work.)

- [ ] **Step 2: Remove operator Workload Identity.** Delete the `workloadIdentity:` block from `dist/chart/values.yaml` (lines ~78-90) and the conditional WI label/SA-annotation template in `dist/chart/templates/manager/manager.yaml` (and any WI bits in `rbac/controller-manager.yaml`). The operator pod no longer carries `azure.workload.identity/use` or a client-id SA annotation.

- [ ] **Step 3: Verify the chart templates render.** Run: `helm template temporal-operator dist/chart >/dev/null && echo OK`. Expected: `OK`, no references to `workloadIdentity`.

- [ ] **Step 4: Build + commit.**

```bash
go build ./... && git add cmd/main.go dist/chart
git commit -s -m "feat(chart): drop operator Workload Identity; pass OPERATOR_IMAGE to manager

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 9: Simplify the example and the Azure e2e suite

**Files:**
- Modify: `examples/cluster-azure-workload-identity/temporalcluster.yaml`; Delete: `examples/cluster-azure-workload-identity/serviceaccount.yaml`; update `examples/cluster-azure-workload-identity/README.md`.
- Modify: `test/e2e/azure/03-temporalcluster.yaml`, `test/e2e/azure/chainsaw-test.yaml`; Delete: `test/e2e/azure/01-serviceaccount.yaml`, `test/e2e/azure/02-secret.yaml`; update `test/e2e/azure/README.md`.

- [ ] **Step 1: Rewrite the example** to the simplified form from the spec (§5): a single `persistence.azureWorkloadIdentity.clientId`, no podTemplates, no Secret, no separate ServiceAccount. Update the README to remove the sidecar/initContainer explanation and the operator `workloadIdentity.enable` install note (operator no longer needs it).

- [ ] **Step 2: Rewrite `test/e2e/azure/03-temporalcluster.yaml`:**

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: azure-e2e
spec:
  version: "1.31.1"
  numHistoryShards: 512
  persistence:
    azureWorkloadIdentity:
      clientId: ($values.clientId)
    defaultStore:
      sql:
        pluginName: postgres12
        host: ($values.pgHost)
        port: 5432
        database: temporal
        user: ($values.pgUser)
        tls: { enabled: true }
    visibilityStore:
      sql:
        pluginName: postgres12
        host: ($values.pgHost)
        port: 5432
        database: temporal_visibility
        user: ($values.pgUser)
        tls: { enabled: true }
```

- [ ] **Step 3: Update `chainsaw-test.yaml`.** Remove the `identity-and-secret` step and the `apply` of `01-serviceaccount.yaml`/`02-secret.yaml`; keep the `cluster-ready` step (apply `03-temporalcluster.yaml`, assert `03-assert.yaml`) and its `catch`. Delete the two obsolete files.

- [ ] **Step 4: Validate offline.** Run: `for f in test/e2e/azure/*.yaml; do python3 -c "import yaml,sys;list(yaml.safe_load_all(open('$f')))"; done && bin/chainsaw lint test --file test/e2e/azure/chainsaw-test.yaml && echo OK`. Expected: `OK`.

- [ ] **Step 5: Commit.**

```bash
git add examples/cluster-azure-workload-identity test/e2e/azure
git commit -s -m "docs,test(e2e): collapse Azure passwordless config to one field

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 10: Update the provisioning harness

**Files:**
- Modify: `hack/azure-e2e.sh`, `test/e2e/azure/README.md`

- [ ] **Step 1: Bind the federated credential to the cluster SA.** In `cmd_up`, set `SA_NAME` to the operator-generated SA name for the e2e cluster (`azure-e2e-azure`, since the cluster is named `azure-e2e` and the default SA is `<cluster>-azure`). Keep the workload federated credential subject `system:serviceaccount:$AZURE_TEST_NS:$SA_NAME`. **Remove** the second `fc-operator-$sfx` federated credential and the `OPERATOR_NS`/`OPERATOR_SA` locals.

- [ ] **Step 2: Drop operator Helm WI flags.** In the `helm install` invocation remove `--set workloadIdentity.enable=true` and `--set workloadIdentity.clientId=...`. Keep image repo/tag, cert-manager, extension allow-list, firewall rule, PG16 grants.

- [ ] **Step 3: shellcheck + syntax.** Run: `bash -n hack/azure-e2e.sh && shellcheck hack/azure-e2e.sh && echo OK`. Expected: `OK`.

- [ ] **Step 4: Update README** to note the operator no longer needs Workload Identity and only the cluster SA gets a federated credential.

- [ ] **Step 5: Commit.**

```bash
git add hack/azure-e2e.sh test/e2e/azure/README.md
git commit -s -m "test(e2e): federate the cluster SA only; drop operator WI

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 11: Full validation (local + real Azure)

- [ ] **Step 1: Local gate.** Run: `make generate manifests && git diff --quiet && make build && make test && make lint`. Expected: no manifest drift, build OK, tests PASS, lint `0 issues`.

- [ ] **Step 2: Real Azure run.** Run: `make azure-e2e-clean && make azure-e2e-up && make azure-e2e-test`. Expected: `--- PASS: chainsaw/azure-passwordless`. On failure, inspect the inspector Job (`kubectl -n azure-e2e get jobs,pods`; the inspector pod's termination message) and the schema Jobs; fix forward with `fix(...)` commits and re-run `make azure-e2e-test`.

- [ ] **Step 3: Tear down.** Run: `make azure-e2e-down` and confirm `az group list --tag app=temporal-operator-e2e -o tsv` eventually empties (or `make azure-e2e-clean`).

- [ ] **Step 4: Finish.** Use the finishing-a-development-branch skill: confirm tests, push to PR #85.

---

## Self-review notes

- **Spec coverage:** API move (T1), delete in-process path (T2), inspect subcommand reusing SQLProber (T3), generated SA/sidecar/init (T4), inspector Job on operator image (T5), Job-based reachability+version with caching (T6), controller wiring + setup-vs-update unchanged + `Inspecting` reason (T7), operator-image discovery + chart WI removal (T8), simplified example/suite (T9), harness federated-cred change (T10), local+real validation (T11).
- **Import cycle:** addressed in Task 6 (closure injection; `resources` never imports `persistence`).
- **Reachability-after-ready trade-off:** documented in the spec (Trade-offs); the inspector runs while not `SchemaReady`/on generation change.
- **Cassandra/static paths:** untouched (T2 keeps `sqlBackend` static; backend selection only diverts SQL + WI).
