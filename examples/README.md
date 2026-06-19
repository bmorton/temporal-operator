# Examples

Curated `TemporalCluster` (and related) custom resources for common scenarios.
Each example assumes the operator is installed and the referenced backing
services/secrets exist.

| Directory | Scenario |
| --- | --- |
| [`cluster-postgres-minimal`](./cluster-postgres-minimal) | Smallest viable Postgres-backed cluster. |
| [`cluster-postgres-full`](./cluster-postgres-full) | Production-leaning sizing, UI, ingress, metrics. |
| [`cluster-cassandra`](./cluster-cassandra) | Cassandra default + visibility store. |
| [`cluster-mtls-cert-manager`](./cluster-mtls-cert-manager) | mTLS via a cert-manager CA issuer. |
| [`cluster-elasticsearch-visibility`](./cluster-elasticsearch-visibility) | Postgres default + Elasticsearch visibility. |
| [`cluster-with-namespaces-and-search-attributes`](./cluster-with-namespaces-and-search-attributes) | Declarative namespace + search attribute. |
| [`schedules`](./schedules) | Declarative `TemporalSchedule` resources (cron, interval, structured calendar, paused). |
| [`devserver`](./devserver) | Disposable single-pod `TemporalDevServer` (SQLite) for local/testing use. |
| [`cluster-cnpg-integrated`](./cluster-cnpg-integrated) | End-to-end with CloudNativePG. |
| [`cluster-azure-postgres-flexible`](./cluster-azure-postgres-flexible) | Azure Database for PostgreSQL Flexible Server (password auth). |
| [`cluster-azure-aks-ingress`](./cluster-azure-aks-ingress) | Temporal UI on AKS via Application Gateway (AGIC). |
| [`cluster-azure-workload-identity`](./cluster-azure-workload-identity) | **Preview:** passwordless Flexible Server via Azure Workload Identity. |
| [`cluster-upgrade`](./cluster-upgrade) | Version upgrade walkthrough (1.30 → 1.31). |
