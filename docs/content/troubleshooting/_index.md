+++
title = "Troubleshooting"
weight = 60
+++

# Troubleshooting

## PersistenceReachable=False

- Check the datastore host/port and that the password Secret exists with the
  referenced key.
- For Postgres, confirm the database and visibility database exist.

## SchemaReady=False

- Inspect the schema Jobs: `kubectl get jobs -l app.kubernetes.io/component=schema`.
- View logs of a failed schema Job for `temporal-sql-tool` / `temporal-cassandra-tool` errors.

## MTLSReady=False

- Ensure cert-manager is installed and the referenced `issuerRef` exists.
- Check the `Certificate` resources: `kubectl describe certificate <cluster>-internode`.

## Available=False

- `kubectl get deploy -l app.kubernetes.io/instance=<cluster>` and inspect
  pod events (image pulls, probe failures, membership join issues).

## Namespace/SearchAttribute not registering

- These wait for the cluster's `Ready=True`. Confirm the cluster is ready first.
