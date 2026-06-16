# mTLS Cluster Health + Operator Client TLS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make an mTLS-enabled `TemporalCluster` reach Ready and let the search-attribute/namespace controllers operate against it, by rendering `systemWorker` TLS, setting internode/frontend `client.serverName` (with a matching cert SAN), and giving the controllers a client `*tls.Config`.

**Architecture:** Three small, independent changes plus tests: (1) add a stable SAN to the internode cert and populate the internode/frontend `serverName` in the rendered config; (2) render a `systemWorker` TLS block reusing the internode cert already mounted on every pod; (3) a `clusterTLSConfig` helper the controllers use to dial the frontend with mTLS. Plus an e2e search-attribute step against the mTLS cluster and a `workflow_dispatch` input to run the mtls suite on demand.

**Tech Stack:** Go, controller-runtime, cert-manager, Temporal server config (YAML template), Chainsaw e2e, GitHub Actions.

---

## File Structure

- `internal/resources/certificates.go` — add `<cluster>-internode` SAN to the internode cert.
- `internal/resources/certificates_test.go` (new) — unit test for the SAN.
- `internal/temporal/configtemplate.go` — populate `InternodeServerName`/`FrontendServerName`; add `SystemWorker*` fields; set them in `buildMTLS`.
- `internal/temporal/templates/config_template.yaml` — render `systemWorker` block.
- `internal/temporal/configtemplate_test.go` — add focused mTLS rendering assertions.
- `internal/temporal/testdata/golden/1.31/postgres-mtls.yaml` — regenerated golden.
- `internal/controller/temporal_tls.go` (new) — `clusterTLSConfig` helper.
- `internal/controller/temporal_tls_test.go` (new) — helper unit tests.
- `internal/controller/temporalsearchattribute_controller.go` — use the helper.
- `internal/controller/temporalnamespace_controller.go` — use the helper.
- `test/e2e/mtls/03-searchattribute.yaml` + `03-assert.yaml` (new) — e2e coverage.
- `test/e2e/mtls/chainsaw-test.yaml` — add the search-attribute step.
- `.github/workflows/e2e.yml` — `workflow_dispatch` suite input + matrix logic.

**Conventions used by these tasks:**
- Internode service/cert helper names: `FrontendServiceName(name) == name + "-frontend"`, internode CommonName `== name + "-internode"` (`internal/resources/certificates.go:89`, `internal/resources/service.go:32`).
- Internode certs are mounted on **every** pod (incl. worker) at `/etc/temporal/certs/internode` with keys `ca.crt`/`tls.crt`/`tls.key` (`internal/resources/deployment.go:201-209`, `internal/temporal/configtemplate.go:57`).
- Golden config tests use the `-update` flag; the mTLS case is named `postgres-mtls` with cluster `{Name: "test", Namespace: "default"}` (`internal/temporal/configtemplate_test.go:49,71`).
- Commit every task; sign off with `-s` (DCO is enforced) and Conventional Commit prefixes.

---

### Task 1: Internode certificate SAN

**Files:**
- Modify: `internal/resources/certificates.go` (`BuildInternodeCertificate`, around line 75-99)
- Test: `internal/resources/certificates_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/resources/certificates_test.go`:

```go
/*
Copyright 2026 Brian Morton.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func mtlsCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			MTLS: &temporalv1alpha1.MTLSSpec{
				Provider:  "cert-manager",
				IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"},
			},
		},
	}
}

func TestBuildInternodeCertificateIncludesServerNameSAN(t *testing.T) {
	cert := BuildInternodeCertificate(mtlsCluster())
	if !slices.Contains(cert.Spec.DNSNames, "test-internode") {
		t.Fatalf("expected DNSNames to contain stable serverName %q, got %v", "test-internode", cert.Spec.DNSNames)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resources/ -run TestBuildInternodeCertificateIncludesServerNameSAN -v`
Expected: FAIL — DNSNames does not contain `test-internode`.

- [ ] **Step 3: Add the SAN**

In `internal/resources/certificates.go`, in `BuildInternodeCertificate`, after the `for _, svc := range EnabledServices(cluster)` loop that builds `dnsNames`, prepend the stable serverName:

```go
	var dnsNames []string
	// Stable serverName used by internode TLS clients; pod IPs are dynamic and
	// not in the cert, so clients verify against this name instead.
	dnsNames = append(dnsNames, InternodeCertName(cluster.Name))
	for _, svc := range EnabledServices(cluster) {
		dnsNames = append(dnsNames, serviceDNSNames(cluster, svc.Name)...)
	}
```

(`InternodeCertName(cluster.Name)` returns `<cluster>-internode`, matching the cert CommonName.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resources/ -run TestBuildInternodeCertificateIncludesServerNameSAN -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resources/certificates.go internal/resources/certificates_test.go
git commit -s -m "fix(mtls): add stable serverName SAN to internode certificate"
```

---

### Task 2: Populate internode/frontend serverName in rendered config

**Files:**
- Modify: `internal/temporal/configtemplate.go` (`buildMTLS`, around line 320-338)
- Test: `internal/temporal/configtemplate_test.go` (add a focused test)
- Regenerate: `internal/temporal/testdata/golden/1.31/postgres-mtls.yaml`

- [ ] **Step 1: Write the failing test**

Append to `internal/temporal/configtemplate_test.go`:

```go
func TestRenderConfigMTLSServerNames(t *testing.T) {
	c := baseCluster()
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
		Provider:  "cert-manager",
		IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"},
	}
	out, err := RenderClusterConfig(c, BuildOptions{
		BindOnIP:                "0.0.0.0",
		BroadcastAddress:        "10.0.0.1",
		DefaultStorePassword:    "default-pw",
		VisibilityStorePassword: "visibility-pw",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`serverName: "test-internode"`,
		`serverName: "test-frontend.default.svc.cluster.local"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n%s", want, out)
		}
	}
}
```

If `strings` is not already imported in the test file, add it to the import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/temporal/ -run TestRenderConfigMTLSServerNames -v`
Expected: FAIL — serverName values render empty (`serverName: ""`).

- [ ] **Step 3: Populate serverName in buildMTLS**

In `internal/temporal/configtemplate.go`, update `buildMTLS` to set the server names. The struct already declares `InternodeServerName` and `FrontendServerName`:

```go
func buildMTLS(cluster *temporalv1alpha1.TemporalCluster) MTLSConfig {
	if cluster.Spec.MTLS == nil {
		return MTLSConfig{}
	}
	refresh := "720h"
	if cluster.Spec.MTLS.RefreshInterval != nil {
		refresh = cluster.Spec.MTLS.RefreshInterval.Duration.String()
	}
	internodeServerName := cluster.Name + "-internode"
	frontendServerName := fmt.Sprintf("%s-frontend.%s.svc.cluster.local", cluster.Name, cluster.Namespace)
	return MTLSConfig{
		Enabled:             true,
		RefreshInterval:     refresh,
		RequireClientAuth:   true,
		InternodeServerCert: internodeCertDir + "/tls.crt",
		InternodeServerKey:  internodeCertDir + "/tls.key",
		InternodeClientCA:   internodeCertDir + "/ca.crt",
		InternodeServerName: internodeServerName,
		FrontendServerCert:  frontendCertDir + "/tls.crt",
		FrontendServerKey:   frontendCertDir + "/tls.key",
		FrontendServerName:  frontendServerName,
	}
}
```

(`fmt` is already imported in this file.)

- [ ] **Step 4: Run the test and regenerate the golden file**

Run: `go test ./internal/temporal/ -run TestRenderConfigMTLSServerNames -v`
Expected: PASS.

Then regenerate the golden config so the table test stays green:

Run: `go test ./internal/temporal/ -run TestRenderConfigGolden -update`
Then verify: `go test ./internal/temporal/ -run TestRenderConfigGolden -v`
Expected: PASS. Inspect `git diff internal/temporal/testdata/golden/1.31/postgres-mtls.yaml` — only the two `serverName` lines should change (internode + frontend).

- [ ] **Step 5: Commit**

```bash
git add internal/temporal/configtemplate.go internal/temporal/configtemplate_test.go internal/temporal/testdata/golden/1.31/postgres-mtls.yaml
git commit -s -m "fix(mtls): set internode and frontend client serverName"
```

---

### Task 3: Render systemWorker TLS block

**Files:**
- Modify: `internal/temporal/configtemplate.go` (`MTLSConfig` struct ~line 109-121; `buildMTLS`)
- Modify: `internal/temporal/templates/config_template.yaml` (after the `frontend:` TLS block, ~line 103)
- Test: `internal/temporal/configtemplate_test.go`
- Regenerate: `internal/temporal/testdata/golden/1.31/postgres-mtls.yaml`

- [ ] **Step 1: Write the failing test**

Append to `internal/temporal/configtemplate_test.go`:

```go
func TestRenderConfigMTLSSystemWorker(t *testing.T) {
	c := baseCluster()
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
		Provider:  "cert-manager",
		IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"},
	}
	out, err := RenderClusterConfig(c, BuildOptions{
		BindOnIP:                "0.0.0.0",
		BroadcastAddress:        "10.0.0.1",
		DefaultStorePassword:    "default-pw",
		VisibilityStorePassword: "visibility-pw",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"systemWorker:",
		"certFile: /etc/temporal/certs/internode/tls.crt",
		"keyFile: /etc/temporal/certs/internode/tls.key",
		`serverName: "test-frontend.default.svc.cluster.local"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n%s", want, out)
		}
	}

	// Without mTLS there must be no systemWorker block.
	plain, err := RenderClusterConfig(baseCluster(), BuildOptions{
		BindOnIP: "0.0.0.0", BroadcastAddress: "10.0.0.1",
		DefaultStorePassword: "p", VisibilityStorePassword: "p",
	})
	if err != nil {
		t.Fatalf("render plain: %v", err)
	}
	if strings.Contains(plain, "systemWorker:") {
		t.Errorf("non-mtls config must not contain systemWorker block\n%s", plain)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/temporal/ -run TestRenderConfigMTLSSystemWorker -v`
Expected: FAIL — `systemWorker:` not present.

- [ ] **Step 3: Add struct fields and populate them**

In `internal/temporal/configtemplate.go`, extend `MTLSConfig`:

```go
type MTLSConfig struct {
	Enabled             bool
	RefreshInterval     string
	RequireClientAuth   bool
	InternodeServerCert string
	InternodeServerKey  string
	InternodeClientCA   string
	InternodeServerName string
	FrontendServerCert  string
	FrontendServerKey   string
	FrontendServerName  string
	SystemWorkerCert    string
	SystemWorkerKey     string
	SystemWorkerCA      string
	SystemWorkerName    string
}
```

In `buildMTLS`, add the system-worker fields to the returned struct (reusing the internode cert and the frontend serverName):

```go
		FrontendServerName:  frontendServerName,
		SystemWorkerCert:    internodeCertDir + "/tls.crt",
		SystemWorkerKey:     internodeCertDir + "/tls.key",
		SystemWorkerCA:      internodeCertDir + "/ca.crt",
		SystemWorkerName:    frontendServerName,
	}
```

- [ ] **Step 4: Render the block in the template**

In `internal/temporal/templates/config_template.yaml`, inside the `{{- if .MTLS.Enabled }}` ... `{{- end }}` `tls:` section, immediately after the `frontend:` block's closing (after the line `                    - {{ .MTLS.InternodeClientCA }}` that ends the frontend `client.rootCaFiles`, i.e. just before the `{{- end }}` at line ~103), add:

```yaml
        systemWorker:
            certFile: {{ .MTLS.SystemWorkerCert }}
            keyFile: {{ .MTLS.SystemWorkerKey }}
            client:
                serverName: {{ .MTLS.SystemWorkerName | quote }}
                rootCaFiles:
                    - {{ .MTLS.SystemWorkerCA }}
```

Keep the existing indentation (8 spaces for `systemWorker:` to match `internode:`/`frontend:` under `tls:`).

- [ ] **Step 5: Run the test and regenerate the golden file**

Run: `go test ./internal/temporal/ -run TestRenderConfigMTLSSystemWorker -v`
Expected: PASS.

Regenerate golden: `go test ./internal/temporal/ -run TestRenderConfigGolden -update`
Verify: `go test ./internal/temporal/ -v`
Expected: PASS. `git diff` on the golden should show only the added `systemWorker` block.

- [ ] **Step 6: Commit**

```bash
git add internal/temporal/configtemplate.go internal/temporal/templates/config_template.yaml internal/temporal/configtemplate_test.go internal/temporal/testdata/golden/1.31/postgres-mtls.yaml
git commit -s -m "fix(mtls): render systemWorker TLS so the worker connects to the frontend"
```

---

### Task 4: clusterTLSConfig helper

**Files:**
- Create: `internal/controller/temporal_tls.go`
- Test: `internal/controller/temporal_tls_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/controller/temporal_tls_test.go`:

```go
/*
Copyright 2026 Brian Morton.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func selfSignedPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-internode"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"test-internode"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestClusterTLSConfigNilWithoutMTLS(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	cfg, err := clusterTLSConfig(context.Background(), c, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil tls config without mtls, got %v", cfg)
	}
}

func TestClusterTLSConfigBuildsFromInternodeSecret(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-internode", Namespace: "default"},
		Data: map[string][]byte{
			"ca.crt":  certPEM,
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(secret).Build()
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			MTLS: &temporalv1alpha1.MTLSSpec{Provider: "cert-manager"},
		},
	}
	cfg, err := clusterTLSConfig(context.Background(), c, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil tls config")
	}
	if cfg.ServerName != "test-frontend.default.svc.cluster.local" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "test-frontend.default.svc.cluster.local")
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 client certificate, got %d", len(cfg.Certificates))
	}
	if cfg.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
}
```

This test references a `testScheme(t)` helper. If one does not already exist in the `controller` test package, add it to this file:

```go
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := temporalv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}
```

(Add `"k8s.io/apimachinery/pkg/runtime"` to imports if you add the helper. If `testScheme` already exists elsewhere in the package, omit this and remove the duplicate.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/controller/ -run TestClusterTLSConfig -v`
Expected: FAIL — `clusterTLSConfig` undefined.

- [ ] **Step 3: Implement the helper**

Create `internal/controller/temporal_tls.go`:

```go
/*
Copyright 2026 Brian Morton.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// clusterTLSConfig builds the client TLS config the operator uses to reach a
// cluster's frontend. It returns (nil, nil) when the cluster has no mTLS, in
// which case callers dial insecurely. When mTLS is enabled it reuses the
// cluster's internode certificate (clientAuth-capable and CA-trusted by the
// frontend) and verifies the frontend against its stable serverName.
func clusterTLSConfig(ctx context.Context, c client.Client, cluster *temporalv1alpha1.TemporalCluster) (*tls.Config, error) {
	if cluster.Spec.MTLS == nil {
		return nil, nil
	}

	var secret corev1.Secret
	key := types.NamespacedName{Namespace: cluster.Namespace, Name: resources.InternodeCertName(cluster.Name)}
	if err := c.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("reading internode cert secret %s: %w", key, err)
	}

	cert, err := tls.X509KeyPair(secret.Data["tls.crt"], secret.Data["tls.key"])
	if err != nil {
		return nil, fmt.Errorf("parsing client key pair: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(secret.Data["ca.crt"]) {
		return nil, fmt.Errorf("no CA certificates found in %s ca.crt", key)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   fmt.Sprintf("%s.%s.svc.cluster.local", resources.FrontendServiceName(cluster.Name), cluster.Namespace),
		MinVersion:   tls.VersionTLS12,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/controller/ -run TestClusterTLSConfig -v`
Expected: PASS (both cases).

- [ ] **Step 5: Commit**

```bash
git add internal/controller/temporal_tls.go internal/controller/temporal_tls_test.go
git commit -s -m "feat(mtls): add clusterTLSConfig helper for operator-to-frontend mTLS"
```

---

### Task 5: Wire controllers to use clusterTLSConfig

**Files:**
- Modify: `internal/controller/temporalsearchattribute_controller.go:68`
- Modify: `internal/controller/temporalnamespace_controller.go:72`

- [ ] **Step 1: Update the search-attribute controller**

In `internal/controller/temporalsearchattribute_controller.go`, replace the client-build block (currently around line 68):

```go
	sac, err := r.clientFactory()(ctx, frontendAddress(&cluster), nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
```

with:

```go
	tlsConfig, err := clusterTLSConfig(ctx, r.Client, &cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building temporal client tls: %w", err)
	}
	sac, err := r.clientFactory()(ctx, frontendAddress(&cluster), tlsConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
```

- [ ] **Step 2: Update the namespace controller**

In `internal/controller/temporalnamespace_controller.go`, replace the client-build block (around line 72):

```go
	tc, err := r.clientFactory()(ctx, frontendAddress(&cluster), nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
```

with:

```go
	tlsConfig, err := clusterTLSConfig(ctx, r.Client, &cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building temporal client tls: %w", err)
	}
	tc, err := r.clientFactory()(ctx, frontendAddress(&cluster), tlsConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
```

- [ ] **Step 3: Verify the package still builds and existing tests pass**

Run: `go build ./... && go test ./internal/controller/ -v`
Expected: PASS. (Existing controller tests inject a `ClientFactory` that ignores the tlsConfig and use clusters without `spec.mtls`, so `clusterTLSConfig` returns `nil` and behavior is unchanged.)

- [ ] **Step 4: Confirm RBAC is already satisfied**

The aggregated operator role already grants secrets access (`internal/controller/temporalcluster_controller.go:58` has `resources=secrets,verbs=get;list;watch;create;update;patch;delete`), so no new marker is required.

Run: `make manifests` and confirm no diff:
Run: `git diff --stat config/ dist/`
Expected: empty (no RBAC change). If a diff appears, commit the regenerated manifests with the next step.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/temporalsearchattribute_controller.go internal/controller/temporalnamespace_controller.go
git commit -s -m "fix(mtls): connect search-attribute and namespace controllers over mTLS"
```

---

### Task 6: E2E — register a search attribute against the mTLS cluster

**Files:**
- Create: `test/e2e/mtls/03-searchattribute.yaml`
- Create: `test/e2e/mtls/03-assert.yaml`
- Modify: `test/e2e/mtls/chainsaw-test.yaml`

- [ ] **Step 1: Create the search-attribute resource**

Create `test/e2e/mtls/03-searchattribute.yaml`:

```yaml
# A search attribute registered against the mTLS cluster. Exercises the
# operator's controller-to-frontend mTLS path.
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalSearchAttribute
metadata:
  name: mtls-customer-id
spec:
  clusterRef:
    name: temporal-mtls
  namespace: default
  name: CustomerId
  type: Keyword
```

- [ ] **Step 2: Create the assertion**

Create `test/e2e/mtls/03-assert.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalSearchAttribute
metadata:
  name: mtls-customer-id
status:
  registered: true
  (conditions[?type == 'Ready'].status | [0]): "True"
```

- [ ] **Step 3: Add the step to the chainsaw test**

In `test/e2e/mtls/chainsaw-test.yaml`, append a new step after the `client-credentials` step (same indentation as the other `- name:` steps):

```yaml
    - name: register-search-attribute
      try:
        - apply:
            file: 03-searchattribute.yaml
        - assert:
            file: 03-assert.yaml
      catch:
        - describe:
            apiVersion: temporal.bmor10.com/v1alpha1
            kind: TemporalSearchAttribute
```

- [ ] **Step 4: Validate the chainsaw test parses**

Run: `./bin/chainsaw lint --test-dir test/e2e/mtls` (if `bin/chainsaw` is absent, run `make install-tools` first).
Expected: no parse/lint errors. (Full e2e execution happens in CI — see Task 7.)

- [ ] **Step 5: Commit**

```bash
git add test/e2e/mtls/03-searchattribute.yaml test/e2e/mtls/03-assert.yaml test/e2e/mtls/chainsaw-test.yaml
git commit -s -m "test(e2e): register a search attribute against the mTLS cluster"
```

---

### Task 7: CI — on-demand e2e suite selection

**Files:**
- Modify: `.github/workflows/e2e.yml` (the `on:` block and the `Compute matrix` step)

- [ ] **Step 1: Add the workflow_dispatch input**

In `.github/workflows/e2e.yml`, replace `  workflow_dispatch: {}` with:

```yaml
  workflow_dispatch:
    inputs:
      suite:
        description: Which suite(s) to run
        type: choice
        options: [default, mtls, upgrade, all]
        default: default
```

- [ ] **Step 2: Update the matrix logic**

Replace the `Compute matrix` step's `run:` script with logic that honors the input:

```yaml
      - id: set
        env:
          EVENT: ${{ github.event_name }}
          SUITE: ${{ github.event.inputs.suite }}
        run: |
          postgres='{"temporal":"1.31.1","persistence":"postgres","suite":"postgres/lifecycle"}'
          mtls='{"temporal":"1.31.1","persistence":"mtls","suite":"mtls"}'
          upgrade='{"temporal":"1.30.0","persistence":"upgrade","suite":"upgrade"}'
          if [ "$EVENT" = "schedule" ]; then
            echo "combos=[$postgres,$mtls,$upgrade]" >> "$GITHUB_OUTPUT"
          elif [ "$EVENT" = "workflow_dispatch" ]; then
            case "$SUITE" in
              mtls)    echo "combos=[$mtls]" >> "$GITHUB_OUTPUT" ;;
              upgrade) echo "combos=[$upgrade]" >> "$GITHUB_OUTPUT" ;;
              all)     echo "combos=[$postgres,$mtls,$upgrade]" >> "$GITHUB_OUTPUT" ;;
              *)       echo "combos=[$postgres]" >> "$GITHUB_OUTPUT" ;;
            esac
          else
            echo "combos=[$postgres]" >> "$GITHUB_OUTPUT"
          fi
```

- [ ] **Step 3: Validate the workflow YAML**

Run: `python -c "import yaml,sys; yaml.safe_load(open('.github/workflows/e2e.yml'))" && echo OK`
Expected: `OK` (valid YAML).

If `actionlint` is available, also run `actionlint .github/workflows/e2e.yml` and expect no errors.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/e2e.yml
git commit -s -m "ci(e2e): add workflow_dispatch suite input to run mtls on demand"
```

---

### Task 8: Full verification, generate, and open the PR

**Files:** none (verification + PR)

- [ ] **Step 1: Regenerate deepcopy + manifests**

Run: `make generate manifests`
Then: `git status --porcelain`
Expected: no unexpected changes. If anything regenerated, commit it:

```bash
git add -A
git commit -s -m "chore: regenerate manifests"
```

(Only commit if there is a diff.)

- [ ] **Step 2: Build**

Run: `make build`
Expected: compiles cleanly.

- [ ] **Step 3: Full test suite**

Run: `make test`
Expected: all unit + envtest suites pass.

- [ ] **Step 4: Lint**

Run: `make lint`
Expected: no findings. Fix any issues and amend the relevant commit.

- [ ] **Step 5: Push and open the PR**

```bash
git push -u origin fix/mtls-cluster-health
gh pr create --fill --base main \
  --title "fix(mtls): make mTLS clusters healthy and operator controllers mTLS-aware" \
  --body "$(cat <<'EOF'
## Summary
Enabling `spec.mtls` previously produced a cluster that never became Ready and
silently broke the search-attribute/namespace controllers. This fixes three
rendering gaps:

- Set internode/frontend `client.serverName` and add a matching `<cluster>-internode`
  SAN so cross-node TLS no longer fails IP-SAN verification.
- Render `tls.systemWorker` so the worker role's internal SDK client connects to
  the frontend over TLS instead of plaintext.
- Give the search-attribute and namespace controllers a client `*tls.Config`
  (reusing the internode cert) so they work against mTLS clusters.

Also adds an e2e search-attribute step against the mTLS cluster and a
`workflow_dispatch` input so the mtls suite can be run on demand
(`gh workflow run E2E -f suite=mtls`).

Design: `docs/superpowers/specs/2026-06-16-mtls-cluster-health-design.md`

## Testing
- `make build`, `make test`, `make lint` green.
- New unit tests: internode SAN, rendered serverName + systemWorker block,
  `clusterTLSConfig`.
- e2e mtls suite extended; run via `gh workflow run E2E -f suite=mtls`.
EOF
)"
```

- [ ] **Step 6: Trigger the mTLS e2e to validate end-to-end**

After the PR is open:

Run: `gh workflow run E2E -f suite=mtls --ref fix/mtls-cluster-health`
Then watch: `gh run list --workflow=E2E --branch fix/mtls-cluster-health`
Expected: the `e2e (mtls)` job passes (cluster reaches Ready, client credentials Secret created, search attribute registered).

---

## Self-Review Notes

- **Spec coverage:** Bug 1 (serverName + SAN) → Tasks 1-2; Bug 2 (systemWorker) → Task 3; Scope B (controllers) → Tasks 4-5; e2e (Ready already asserted by existing `01-assert.yaml`, search-attribute added) → Task 6; CI on-demand suite → Task 7. All spec sections mapped.
- **Type consistency:** `MTLSConfig` field names (`InternodeServerName`, `FrontendServerName`, `SystemWorkerCert/Key/CA/Name`) are used identically in `buildMTLS`, the struct, and the template (`.MTLS.SystemWorker*`). `clusterTLSConfig(ctx, client.Client, *TemporalCluster)` signature matches both controller call sites. serverName strings (`test-internode`, `test-frontend.default.svc.cluster.local`) are consistent across cert SAN, config render, and TLS-config tests.
- **No placeholders:** every code/command step is concrete.
