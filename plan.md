# Temporal Operator — Build Plan

A coding-agent-ready, milestone-driven implementation plan for a modern Kubernetes operator for Temporal, written in Go using the Operator SDK. This plan operationalizes the architectural research already completed and is structured for incremental delivery.

---

## Progress log

> Concrete project parameters (locked in): module `github.com/bmorton/temporal-operator`,
> CRD group `temporal.bmor10.com` (kubebuilder `--domain bmor10.com`), owner "Brian Morton",
> Go `1.26.3`. Work happens directly in this repo (no PRs for now).
>
> Environment note: this devcontainer has no Docker daemon or kind, so the
> `make docker-build` and `kind`-based acceptance checks are deferred/unverified
> locally. `make build`, `make test` (envtest), and `make lint` are the local gates.

- [x] **Milestone 0 — Repository bootstrap and governance.** Apache 2.0 LICENSE,
  README, CoC (Contributor Covenant 2.1), CONTRIBUTING, GOVERNANCE, MAINTAINERS,
  SECURITY, CODEOWNERS, issue/PR templates, dependabot, CI skeleton, commitlint,
  DCO workflow. _(commit `bb1af86`)_
- [x] **Milestone 1 — Operator scaffold + no-op reconciler.** Kubebuilder v4.10.1
  scaffold (`42c4c0e`), minimal `TemporalCluster` type + `Ready=False/NotImplemented`
  reconciler, envtest, tightened golangci-lint, consolidated `ci.yml`, Makefile
  targets. `make build/test/lint` green. _(commit `8ec5ac0`)_
- [x] **Milestone 2 — CRD design v1alpha1.** Full `TemporalCluster` spec/status,
  three satellite CRD stubs (resource-only), CEL validations (immutable shards,
  exactly-one datastore), conditions enumeration, samples, `crd-ref-docs` +
  `make api-docs`, envtest for CEL. _(commit `23af968`)_
- [x] **Milestone 3 — Webhooks.** TemporalCluster defaulting + validation
  (version/datastore/mTLS/shard-immutability/upgrade-path/preventDeletion),
  satellite validating webhooks, seeded version matrix (`internal/temporal`),
  cert-manager webhook certs, tests. Conversion webhook deferred (single
  version). _(commit `6207411`)_
- [x] **Milestone 4 — Version matrix + config template engine.** Generated
  version matrix (`hack/version-matrix.yaml` → `versions_gen.go`), text/template
  + sprig config renderer with golden tests across Postgres/Cassandra/ES/mTLS/
  archival/internal-frontend, dynamic-config renderer (removed-key rejection
  wired into webhook), persistence `SecretResolver`. _(commit `6cb462e`)_
- [x] **Milestone 5 — Persistence reconciliation (Postgres).** SQL prober +
  schema inspector (pgx), schema version compare, admin-tools schema Job builder,
  persistence sub-reconciler (probe → PersistenceReachable; setup/update Jobs →
  SchemaReady) with Job ownership/watches, envtest for all transitions, unit
  tests, and Chainsaw e2e smoke files (CNPG). _(commit pending)_
- [x] **Milestone 6 — Service deployment and rollout.** Deployment/Service/PDB/
  ConfigMap/Secret builders, service sub-reconciler (SSA, config-hash rollout),
  status rollup (Available/Ready, phase), ClusterReady event, owned-resource
  watches, lifecycle envtest + unit tests + Chainsaw lifecycle e2e. Config is
  stored in a Secret (embeds credentials). _(commit `922a21b`)_
- [x] **Milestone 7 — mTLS with cert-manager.** mTLS sub-reconciler (internode +
  frontend Certificates, MTLSReady condition, cert mounts, cert-hash rotation),
  Ready gated on MTLSReady, TemporalClusterClient controller (client credential
  Secret), cert-manager scheme/CRDs in envtest, tests, Chainsaw mTLS e2e.
  _(commit `b716f69`)_
- [x] **Milestone 8 — temporal-ui, monitoring, archival.** UI Deployment/Service/
  Ingress (+ auto UI client cert under mTLS), ServiceMonitor (unstructured,
  CRD-gated via RESTMapper), metrics port on headless Services, starter Grafana
  dashboard, archival rendered in config. Tests + UI Chainsaw e2e. _(commit `267a316`)_
- [x] **Milestone 9 — Cassandra and Elasticsearch backends.** Datastore `Backend`
  abstraction + injectable factory, gocql Cassandra backend, HTTP Elasticsearch
  backend (inline index-template), `temporal-cassandra-tool` schema Jobs, tests,
  K8ssandra/ECK Chainsaw e2e. _(commit `faf3a5f`)_
- [x] **Milestone 10 — Upgrades across Temporal versions.** Upgrade sub-reconciler
  with the `status.upgrade` phase machine (preflight → schema → ordered per-service
  rolling restarts → post-upgrade), per-service version threading, Rollbackable
  flag, events, envtest, upgrade Chainsaw e2e, nightly upgrade-matrix workflow.
  _(commit `f81e7b1`)_
- [x] **Milestone 11 — `TemporalNamespace` reconciler.** Temporal gRPC
  NamespaceClient (injectable factory), namespace controller (register/update/
  drift/finalizer-delete gated on cluster Ready), API + status fields, envtest
  with fake client, namespace Chainsaw e2e. _(commit `beac37f`)_
- [x] **Milestone 12 — `TemporalSearchAttribute` reconciler.** Search-attribute
  client ops (List/Add/Remove), controller (register + poll-visible +
  finalizer-remove), API fields, envtest, search-attribute Chainsaw e2e.
  _(commit `915e9f3`)_
- [x] **Milestone 13 — Helm chart, OLM bundle, kustomize distribution.** Helm
  chart (kubebuilder helm/v2-alpha) with metadata/NOTES/README/Artifact Hub,
  `helm lint` clean; `dist/install.yaml` via build-installer; OLM bundle scaffold
  (CSV, annotations, scorecard, bundle.Dockerfile); recovered dist/ tracking.
  _(commit `2018072`)_
- [x] **Milestone 14 — e2e in CI.** Kind-based e2e workflow (build/load image,
  install cert-manager + CNPG, Helm install, Chainsaw suite) with PR single-combo
  and nightly matrix, diagnostics upload, aggregated status check. namespace.so
  runner deferred (no credentials); kind is the practical path. _(commit `c2b8ac5`)_
- [ ] **Milestone 15 — Release engineering** _(next up)_

---

## How to use this document

This plan is written for an autonomous (or semi-autonomous) coding agent working in iterative passes against a Git repository. Each milestone is a coherent unit of work with explicit deliverables, acceptance criteria, and verification commands. Within a milestone, tasks are ordered by dependency. Do not skip ahead.

**Conventions used throughout:**

- `RUN:` — a shell command the agent should execute and confirm exits 0.
- `WRITE:` — a file the agent should create.
- `VERIFY:` — an automated check that must pass before the milestone is considered done.
- `OUT-OF-SCOPE:` — explicitly deferred to a later milestone; do not implement now.
- All Go module paths assume `github.com/<org>/temporal-operator`. Replace `<org>` with the actual GitHub organization at scaffold time.
- All CRD groups use `temporal.<org>.io`. Replace `<org>` consistently.

**Global definition of "done" for any task:**

1. Code compiles (`go build ./...`).
2. Unit and envtest suites pass (`make test`).
3. Linters pass (`make lint`).
4. CRD/manifest regeneration is current (`make manifests generate` produces no diff).
5. New behavior is covered by a test (unit, envtest, or Chainsaw, whichever is appropriate per the testing pyramid below).
6. Documentation is updated for any user-visible change.

**Testing pyramid (applied per milestone):**

- ~70% envtest (controller behavior with real apiserver semantics).
- ~20% unit (pure logic, helpers, template rendering).
- ~10% Chainsaw e2e (full lifecycle on real cluster).

---

## Target versions (pin these in `go.mod` / tool versions)

| Component | Version | Notes |
|---|---|---|
| Go | 1.26.3 | Tracks latest stable; Kubebuilder v4.10 scaffolds work on >=1.24 |
| Kubebuilder | v4.10.1 | v4.10.0 was retracted as a Go module |
| Operator SDK | v1.41+ | Aligned with Kubebuilder 4.6+, controller-runtime v0.21 |
| controller-runtime | v0.23.3 | Ships with Kubebuilder 4.10 |
| controller-tools (controller-gen) | v0.17+ | CEL validations, conditions support |
| Ginkgo / Gomega | v2.22+ / v1.36+ | Standard Kubebuilder test deps |
| envtest binaries | k8s 1.33.x | `setup-envtest use 1.33.x` |
| cert-manager | v1.20+ | For the operator's own webhook serving certs |
| golangci-lint | v2.x | Stricter scaffold in Kubebuilder v4.4+ |
| Chainsaw | latest stable | Kyverno project, replaces kuttl |
| kind | v0.27+ | Contributor-local fallback |
| Helm | v3.16+ | Chart packaging |

**Supported Temporal versions at v1.0 of the operator:** 1.27.x, 1.28.x, 1.29.x, 1.30.x, 1.31.x (sequential upgrades only between adjacent minors). v0.1 of the operator targets exactly one Temporal version (1.31.x) and expands from there.

---

## Repository layout (target end-state)

```
.
├── api/                                   # CRD type definitions
│   └── v1alpha1/
│       ├── groupversion_info.go
│       ├── temporalcluster_types.go
│       ├── temporalnamespace_types.go
│       ├── temporalsearchattribute_types.go
│       ├── temporalclusterclient_types.go
│       └── zz_generated.deepcopy.go
├── cmd/
│   └── main.go                            # Manager bootstrap
├── config/                                # Kustomize bases
│   ├── crd/
│   ├── default/
│   ├── manager/
│   ├── rbac/
│   ├── webhook/
│   ├── certmanager/
│   ├── prometheus/
│   ├── network-policy/
│   └── samples/
├── dist/
│   ├── chart/                             # Auto-generated Helm chart (helm/v2-alpha plugin)
│   └── install.yaml                       # `make build-installer` output
├── docs/                                  # Hugo Docsy site source
├── examples/                              # Sample CRs for users
├── hack/
│   ├── boilerplate.go.txt
│   ├── tools/
│   └── version-matrix.go                  # Compatibility matrix generator
├── internal/
│   ├── controller/
│   │   ├── temporalcluster_controller.go
│   │   ├── temporalcluster_persistence.go
│   │   ├── temporalcluster_services.go
│   │   ├── temporalcluster_mtls.go
│   │   ├── temporalcluster_monitoring.go
│   │   ├── temporalcluster_ui.go
│   │   ├── temporalcluster_upgrade.go
│   │   ├── temporalnamespace_controller.go
│   │   ├── temporalsearchattribute_controller.go
│   │   └── temporalclusterclient_controller.go
│   ├── webhook/
│   │   └── v1alpha1/
│   │       ├── temporalcluster_webhook.go
│   │       ├── temporalnamespace_webhook.go
│   │       └── temporalsearchattribute_webhook.go
│   ├── persistence/
│   │   ├── sql.go
│   │   ├── cassandra.go
│   │   ├── elasticsearch.go
│   │   └── schema.go
│   ├── temporal/
│   │   ├── client.go                      # gRPC client to a TemporalCluster
│   │   ├── configtemplate.go              # config_template.yaml rendering
│   │   └── versions.go                    # Compatibility matrix
│   ├── resources/                         # Builders for Deployments, Services, ConfigMaps, etc.
│   ├── reconciler/
│   │   ├── conditions.go
│   │   ├── status.go
│   │   └── requeue.go
│   └── version/
│       └── version.go
├── test/
│   ├── e2e/                               # Chainsaw tests
│   │   ├── postgres/
│   │   ├── cassandra/
│   │   ├── upgrade/
│   │   ├── mtls/
│   │   └── _fixtures/
│   ├── envtest/                           # Ginkgo envtest suites
│   └── utils/
├── .github/
│   ├── workflows/
│   │   ├── ci.yml
│   │   ├── e2e.yml
│   │   ├── nightly.yml
│   │   ├── release.yml
│   │   └── docs.yml
│   ├── dependabot.yml
│   ├── CODEOWNERS
│   └── ISSUE_TEMPLATE/
├── .chainsaw.yaml
├── .golangci.yml
├── .goreleaser.yml
├── Dockerfile
├── Makefile
├── PROJECT                                # Kubebuilder project file
├── go.mod
├── go.sum
├── CHANGELOG.md
├── CODE_OF_CONDUCT.md
├── CONTRIBUTING.md
├── GOVERNANCE.md
├── LICENSE
├── MAINTAINERS.md
├── README.md
└── SECURITY.md
```

---

## Milestone 0 — Repository bootstrap and governance

**Goal:** stand up an empty but correctly-shaped repository with all the meta-files, CI scaffolding, and governance content in place before any operator code is written.

### Tasks

1. **Initialize the repository.**
   - `RUN: git init && git checkout -b main`
   - `WRITE: LICENSE` — Apache 2.0.
   - `WRITE: README.md` — placeholder with project summary, status badge slots, and "see docs site" link.
   - `WRITE: CODE_OF_CONDUCT.md` — Contributor Covenant 2.1.
   - `WRITE: CONTRIBUTING.md` — quickstart, conventional commits requirement, DCO, how to run tests.
   - `WRITE: GOVERNANCE.md` — maintainer roles, decision process, lazy consensus rules.
   - `WRITE: MAINTAINERS.md` — initial maintainer list.
   - `WRITE: SECURITY.md` — security disclosure policy with private security advisory link.
   - `WRITE: .github/CODEOWNERS` — `* @<org>/maintainers`.

2. **Scaffold issue and PR templates.**
   - `WRITE: .github/ISSUE_TEMPLATE/bug_report.yml`
   - `WRITE: .github/ISSUE_TEMPLATE/feature_request.yml`
   - `WRITE: .github/ISSUE_TEMPLATE/config.yml` — link to Discussions for questions.
   - `WRITE: .github/PULL_REQUEST_TEMPLATE.md`

3. **Dependabot and Renovate.**
   - `WRITE: .github/dependabot.yml` — daily for `gomod`, `github-actions`, `docker`.

4. **Initial CI skeleton (will be filled in M1).**
   - `WRITE: .github/workflows/ci.yml` — minimal: checkout, set up Go, `go vet`, fail-fast off. This gets refined in every later milestone.

5. **Conventional commits enforcement.**
   - `WRITE: .github/workflows/conventional-commits.yml` — uses `wagoid/commitlint-github-action`.
   - `WRITE: commitlint.config.js` — extends `@commitlint/config-conventional`.

6. **DCO check.**
   - `WRITE: .github/workflows/dco.yml` — uses `tim-actions/dco@v1.1.0`.

### Acceptance criteria

- `VERIFY: git log` shows initial commit with all bootstrap files.
- `VERIFY: gh repo view` (or equivalent) confirms repo exists with default branch `main` and branch protection enabled (require PR review, require status checks, require linear history).
- `VERIFY:` CI runs and the trivial `go vet` job passes on a no-op PR.

### Out of scope

- Any operator code.
- Any CRD types.
- Helm chart, OLM bundle.

---

## Milestone 1 — Operator scaffold and "hello world" reconciler

**Goal:** scaffold the project with Kubebuilder, get a no-op `TemporalCluster` CRD into a kind cluster, and produce a reconciler that does nothing except set a `Ready=False, reason=NotImplemented` condition. This validates the toolchain end-to-end.

### Tasks

1. **Scaffold with Kubebuilder.**
   - `RUN: kubebuilder init --domain <org>.io --repo github.com/<org>/temporal-operator --owner "<Org>" --project-name temporal-operator`
   - `RUN: kubebuilder create api --group temporal --version v1alpha1 --kind TemporalCluster --resource --controller`
   - Commit the unmodified scaffold so any drift is reviewable.

2. **Pin tool versions.**
   - `WRITE: Makefile` — augment scaffolded Makefile with explicit version pins for `KUBEBUILDER_VERSION`, `CONTROLLER_TOOLS_VERSION`, `ENVTEST_K8S_VERSION=1.33.0`, `GOLANGCI_LINT_VERSION=v2.0.0`, `CHAINSAW_VERSION`, `KIND_VERSION`.
   - Add Make targets: `lint`, `lint-fix`, `chainsaw`, `kind-up`, `kind-down`, `kind-load`, `install-tools`.

3. **Replace kube-rbac-proxy with controller-runtime metrics auth.**
   - Edit `cmd/main.go` to use `metricsserver.Options{ FilterProvider: filters.WithAuthenticationAndAuthorization, SecureServing: true, CertDir: ... }`.
   - Remove any `kube-rbac-proxy` sidecar from `config/default/manager_auth_proxy_patch.yaml` (delete the file; remove from kustomization).
   - VERIFY no references to `gcr.io/kubebuilder/kube-rbac-proxy` remain (`grep -r kube-rbac-proxy .`).

4. **Define the minimal `TemporalCluster` type.**
   - `WRITE: api/v1alpha1/temporalcluster_types.go` with just `Spec.Version string` and a status with `Conditions []metav1.Condition` and `ObservedGeneration int64`. Register CRD markers: `+kubebuilder:object:root=true`, `+kubebuilder:resource:scope=Namespaced,shortName=tc`, `+kubebuilder:subresource:status`, `+kubebuilder:printcolumn` for `Version`, `Ready`, `Age`.

5. **No-op reconciler.**
   - `internal/controller/temporalcluster_controller.go`:
     - `Reconcile` fetches the CR, sets `Ready=False, Reason=NotImplemented, Message="Reconciler scaffold only"`, updates status with `ObservedGeneration`, returns `ctrl.Result{}`.
     - SetupWithManager watches `TemporalCluster`.
   - Use `log.FromContext(ctx)` for logging.

6. **Linting baseline.**
   - `WRITE: .golangci.yml` — start from Kubebuilder v4.10 scaffold defaults; enable `errcheck`, `govet`, `staticcheck`, `revive`, `gocyclo` (threshold 15), `gofumpt`, `goimports`, `misspell`, `nolintlint`, `unused`.
   - Run `make lint` until clean.

7. **First envtest.**
   - `WRITE: internal/controller/temporalcluster_controller_test.go`:
     - Ginkgo describe "TemporalCluster controller".
     - Test: creating a `TemporalCluster` results in status with a `Ready` condition with `Reason=NotImplemented` within 5s.
   - `WRITE: internal/controller/suite_test.go` for envtest setup (Kubebuilder scaffolds this; adapt).

8. **CI: full lint + test.**
   - Update `.github/workflows/ci.yml`:
     - matrix on Go versions `[1.26.x]` (single entry for now).
     - jobs: `lint`, `test` (with envtest binaries cached), `build` (`make docker-build`).
     - Use `actions/cache@v4` for `~/go/pkg/mod` and `~/.cache/go-build` and the envtest binary cache.

9. **First image build, no push.**
   - Ensure `make docker-build IMG=temporal-operator:dev` succeeds and the image is loadable into kind.

### Acceptance criteria

- `VERIFY: make manifests generate fmt vet build` exits 0.
- `VERIFY: make test` runs envtest and passes.
- `VERIFY: make lint` exits 0.
- `VERIFY: make docker-build` exits 0.
- `VERIFY: kind create cluster && make install && kubectl apply -f config/samples/temporal_v1alpha1_temporalcluster.yaml && kubectl wait --for=condition=Ready=false temporalcluster/temporalcluster-sample --timeout=30s` works.

### Out of scope

- Any real Temporal logic.
- Webhooks (next milestone).
- More than one CRD.

---

## Milestone 2 — CRD design v1alpha1 (no behavior yet)

**Goal:** define the full `TemporalCluster` schema (plus the four satellite CRDs as empty stubs) without implementing reconciliation. The CRD shape must be reviewable independently from controller logic.

### Tasks

1. **Define `TemporalCluster` types in full.**
   - In `api/v1alpha1/temporalcluster_types.go`:

```go
type TemporalClusterSpec struct {
    // Version is the Temporal server version, e.g. "1.31.2".
    // +kubebuilder:validation:Pattern=`^\d+\.\d+\.\d+$`
    Version string `json:"version"`

    // NumHistoryShards is the number of history shards.
    // IMMUTABLE after creation. Choose carefully: 512 small prod, 4096 large prod.
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=16384
    // +kubebuilder:default=512
    NumHistoryShards int32 `json:"numHistoryShards"`

    // Image is the Temporal server image. Default: temporalio/server:<Version>
    // +optional
    Image string `json:"image,omitempty"`

    // ImagePullSecrets
    // +optional
    ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

    // Services configures each Temporal service.
    // +optional
    Services ServicesSpec `json:"services,omitempty"`

    // Persistence is required: defaultStore and visibilityStore.
    Persistence PersistenceSpec `json:"persistence"`

    // MTLS configures mTLS (cert-manager-driven by default).
    // +optional
    MTLS *MTLSSpec `json:"mtls,omitempty"`

    // DynamicConfig is a passthrough for Temporal's dynamic config.
    // +optional
    DynamicConfig *DynamicConfigSpec `json:"dynamicConfig,omitempty"`

    // UI configures temporal-ui as part of this cluster.
    // +optional
    UI *UISpec `json:"ui,omitempty"`

    // Metrics configures Prometheus integration.
    // +optional
    Metrics *MetricsSpec `json:"metrics,omitempty"`

    // Archival is per-namespace; this is the cluster-wide enablement.
    // +optional
    Archival *ArchivalSpec `json:"archival,omitempty"`

    // Authorization configures the authorizer/claimMapper.
    // +optional
    Authorization *AuthorizationSpec `json:"authorization,omitempty"`

    // ClusterMetadata is a passthrough for multi-cluster setup.
    // +optional
    ClusterMetadata *ClusterMetadataSpec `json:"clusterMetadata,omitempty"`
}

type ServicesSpec struct {
    Frontend         *ServiceSpec `json:"frontend,omitempty"`
    History          *ServiceSpec `json:"history,omitempty"`
    Matching         *ServiceSpec `json:"matching,omitempty"`
    Worker           *ServiceSpec `json:"worker,omitempty"`
    InternalFrontend *InternalFrontendSpec `json:"internalFrontend,omitempty"`
    Overrides        *ServiceOverrides `json:"overrides,omitempty"` // shared defaults
}

type ServiceSpec struct {
    // +kubebuilder:default=1
    // +kubebuilder:validation:Minimum=1
    Replicas    *int32                    `json:"replicas,omitempty"`
    Resources   corev1.ResourceRequirements `json:"resources,omitempty"`
    PodTemplate *PodTemplateOverride      `json:"podTemplate,omitempty"`
    Service     *ServiceExposureSpec      `json:"service,omitempty"`
    NodeSelector map[string]string        `json:"nodeSelector,omitempty"`
    Tolerations  []corev1.Toleration      `json:"tolerations,omitempty"`
    Affinity     *corev1.Affinity         `json:"affinity,omitempty"`
    TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}
```

   - Define `PersistenceSpec`, `DatastoreSpec`, `SQLDatastoreSpec`, `CassandraDatastoreSpec`, `ElasticsearchDatastoreSpec` with the fields from the architectural plan. Critically:
     - `passwordSecretRef` (mandatory for password-auth) and `passwordCommandSecretRef` (Temporal 1.31+ IAM auth, optional).
     - `tls` sub-struct with `caSecretRef`, `certSecretRef`, `keySecretRef`.
     - `schemaVersion: "auto" | "<pinned>"`.
   - Define `MTLSSpec` (provider, internodeCA, frontend cert spec, refreshInterval, renewBefore).
   - Define `DynamicConfigSpec.Values` as `map[string][]DynamicConfigValue` (each value has `value runtime.RawExtension` and an optional `constraints` block).
   - Define `UISpec` (enabled, version, replicas, ingress, auth, codecServer).
   - Define `MetricsSpec` (enabled, port, serviceMonitor sub-spec).
   - Define `ArchivalSpec`, `AuthorizationSpec`, `ClusterMetadataSpec` as passthroughs initially.

2. **Define `TemporalClusterStatus`.**

```go
type TemporalClusterStatus struct {
    // +optional
    Phase string `json:"phase,omitempty"`

    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // +optional
    Version string `json:"version,omitempty"`

    // NumHistoryShards as observed from the database, not the spec.
    // +optional
    NumHistoryShards int32 `json:"numHistoryShards,omitempty"`

    // +optional
    Services map[string]ServiceStatus `json:"services,omitempty"`

    // +optional
    Persistence PersistenceStatus `json:"persistence,omitempty"`

    // +optional
    Endpoints EndpointsStatus `json:"endpoints,omitempty"`

    // +optional
    Upgrade *UpgradeStatus `json:"upgrade,omitempty"`
}
```

   - Define `PersistenceStatus.SchemaVersions` map, `History []SchemaUpgradeRecord`, `Reachable bool`.

3. **Define condition type constants.**
   - `WRITE: api/v1alpha1/conditions.go`:

```go
const (
    ConditionReady                = "Ready"
    ConditionAvailable            = "Available"
    ConditionProgressing          = "Progressing"
    ConditionDegraded             = "Degraded"
    ConditionPersistenceReachable = "PersistenceReachable"
    ConditionSchemaReady          = "SchemaReady"
    ConditionMTLSReady            = "MTLSReady"
    ConditionUpgradeBlocked       = "UpgradeBlocked"
    ConditionShardCountLocked     = "ShardCountLocked"
)

const (
    ReasonNotImplemented        = "NotImplemented"
    ReasonReconciling           = "Reconciling"
    ReasonPersistenceUnreachable = "PersistenceUnreachable"
    ReasonSchemaMigrating       = "SchemaMigrating"
    ReasonShardCountImmutable   = "ShardCountImmutable"
    ReasonVersionUnsupported    = "VersionUnsupported"
    ReasonUpgradePathInvalid    = "UpgradePathInvalid"
    ReasonRolloutInProgress     = "RolloutInProgress"
    ReasonAllServicesReady      = "AllServicesReady"
    // ... full enumeration
)
```

4. **Stub the satellite CRDs.**
   - `RUN: kubebuilder create api --group temporal --version v1alpha1 --kind TemporalNamespace --resource --controller=false`
   - Same for `TemporalSearchAttribute`, `TemporalClusterClient`.
   - Each gets a minimal spec (e.g., `ClusterRef LocalObjectReference`) and a status with `Conditions` and `ObservedGeneration`.
   - **Do not** scaffold controllers for these in this milestone; they come in later milestones.

5. **Hub-and-spoke conversion preparation.**
   - Even with only v1alpha1, lay the groundwork:
     - `WRITE: api/v1alpha1/conversion.go` documenting that v1alpha1 is the storage version (with marker `+kubebuilder:storageversion`).
     - When v1beta1 is added later, conversion webhooks plug in here.

6. **Markers and CRD generation.**
   - Use `+kubebuilder:validation:XValidation` (CEL) where appropriate. Examples:
     - On `Spec`: `+kubebuilder:validation:XValidation:rule="self.numHistoryShards == oldSelf.numHistoryShards",message="numHistoryShards is immutable"`. (Validates on update; combine with admission webhook for stronger guarantees.)
     - On `Persistence`: `+kubebuilder:validation:XValidation:rule="has(self.defaultStore.sql) != has(self.defaultStore.cassandra)",message="exactly one of sql or cassandra must be set"`.
   - `RUN: make manifests generate` and commit `config/crd/bases/*.yaml`.

7. **Printer columns and shortNames.**
   - `TemporalCluster`: shortName `tc`, columns `Version, Ready, Shards, Age`.
   - `TemporalNamespace`: shortName `tns`, columns `Cluster, Retention, Ready, Age`.
   - `TemporalSearchAttribute`: shortName `tsa`, columns `Cluster, Type, Registered, Age`.
   - `TemporalClusterClient`: shortName `tcc`, columns `Cluster, Secret, Ready, Age`.

8. **Sample CRs.**
   - `WRITE: config/samples/temporal_v1alpha1_temporalcluster.yaml` — minimal valid CR with Postgres references (the Secrets do not need to exist; this is for CRD validation).
   - One sample per satellite CRD.

9. **Document the CRD reference.**
   - Add `gen-crd-api-reference-docs` or `crd-ref-docs` as a tool.
   - `RUN: make api-docs` to generate `docs/api/v1alpha1.md`.

### Acceptance criteria

- `VERIFY: make manifests generate` produces no diff.
- `VERIFY: kubectl apply --dry-run=server -f config/samples/` succeeds against a cluster with CRDs installed.
- `VERIFY: kubectl explain temporalcluster.spec` shows all fields with descriptions.
- `VERIFY:` CEL validations reject obvious violations (e.g., setting both `sql` and `cassandra`).

### Out of scope

- Reconciler logic (still no-op except status).
- Webhooks (next milestone).
- Any Temporal-version-specific config template.

---

## Milestone 3 — Webhooks (validating, defaulting, conversion stub)

**Goal:** wire up admission webhooks so that invariants are enforced at admission time, not just at reconcile time.

### Tasks

1. **Scaffold webhooks in the new location.**
   - `RUN: kubebuilder create webhook --group temporal --version v1alpha1 --kind TemporalCluster --defaulting --programmatic-validation --conversion`
   - This places files under `internal/webhook/v1alpha1/` (Kubebuilder v4.3+ location).

2. **Implement `CustomDefaulter` for `TemporalCluster`.**
   - Default `spec.image` to `temporalio/server:<Version>` if empty.
   - Default `spec.services.frontend.replicas` to 2; `history.replicas` to 3; `matching.replicas` to 2; `worker.replicas` to 1.
   - Default `spec.metrics.enabled` to true, `spec.metrics.port` to 9090.
   - Default `spec.ui.version` to a known-good UI version per server version (look up in the version matrix).
   - Default `spec.mtls.refreshInterval` to `720h`, `renewBefore` to `240h`.
   - **Stamp annotation `temporal.<org>.io/initial-shards`** on creation with the value of `numHistoryShards`. This is the source of truth for immutability checks.

3. **Implement `CustomValidator` for `TemporalCluster`.**
   - On Create:
     - `spec.version` must be in the supported matrix (`internal/temporal/versions.go`).
     - Exactly one of `defaultStore.sql` / `defaultStore.cassandra` is set.
     - Visibility store must be one of `sql`, `cassandra`, `elasticsearch`; reject Cassandra visibility for Temporal versions where it's removed.
     - If `mtls.provider == "cert-manager"`, validate `issuerRef` is present.
   - On Update:
     - `spec.numHistoryShards` cannot change from the initial annotation value. Reject with reason `ShardCountImmutable`.
     - `spec.version` may only advance to a version with `AllowedFromVersions` including the current version. Reject skips with reason `UpgradePathInvalid`.
     - `spec.persistence.defaultStore.{sql|cassandra}` driver cannot change (no migrating Postgres→Cassandra).
   - On Delete:
     - If `spec.preventDeletion: true`, reject. (Optional safety, modeled on CNPG.)

4. **Implement webhooks for `TemporalNamespace` and `TemporalSearchAttribute`.**
   - Validate `clusterRef` is non-empty.
   - Validate `type` is one of `Keyword, Text, Int, Double, Bool, Datetime, KeywordList` for `TemporalSearchAttribute`.
   - Reject changes to `type` (search attribute types are immutable in Temporal).

5. **Conversion webhook scaffolding.**
   - Even with only v1alpha1, set `+kubebuilder:storageversion` and verify the conversion webhook serves correctly. This catches misconfiguration early.

6. **cert-manager-driven webhook serving certs.**
   - `config/certmanager/` — Issuer + Certificate for the operator's webhook Service.
   - `config/webhook/manifests.yaml` — annotated with `cert-manager.io/inject-ca-from`.
   - Adjust `config/default/kustomization.yaml` to enable the cert-manager patch.

7. **Webhook tests.**
   - `internal/webhook/v1alpha1/temporalcluster_webhook_test.go`:
     - Table-driven Ginkgo tests for all reject paths.
     - Verify defaulting fills in expected fields.
     - Verify CEL rules also fire (belt and braces).

### Acceptance criteria

- `VERIFY: make test` covers webhook code with > 80% coverage.
- `VERIFY:` envtest: creating an invalid CR is rejected with the expected reason.
- `VERIFY:` envtest: updating `numHistoryShards` is rejected.
- `VERIFY:` envtest: updating `version` to a non-adjacent minor is rejected.

### Out of scope

- Conversion logic between versions (only one version exists).
- Webhook integration with Vault / external CA providers.

---

## Milestone 4 — Version compatibility matrix and config template engine

**Goal:** build the two foundational pieces that every later milestone depends on: the static version matrix and the `config_template.yaml` renderer.

### Tasks

1. **Define the version matrix.**
   - `WRITE: internal/temporal/versions.go`:

```go
package temporal

type VersionInfo struct {
    Version              string   // "1.31"
    PatchVersions        []string // ["1.31.0", "1.31.1", "1.31.2"]
    MinSchemaSQL         string   // "v12.1.14"
    MinSchemaCassandra   string   // "v1.10"
    MinSchemaES          string   // "v14"
    AllowedFromVersions  []string // ["1.30"]
    UISeries             string   // "2.40"
    Removed              []string // dynamic config keys removed in this version
    Added                []string // dynamic config keys added in this version
    DefaultDynamicConfig map[string]interface{}
}

var SupportedVersions = []VersionInfo{
    {Version: "1.27", PatchVersions: []string{...}, MinSchemaSQL: "v12.1.10", MinSchemaES: "v9", AllowedFromVersions: []string{"1.26"}, ...},
    {Version: "1.28", ...},
    {Version: "1.29", ...},
    {Version: "1.30", ...},
    {Version: "1.31", MinSchemaSQL: "v12.1.14", MinSchemaES: "v14", AllowedFromVersions: []string{"1.30"}, ...},
}

func LookupVersion(v string) (*VersionInfo, error) { ... }
func ValidateUpgradePath(from, to string) error { ... }
func ResolveLatestPatch(minor string) (string, error) { ... }
```

   - **Source of truth:** Temporal's release notes (`github.com/temporalio/temporal/releases`). Generate this table from a YAML file under `hack/version-matrix.yaml` and `hack/gen-version-matrix.go` so updating it is an edit-one-file operation.

2. **Config template engine.**
   - `WRITE: internal/temporal/configtemplate.go` — renders `config_template.yaml` per service per version.
   - Use Go's `text/template` with `sprig` for utilities.
   - Templates live under `internal/temporal/templates/<version>/config_template.yaml` (one file per supported minor, or a single file with `{{ if gte .Version "1.31" }}` gates — start with one file with gates, split if it gets unwieldy).
   - Inputs: full `TemporalCluster` CR, resolved Temporal version, persistence config, dynamic config path, mTLS paths, etc.
   - Outputs: a `string` per service that is the contents of the ConfigMap mounted into that service.

3. **Golden-file tests for config rendering.**
   - For each supported version and each major scenario (Postgres no-mTLS, Postgres mTLS, Cassandra, ES visibility, archival on, internal frontend), produce a `testdata/golden/<version>/<scenario>.yaml` file.
   - `internal/temporal/configtemplate_test.go` compares rendered output against golden files.
   - `make test-golden-update` regenerates them when intentional.

4. **Dynamic config renderer.**
   - `internal/temporal/dynamicconfig.go` — renders a `dynamicconfig.yaml` from `spec.dynamicConfig.values`.
   - Validate keys against the version matrix: warn (via event) on unknown keys, reject (via webhook) keys removed in the target version.

5. **Connection-string and password handling.**
   - `internal/persistence/secrets.go` — resolves `passwordSecretRef` / `passwordCommandSecretRef` into config snippets.
   - For Temporal 1.31+, the `passwordCommand` field is emitted in the config and the operator mounts the command-yielding script as a Secret with execute permissions.

### Acceptance criteria

- `VERIFY: go test ./internal/temporal/...` passes with golden file comparisons.
- `VERIFY: hack/gen-version-matrix.go` regenerates `versions.go` deterministically.
- `VERIFY:` Rendering a config for Temporal 1.31 with `passwordCommand` produces the correct YAML keys.

### Out of scope

- Actually deploying anything. Pure pure logic + tests.

---

## Milestone 5 — Persistence reconciliation (Postgres-only) and schema management

**Goal:** make the operator able to verify a Postgres database is reachable and run schema setup/migration as Jobs. This is the first milestone where the operator does real work.

### Tasks

1. **Persistence reachability probe.**
   - `internal/persistence/sql.go`:
     - `func Probe(ctx, dsn) error` — opens a connection, runs `SELECT 1`, closes.
     - Used by reconciler to set `PersistenceReachable` condition.
   - Pluggable for Cassandra later (`internal/persistence/cassandra.go` stub).

2. **Schema version query.**
   - `internal/persistence/schema.go`:
     - `func CurrentSQLSchemaVersion(ctx, dsn, storeType) (string, error)` — queries `schema_version` table (default and visibility).
     - Returns empty string if the table doesn't exist (interpreted as "bootstrap needed").

3. **Admintools image reference.**
   - For each Temporal version, the admintools image is `temporalio/admin-tools:<Version>` (verify against Temporal Helm chart). Encode in `versions.go`.

4. **Schema Job builder.**
   - `internal/resources/schemajob.go`:
     - Builds a `batch/v1.Job` that runs:
       - `temporal-sql-tool ... setup-schema -v 0.0` (initial setup)
       - `temporal-sql-tool ... update-schema -d /etc/temporal/schema/postgresql/v12` (upgrade)
     - One Job per (store, action) — `default-setup`, `default-update`, `visibility-setup`, `visibility-update`.
     - Mount the password secret as an env var; if `passwordCommandSecretRef` is set, mount the script and use `--password-command`.
     - Set `ownerReferences` to the `TemporalCluster`.
     - `backoffLimit: 3`, `ttlSecondsAfterFinished: 600`.

5. **Schema reconciler.**
   - `internal/controller/temporalcluster_persistence.go`:
     - Sub-reconciler invoked from the main reconciler.
     - Algorithm:
       1. Resolve DSN from spec + secrets.
       2. Probe → set `PersistenceReachable`.
       3. If unreachable, requeue 30s.
       4. Query current schema versions.
       5. Compare against `versionInfo.MinSchemaSQL`. If lower:
          a. If no schema_version table → run setup Job.
          b. Else → run update Job.
       6. Watch the Job; on success, re-query and set `SchemaReady=True`.
       7. On failure, set `SchemaReady=False, reason=SchemaMigrationFailed` with the Job's failure message.

6. **Owned-resource watches.**
   - Update `SetupWithManager` to watch `Job`s with the cluster's owner-reference and trigger reconcile.

7. **Tests.**
   - Envtest:
     - Fake DSN, mock probe → verifies condition transitions.
     - Job creation → verifies Job spec matches expectations (image, args, env, mounts).
     - Job success → verifies `SchemaReady=True`.
     - Job failure → verifies `SchemaReady=False` with the Job message.
   - Unit: DSN building from spec + secrets.

8. **Chainsaw smoke test.**
   - `WRITE: test/e2e/postgres/01-fixtures-cnpg.yaml` — installs CNPG operator, then provisions a `Cluster` named `temporal-pg`.
   - `WRITE: test/e2e/postgres/02-secrets.yaml` — bootstrap user/password Secret (CNPG generates this; reference it).
   - `WRITE: test/e2e/postgres/03-temporalcluster-schemaonly.yaml` — a CR pointing at the CNPG-provided Postgres; we only assert `SchemaReady=True` (services not yet implemented).
   - `WRITE: test/e2e/postgres/03-assert.yaml` — JMESPath assertion on conditions.

### Acceptance criteria

- `VERIFY:` envtest covers all schema reconciler transitions.
- `VERIFY:` Chainsaw e2e on a kind cluster (using CNPG) brings the CR to `SchemaReady=True`.
- `VERIFY:` `kubectl get tc` shows the cluster with `Ready=False` (services not done) but `SchemaReady=True` in conditions.

### Out of scope

- Cassandra (M9).
- Elasticsearch visibility (M9).
- Actually deploying Temporal services (M6).

---

## Milestone 6 — Service deployment and rollout (single Temporal version)

**Goal:** the operator deploys Frontend, History, Matching, Worker services as Deployments + Services, with PDBs and topology spread, against Temporal 1.31.x only. No upgrades yet.

### Tasks

1. **Resource builders.**
   - `internal/resources/deployment.go`:
     - `BuildFrontendDeployment(cluster, configHash) *appsv1.Deployment`
     - Same for history, matching, worker, internal-frontend.
     - Common helper applies labels, annotations, image, resources, probes, security context, topology spread, affinity, tolerations, node selector, pod template overrides.
   - `internal/resources/service.go`:
     - Headless Service per service (for Ringpop membership: ClusterIP=None).
     - Regular ClusterIP Service for frontend gRPC (port 7233) and HTTP (port 7243).
   - `internal/resources/configmap.go`:
     - One ConfigMap with `config_template.yaml`.
     - Separate ConfigMap with `dynamicconfig.yaml`.
   - `internal/resources/pdb.go`:
     - `policy/v1.PodDisruptionBudget` per service with `maxUnavailable: 1`.

2. **Standard labels.**
   - Every resource gets:
     - `app.kubernetes.io/name: temporal`
     - `app.kubernetes.io/instance: <cluster.Name>`
     - `app.kubernetes.io/component: <frontend|history|matching|worker|internal-frontend>`
     - `app.kubernetes.io/managed-by: temporal-operator`
     - `temporal.<org>.io/cluster: <cluster.Name>`
     - `temporal.<org>.io/version: <resolved.Version>`

3. **Pod probes.**
   - Liveness: gRPC health probe on the service's gRPC port (use `grpc` probe type, available in K8s 1.24+).
   - Readiness: same, with shorter timeout.
   - Startup probe: gRPC, with `failureThreshold: 30` to allow slow membership joins.

4. **Service controller.**
   - `internal/controller/temporalcluster_services.go`:
     - Builds desired state from spec.
     - Server-side apply via `client.Patch(ctx, obj, client.Apply, client.ForceOwnership, fieldOwner)` with fieldOwner `temporal-operator/services`.
     - Watches Deployments owned by the cluster and updates `status.services.<name>` with `ReadyReplicas`, `Version`, `Image`.

5. **Configuration drift handling.**
   - The config ConfigMap's hash is computed and stored in `pod.metadata.annotations["temporal.<org>.io/config-hash"]`. Changes to the ConfigMap force a rollout (standard pattern).
   - Dynamic config does NOT force a rollout — it changes on disk and Temporal picks it up via polling.

6. **Default topology spread and PDBs.**
   - Default `topologySpreadConstraints`:
     ```yaml
     - maxSkew: 1
       topologyKey: topology.kubernetes.io/zone
       whenUnsatisfiable: ScheduleAnyway
       labelSelector: { matchLabels: { ... } }
     ```
   - Apply only if user has not set their own.

7. **Status rollup.**
   - When all services have `ReadyReplicas >= 1`, set `Available=True`.
   - When `SchemaReady && PersistenceReachable && Available`, set `Ready=True`.
   - `status.phase` transitions: `Pending → ProvisioningSchema → DeployingServices → Ready`.

8. **Events.**
   - Emit events for major transitions: `SchemaUpgraded`, `ServiceRolledOut`, `ClusterReady`, `Degraded`.

9. **Tests.**
   - Envtest: full lifecycle — CR create → schema → services → Ready. Assert all expected K8s objects exist with correct fields.
   - Chainsaw: `test/e2e/postgres/lifecycle/` — provision Postgres, deploy operator, apply CR, wait `Ready=True`, exec `tctl --address $frontend:7233 cluster get-system-info` to confirm liveness.

### Acceptance criteria

- `VERIFY:` Envtest lifecycle from CR create to `Ready=True` (with all dependent objects mocked) under 60s.
- `VERIFY:` Chainsaw e2e against CNPG-provisioned Postgres on kind reaches `Ready=True` within 5 minutes.
- `VERIFY:` `kubectl get tc -o wide` shows the cluster ready with version/shards/age columns populated.
- `VERIFY:` `tctl cluster health` succeeds against the deployed frontend.

### Out of scope

- mTLS (M7).
- UI (M8).
- Upgrades across versions (M10).
- Namespaces / search attributes (M11–M12).
- Cassandra / Elasticsearch (M9).

---

## Milestone 7 — mTLS with cert-manager

**Goal:** wire cert-manager certificates for internode and frontend mTLS, mount them, configure Temporal services, and rotate cleanly.

### Tasks

1. **cert-manager integration.**
   - `internal/controller/temporalcluster_mtls.go`:
     - If `spec.mtls.provider == "cert-manager"`:
       1. Create an `Issuer` if user provided self-signed mode, OR use `spec.mtls.internodeCA.issuerRef`.
       2. Create CA `Certificate` (self-signed root) if user wants operator-managed CA.
       3. Create per-service `Certificate` resources for internode mTLS:
          - `<cluster>-frontend-internode`
          - `<cluster>-history-internode`
          - `<cluster>-matching-internode`
          - `<cluster>-worker-internode`
          - `<cluster>-internal-frontend-internode` (if enabled)
       4. Create `<cluster>-frontend-server` Certificate with user-supplied DNS names + the in-cluster Service DNS.
       5. Create `<cluster>-client-ca` if client cert verification is enabled.

2. **Configuration update.**
   - Update the config template to render `tls.internode.server.{certFile,keyFile,clientCAFiles}` and `tls.frontend.server.{...}` pointing at the mounted secrets.
   - Default `serverName` derivation: `<service>.<namespace>.svc.cluster.local`.

3. **Volume mounts.**
   - Each service Deployment mounts:
     - `/etc/temporal/tls/internode/` → its internode cert Secret.
     - `/etc/temporal/tls/frontend/` → the frontend server cert Secret (frontend pods only).
     - `/etc/temporal/tls/client-ca/` → client CA (frontend pods only).

4. **Rotation.**
   - Watch the cert Secrets. On change:
     - Compute a new "cert hash" annotation.
     - Patch the relevant Deployments with the new annotation → triggers rolling restart.
     - Emit event `CertificatesRotated`.

5. **`MTLSReady` condition.**
   - True when all required Certificates report `Ready=True`.
   - False with reason `CertificateIssuanceFailed` if any Certificate is in `Failed` state.

6. **`TemporalClusterClient` controller.**
   - `internal/controller/temporalclusterclient_controller.go`:
     - For each `TemporalClusterClient` CR, create a `Certificate` from the cluster's client CA.
     - Produce a Secret named per `spec.secretName` with `tls.crt`, `tls.key`, `ca.crt` keys — ready for an application worker to consume.
     - Status: `Ready=True` when secret is populated.

7. **Tests.**
   - Envtest with the cert-manager CRDs installed (via `setup-envtest` extra CRDs):
     - Create CR with mTLS enabled → assert all Certificates exist with expected DNS names.
     - Simulate cert secret update → assert Deployment annotation changes.
   - Chainsaw `test/e2e/mtls/`:
     - Real cert-manager installed.
     - Cluster with mTLS comes up `Ready=True`.
     - Application client using `TemporalClusterClient` secret can connect.

### Acceptance criteria

- `VERIFY:` Cluster with mTLS reaches `Ready=True` on Chainsaw.
- `VERIFY:` Rotating an internode cert Secret triggers a rolling restart within 30s.
- `VERIFY:` Connecting `tctl --tls-server-name ... --tls-cert-path ... --tls-key-path ...` succeeds.

### Out of scope

- Vault / SPIFFE / external CA integrations (post-v1).
- Cipher suite / TLS version pinning (passthrough only).

---

## Milestone 8 — temporal-ui, monitoring, archival

**Goal:** ship the user-visible periphery: the UI, ServiceMonitors with a Grafana dashboard, and archival configuration plumbing.

### Tasks

1. **UI Deployment.**
   - `internal/controller/temporalcluster_ui.go`:
     - When `spec.ui.enabled == true`, build:
       - `Deployment` with `temporalio/ui:<resolved version>`.
       - `Service` (ClusterIP).
       - `Ingress` if `spec.ui.ingress.enabled`.
     - Render env vars: `TEMPORAL_ADDRESS`, `TEMPORAL_TLS_*`, `TEMPORAL_AUTH_*`, `TEMPORAL_CODEC_ENDPOINT`.
     - If mTLS is on, mount the frontend client cert (issued via `TemporalClusterClient` automatically for the UI).
     - Liveness/readiness on the UI HTTP port.

2. **Monitoring.**
   - `internal/controller/temporalcluster_monitoring.go`:
     - When `spec.metrics.serviceMonitor.enabled` and the `ServiceMonitor` CRD exists (probe via discovery):
       - Create one `ServiceMonitor` per service, scraping the metrics port.
       - Labels match the user-supplied `spec.metrics.serviceMonitor.labels` for Prometheus Operator selection.

3. **Grafana dashboard.**
   - `WRITE: dist/dashboards/temporal-server.json` — adapted from Temporal's published Grafana dashboards (server metrics, persistence latency, shard ownership distribution).
   - Ship via the Helm chart as a ConfigMap with label `grafana_dashboard: "1"` for kube-prometheus-stack auto-discovery.

4. **Archival.**
   - Render `archival.{history,visibility}` blocks in `config_template.yaml` from `spec.archival`.
   - Validate provider-specific fields (S3 needs `bucket`, GCS needs `bucket`, filesystem needs path).
   - Mount any required AWS/GCS credentials Secrets.

5. **Tests.**
   - Envtest: UI Deployment + Service + Ingress all present and correctly configured.
   - Envtest: ServiceMonitor exists when CRD is present and is absent when CRD is missing.
   - Chainsaw: Install kube-prometheus-stack alongside, verify metrics are scraped.

### Acceptance criteria

- `VERIFY:` Cluster with UI enabled exposes a working UI reachable via Ingress.
- `VERIFY:` Prometheus targets show all Temporal services scraping successfully.
- `VERIFY:` Loading the shipped dashboard in Grafana shows live metrics.

### Out of scope

- Loki / log aggregation integration.
- Custom alerting rules (ship a starter `PrometheusRule` only).

---

## Milestone 9 — Cassandra and Elasticsearch backends

**Goal:** parity for Cassandra (default store) and Elasticsearch (visibility store).

### Tasks

1. **Cassandra persistence.**
   - `internal/persistence/cassandra.go`:
     - Probe via gocql.
     - Schema query against `schema_version` keyspace.
     - Schema Job uses `temporal-cassandra-tool`.
   - Config template renders the `cassandra` plugin block.
   - Validation: TLS, consistency level, keyspace, hosts.

2. **Elasticsearch visibility.**
   - `internal/persistence/elasticsearch.go`:
     - Probe via `GET /` (or `GET /_cluster/health`).
     - Index template management:
       - For Temporal 1.31, the required template version is `v14`.
       - Apply the template via the ES REST API as a one-shot reconcile step.
     - Schema "Job" is actually a small Go-driven step (no `temporal-elasticsearch-tool` historically; if one is available in the admintools image for the target version, use it).
   - Render `persistence.advancedVisibilityStore.elasticsearch` in the config template.

3. **Tests.**
   - Envtest for both: same shape as Postgres.
   - Chainsaw `test/e2e/cassandra/` using K8ssandra Operator.
   - Chainsaw `test/e2e/elasticsearch/` using ECK Operator for ES.

### Acceptance criteria

- `VERIFY:` Cluster with Cassandra default + ES visibility reaches `Ready=True` end-to-end.
- `VERIFY:` Index template v14 is applied to ES and Temporal can write/read visibility records.

### Out of scope

- OpenSearch support (likely works as ES-compatible; mark as untested and best-effort).
- Multi-region Cassandra (single DC only in v0.x).

---

## Milestone 10 — Upgrades across Temporal versions

**Goal:** the operator can take a running cluster from Temporal vN to vN+1 without intervention, with the correct sequencing and safety checks.

### Tasks

1. **Upgrade detection.**
   - In the main reconciler, compare `status.version` with `spec.version`. If different, enter upgrade flow.
   - Webhook already enforced adjacency; controller assumes the path is valid but re-validates defensively.

2. **`internal/controller/temporalcluster_upgrade.go`.**
   - State machine in `status.upgrade.phase`:
     - `Pending`
     - `PreflightChecks` — validate schema floor, validate DB reachable, fire pre-upgrade hook Job if defined.
     - `SchemaMigrating` — run schema update Jobs (default first, then visibility).
     - `RollingFrontend`
     - `RollingHistory` — slowest; uses small `maxSurge`, long readiness wait, asserts membership stability via the frontend health endpoint between batches.
     - `RollingMatching`
     - `RollingInternalFrontend` (if enabled)
     - `RollingWorker`
     - `PostUpgrade` — fire post-upgrade hook Job if defined; verify `tctl cluster health`.
     - `Complete`
   - Each phase has a guard: only advance if previous phase's deployments are fully rolled out.
   - Emit events at each transition.

3. **`status.upgrade.rollbackable` flag.**
   - True initially; flips to false the moment `SchemaMigrating` starts.

4. **Hook Jobs.**
   - From `spec.upgradeHooks.{preSchema, preServices, postUpgrade}`:
     - Each is a `corev1.PodTemplateSpec` wrapped in a Job; the operator just runs it and waits for success.
     - On hook failure, halt the upgrade with `UpgradeBlocked=True, reason=HookFailed`.

5. **Tests.**
   - Envtest: simulate upgrade by changing `spec.version`; assert phase transitions in order, with mocked Job completions.
   - Chainsaw `test/e2e/upgrade/`:
     - Bring up cluster at 1.30.x.
     - Update CR to 1.31.x.
     - Assert phase machine completes within timeout.
     - Verify `tctl cluster get-system-info` reports the new version.

6. **Upgrade matrix in CI.**
   - Nightly workflow exercises every adjacent pair: 1.27→1.28, 1.28→1.29, 1.29→1.30, 1.30→1.31.

### Acceptance criteria

- `VERIFY:` Chainsaw upgrade test (1.30 → 1.31) passes end-to-end.
- `VERIFY:` Attempt to skip a minor is rejected by webhook with `UpgradePathInvalid`.
- `VERIFY:` Failure injection at each phase produces correct `UpgradeBlocked` reasons.

### Out of scope

- Automatic rollback (not safe across schema migrations; document only).
- Cross-cluster replication upgrade coordination.

---

## Milestone 11 — `TemporalNamespace` reconciler

**Goal:** declaratively manage Temporal namespaces against a running cluster.

### Tasks

1. **gRPC client to Temporal.**
   - `internal/temporal/client.go`:
     - `func ClientForCluster(ctx, cluster) (operatorservice.OperatorServiceClient, workflowservice.WorkflowServiceClient, error)`
     - Resolves frontend (or internal frontend if enabled) endpoint, loads client cert from `TemporalClusterClient` Secret, builds a connection.
     - Caches connections per cluster with health checks.

2. **`TemporalNamespace` reconciler.**
   - `internal/controller/temporalnamespace_controller.go`:
     - Wait for `TemporalCluster.status.conditions[Ready]=True`.
     - On create: call `OperatorService.RegisterNamespace`.
     - On update: call `OperatorService.UpdateNamespace` (retention, archival overrides, custom search attribute bindings).
     - On delete: if `spec.allowDeletion=true`, call `OperatorService.DeleteNamespace`; else, just remove the finalizer.
   - Status: `Registered bool`, `NamespaceID string`, `LastUpdated metav1.Time`.

3. **Drift detection.**
   - Periodically (every 5m) call `DescribeNamespace` and compare to spec. If drift detected, reconcile back unless `spec.driftDetection: ignore`.

4. **Tests.**
   - Envtest: with a mock Temporal gRPC server.
   - Chainsaw: full cluster + namespace flow; assert namespace appears in `tctl namespace list`.

### Acceptance criteria

- `VERIFY:` Creating a `TemporalNamespace` CR results in a registered namespace within 30s of cluster Ready.
- `VERIFY:` Updating `spec.retention` propagates within 1 reconcile cycle.
- `VERIFY:` Deleting the CR with `allowDeletion: true` deletes the namespace.

### Out of scope

- Custom replication settings (multi-cluster).
- Per-namespace archival URI overrides beyond passthrough.

---

## Milestone 12 — `TemporalSearchAttribute` reconciler

**Goal:** declaratively register custom search attributes.

### Tasks

1. **Reconciler.**
   - `internal/controller/temporalsearchattribute_controller.go`:
     - Resolve cluster + connect via the cached gRPC client.
     - Call `OperatorService.AddSearchAttributes`.
     - Poll `ListSearchAttributes` until visible (the registration triggers a system workflow; can take seconds).
     - Status: `Registered bool, RegisteredAt metav1.Time, Type string`.
     - Finalizer: on delete, call `RemoveSearchAttributes` if `spec.allowDeletion: true`.

2. **Type immutability** — already enforced by webhook.

3. **Tests.**
   - Envtest with mock gRPC server.
   - Chainsaw: create attribute, verify visible via `tctl search-attribute list`.

### Acceptance criteria

- `VERIFY:` Search attribute appears in Temporal within 60s of CR creation.

### Out of scope

- Search attribute renames (Temporal doesn't support; document).

---

## Milestone 13 — Helm chart, OLM bundle, kustomize distribution

**Goal:** publishable, three-format distribution. Up to here, install has been via `make install && make deploy`. This milestone makes it real.

### Tasks

1. **Helm chart via Kubebuilder's `helm/v2-alpha` plugin.**
   - `RUN: kubebuilder edit --plugins=helm/v2-alpha`
   - Customize generated chart under `dist/chart/`:
     - Templated CRDs under `templates/crds/` gated on `crds.install` value.
     - Operator Deployment, RBAC, ServiceAccount, Service.
     - ServiceMonitor (gated on `monitoring.enabled`).
     - Webhook config (gated on `webhooks.enabled`).
     - cert-manager Issuer/Certificate (gated on `webhooks.certManager.enabled`).
   - `WRITE: dist/chart/values.yaml` — heavily commented; sane defaults.
   - `WRITE: dist/chart/templates/NOTES.txt` — quickstart.
   - Add `helm-docs` to tools; auto-generate `dist/chart/README.md`.

2. **OLM bundle.**
   - `RUN: operator-sdk generate kustomize manifests -q`
   - `RUN: operator-sdk generate bundle --version 0.1.0 --channels stable,alpha --default-channel stable`
   - Hand-edit the resulting `ClusterServiceVersion`: descriptions, screenshots, capability level (start at "Basic Install", target "Full Lifecycle" by v1.0), icon, links, maintainers.
   - `WRITE: bundle/tests/scorecard/config.yaml`.

3. **Raw kustomize.**
   - `make build-installer` produces `dist/install.yaml` — single-file manifest for `kubectl apply -f`.

4. **Artifact Hub metadata.**
   - `WRITE: dist/chart/artifacthub-pkg.yaml`.
   - `WRITE: bundle/artifacthub-repo.yml`.

5. **Tests.**
   - CI job that:
     - Installs the Helm chart on a kind cluster, then runs a Chainsaw smoke test.
     - Installs the OLM bundle via `operator-sdk run bundle` and runs the same.
     - Installs via raw kustomize and runs the same.

### Acceptance criteria

- `VERIFY:` All three install paths produce a working operator that passes the lifecycle Chainsaw test.
- `VERIFY:` `helm lint dist/chart/` clean.
- `VERIFY:` `operator-sdk bundle validate ./bundle --select-optional name=operatorhub` clean.

### Out of scope

- Operator catalog publication (manual PR in M16).

---

## Milestone 14 — namespace.so e2e in CI

**Goal:** move e2e tests off kind in CI and onto ephemeral namespace.so clusters for speed and matrix scale.

### Tasks

1. **Namespace setup.**
   - In `.github/workflows/e2e.yml`:
     - `permissions: { id-token: write, contents: read }`.
     - Steps: `nscloud-setup@v0`, `nscloud-cluster-action@v0`, `nscloud-setup-buildx-action@v0`.
     - Build operator image via Buildx and push to the per-run ephemeral registry that namespace.so exposes (or push to GHCR with a SHA tag).
     - `kubectl apply` for cert-manager, CNPG, K8ssandra (matrix-gated).
     - Install operator via Helm chart from local build.
     - Run Chainsaw suite.

2. **Per-PR vs nightly matrix.**
   - Per PR: `{ temporal: [1.31], persistence: [postgres], k8s: [1.33] }` — single combo.
   - Nightly: full matrix `{ temporal: [1.27..1.31], persistence: [postgres, cassandra], k8s: [1.31, 1.32, 1.33] }` with `fail-fast: false`.

3. **kind fallback workflow.**
   - Keep `.github/workflows/e2e-kind.yml` for contributors without namespace.so credentials and for verifying parity.

4. **Test result reporting.**
   - Use `actions/upload-artifact` for Chainsaw logs.
   - Use a status-check aggregator job so the matrix shows a single required check.

### Acceptance criteria

- `VERIFY:` PR e2e runs in < 10 min wall time.
- `VERIFY:` Nightly matrix completes within 90 min.

### Out of scope

- Performance / load benchmarking — separate effort post-v1.

---

## Milestone 15 — Release engineering

**Goal:** repeatable, signed, supply-chain-hardened releases.

### Tasks

1. **goreleaser.**
   - `WRITE: .goreleaser.yml`:
     - Multi-arch builds (linux/amd64, linux/arm64).
     - Docker manifests pushed to `ghcr.io/<org>/temporal-operator`.
     - Source archive.
     - SBOM via `syft`.
     - Cosign keyless signing of images and SBOMs.

2. **SLSA provenance.**
   - Use the SLSA Level 3 generator: `slsa-framework/slsa-github-generator` reusable workflow.

3. **Release-Please.**
   - `WRITE: .github/workflows/release-please.yml` — Google's release-please action with `release-type: go`.
   - Maintains `CHANGELOG.md` and version bumps from Conventional Commits.

4. **Publish flow.**
   - `release.yml` triggers on tag `v*`:
     1. goreleaser → image + signatures + SBOM + GitHub Release.
     2. Helm chart push to `oci://ghcr.io/<org>/charts/temporal-operator`.
     3. OLM bundle build + push to `quay.io/<org>/temporal-operator-bundle`.
     4. Docs site build + deploy.

5. **Verification documentation.**
   - `WRITE: docs/content/install/verifying-releases.md` — `cosign verify` instructions.

### Acceptance criteria

- `VERIFY:` Tagging `v0.1.0` produces signed images, SBOMs, attested provenance, a Helm chart, an OLM bundle, and a GitHub Release — all without manual intervention.
- `VERIFY:` `cosign verify ghcr.io/<org>/temporal-operator:v0.1.0 --certificate-identity-regexp='.*' --certificate-oidc-issuer=https://token.actions.githubusercontent.com` succeeds.

### Out of scope

- Homebrew tap for a future kubectl plugin (M17).

---

## Milestone 16 — Documentation site and examples

**Goal:** a docs site that lets a stranger go from zero to a running cluster in 15 minutes.

### Tasks

1. **Hugo + Docsy.**
   - `RUN: hugo new site docs --format yaml && cd docs && git submodule add https://github.com/google/docsy themes/docsy`
   - Configure for versioned docs (one branch per minor).

2. **Required pages.**
   - `index.md` — landing.
   - `getting-started/` — install via Helm, deploy a sample cluster with CNPG, run a workflow.
   - `architecture/` — operator architecture, CRD model.
   - `installation/` — Helm, OLM, kustomize.
   - `reference/crds/` — auto-generated from `crd-ref-docs`.
   - `operations/` — interpreting status, common failures, upgrade runbook.
   - `examples/` — curated walkthroughs.
   - `upgrades/` — per-Temporal-version upgrade guide.
   - `troubleshooting/`.
   - `contributing/` — link to repo CONTRIBUTING.md.

3. **Sample CRs.**
   - `examples/cluster-postgres-minimal/`
   - `examples/cluster-postgres-full/`
   - `examples/cluster-cassandra/`
   - `examples/cluster-mtls-cert-manager/`
   - `examples/cluster-elasticsearch-visibility/`
   - `examples/cluster-with-namespaces-and-search-attributes/`
   - `examples/cluster-cnpg-integrated/` — showcase.
   - `examples/cluster-upgrade/` — diff-driven walkthrough.

4. **Cloudflare Pages deployment.**
   - `WRITE: .github/workflows/docs.yml` — preview deploys on PR, production on push to main.

5. **OperatorHub.io submission.**
   - PR to `k8s-operatorhub/community-operators` with the bundle directory.
   - Tracker: `docs/internal/operatorhub-submission.md`.

### Acceptance criteria

- `VERIFY:` Docs site builds clean and deploys.
- `VERIFY:` Following the Getting Started guide on a fresh kind cluster, end-to-end, takes < 15 minutes and ends with a successful workflow run.

### Out of scope

- Video walkthroughs.
- Translated docs.

---

## Milestone 17 — Stretch goals (post-v0.5, en route to v1)

These are explicitly out of scope for v0.x and tracked separately. Listed here so the agent knows where the project is heading and can leave seams in the code.

1. **`TemporalNexusEndpoint` CRD** (Temporal 1.25+; full Nexus support).
2. **`TemporalWorkerDeployment` CRD** (Temporal 1.27+, GA in 1.31; Worker Versioning).
3. **`TemporalSchedule` CRD** (declarative Schedules).
4. **kubectl plugin** (`kubectl temporal status`, `kubectl temporal exec tctl`, `kubectl temporal logs frontend`).
5. **Multi-cluster replication** via a `TemporalClusterReplication` CR that references two `TemporalCluster` objects (possibly across kubeconfigs).
6. **`v1beta1` API graduation** with conversion webhooks.
7. **Hibernation** (`spec.hibernate: true` scales all services to 0, preserves config).
8. **CNPG-I-style plugin architecture** for persistence, mTLS, archival providers.
9. **Backup/restore reference workflows** as recipes (not operator-managed).
10. **OpenShift conformance and certification.**

---

## Cross-cutting concerns

These apply to every milestone; the agent should not treat them as separate tasks but as ambient quality requirements.

### Observability of the operator itself

- Structured logs with `controller-runtime`'s logr (configurable to slog).
- Operator's own Prometheus metrics (controller-runtime exposes reconcile latency, error rates by controller).
- Operator's own `ServiceMonitor` shipped in the chart.

### Security

- Operator runs as non-root, `readOnlyRootFilesystem: true`, drop ALL capabilities.
- Minimum RBAC — generate from `+kubebuilder:rbac` markers, never hand-edit ClusterRoles upward.
- NetworkPolicy in `config/network-policy/` allowing only what the operator needs (apiserver, webhook ingress, the Temporal frontends it manages).
- No secrets in logs (enforce via lint rule).
- `govulncheck` on every CI run.

### Performance

- Cache selectors in the manager limit watches to objects with `app.kubernetes.io/managed-by: temporal-operator` where possible.
- Predicates skip status-only updates.
- Reconciles are short; long-running work is in Jobs.

### Maintainability

- Every controller file has a corresponding test file in the same package.
- Every new public type has a Go doc comment.
- ADRs (Architecture Decision Records) under `docs/adr/` for non-trivial design choices.
- A `MAINTAINERS.md` and rotating triage assignments.

### Backward compatibility commitments

- `v1alpha1`: may break across patch releases of the operator (document loudly).
- `v1beta1` (when introduced): may break only across minor operator releases.
- `v1` (when introduced): may break only across major operator releases, with a one-version deprecation window.

---

## Open architectural decisions the agent should surface, not silently choose

When the agent reaches a decision point listed here, it should pause and ask the human:

1. **Deployment vs StatefulSet for History.** Default `Deployment`; expose `spec.services.history.deploymentMode` for advanced users.
2. **One reconciler vs split reconcilers.** This plan calls for split sub-reconcilers coordinated via status conditions. Confirm before deviating.
3. **UI as part of `TemporalCluster` vs separate `TemporalUI` CRD.** This plan bundles. Confirm before splitting.
4. **Helm vs OLM as the documented "blessed" install path.** This plan defaults to Helm. Confirm.
5. **Conventional Commits + Release-Please vs goreleaser-changelog.** This plan uses Release-Please.
6. **Cloudflare Pages vs GitHub Pages for docs.** This plan picks Cloudflare for preview deploys.
7. **Whether to ship a kubectl plugin in v0.x.** This plan defers to v1.

---

## Suggested cadence

Assuming one engineer or a focused coding agent:

- M0–M2: 2 weeks (bootstrap, scaffold, CRDs).
- M3–M4: 2 weeks (webhooks, matrix, templates).
- M5–M6: 3 weeks (persistence + services — the bulk of the operator).
- M7: 2 weeks (mTLS).
- M8: 1 week (UI, monitoring).
- M9: 2 weeks (Cassandra + ES).
- M10: 3 weeks (upgrades — the trickiest milestone).
- M11–M12: 1 week each (namespaces, search attributes).
- M13: 1 week (distribution).
- M14: 1 week (namespace.so CI).
- M15: 1 week (release).
- M16: 2 weeks (docs).

**Total: ~22 weeks to a polished v0.5.** A coding agent working in parallel on independent milestones can compress this materially (M7, M8, M13, M16 are independent of M10 and can run in parallel).

---

## What to do first

The very first action: **Milestone 0, task 1**. Initialize the repo, write the governance and bootstrap files, set up branch protection, and open the first PR with the scaffold. Do not skip ahead. Every later milestone depends on the discipline established here.

Subsequent runs of the coding agent should:

1. Read this plan.
2. Read the most recent `CHANGELOG.md` and the current state of `PROJECT`, `go.mod`, and the `internal/controller/` directory.
3. Identify the next unchecked milestone.
4. Execute its tasks in order.
5. Open a single PR per milestone (or per logical sub-section of a large milestone like M6 or M10).
6. Update this document's milestone status when complete.

The plan is the source of truth. Updates to it are themselves PRs.

