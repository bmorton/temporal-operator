# Operator UI — Design

**Status:** Approved (brainstorming)
**Date:** 2026-06-17
**Author:** Brian Morton (with Copilot)

## Summary

Add a small, focused, read-only web UI embedded in the temporal-operator binary.
It gives operators a control-plane overview of the resources the operator
manages: every `TemporalCluster` across all namespaces and their reconciliation
state, plus the satellite CRDs (`TemporalClusterClient`, `TemporalNamespace`,
`TemporalSearchAttribute`) related to each cluster.

The UI is strictly read-only in v1. It does not duplicate `temporal-ui` — there
is no workflow/namespace browsing inside Temporal and no execution history. It
renders only what the operator already knows from its CRDs and their status.

## Goals

- A useful at-a-glance overview of operator-managed clusters and their health.
- Drill-down detail per cluster: services, persistence/schema, mTLS, endpoints,
  conditions, in-flight upgrades, and related satellite resources.
- Zero extra Kubernetes API-server load (read from the manager's informer cache).
- No new RBAC beyond what the controllers already hold.
- Opt-in: disabled by default; existing deployments are unchanged.
- Self-contained operator image: no Node, no CDN, no runtime front-end tooling.

## Non-goals (v1)

- Live data pulled from the Temporal clusters themselves via the Temporal gRPC
  API (namespace lists, live cluster health). This is a planned **future phase
  ("B")** and the design leaves room for it.
- Any mutating actions (edit/delete/force-reconcile). Read-only only.
- Per-user authorization / RBAC inside the UI.
- SSE/WebSocket live push (planned future upgrade — see Data layer seam).
- TemplUI / Tailwind. v1 uses plain templ + hand-written CSS.

## Decisions (locked during brainstorming)

1. **Scope:** operator-managed CRDs + reconciliation states only (option "A").
   Live Temporal gRPC data ("B") is deferred.
2. **Runtime:** served by the operator binary itself, sharing the
   controller-runtime manager's cache (option "A").
3. **Auth:** forward-auth / proxy-trust. No login in the operator; front it with
   an Ingress + Authelia (or oauth2-proxy/Pomerium). The UI optionally consumes
   trusted identity headers.
4. **v1 pages:** Overview + Cluster detail. "Topologies" means the
   service/resource makeup of a cluster, not a node-graph diagram.
5. **Read-only:** strictly, in v1.
6. **Front-end stack:** plain templ + htmx + Alpine.js, minimal hand-written
   CSS. No Tailwind/TemplUI in v1, but the package layout leaves the door open
   to adopt them later.
7. **Freshness:** htmx polling now, structured so SSE watch-push can replace it
   later without touching handlers or templates (Approach 3 — hybrid).

## Architecture

### Runtime wiring

- A new `internal/ui` package exposes `ui.Server`, implementing
  `sigs.k8s.io/controller-runtime/pkg/manager.Runnable`.
- In `cmd/main.go`, behind a `--ui-bind-address` flag (default `:8082`; empty
  string disables the UI), the server is registered with `mgr.Add(server)`. It
  starts and stops with the manager and uses the manager's signal handling /
  context for graceful shutdown.
- The server runs a standard `net/http` `http.Server` with a `http.ServeMux`
  router. Plain HTTP on the pod — TLS terminates at the ingress.

### Data access

- The server holds a `client.Reader` sourced from `mgr.GetClient()` (the
  informer-backed cached client). All reads (`List`/`Get` of `TemporalCluster`
  and the satellite CRDs) are served from the in-memory cache.
- No extra API-server load and no new RBAC: the controllers already watch these
  types cluster-wide, so the UI sees exactly what they do.
- Caveat: the UI can only show namespaces/types the manager already caches.
  Because the controllers watch these CRDs cluster-wide, this is the full set.

### Separation of concerns (the seam)

- The HTTP/templ layer never touches `client.Reader` directly.
- A `ui.DataSource` interface sits between handlers and Kubernetes, returning
  view-model structs:
  - `ListClusters(ctx) ([]ClusterSummary, error)`
  - `GetCluster(ctx, namespace, name) (*ClusterDetail, error)`
- v1 implements `DataSource` with the cached client + polling. This interface is
  the seam that lets an SSE/watch-push implementation slot in later (Approach 3)
  without changing handlers or templates.

## Package layout

```
internal/ui/
  server.go          # ui.Server: Runnable, http.Server, router, wiring
  datasource.go      # DataSource interface + cached-client implementation
  viewmodel.go       # plain structs templates render (ClusterSummary,
                     #   ClusterDetail, ServiceRow, ConditionRow, badge enums)
  config.go          # Options (bind addr, refresh interval, base path,
                     #   trusted auth headers, require-auth)
  identity.go        # reads forward-auth headers; exposes current user
  handlers/          # http.Handler funcs: overview, detail, htmx fragments
  layouts/           # base.templ (html shell, nav, embedded assets)
  pages/             # overview.templ, cluster_detail.templ
  components/        # status_badge.templ, cluster_card.templ,
                     #   service_table.templ, condition_list.templ,
                     #   persistence_panel.templ, endpoints_panel.templ,
                     #   related_resources.templ
  static/            # vendored htmx.min.js, alpine.min.js, app.css
  static.go          # //go:embed static/* + fingerprinted asset serving
```

**Principles**

- View-models (`viewmodel.go`) are the contract between data and rendering.
  Handlers map k8s types → view-models; templates only ever see view-models.
- Components are small and single-purpose (one badge, one service table).
- JS/CSS are vendored and embedded via `go:embed` — no CDN, works air-gapped.
- This mirrors the layout of `bmorton/mx`'s `internal/ui` package
  (`components/`, `handlers/`, `layouts/`, `pages/`, `utils/`), minus the
  broadcaster (deferred with SSE).

## Routes

All routes are `GET` and read-only. `{base}` defaults to `/` and is configurable
via `--ui-base-path`.

| Route | Purpose |
| --- | --- |
| `GET /` | Overview: full page (shell + cluster grid). |
| `GET /partials/clusters` | Cluster-grid fragment; overview polls this via `hx-get` every refresh interval and swaps in place. |
| `GET /clusters/{namespace}/{name}` | Cluster detail: full page. |
| `GET /partials/clusters/{namespace}/{name}` | Detail body fragment for htmx polling. |
| `GET /static/...` | Embedded, fingerprinted assets (long cache headers). |
| `GET /healthz` | UI liveness (separate from the manager's probe endpoint). |

## Data flow & rendering

**Request flow:** handler → `DataSource` (cached `List`/`Get`) → map k8s types
into view-models → render templ component/page → write HTML.

- Full-page requests render shell + content. `/partials/*` requests render only
  the inner fragment, so htmx swaps are cheap and flicker-free.

**View-model mapping** is where the domain logic lives:

- Phase + `Ready` condition → badge state (`ok` / `warn` / `error` / `pending`).
- Per-service `Ready/Desired` (frontend, history, matching, worker,
  internal-frontend) → progress + color.
- Persistence: `PersistenceReachable` + `SchemaReady` conditions/status →
  a status line.
- `Status.Upgrade != nil` → an upgrade banner with from→to version and the
  `Rollbackable` flag.
- Related satellite resources matched to a cluster by namespace + their cluster
  reference fields.

**Rendering choices**

- Server-side templ for everything.
- htmx for polling and partial swaps.
- Alpine.js only for pure-client state (collapsible panels, active tab). No
  business logic in JS.
- Empty and error states are first-class: "no clusters yet" renders a friendly
  empty state; a cache read error renders an inline message — never a blank page.

## Authentication (forward-auth)

- No login in the operator. `identity.go` reads configurable trusted headers
  (defaults `Remote-User`, `Remote-Groups`, `Remote-Email`) and exposes the user
  to the layout ("Signed in as …").
- `--ui-require-auth` makes the server respond `401` when the user header is
  absent, so a misconfigured proxy fails closed.
- The UI binds its own port; front it with an Ingress + Authelia forward-auth (or
  oauth2-proxy / Pomerium). We ship example manifests and docs but do not manage
  the proxy. TLS terminates at the ingress; the pod serves plain HTTP.

## Configuration / flags (`cmd/main.go`)

| Flag | Default | Purpose |
| --- | --- | --- |
| `--ui-bind-address` | `:8082` | UI listen address; empty string disables the UI (opt-in). |
| `--ui-refresh-interval` | `5s` | htmx poll interval. |
| `--ui-base-path` | `/` | URL base path for serving under a sub-path. |
| `--ui-require-auth` | `false` | Return 401 when no trusted user header is present. |
| `--ui-trusted-user-header` | `Remote-User` | Identity header for the username. |
| `--ui-trusted-groups-header` | `Remote-Groups` | Identity header for groups. |
| `--ui-trusted-email-header` | `Remote-Email` | Identity header for email. |

The UI is disabled by default, so existing deployments are unaffected.

## Build

- Add `templ` as a pinned tool in the Makefile (`make install-tools`) and a
  `make ui-generate` target (`templ generate`).
- Commit generated `*_templ.go` so `make build` and CI need no new runtime
  tooling. No Node, no Tailwind.
- Wire `ui-generate` into the `make generate` dependency chain (or document it
  clearly) so generated templates stay in sync.
- `cmd/preview-wasm`'s `wasm_build_test.go` is unaffected: the UI is server-only,
  uses normal build tags, and is not part of the `js/wasm` build.

## Testing

- Unit tests for view-model mapping (k8s types → badge/service/persistence/
  upgrade state) — the logic-heavy part. Table-driven, matching existing style.
- Handler tests with `httptest` + a fake `DataSource`, asserting rendered
  fragments and the `--ui-require-auth` gate (401 when header missing).
- A fake-client `DataSource` test (using controller-runtime's fake client) to
  verify list/get → view-model wiring.
- No envtest required (pure read + mapping).

## Deployment / RBAC

- No new RBAC: the operator already watches these CRDs.
- Add an optional UI `Service` plus example `Ingress` + Authelia forward-auth
  manifests under `config/` and `examples/`, kept out of the default
  kustomization so the UI remains strictly opt-in.
- Add a README/docs section describing how to enable and front the UI.

## Future phases (out of scope for v1)

- SSE live push via a watch-fed broadcaster (drop-in behind `DataSource`).
- Live Temporal gRPC data (the "B" scope): namespace lists, live cluster health.
- Mutating actions (force-reconcile, etc.), gated behind a per-user authz story.
- TemplUI / Tailwind adoption for richer components.
