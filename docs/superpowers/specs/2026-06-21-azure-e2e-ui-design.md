# Enable and verify the Web UI on the Azure standing cluster

## Problem

The standing `TemporalCluster` brought up by the Azure make commands
(`make azure-e2e-deploy` / `make azure-e2e-up-deploy`, via
`test/e2e/azure/03-temporalcluster.yaml`) does not enable the Temporal Web UI.
We want the UI running on that cluster, the Azure Chainsaw suite to confirm it,
and a documented way to reach it.

## Background

- The operator already supports the UI via `spec.ui.enabled` (`api/v1alpha1`
  `UISpec`). When enabled, it creates a `<cluster>-ui` Deployment (image
  `temporalio/ui`, container port 8080, HTTP readiness probe on `/`) and a
  ClusterIP Service on port 8080 (`internal/resources/ui.go`,
  `internal/controller/temporalcluster_ui.go` → `plan.PlanUI`).
- The UI connects to the Temporal **frontend over gRPC** (`TEMPORAL_ADDRESS`),
  not to Postgres. **Enabling it adds zero database connections**, so the B1ms
  connection budget documented in `test/e2e/azure/README.md` is unaffected.
- The Azure cluster does not use mTLS, so the UI connects to the frontend
  without client certificates.
- An existing pattern verifies the UI in `test/e2e/ui/01-assert.yaml`: assert the
  UI Deployment `readyReplicas > 0` and the Service serves port 8080.

## Design

### 1. Enable the UI on the standing Azure cluster

Add to `test/e2e/azure/03-temporalcluster.yaml`:

```yaml
spec:
  ui:
    enabled: true
```

This yields an `azure-e2e-ui` Deployment (replicas default 1) + Service.

### 2. E2E verification (option A — declarative readiness)

Append to `test/e2e/azure/03-assert.yaml` two Chainsaw assertions mirroring
`test/e2e/ui/01-assert.yaml`:

- `Deployment/azure-e2e-ui` with `(readyReplicas > `0`): true`
- `Service/azure-e2e-ui` with `(ports[?name == 'http'].port | [0]): 8080`

Because the UI Deployment has an HTTP readiness probe on `/`,
`readyReplicas > 0` proves the UI HTTP server is serving. The cluster already
asserts `phase: Ready` (frontend up), so the UI has a frontend to talk to.

The assert comment is updated to note the UI as an additional verified actor.

### 3. Docs: accessing the UI

Add an "Accessing the Web UI" subsection to
`docs/content/installation/azure.md` (which already has an Azure UI section)
with a port-forward snippet for clusters without an ingress:

```sh
kubectl -n azure-e2e port-forward svc/azure-e2e-ui 8080:8080
# open http://localhost:8080
```

The access snippet lives in the docs site, not in
`test/e2e/azure/README.md`.

## Out of scope

- No ingress for the UI (the Azure e2e installs no ingress controller).
- No HTTP smoke test against the frontend (option B) — declarative readiness is
  sufficient and matches the existing UI suite.
- No operator code changes — the UI feature already exists.

## Testing

- `make azure-e2e-deploy` (or `up-deploy`) stands up the UI; manual
  `kubectl get deploy -n azure-e2e azure-e2e-ui` shows `READY 1/1`.
- `make azure-e2e-test` runs Chainsaw; the new assertions pass.
- Validate live on the standing cluster (az/kubectl access) before opening the
  PR.

## Branch / PR

- Branch `test/azure-e2e-ui` off `main` (PR #88 already merged, so `main`
  contains the Azure e2e fixes the UI depends on).
- Open a PR for the change.
