+++
title = "Verifying Releases"
weight = 25
+++

# Verifying releases

Every `temporal-operator` release is built with [GoReleaser](https://goreleaser.com),
its container images and checksums are signed with [Cosign](https://docs.sigstore.dev/)
(keyless, via GitHub OIDC), and a [SLSA Level 3](https://slsa.dev/) provenance
attestation is published with the GitHub Release.

## Verify the container image signature

```sh
cosign verify ghcr.io/bmorton/temporal-operator:v0.1.0 \
  --certificate-identity-regexp='^https://github.com/bmorton/temporal-operator/.github/workflows/release.yml@.*$' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

For a quick check that accepts any signing identity from this repo's workflows:

```sh
cosign verify ghcr.io/bmorton/temporal-operator:v0.1.0 \
  --certificate-identity-regexp='.*' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

## Verify the checksums signature

```sh
cosign verify-blob \
  --signature checksums.txt.sig \
  --certificate-identity-regexp='.*' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  checksums.txt
```

## Verify SLSA provenance

Download `multiple.intoto.jsonl` from the GitHub Release, then use the
[slsa-verifier](https://github.com/slsa-framework/slsa-verifier):

```sh
slsa-verifier verify-artifact \
  --provenance-path multiple.intoto.jsonl \
  --source-uri github.com/bmorton/temporal-operator \
  temporal-operator_0.1.0_linux_amd64.tar.gz
```

## Software Bill of Materials (SBOM)

Each archive ships with a [Syft](https://github.com/anchore/syft)-generated
SBOM (`*.sbom.json`) attached to the GitHub Release for supply-chain auditing.
