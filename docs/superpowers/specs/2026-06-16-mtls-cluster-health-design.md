# mTLS cluster health + operator client TLS — design

Date: 2026-06-16
Status: Approved

## Problem

Enabling mTLS on a `TemporalCluster` (`spec.mtls` with a cert-manager
`issuerRef`) currently produces a cluster that never becomes healthy, and
silently breaks the search-attribute / namespace controllers. Three concrete
defects, all in the operator's mTLS rendering:

1. **Internode/frontend `client.serverName` is empty.** `buildMTLS()`
   (`internal/temporal/configtemplate.go`) never populates `InternodeServerName`
   or `FrontendServerName`, so `config_template.yaml` renders
   `client.serverName: ""`. With an empty serverName, Temporal verifies the peer
   certificate against the **dialed pod IP**, but the internode certificate only
   carries DNS SANs (`internal/resources/certificates.go`). Result:
   `x509: cannot validate certificate for <pod-ip> because it doesn't contain
   any IP SANs`.

2. **No `tls.systemWorker` block.** The template renders only `internode` and
   `frontend` TLS. The `worker` role runs an internal SDK client that connects to
   the frontend via `publicClient.hostPort`; with no system-worker TLS it dials
   in plaintext against a now-TLS frontend. Result:
   `tls: first record does not look like a TLS handshake`.

3. **Operator controllers connect insecurely.** The search-attribute and
   namespace controllers call their client factory with `nil` TLS
   (`temporalsearchattribute_controller.go`, `temporalnamespace_controller.go`).
   Once the frontend requires client auth, these reconcilers can no longer
   register namespaces or search attributes against an mTLS cluster.

`MTLSConfig.RequireClientAuth` is hardcoded `true`
(`configtemplate.go`), and none of these are exposed via the CR, so there is no
kubectl-level workaround. All three require code changes.

## Goals

- An mTLS-enabled `TemporalCluster` reaches `Ready`/`Available` (internode and
  system worker connect successfully).
- The search-attribute and namespace controllers operate against an mTLS
  cluster.
- No change to the non-mTLS code path.

## Non-goals

- Making `requireClientAuth` configurable.
- Separate cryptographic identities per component (system worker, operator).
  Reusing the internode cert is acceptable for now (see Decisions).
- Changing the `TemporalClusterClient` (worker credentials) feature — it already
  works.

## Decisions

- **serverName strategy.** Pod IPs are dynamic, so IP SANs are not viable. Set a
  stable `client.serverName` matching a DNS SAN:
  - Internode: serverName `<cluster>-internode`; add that name as a SAN on the
    internode certificate (it already is the CommonName).
  - Frontend: serverName `<frontend-service>.<namespace>.svc.cluster.local`
    (already a SAN on the frontend cert). Internal clients and the system worker
    (via `publicClient`) use this name.
- **Client-cert source (system worker + controllers).** Reuse the existing
  internode certificate. It has `clientAuth` usage and is signed by the cluster
  CA (the frontend's `clientCaFiles`), so the frontend accepts it. It is already
  mounted on every pod, including the worker, so the system worker needs config
  rendering only — no new Certificate, mount, or deployment change.

## Design

### 1. Internode certificate SAN
`internal/resources/certificates.go` → `BuildInternodeCertificate`: prepend
`<cluster>-internode` to `DNSNames` so the chosen serverName resolves to a SAN.

### 2. MTLSConfig + rendering
`internal/temporal/configtemplate.go`:
- Add fields: `InternodeServerName`, `FrontendServerName` (already declared),
  plus `SystemWorkerCert`, `SystemWorkerKey`, `SystemWorkerServerName`,
  `SystemWorkerClientCA`.
- `buildMTLS()` populates:
  - `InternodeServerName = <cluster>-internode`
  - `FrontendServerName  = <frontend-svc>.<ns>.svc.cluster.local`
  - `SystemWorkerCert/Key = internodeCertDir + /tls.crt|/tls.key`
  - `SystemWorkerClientCA = internodeCertDir + /ca.crt`
  - `SystemWorkerServerName = FrontendServerName`

`internal/temporal/templates/config_template.yaml`: under `global.tls`, when
mTLS is enabled, render a `systemWorker:` block:
```yaml
systemWorker:
    certFile: {{ .MTLS.SystemWorkerCert }}
    keyFile: {{ .MTLS.SystemWorkerKey }}
    client:
        serverName: {{ .MTLS.SystemWorkerServerName | quote }}
        rootCaFiles:
            - {{ .MTLS.SystemWorkerClientCA }}
```
The existing `internode.client.serverName` and `frontend.client.serverName`
fields now render non-empty.

### 3. Controller TLS
New helper (shared by both controllers, e.g. in `internal/controller`):
`clusterTLSConfig(ctx, c client.Client, cluster) (*tls.Config, error)`:
- Returns `nil, nil` when `cluster.Spec.MTLS == nil`.
- Otherwise reads Secret `<cluster>-internode` in the cluster namespace; builds a
  `*tls.Config` with `RootCAs` from `ca.crt`, `Certificates` from
  `tls.crt`/`tls.key`, and `ServerName` =
  `<frontend-svc>.<ns>.svc.cluster.local`.

`temporalsearchattribute_controller.go` and `temporalnamespace_controller.go`:
build the config and pass it to the client factory instead of `nil`. Add RBAC
marker `+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch` to
both; regenerate manifests.

## Testing

- **Unit — config:** `configtemplate_test.go` golden assertions that an mTLS
  cluster renders non-empty `internode`/`frontend` `client.serverName` and a
  `systemWorker` block with the internode cert paths.
- **Unit — cert:** assert `<cluster>-internode` is present in the internode
  cert's `DNSNames`.
- **Unit — controllers:** `clusterTLSConfig` returns `nil` without mTLS, and a
  `*tls.Config` with the expected `ServerName` and a populated client cert when
  the internode Secret exists.
- **E2E:** extend `test/e2e/mtls` to assert the cluster reaches Ready/Available
  (worker + internode actually connect) and that a `TemporalSearchAttribute`
  registers against the mTLS cluster.

### CI: on-demand e2e suite selection
Today `.github/workflows/e2e.yml` only runs the mtls suite on the nightly
`schedule` event; `pull_request` and `workflow_dispatch` run only
`postgres/lifecycle`. Add a `workflow_dispatch` input so the mtls suite (and the
others) can be triggered on demand to validate this fix:
```yaml
on:
  workflow_dispatch:
    inputs:
      suite:
        description: Which suite(s) to run
        type: choice
        options: [default, mtls, upgrade, all]
        default: default
```
Update the `Compute matrix` step so:
- `schedule` → full matrix (unchanged).
- `workflow_dispatch` → the combo(s) selected by `inputs.suite` (`default` =
  postgres/lifecycle; `all` = full matrix).
- `pull_request` → postgres/lifecycle (unchanged).

Trigger with `gh workflow run E2E -f suite=mtls` (or `-f suite=all`).

## Risks

- Reusing the internode cert as the operator/system-worker client identity means
  the operator presents an "internode" identity to the frontend. Acceptable
  while authorization is not enabled; revisit if per-identity authz is added.
- serverName must stay in sync with the cert SANs; covered by unit tests.

## Addendum: gRPC health probes under mTLS (found during verification)

End-to-end verification surfaced a fourth defect, independent of the TLS-config
rendering above. With mTLS enabled the request-serving services (frontend,
history, matching) stayed `0/1` and the cluster never reached `Ready`, because
Kubernetes' native gRPC prober dials the health endpoint without a client
certificate and cannot complete the mutual-TLS handshake on the
`requireClientAuth` port. The servers were healthy; only the probes failed. (The
worker came up because it has no probe.)

**Fix:** when mTLS is enabled, the request-serving services use a TCP probe
instead of a gRPC probe. Non-mTLS clusters keep the gRPC probe.

**Known limitation / future work:** a TCP probe only confirms the port accepts
connections, not gRPC-level health — a weaker signal that is acceptable for now
but not ideal for production. A future changeset should adopt richer health
checking under mTLS, e.g. a `grpc-health-probe` exec probe with the internode
client cert, or first-class support for a service mesh (Linkerd, or
Istio/Envoy) that terminates mTLS in a sidecar so Temporal serves plaintext and
standard gRPC probes apply. User-facing docs:
`docs/content/operations/_index.md` ("mTLS health probes").
