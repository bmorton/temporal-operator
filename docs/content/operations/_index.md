+++
title = "Operations"
weight = 40
+++

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

## mTLS health probes

When mTLS is enabled, the request-serving services (frontend, history, matching)
use **TCP** startup/readiness/liveness probes instead of gRPC ones. Kubernetes'
native gRPC prober dials the health endpoint without a client certificate, so it
cannot complete the mutual-TLS handshake on Temporal's `requireClientAuth` port —
a gRPC probe would time out forever and the pods would never become `Ready`. The
worker service has no probe at all (it serves no client-facing gRPC), matching the
upstream Temporal Helm chart.

**Caveat:** a TCP probe only confirms that the gRPC port is *accepting
connections*, not that the service is reporting healthy through the gRPC health
API. It is therefore a weaker liveness/readiness signal than the gRPC probe used
on non-mTLS clusters (which are unaffected and keep the more precise probe). This
is an acceptable trade-off to keep mTLS clusters functional, but it is not ideal
for production-grade health gating.

> **Future work:** richer health checking under mTLS is planned — for example an
> exec probe using `grpc-health-probe` with the internode client certificate, or
> delegating mTLS to a service mesh (Linkerd, or Istio/Envoy) so Temporal serves
> plaintext behind the sidecar and standard gRPC probes apply. Until then, TCP
> probes are the supported behavior for mTLS clusters.
