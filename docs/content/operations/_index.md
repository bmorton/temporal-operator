# Operations

## Reading status

```sh
kubectl get tc -o wide
kubectl describe tc <name>
```

Key conditions:

- `PersistenceReachable` — the datastore is reachable.
- `SchemaReady` — the schema is at the required version.
- `MTLSReady` — certificates are issued (mTLS clusters only).
- `Available` — all services have at least one ready replica.
- `Ready` — the cluster is fully operational.

`status.phase` gives a coarse summary; `status.services` reports per-service
ready/desired replicas; `status.persistence.schemaVersions` reports the observed
schema versions.

## Config and rollouts

The rendered server config is stored in the `<cluster>-config` Secret. A change
to the rendered config updates a `config-hash` pod annotation, triggering a
rolling restart. Dynamic config lives in the `<cluster>-dynamicconfig` ConfigMap
and is hot-reloaded by Temporal without a restart.

## Certificate rotation

cert-manager rotates the cert Secrets; the operator stamps a `cert-hash`
annotation so rotations roll the pods, and Temporal also hot-reloads certs via
`tls.refreshInterval`.
