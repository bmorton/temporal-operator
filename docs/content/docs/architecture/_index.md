+++
title = "Architecture"
weight = 30
aliases = ["/architecture/"]
+++

The operator follows the standard controller-runtime model: each custom
resource has a reconciler that drives observed state toward desired state.

## Custom resources

| Kind | Short | Purpose |
|---|---|---|
| `TemporalCluster` | `tc` | A complete Temporal server deployment. |
| `TemporalDevServer` | `tds` | A disposable single-pod dev server (SQLite); for development/CI, not production. |
| `TemporalNamespace` | `tns` | A namespace within a managed cluster. |
| `TemporalSearchAttribute` | `tsa` | A custom search attribute registration. |
| `TemporalSchedule` | `tsch` | A declarative Temporal schedule (cron or interval). |
| `TemporalClusterClient` | `tcc` | Generated mTLS client credentials. |

## TemporalCluster reconciliation

The `TemporalCluster` reconciler runs a sequence of sub-reconcilers:

1. **Persistence** — probes the datastore(s) and runs schema setup/migration
   via `temporal-sql-tool` / `temporal-cassandra-tool` Jobs (SQL, Cassandra) or
   inline index-template application (Elasticsearch). Sets
   `PersistenceReachable` and `SchemaReady`.
2. **mTLS** — issues internode and frontend cert-manager `Certificate`s and sets
   `MTLSReady`.
3. **Upgrade** — when `spec.version` differs from the running version, runs an
   ordered, per-service rolling upgrade state machine (`status.upgrade`).
4. **Services** — renders the server config (stored in a Secret, since it embeds
   credentials) and server-side-applies the Deployments, headless Services, a
   frontend Service, and PodDisruptionBudgets. Sets `Available`.
5. **UI / Monitoring** — optionally deploys temporal-ui and a `ServiceMonitor`.

`Ready` is the rollup of `PersistenceReachable && SchemaReady && Available`
(and `MTLSReady` when mTLS is enabled). `status.phase` reports
`Pending → ProvisioningSchema → DeployingServices → Ready` (or `Upgrading`).

## Version matrix

Supported Temporal versions and their schema/UI requirements live in
`internal/temporal/versions_gen.go`, generated from `hack/version-matrix.yaml`.
