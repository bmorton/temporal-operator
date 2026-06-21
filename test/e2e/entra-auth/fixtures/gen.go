//go:build ignore

// gen.go generates the test-only RSA keypair, JWKS document, and JWT tokens
// used by the hermetic JWKS server-JWT e2e suite.
//
// Run once from this directory:
//
//	go run gen.go
//
// Outputs (all committed):
//
//	private-key.pem    — test-only RSA-2048 private key (NEVER use in production)
//	jwks.json          — public key as a JWK Set (kid=e2e, alg=RS256, use=sig)
//	token-valid.txt    — RS256 JWT signed with the primary key
//	token-invalid.txt  — RS256 JWT signed with a DIFFERENT key (signature mismatch)
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"

	_ "crypto/sha256" // ensure SHA-256 is linked
)

const (
	kid   = "e2e"
	expTS = 4102444800 // 2100-01-01T00:00:00Z — never expires in CI
)

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func bigB64(n *big.Int) string {
	return b64url(n.Bytes())
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func publicJWK(pub *rsa.PublicKey, kid string) jwk {
	return jwk{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: kid,
		N:   bigB64(pub.N),
		E:   bigB64(big.NewInt(int64(pub.E))),
	}
}

func genKey() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	check(err, "generate RSA key")
	return k
}

func writePEM(path string, key *rsa.PrivateKey) {
	f, err := os.Create(path)
	check(err, "create "+path)
	defer f.Close()
	check(pem.Encode(f, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), "write PEM "+path)
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	check(err, "marshal JSON")
	check(os.WriteFile(path, append(b, '\n'), 0o644), "write "+path)
}

func writeText(path, content string) {
	check(os.WriteFile(path, []byte(content+"\n"), 0o644), "write "+path)
}

func signJWT(headerB64, payloadB64 string, key *rsa.PrivateKey) string {
	msg := headerB64 + "." + payloadB64
	digest := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	check(err, "sign JWT")
	return msg + "." + b64url(sig)
}

func check(err any, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL %s: %v\n", msg, err)
		os.Exit(1)
	}
}

func main() {
	primary := genKey()
	other := genKey()

	writePEM("private-key.pem", primary)
	fmt.Println("wrote private-key.pem")

	set := jwkSet{Keys: []jwk{publicJWK(&primary.PublicKey, kid)}}
	writeJSON("jwks.json", set)
	fmt.Println("wrote jwks.json")

	headerJSON, _ := json.Marshal(map[string]string{
		"alg": "RS256",
		"kid": kid,
		"typ": "JWT",
	})
	claimsJSON, _ := json.Marshal(map[string]any{
		"sub":   "e2e",
		"exp":   expTS,
		"roles": []string{"default:read", "default:write", "temporal-system:admin"},
	})

	headerB64 := b64url(headerJSON)
	payloadB64 := b64url(claimsJSON)

	writeText("token-valid.txt", signJWT(headerB64, payloadB64, primary))
	fmt.Println("wrote token-valid.txt")

	writeText("token-invalid.txt", signJWT(headerB64, payloadB64, other))
	fmt.Println("wrote token-invalid.txt (signed with different key — will fail verification)")
}
