+++
title = "Operations"
weight = 40
aliases = ["/operations/"]
+++

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

## Exposing the Temporal UI

`spec.ui` can publish temporal-ui in two ways. Both are optional and can be on
at the same time.

| Field | Creates | You provide |
| --- | --- | --- |
| `ui.ingress` | an `Ingress` named `<cluster>-ui` | an ingress controller |
| `ui.httpRoute` | a Gateway API `HTTPRoute` named `<cluster>-ui` | a `Gateway` and its listeners |

With the Gateway API the operator owns only the route. The `Gateway`, its
listeners and TLS termination stay with whoever runs the Gateway:

```yaml
spec:
  ui:
    enabled: true
    httpRoute:
      enabled: true
      parentRefs:
        - name: public
          namespace: gateways   # defaults to the cluster's namespace
          sectionName: https    # optional: one listener only
      hostnames:
        - temporal.example.com
```

A Gateway in another namespace must allow routes from the cluster's namespace
through its listeners' `allowedRoutes`. The Gateway API CRDs must be installed;
if they are not, the operator skips the route and records a
`GatewayAPINotInstalled` warning event. Install them before the operator starts,
or restart it afterwards, so it watches the routes it creates.

To move from an Ingress to a Gateway, enable `ui.httpRoute` alongside
`ui.ingress`, switch traffic over, then disable `ui.ingress`. See the
`cluster-gateway-api` example for a full walkthrough.

## Turning features off

Disabling an optional feature removes the objects the operator created for it:

| Change | Removed |
| --- | --- |
| `ui.enabled: false` | UI Deployment, Service, Ingress, HTTPRoute, client Certificate |
| `ui.ingress.enabled: false` | UI Ingress |
| `ui.httpRoute.enabled: false` | UI HTTPRoute |
| `services.internalFrontend.enabled: false` | internal-frontend Deployment, Service, PodDisruptionBudget |
| `metrics.serviceMonitor.enabled: false` | ServiceMonitor |
| `mtls` removed | internode, frontend and UI client Certificates |

Removing the block entirely has the same effect as setting `enabled: false`.
Only objects the operator created are deleted: an object with the same name
that you made yourself is left alone. Secrets issued by cert-manager follow
cert-manager's own cleanup settings.

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

{{< callout type="info" >}}
**Future work:** richer health checking under mTLS is planned — for example an
exec probe using `grpc-health-probe` with the internode client certificate, or
delegating mTLS to a service mesh (Linkerd, or Istio/Envoy) so Temporal serves
plaintext behind the sidecar and standard gRPC probes apply. Until then, TCP
probes are the supported behavior for mTLS clusters.
{{< /callout >}}
