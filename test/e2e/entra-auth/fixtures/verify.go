//go:build ignore

// verify.go — offline crypto fixture verifier.
// Confirms that token-valid.txt verifies against jwks.json and that
// token-invalid.txt does NOT.
//
// Run from the fixtures directory:
//
//	go run verify.go
package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strings"
)

type jwkSet struct {
	Keys []struct {
		Kid string `json:"kid"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func b64dec(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func loadJWKS(path string) (*rsa.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var set jwkSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return nil, err
	}
	for _, k := range set.Keys {
		if k.Kid == "e2e" {
			nBytes, err := b64dec(k.N)
			if err != nil {
				return nil, err
			}
			eBytes, err := b64dec(k.E)
			if err != nil {
				return nil, err
			}
			n := new(big.Int).SetBytes(nBytes)
			e := new(big.Int).SetBytes(eBytes)
			pub := &rsa.PublicKey{N: n, E: int(e.Int64())}
			return pub, nil
		}
	}
	return nil, fmt.Errorf("kid=e2e not found in JWKS")
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func verifyJWT(token string, pub *rsa.PublicKey) error {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("malformed JWT: expected 3 parts, got %d", len(parts))
	}
	sigInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(sigInput))
	sigBytes, err := b64dec(parts[2])
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sigBytes)
}

func main() {
	pub, err := loadJWKS("jwks.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL load JWKS: %v\n", err)
		os.Exit(1)
	}

	priv, err := loadPrivateKey("private-key.pem")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL load private key: %v\n", err)
		os.Exit(1)
	}

	// Confirm private key matches the JWKS public key
	if pub.N.Cmp(priv.PublicKey.N) != 0 {
		fmt.Println("FAIL: private-key.pem does not match jwks.json public key")
		os.Exit(1)
	}
	fmt.Println("OK: private-key.pem matches jwks.json public key")

	validTok, err := os.ReadFile("token-valid.txt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL read token-valid.txt: %v\n", err)
		os.Exit(1)
	}
	invalidTok, err := os.ReadFile("token-invalid.txt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL read token-invalid.txt: %v\n", err)
		os.Exit(1)
	}

	pass := true

	if err := verifyJWT(string(validTok), pub); err != nil {
		fmt.Printf("FAIL: token-valid.txt did NOT verify: %v\n", err)
		pass = false
	} else {
		fmt.Println("PASS: token-valid.txt verifies against jwks.json (RS256, kid=e2e)")
	}

	if err := verifyJWT(string(invalidTok), pub); err != nil {
		fmt.Printf("PASS: token-invalid.txt correctly FAILS verification (%v)\n", err)
	} else {
		fmt.Println("FAIL: token-invalid.txt unexpectedly PASSED verification — tokens are not distinct")
		pass = false
	}

	if pass {
		fmt.Println("\nAll checks PASSED — fixtures are sound.")
	} else {
		fmt.Println("\nSome checks FAILED — fixtures need regeneration.")
		os.Exit(1)
	}
}
