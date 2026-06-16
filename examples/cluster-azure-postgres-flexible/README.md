# Azure Database for PostgreSQL Flexible Server (password auth)

A `TemporalCluster` backed by Azure Database for PostgreSQL Flexible Server using
password authentication over TLS. This is the simplest Azure persistence option
and works with the operator today.

## Prerequisites

1. A Flexible Server instance reachable from your AKS cluster (public access with
   firewall rules, or private access / VNet integration).
2. Two databases created on the server **before** applying this manifest — the
   operator runs `setup-schema` but does not create databases:

   ```sql
   CREATE DATABASE temporal;
   CREATE DATABASE temporal_visibility;
   ```

3. `max_connections` raised on the server (Server parameters blade). Temporal
   opens several pools per pod; the small-SKU default can be exhausted. ~200 is a
   safe starting point.

## Apply

Edit `temporalcluster.yaml` to set the server FQDN, user, and password, then:

```sh
kubectl apply -f temporalcluster.yaml
```

TLS is required by Flexible Server; `tls.enabled: true` is set on both stores.
Azure's certificate chains to a public root, so no CA secret is needed.
