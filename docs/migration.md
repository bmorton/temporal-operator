# Temporal cluster migrations

`TemporalMigration` moves client traffic from an external Temporal cluster to an
operator-managed `TemporalCluster` through a managed upgrade proxy. The model is
intentionally drain-based: Temporal workflow history and mutable state remain
cluster-local, so the proxy routes requests but never copies workflow history,
visibility records, search attributes, or namespace metadata between clusters.
Prepare the target cluster before cutover and treat the source as authoritative
for every workflow that was started there.

## Lifecycle

1. **Passthrough**: create the `TemporalMigration` with `spec.cutover: false`.
   The operator creates a proxy Service and Deployment, then the proxy forwards
   all requests to `spec.source.address`.
2. **Cutover**: repoint clients and workers to `status.proxyEndpoint`, then set
   `spec.cutover: true`. New workflow starts are routed to the target cluster,
   while operations for workflows already running on the source continue to use
   the source.
3. **Drain**: keep both clusters serving work. The controller periodically counts
   running source workflows for each migrated namespace and publishes the result
   in `status.draining`.
4. **Complete**: when every tracked source namespace reports zero running
   workflows for a stable-zero window over multiple checks, `status.phase`
   becomes `Complete`.

## Spec fields

- `source.address` (required): frontend host and port for the external source
  cluster, for example `old-temporal-frontend.legacy.svc.cluster.local:7233`.
- `source.tls`: enables TLS from the proxy to the source. Set
  `source.tls.enabled: true`; optionally set `source.tls.serverName` when the
  certificate name differs from the address.
- `source.tls.secretRef`: Kubernetes Secret in the `TemporalMigration` namespace
  containing `ca.crt` for source verification. Include `tls.crt` and `tls.key`
  for client mTLS to the source.
- `targetRef.name` (required): name of the operator-managed `TemporalCluster` in
  the same namespace that receives new workflow starts after cutover.
- `namespaces`: optional source-to-target namespace mappings. Omit it to migrate
  every source namespace discovered by the controller. When `target` is omitted,
  it defaults to the `source` namespace name.
- `cutover`: manual safety gate. `false` keeps passthrough mode; `true` starts
  target routing for new workflows and begins drain tracking.
- `proxy`: optional proxy Deployment tuning, including `replicas`, `resources`,
  and `image` override.

## Worker requirement

Run workers against **both** clusters for the full migration window. Source
workers are required to finish workflows that started before cutover; target
workers are required for workflows that start after cutover. Stopping source
workers early can leave old executions stuck, and stopping target workers after
cutover prevents new executions from making progress.

## Example operation

```sh
kubectl apply -f examples/migration/01-temporalmigration.yaml
kubectl get temporalmigration orders-migration -n temporal-system \
  -o jsonpath='{.status.proxyEndpoint}'
```

Update client and worker frontend addresses to the proxy endpoint. When the
source and target are ready and both worker pools are running, cut over:

```sh
kubectl patch temporalmigration orders-migration -n temporal-system \
  --type merge -p '{"spec":{"cutover":true}}'
kubectl get temporalmigration orders-migration -n temporal-system -w
```

Watch `status.draining[*].sourceRunningWorkflows` until the phase is `Complete`.
Long-running, cron, or intentionally never-ending workflows can keep the source
alive indefinitely; either let them finish, terminate them intentionally, or keep
the migration/proxy in place until they are no longer needed.

## Teardown

Completion does not automatically move clients away from the proxy. After
`status.phase` is `Complete`, repoint clients and workers directly to the target
TemporalCluster frontend, verify traffic no longer uses `status.proxyEndpoint`,
and delete the `TemporalMigration`:

```sh
kubectl delete temporalmigration orders-migration -n temporal-system
```

The proxy Service, ConfigMap, and Deployment are owned by the
`TemporalMigration` and are garbage-collected after deletion. Keep the source
cluster available until you have confirmed no clients, workers, or operational
processes still need it.
