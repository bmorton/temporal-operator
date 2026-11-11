# TemporalClusterProxy mux — server + client example

Deploys an s2s-proxy mux link between two Temporal replication clusters using
the `TemporalClusterProxy` CRD.

## Architecture

```text
cluster-a                      cluster-b
┌─────────────────────┐        ┌─────────────────────┐
│  Temporal frontend  │        │  Temporal frontend  │
│      :7233          │        │      :7233          │
│         ↕           │        │         ↕           │
│  client proxy       │◄──mux──│  server proxy       │
│  (to-cluster-b)     │  6334  │  (to-cluster-a)     │
│  port 6233 (local)  │        │  port 6233 (local)  │
└─────────────────────┘        └─────────────────────┘
```

- **server.yaml** — `TemporalClusterProxy` with `role: server` deployed next to
  `cluster-b`. Opens the mux port (6334) and exposes it via a LoadBalancer
  Service so cluster-a's client proxy can dial in.
- **client.yaml** — `TemporalClusterProxy` with `role: client` deployed next to
  `cluster-a`. Dials out to the server proxy's LoadBalancer address.

## Prerequisites

- Both `cluster-a` and `cluster-b` `TemporalCluster` resources are Ready.
- cert-manager is installed and a `temporal-ca` Issuer exists in `temporal-system`.
- The server proxy's LoadBalancer IP/hostname is known before applying the client.

## Apply

```sh
# On the cluster hosting cluster-b: apply the server proxy
kubectl apply -n temporal-system -f server.yaml

# Retrieve the assigned LoadBalancer address:
kubectl get svc -n temporal-system to-cluster-a-s2s-proxy \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'

# Edit client.yaml: replace cluster-b-proxy.example.com with the address above.
# On the cluster hosting cluster-a: apply the client proxy
kubectl apply -n temporal-system -f client.yaml

# Verify both proxies are Ready:
kubectl get tcproxy -n temporal-system
```

## Status

Once both proxies reach `Ready=True`, each cluster's frontend has the other
registered as a remote peer and replication can flow through the mux tunnel.

```sh
kubectl describe tcproxy -n temporal-system to-cluster-a
kubectl describe tcproxy -n temporal-system to-cluster-b
```
