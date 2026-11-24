# Temporal UI through the Gateway API

Exposes temporal-ui with a Gateway API `HTTPRoute` instead of an Ingress.

- `gateway.yaml`: an example `Gateway` with an HTTPS listener. You own this
  object. TLS terminates here, so the certificate is configured on the listener
  and not on the `TemporalCluster`.
- `temporalcluster.yaml`: a cluster with `spec.ui.httpRoute` enabled. The
  operator creates an `HTTPRoute` named `<cluster>-ui` that sends `/` to the UI
  Service on port 8080.

## Requirements

- The Gateway API CRDs (`gateway.networking.k8s.io/v1`) and a Gateway
  implementation are installed. If the `HTTPRoute` CRD is missing, the operator
  skips the route and records a `GatewayAPINotInstalled` warning event on the
  cluster. If you install the CRDs after the operator started, restart the
  operator so it also watches the routes it creates.
- When the Gateway is in another namespace, its listener must allow routes from
  the cluster's namespace through `allowedRoutes`, as `gateway.yaml` does.

## Check the route

```sh
kubectl -n temporal get httproute gateway-ui \
  -o jsonpath='{.status.parents[*].conditions[?(@.type=="Accepted")].status}'
```

`True` means the Gateway accepted the route.

## Moving from an Ingress

`ui.ingress` and `ui.httpRoute` can be enabled together:

1. Add `ui.httpRoute` next to the existing `ui.ingress` and apply.
2. Confirm the route is accepted and serves the UI, then move DNS to the
   Gateway's address.
3. Set `ui.ingress.enabled: false` (or remove the block). The operator deletes
   the Ingress it created.
