# test/e2e/entra-auth/fixtures

Test-only cryptographic fixtures for the hermetic JWKS server-JWT e2e suite.

## ⚠️ WARNING — test keys only

`private-key.pem` is a **test-only** RSA private key committed intentionally
to the repository for deterministic e2e testing. It **must never** be used in
any non-test context and **must never** be rotated into a production JWKS
endpoint.

## Files

| File | Description |
|------|-------------|
| `private-key.pem` | RSA-2048 private key (test-only) |
| `jwks.json` | JWK Set with the corresponding public key (`kid=e2e`, `alg=RS256`, `use=sig`) |
| `token-valid.txt` | RS256 JWT signed with the primary key — verifies successfully |
| `token-invalid.txt` | RS256 JWT signed with a **different** key — signature verification fails |
| `gen.go` | Generator (`//go:build ignore`); run once with `go run gen.go` |
| `verify.go` | Offline verifier (`//go:build ignore`); run with `go run verify.go` |

## JWT claims

```json
{
  "alg": "RS256",
  "kid": "e2e",
  "typ": "JWT"
}
{
  "sub": "e2e",
  "exp": 4102444800,
  "roles": ["default:read", "default:write", "temporal-system:admin"]
}
```

`exp: 4102444800` is 2100-01-01T00:00:00Z — the token never expires in CI.

## Regenerating

If you need fresh keys (e.g., after a security incident — though these are
test-only and have no production impact):

```sh
cd test/e2e/entra-auth/fixtures
go run gen.go
go run verify.go   # must print: All checks PASSED
```

Then update the `jwks.json` inline in `../01-jwks.yaml` (ConfigMap data) and
the token values in `../03-authz-allow.yaml` (ConfigMap data, also consumed by `../04-authz-deny.yaml`).

## Verification

```
$ go run verify.go
OK: private-key.pem matches jwks.json public key
PASS: token-valid.txt verifies against jwks.json (RS256, kid=e2e)
PASS: token-invalid.txt correctly FAILS verification (crypto/rsa: verification error)

All checks PASSED — fixtures are sound.
```
