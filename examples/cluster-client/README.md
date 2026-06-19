# TemporalClusterClient — mTLS client credentials

A `TemporalClusterClient` issues mTLS client credentials for a Temporal cluster.
The operator requests a cert-manager `Certificate` from the cluster's mTLS CA
issuer and writes the result (`tls.crt`, `tls.key`, `ca.crt`) into a Kubernetes
Secret. Workers and client applications can then mount that Secret to connect to
the cluster's frontend over mTLS.

> **Requirement:** this CRD only works against an mTLS-enabled `TemporalCluster`
> (`mtls.provider: cert-manager`). Against a non-mTLS `TemporalCluster` the
> controller reports a `ClusterMTLSDisabled` condition, and against a
> `TemporalDevServer` it reports `DevServerUnsupported` — in both cases it issues
> no certificate.

**Prerequisites:** [cert-manager](https://cert-manager.io/) must be installed in
the cluster.

## Files

| File | Description |
| --- | --- |
| [`issuer.yaml`](./issuer.yaml) | Self-signed root `Issuer`, CA `Certificate`, and `temporal-ca-issuer` CA `Issuer`. Apply first. |
| [`temporalcluster.yaml`](./temporalcluster.yaml) | mTLS-enabled `TemporalCluster` (`temporal-mtls`) that references `temporal-ca-issuer`. |
| [`clusterclient.yaml`](./clusterclient.yaml) | `TemporalClusterClient` that issues credentials from the cluster above. |

## Apply

```sh
# 1. Bootstrap the cert-manager CA issuer
kubectl apply -f issuer.yaml

# 2. Create the mTLS-enabled cluster (adjust persistence config for your env)
kubectl apply -f temporalcluster.yaml

# 3. Issue client credentials
kubectl apply -f clusterclient.yaml
```

Wait for the cluster to become ready, then check the client:

```sh
kubectl get temporalclusterclients        # short name: tcc
kubectl describe tcc temporal-mtls-client
```

The `READY` column becomes `True` once cert-manager has issued the certificate
(`CertificateReady` condition). Inspect the generated Secret:

```sh
kubectl get secret temporal-mtls-client -o yaml
# Data keys: tls.crt, tls.key, ca.crt
```

## Using the credentials

Mount the Secret in a worker or client deployment to connect over mTLS:

```yaml
volumes:
  - name: temporal-client-tls
    secret:
      secretName: temporal-mtls-client
containers:
  - name: worker
    volumeMounts:
      - name: temporal-client-tls
        mountPath: /etc/temporal/tls
        readOnly: true
```

Pass the mounted paths to your Temporal SDK's TLS configuration
(`ClientCertPath`, `ClientKeyPath`, `CACertPath`) when dialing the cluster's
frontend (`<cluster-name>-frontend.<namespace>:7233`).

## Caveats

- The cluster referenced by `clusterRef.name` must exist in the **same
  namespace** as the `TemporalClusterClient`.
- If the cluster has no mTLS configured, the controller sets a
  `ClusterMTLSDisabled` condition and does not issue a certificate.
- `secretName` defaults to the `TemporalClusterClient` resource name when
  omitted.
