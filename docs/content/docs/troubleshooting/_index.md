+++
title = "Troubleshooting"
weight = 60
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
