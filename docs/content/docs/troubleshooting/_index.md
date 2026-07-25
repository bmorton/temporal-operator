+++
title = "Troubleshooting"
weight = 60
aliases = ["/troubleshooting/"]
+++

## PersistenceReachable=False

- Check the datastore host/port and that the password Secret exists with the
  referenced key.
- The operator pings the datastore from its own pod (in `temporal-system`), so a
  Postgres `host` must be a namespace-qualified FQDN
  (e.g. `temporal-pg-rw.temporal.svc.cluster.local`), not a bare service name.
- For Postgres, confirm the database and visibility database exist. The operator
  does not create databases — both `temporal` and `temporal_visibility` must
  exist before schema setup.
- `remaining connection slots are reserved for ... SUPERUSER` (SQLSTATE 53300)
  means Postgres ran out of connections. Each service pod pools to both stores;
  raise `max_connections` (200 is a good starting point for a small cluster).

## SchemaReady=False

- Inspect the schema Jobs: `kubectl get jobs -l app.kubernetes.io/component=schema`.
- View logs of a failed schema Job for `temporal-sql-tool` / `temporal-cassandra-tool` errors.

## MTLSReady=False

- Ensure cert-manager is installed and the referenced `issuerRef` exists.
- Check the `Certificate` resources: `kubectl describe certificate <cluster>-internode`.

## Available=False

- `kubectl get deploy -l app.kubernetes.io/instance=<cluster>` and inspect
  pod events (image pulls, probe failures, membership join issues).
- On mTLS clusters the request-serving pods use TCP probes rather than gRPC ones
  (the native gRPC prober cannot present a client certificate); see the
  "mTLS health probes" section under Operations for details.

## Namespace/SearchAttribute not registering

- These wait for the cluster's `Ready=True`. Confirm the cluster is ready first.

## Stalled upgrades

When a service does not roll out to the new version within the upgrade phase
timeout (15 minutes by default, `--upgrade-phase-timeout`), the operator sets:

```text
UpgradeBlocked=True  reason=UpgradeStalled
Degraded=True        reason=RolloutStalled
```

`status.upgrade.stalledService` names the service and `status.upgrade.message`
carries the Deployment's own reason — usually an image pull failure, a
crashlooping pod, or unschedulable replicas.

```sh
kubectl get temporalcluster my-cluster -o jsonpath='{.status.upgrade}' | jq
kubectl describe deployment my-cluster-frontend
```

The cluster keeps running mixed versions while blocked. The condition clears on
its own as soon as the rollout completes, so fixing the underlying cause is
usually all that is required.

To abandon the upgrade instead, set `spec.version` back to
`status.upgrade.fromVersion`. That is the only version change accepted while an
upgrade is in flight; any other value is rejected by the webhook. If the schema
has already migrated (`status.upgrade.rollbackable: false`) the revert is still
accepted, but the API server returns a warning: Temporal schema migrations are
forward-only, so the older binaries will run against the newer schema. Confirm
that combination is supported before proceeding.

## Failed schema migrations

A schema Job that exhausts its retry budget is deleted and recreated at bounded
intervals — after 1 minute, then 5 minutes, then 15 minutes. While retrying:

```text
SchemaReady=False  reason=SchemaMigrationRetrying
```

After all three recreations have failed, the operator gives up and reports:

```text
SchemaReady=False  reason=SchemaMigrationFailed
Degraded=True      reason=SchemaMigrationFailed
```

The final failed Job is deliberately retained so its logs stay available. The
exact Job name is embedded in the condition message; it follows the pattern
`<cluster>-schema-<store>-<action>`:

```sh
kubectl -n <namespace> logs job/<cluster>-schema-default-update
kubectl -n <namespace> logs job/<cluster>-schema-visibility-update
```

`status.persistence.schemaAttempts` records the attempt count and the last
error per store. To recover: fix the root cause (most often database credentials
or connectivity), then delete the failed Job. If the new Job succeeds, the
attempt count resets automatically. If you want the full three-attempt retry
budget restored before the new Job completes — for example when the fix might
not be perfect — also clear the attempt count with a status patch:

```sh
kubectl patch temporalcluster <name> --type=merge --subresource=status \
  -p '{"status":{"persistence":{"schemaAttempts":null}}}'
```

## Abandoned cleanup

If a `TemporalNamespace`, `TemporalSchedule`, `TemporalSearchAttribute`, or
`TemporalClusterConnection` is deleted while its cluster is unreachable, the
operator retries for five minutes before releasing the finalizer so deletion can
complete. When that happens it emits a warning event:

```sh
kubectl get events --field-selector reason=CleanupAbandoned
```

The Kubernetes object is gone but the Temporal-side object was **not** removed.
Delete it manually with `temporal operator namespace delete` or the equivalent
for the resource type. The `temporal_operator_cleanup_abandoned_total` metric
counts these, and the `TemporalCleanupAbandoned` alert fires on any increase.

Deletion always terminates. The operator never leaves a resource stuck in
`Terminating` because a cluster is unreachable.
