+++
title = "Installation"
weight = 20
+++

# Installation

The operator ships in three formats.

## Helm (recommended)

```sh
helm install temporal-operator oci://ghcr.io/bmorton/charts/temporal-operator \
  --namespace temporal-system --create-namespace
```

See the [chart values](https://github.com/bmorton/temporal-operator/blob/main/dist/chart/values.yaml)
for configuration. Webhooks require cert-manager (`webhook.enable=true` by
default).

## Raw kustomize / single manifest

```sh
kubectl apply -f https://github.com/bmorton/temporal-operator/releases/latest/download/install.yaml
```

## OLM bundle

```sh
operator-sdk run bundle ghcr.io/bmorton/temporal-operator-bundle:v0.1.0
```

## Verifying releases

See [verifying-releases](./verifying-releases.md) for Cosign and SLSA
verification.
