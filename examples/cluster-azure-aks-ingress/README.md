# Temporal UI on AKS via Application Gateway (AGIC)

Exposes the Temporal Web UI through the Azure Application Gateway Ingress
Controller (AGIC).

## Prerequisites

- An AKS cluster with the AGIC add-on enabled (`az aks enable-addons --addons
  ingress-appgw ...`) or AGIC installed via Helm.
- A DNS record pointing `temporal.example.com` at the Application Gateway public
  IP.
- A working persistence backend — combine this with
  [`cluster-azure-postgres-flexible`](../cluster-azure-postgres-flexible).

## Apply

```sh
kubectl apply -f temporalcluster.yaml
```

The `ingressClassName: azure-application-gateway` selects AGIC; the
`appgw.ingress.kubernetes.io/*` annotations tune the Application Gateway backend.
Add TLS via an `appgw.ingress.kubernetes.io/appgw-ssl-certificate` annotation or
the UI ingress `tlsSecretName` field once you have a certificate provisioned.
