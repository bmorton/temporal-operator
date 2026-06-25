# Microsoft Entra Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a `TemporalCluster` validate Microsoft Entra-issued JWTs on the frontend (service-to-service authn/authz) and require Entra OIDC login on the bundled UI, with typed, Entra-aware config, full docs, and layered tests.

**Architecture:** Extend the existing config-rendering pipeline (`internal/temporal`) to emit Temporal's `global.authorization` block (jwtKeyProvider + claim mapper) from a typed `AuthorizationSpec`, and extend the UI deployment builder (`internal/resources/ui.go`) to emit `TEMPORAL_AUTH_*` env vars from a typed `UIAuthSpec`. Both use stock `temporalio/server` / `temporalio/ui` images and keep raw passthrough escape hatches. A hermetic JWKS-based Chainsaw suite proves enforcement on kind; a `hack/entra-e2e.sh` script provisions a longstanding Entra app for manual real-Entra verification.

**Tech Stack:** Go, controller-runtime/kubebuilder CRDs, Go `text/template` + sprig, `sigs.k8s.io/yaml`, Chainsaw e2e, Helm chart (`dist/chart`), Hugo docs.

## Global Constraints

- Module `github.com/bmorton/temporal-operator`; API group domain `temporal.bmor10.com`; copyright owner "Brian Morton" (use the existing Apache-2.0 header from sibling files verbatim on new `.go` files).
- Go version pinned in `go.mod` (currently `go 1.26.4`). Do not bump.
- Stock `temporalio/server` and `temporalio/ui` images only — no custom claim-mapper plugin / custom image.
- Every commit MUST be signed off: `git commit -s` (DCO enforced in CI).
- Conventional Commits. The `UISpec.Auth` type change is breaking → use a `feat!:` subject (or `BREAKING CHANGE:` footer). Append `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>` to every commit message.
- Append the trailer with a blank line before it.
- Do NOT hand-edit version numbers or `CHANGELOG.md` (release-please owns them).
- Do NOT run `make helm-chart` (destructive); hand-edit `dist/chart/...`.
- After any `api/v1alpha1` change run `make generate manifests api-docs docs-crd-reference` and commit the regenerated `config/crd/...`, `docs/api/v1alpha1.md`, `docs/content/reference/_index.md`, and propagate CRD schema into `dist/chart/templates/crd/` (kind/CI e2e installs via Helm from `dist/chart`).
- Reuse the existing `SecretKeyReference` type (`api/v1alpha1/persistence_types.go:219`, fields `Name`, `Key`) for secret references.
- Run `make build`, `make test`, `make lint` before opening the PR.
- Work on branch `feat/entra-authentication` (already created); open a PR, do not push to `main`.

---

## File Structure

- `api/v1alpha1/shared_types.go` — extend `AuthorizationSpec`; add `JWTKeyProviderSpec`, `EntraAuthSpec`; change `UISpec.Auth` to `*UIAuthSpec`; add `UIAuthSpec`, `EntraUIAuthSpec`.
- `api/v1alpha1/zz_generated.deepcopy.go` — regenerated (`make generate`).
- `internal/temporal/configtemplate.go` — extend `AuthConfig`, rewrite `buildAuth`, add a `toYaml` template func.
- `internal/temporal/templates/config_template.yaml` — extend the `authorization:` block.
- `internal/temporal/configtemplate_test.go` — golden tests for the authorization block.
- `internal/resources/ui.go` — emit `TEMPORAL_AUTH_*` env vars.
- `internal/resources/ui_test.go` — new test file for UI auth env.
- `internal/webhook/v1alpha1/temporalcluster_webhook.go` — validation rules.
- `internal/webhook/v1alpha1/temporalcluster_webhook_test.go` — validation tests.
- `config/crd/...`, `dist/chart/templates/crd/...`, `docs/api/v1alpha1.md`, `docs/content/reference/_index.md` — regenerated/propagated.
- `examples/cluster-entra-auth/` — `temporalcluster.yaml`, `onepassworditem.yaml`, `README.md`.
- `test/e2e/entra-auth/` — hermetic JWKS Chainsaw suite + fixtures.
- `hack/entra-e2e.sh`, `Makefile` — longstanding Entra app script + targets.
- `docs/content/operations/authentication.md`, `docs/content/installation/azure.md` — guide + cross-link.

---

### Task 1: Server authorization API types + config rendering

**Files:**
- Modify: `api/v1alpha1/shared_types.go` (AuthorizationSpec ~line 193; add new types)
- Modify: `internal/temporal/configtemplate.go` (AuthConfig ~134, buildAuth ~352, RenderConfig funcs ~441)
- Modify: `internal/temporal/templates/config_template.yaml` (authorization block ~117-121)
- Test: `internal/temporal/configtemplate_test.go`
- Regenerate: `api/v1alpha1/zz_generated.deepcopy.go` via `make generate`

**Interfaces:**
- Produces (API): `AuthorizationSpec{ Authorizer, ClaimMapper, PermissionsClaimName string; JWTKeyProvider *JWTKeyProviderSpec; Entra *EntraAuthSpec; Config *runtime.RawExtension }`; `JWTKeyProviderSpec{ KeySourceURIs []string; RefreshInterval *metav1.Duration }`; `EntraAuthSpec{ TenantID string }`.
- Produces (Go): `AuthConfig{ Authorizer, ClaimMapper, PermissionsClaimName, RefreshInterval string; KeySourceURIs []string; ExtraConfig map[string]interface{} }`; `buildAuth(*TemporalCluster) (*AuthConfig, error)`.
- Consumed by: Task 3 (webhook reads the same spec fields), Task 4 (manifests), Task 5/6/8 (YAML using these fields).

- [ ] **Step 1: Write the failing test**

Add to `internal/temporal/configtemplate_test.go` (it already imports `temporalv1alpha1`, `testing`, and `sigs.k8s.io/yaml`; add `"strings"` and `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` if not present):

```go
func TestRenderConfig_AuthorizationEntra(t *testing.T) {
	cluster := baseCluster() // existing helper; if absent, build a *temporalv1alpha1.TemporalCluster inline like other tests
	cluster.Spec.Authorization = &temporalv1alpha1.AuthorizationSpec{
		Entra: &temporalv1alpha1.EntraAuthSpec{TenantID: "11111111-2222-3333-4444-555555555555"},
	}

	out, err := temporal.RenderClusterConfig(cluster, temporal.BuildOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		"authorization:",
		`authorizer: "default"`,
		`claimMapper: "default"`,
		`permissionsClaimName: "roles"`,
		"jwtKeyProvider:",
		"https://login.microsoftonline.com/11111111-2222-3333-4444-555555555555/discovery/v2.0/keys",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderConfig_AuthorizationExplicitAndPassthrough(t *testing.T) {
	cluster := baseCluster()
	cluster.Spec.Authorization = &temporalv1alpha1.AuthorizationSpec{
		JWTKeyProvider: &temporalv1alpha1.JWTKeyProviderSpec{
			KeySourceURIs:   []string{"https://example.test/jwks"},
			RefreshInterval: &metav1.Duration{Duration: 2 * time.Minute},
		},
		Config: &runtime.RawExtension{Raw: []byte(`{"permissionsClaimName":"perms"}`)},
	}

	out, err := temporal.RenderClusterConfig(cluster, temporal.BuildOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"https://example.test/jwks",
		`refreshInterval: "2m0s"`,
		"permissionsClaimName: perms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, out)
		}
	}
}
```

> If `baseCluster()` does not exist, copy the inline cluster construction used by the nearest existing `TestRenderConfig_*` test in this file and add `Authorization` to it. Add imports `"time"`, `"k8s.io/apimachinery/pkg/runtime"`, and `metav1` as needed.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/temporal/ -run TestRenderConfig_Authorization -v`
Expected: compile error (unknown fields `Entra`, `JWTKeyProvider`, `PermissionsClaimName`) — confirms the types are not yet defined.

- [ ] **Step 3: Add the API types**

In `api/v1alpha1/shared_types.go`, replace the existing `AuthorizationSpec` (currently at ~line 193) with:

```go
// AuthorizationSpec configures the frontend authorizer, claim mapper, and JWT
// key provider used to validate inbound bearer tokens.
type AuthorizationSpec struct {
	// Authorizer is the Temporal authorizer plugin. Defaults to "default" when
	// JWT validation is configured. Use "" for the no-op (allow-all) authorizer.
	// +optional
	Authorizer string `json:"authorizer,omitempty"`
	// ClaimMapper is the Temporal claim mapper. Defaults to "default" when JWT
	// validation is configured.
	// +optional
	ClaimMapper string `json:"claimMapper,omitempty"`
	// PermissionsClaimName maps to global.authorization.permissionsClaimName.
	// Defaults to "roles" when Entra is set, otherwise "permissions".
	// +optional
	PermissionsClaimName string `json:"permissionsClaimName,omitempty"`
	// JWTKeyProvider configures JWKS-based token signature validation.
	// +optional
	JWTKeyProvider *JWTKeyProviderSpec `json:"jwtKeyProvider,omitempty"`
	// Entra derives the Entra JWKS keySourceURI from a tenant ID and applies
	// sensible JWT defaults.
	// +optional
	Entra *EntraAuthSpec `json:"entra,omitempty"`
	// Config is a passthrough merged into the authorization block for any knob
	// not modeled above.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config *runtime.RawExtension `json:"config,omitempty"`
}

// JWTKeyProviderSpec configures JWKS-based JWT validation.
type JWTKeyProviderSpec struct {
	// KeySourceURIs are JWKS endpoints used to validate token signatures.
	// +optional
	KeySourceURIs []string `json:"keySourceURIs,omitempty"`
	// RefreshInterval controls how often keys are refreshed, e.g. "1m".
	// +optional
	RefreshInterval *metav1.Duration `json:"refreshInterval,omitempty"`
}

// EntraAuthSpec is a Microsoft Entra convenience for server JWT validation.
type EntraAuthSpec struct {
	// TenantID is the Entra (Azure AD) tenant. Derives the JWKS keySourceURI
	// https://login.microsoftonline.com/{tenantID}/discovery/v2.0/keys.
	// +kubebuilder:validation:MinLength=1
	TenantID string `json:"tenantID"`
}
```

`metav1` and `runtime` are already imported in `shared_types.go` (verify the import block; both are used by sibling types).

- [ ] **Step 4: Add the `toYaml` template func and extend `AuthConfig` + `buildAuth`**

In `internal/temporal/configtemplate.go`:

(a) Add `"sigs.k8s.io/yaml"` to imports if not present (it is used in this package's tests but confirm the non-test file imports it; add if missing).

(b) Replace `AuthConfig` (~line 134) with:

```go
// AuthConfig holds resolved authorization settings.
type AuthConfig struct {
	Authorizer           string
	ClaimMapper          string
	PermissionsClaimName string
	KeySourceURIs        []string
	RefreshInterval      string
	ExtraConfig          map[string]interface{}
}
```

(c) Replace `buildAuth` (~line 352) with:

```go
func buildAuth(cluster *temporalv1alpha1.TemporalCluster) (*AuthConfig, error) {
	auth := cluster.Spec.Authorization
	if auth == nil {
		return nil, nil
	}

	cfg := &AuthConfig{
		Authorizer:           auth.Authorizer,
		ClaimMapper:          auth.ClaimMapper,
		PermissionsClaimName: auth.PermissionsClaimName,
	}
	if auth.JWTKeyProvider != nil {
		cfg.KeySourceURIs = append(cfg.KeySourceURIs, auth.JWTKeyProvider.KeySourceURIs...)
		if auth.JWTKeyProvider.RefreshInterval != nil {
			cfg.RefreshInterval = auth.JWTKeyProvider.RefreshInterval.Duration.String()
		}
	}
	if auth.Entra != nil {
		cfg.KeySourceURIs = append(cfg.KeySourceURIs,
			fmt.Sprintf("https://login.microsoftonline.com/%s/discovery/v2.0/keys", auth.Entra.TenantID))
		if cfg.PermissionsClaimName == "" {
			cfg.PermissionsClaimName = "roles"
		}
	}

	jwtConfigured := len(cfg.KeySourceURIs) > 0
	if jwtConfigured {
		if cfg.Authorizer == "" {
			cfg.Authorizer = "default"
		}
		if cfg.ClaimMapper == "" {
			cfg.ClaimMapper = "default"
		}
		if cfg.PermissionsClaimName == "" {
			cfg.PermissionsClaimName = "permissions"
		}
	}

	if auth.Config != nil && len(auth.Config.Raw) > 0 {
		extra := map[string]interface{}{}
		if err := yaml.Unmarshal(auth.Config.Raw, &extra); err != nil {
			return nil, fmt.Errorf("authorization.config: %w", err)
		}
		cfg.ExtraConfig = extra
	}

	if cfg.Authorizer == "" && cfg.ClaimMapper == "" && !jwtConfigured && cfg.ExtraConfig == nil {
		return nil, nil
	}
	return cfg, nil
}
```

(d) In `BuildConfigData` (~line 409) change `Authorization: buildAuth(cluster),` to:

```go
	auth, err := buildAuth(cluster)
	if err != nil {
		return nil, fmt.Errorf("authorization: %w", err)
	}
```

and set `Authorization: auth,` in the `ConfigData` literal (move the assignment after the literal if cleaner, mirroring how `defaultStore`/`visStore` errors are handled earlier in the function).

(e) In `RenderConfig` (~line 441) register a `toYaml` func after the sprig map is built:

```go
	funcs["toYaml"] = func(v interface{}) (string, error) {
		out, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(out), "\n"), nil
	}
```

Add `"strings"` to the file imports if not present.

- [ ] **Step 5: Extend the config template authorization block**

In `internal/temporal/templates/config_template.yaml` replace the block at lines 117-121:

```yaml
{{- if .Authorization }}
    authorization:
{{- if .Authorization.Authorizer }}
        authorizer: {{ .Authorization.Authorizer | quote }}
{{- end }}
{{- if .Authorization.ClaimMapper }}
        claimMapper: {{ .Authorization.ClaimMapper | quote }}
{{- end }}
{{- if .Authorization.PermissionsClaimName }}
        permissionsClaimName: {{ .Authorization.PermissionsClaimName | quote }}
{{- end }}
{{- if .Authorization.KeySourceURIs }}
        jwtKeyProvider:
            keySourceURIs:
{{- range .Authorization.KeySourceURIs }}
                - {{ . }}
{{- end }}
{{- if .Authorization.RefreshInterval }}
            refreshInterval: {{ .Authorization.RefreshInterval | quote }}
{{- end }}
{{- end }}
{{- if .Authorization.ExtraConfig }}
{{ toYaml .Authorization.ExtraConfig | indent 8 }}
{{- end }}
{{- end }}
```

- [ ] **Step 6: Regenerate deepcopy and run the tests**

Run: `make generate && go test ./internal/temporal/ -run TestRenderConfig_Authorization -v`
Expected: PASS for both new tests.

- [ ] **Step 7: Run the full package tests to check no regression**

Run: `go test ./internal/temporal/ ./api/...`
Expected: PASS (existing `TestRenderConfig_*` that set `Authorization.Authorizer` still render, now gated per-field).

> If any pre-existing authorization test asserted both `authorizer` and `claimMapper` are always emitted even when empty, update it to match the new per-field gating.

- [ ] **Step 8: Commit**

```bash
git add api/v1alpha1/shared_types.go api/v1alpha1/zz_generated.deepcopy.go internal/temporal/
git commit -s -m "$(printf 'feat(auth): render Entra/JWT authorization in server config\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>')"
```

---

### Task 2: UI OIDC API type + UI deployment env rendering

**Files:**
- Modify: `api/v1alpha1/shared_types.go` (UISpec.Auth ~line 126-129; add new types)
- Modify: `internal/resources/ui.go` (`uiEnv` ~line 56)
- Create: `internal/resources/ui_test.go`
- Regenerate: `api/v1alpha1/zz_generated.deepcopy.go` via `make generate`

**Interfaces:**
- Produces (API): `UISpec.Auth *UIAuthSpec`; `UIAuthSpec{ Enabled bool; Entra *EntraUIAuthSpec; ProviderURL, ClientID string; ClientSecretRef *SecretKeyReference; Scopes []string; CallbackURL string; ExtraEnv *runtime.RawExtension }`; `EntraUIAuthSpec{ TenantID string }`.
- Consumed by: Task 3 (webhook), Task 4 (manifests), Task 5 (example).

- [ ] **Step 1: Write the failing test**

Create `internal/resources/ui_test.go` (use the Apache header from `internal/resources/ui.go` lines 1-15 verbatim):

```go
package resources_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

func uiAuthCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metaObjectMeta("entra", "default"), // see note below
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version: "1.31.1",
			UI: &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth: &temporalv1alpha1.UIAuthSpec{
					Enabled:         true,
					Entra:           &temporalv1alpha1.EntraUIAuthSpec{TenantID: "tenant-abc"},
					ClientID:        "client-123",
					ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "ui-oidc", Key: "client-secret"},
					CallbackURL:     "https://temporal.example.test/auth/sso/callback",
				},
			},
		},
	}
}

func envByName(env []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, e := range env {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

func TestBuildUIDeployment_EntraAuthEnv(t *testing.T) {
	dep := resources.BuildUIDeployment(uiAuthCluster())
	env := dep.Spec.Template.Spec.Containers[0].Env

	cases := map[string]string{
		"TEMPORAL_AUTH_ENABLED":      "true",
		"TEMPORAL_AUTH_TYPE":         "oidc",
		"TEMPORAL_AUTH_PROVIDER_URL": "https://login.microsoftonline.com/tenant-abc/v2.0",
		"TEMPORAL_AUTH_CLIENT_ID":    "client-123",
		"TEMPORAL_AUTH_SCOPES":       "openid,profile,email",
		"TEMPORAL_AUTH_CALLBACK_URL": "https://temporal.example.test/auth/sso/callback",
	}
	for name, want := range cases {
		e, ok := envByName(env, name)
		if !ok {
			t.Fatalf("missing env %s", name)
		}
		if e.Value != want {
			t.Errorf("%s = %q, want %q", name, e.Value, want)
		}
	}

	secret, ok := envByName(env, "TEMPORAL_AUTH_CLIENT_SECRET")
	if !ok || secret.ValueFrom == nil || secret.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("TEMPORAL_AUTH_CLIENT_SECRET should use a secretKeyRef")
	}
	if secret.ValueFrom.SecretKeyRef.Name != "ui-oidc" || secret.ValueFrom.SecretKeyRef.Key != "client-secret" {
		t.Errorf("secretKeyRef = %+v", secret.ValueFrom.SecretKeyRef)
	}
}

func TestBuildUIDeployment_NoAuthByDefault(t *testing.T) {
	c := uiAuthCluster()
	c.Spec.UI.Auth = nil
	dep := resources.BuildUIDeployment(c)
	if _, ok := envByName(dep.Spec.Template.Spec.Containers[0].Env, "TEMPORAL_AUTH_ENABLED"); ok {
		t.Errorf("auth env should be absent when UI.Auth is nil")
	}
}
```

> `metaObjectMeta` is illustrative. Use the same `metav1.ObjectMeta{Name: ..., Namespace: ...}` construction that existing `internal/resources/*_test.go` files use (e.g. `builders_test.go`); import `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` and replace the helper call with a literal.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/resources/ -run TestBuildUIDeployment_EntraAuthEnv -v`
Expected: compile error (unknown field `UIAuthSpec`) — confirms types not yet defined.

- [ ] **Step 3: Add the UI auth API types**

In `api/v1alpha1/shared_types.go`, change the `UISpec.Auth` field (lines 126-129) from:

```go
	// Auth is a passthrough for temporal-ui authentication config.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Auth *runtime.RawExtension `json:"auth,omitempty"`
```

to:

```go
	// Auth configures temporal-ui authentication (OIDC, e.g. Microsoft Entra).
	// +optional
	Auth *UIAuthSpec `json:"auth,omitempty"`
```

Add these types near `UISpec`:

```go
// UIAuthSpec configures temporal-ui OIDC authentication.
type UIAuthSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// Entra derives ProviderURL from a Microsoft Entra tenant ID.
	// +optional
	Entra *EntraUIAuthSpec `json:"entra,omitempty"`
	// ProviderURL is the OIDC issuer URL (set directly or via Entra).
	// +optional
	ProviderURL string `json:"providerURL,omitempty"`
	// +optional
	ClientID string `json:"clientID,omitempty"`
	// ClientSecretRef references a Secret key holding the OIDC client secret.
	// +optional
	ClientSecretRef *SecretKeyReference `json:"clientSecretRef,omitempty"`
	// Scopes default to ["openid", "profile", "email"].
	// +optional
	Scopes []string `json:"scopes,omitempty"`
	// +optional
	CallbackURL string `json:"callbackURL,omitempty"`
	// ExtraEnv is a passthrough of additional temporal-ui auth env vars
	// (map of string to string).
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	ExtraEnv *runtime.RawExtension `json:"extraEnv,omitempty"`
}

// EntraUIAuthSpec is a Microsoft Entra convenience for UI OIDC login.
type EntraUIAuthSpec struct {
	// +kubebuilder:validation:MinLength=1
	TenantID string `json:"tenantID"`
}
```

- [ ] **Step 4: Render the auth env vars in `uiEnv`**

In `internal/resources/ui.go`, add `"sigs.k8s.io/yaml"` and `"strings"` to imports, then append before `return env` in `uiEnv` (~line 75):

```go
	if cluster.Spec.UI != nil && cluster.Spec.UI.Auth != nil && cluster.Spec.UI.Auth.Enabled {
		a := cluster.Spec.UI.Auth
		providerURL := a.ProviderURL
		if a.Entra != nil {
			providerURL = fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", a.Entra.TenantID)
		}
		scopes := a.Scopes
		if len(scopes) == 0 {
			scopes = []string{"openid", "profile", "email"}
		}
		env = append(env,
			corev1.EnvVar{Name: "TEMPORAL_AUTH_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_TYPE", Value: "oidc"},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_PROVIDER_URL", Value: providerURL},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_CLIENT_ID", Value: a.ClientID},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_SCOPES", Value: strings.Join(scopes, ",")},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_CALLBACK_URL", Value: a.CallbackURL},
		)
		if a.ClientSecretRef != nil {
			key := a.ClientSecretRef.Key
			if key == "" {
				key = "password"
			}
			env = append(env, corev1.EnvVar{
				Name: "TEMPORAL_AUTH_CLIENT_SECRET",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: a.ClientSecretRef.Name},
						Key:                  key,
					},
				},
			})
		}
		if a.ExtraEnv != nil && len(a.ExtraEnv.Raw) > 0 {
			extra := map[string]string{}
			if err := yaml.Unmarshal(a.ExtraEnv.Raw, &extra); err == nil {
				for k, v := range extra {
					env = append(env, corev1.EnvVar{Name: k, Value: v})
				}
			}
		}
	}
```

- [ ] **Step 5: Regenerate deepcopy and run the tests**

Run: `make generate && go test ./internal/resources/ -run TestBuildUIDeployment -v`
Expected: PASS.

- [ ] **Step 6: Run the package tests to check no regression**

Run: `go test ./internal/resources/ ./api/...`
Expected: PASS.

> The `UISpec.Auth` type change may break a deepcopy reference or any test that set `Auth: &runtime.RawExtension{...}`. Search and update: `grep -rn "UI.*Auth\|\.Auth" internal/ api/ | grep -i auth`. Fix any `runtime.RawExtension` UI.Auth literals to the new `*UIAuthSpec`.

- [ ] **Step 7: Commit**

```bash
git add api/v1alpha1/shared_types.go api/v1alpha1/zz_generated.deepcopy.go internal/resources/ui.go internal/resources/ui_test.go
git commit -s -m "$(printf 'feat(ui)!: configure temporal-ui Entra OIDC login\n\nReplaces the unused UISpec.Auth passthrough with a typed UIAuthSpec.\n\nBREAKING CHANGE: spec.ui.auth is now a typed object, not raw YAML.\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>')"
```

---

### Task 3: Webhook validation

**Files:**
- Modify: `internal/webhook/v1alpha1/temporalcluster_webhook.go` (`validateSpec` ~line 123)
- Modify: `internal/webhook/v1alpha1/temporalcluster_webhook_test.go`

**Interfaces:**
- Consumes: `AuthorizationSpec`, `UIAuthSpec` from Tasks 1-2.
- Produces: validation errors appended in `validateSpec`.

- [ ] **Step 1: Write the failing test**

Add to `internal/webhook/v1alpha1/temporalcluster_webhook_test.go` (match the existing test style in that file for constructing a cluster + calling `ValidateCreate`):

```go
func TestValidate_UIAuthRequiresClientFields(t *testing.T) {
	v := &TemporalClusterCustomValidator{}
	c := validBaseCluster() // existing helper or inline minimal valid cluster
	c.Spec.UI = &temporalv1alpha1.UISpec{
		Enabled: true,
		Auth:    &temporalv1alpha1.UIAuthSpec{Enabled: true}, // missing clientID/secret/callback
	}
	if _, err := v.ValidateCreate(context.Background(), c); err == nil {
		t.Fatalf("expected validation error for incomplete ui.auth")
	}
}

func TestValidate_UIAuthValid(t *testing.T) {
	v := &TemporalClusterCustomValidator{}
	c := validBaseCluster()
	c.Spec.UI = &temporalv1alpha1.UISpec{
		Enabled: true,
		Auth: &temporalv1alpha1.UIAuthSpec{
			Enabled:         true,
			Entra:           &temporalv1alpha1.EntraUIAuthSpec{TenantID: "t"},
			ClientID:        "c",
			ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "s", Key: "k"},
			CallbackURL:     "https://x/cb",
		},
	}
	if _, err := v.ValidateCreate(context.Background(), c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

> If `validBaseCluster()` doesn't exist, build a minimal valid `*temporalv1alpha1.TemporalCluster` inline copying the construction used by an existing passing test in this file. Ensure `context` and `temporalv1alpha1` are imported.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/webhook/... -run TestValidate_UIAuth -v`
Expected: FAIL (`TestValidate_UIAuthRequiresClientFields` gets no error).

- [ ] **Step 3: Add validation in `validateSpec`**

In `internal/webhook/v1alpha1/temporalcluster_webhook.go`, inside `validateSpec` (before `return errs`), append:

```go
	if cluster.Spec.UI != nil && cluster.Spec.UI.Auth != nil && cluster.Spec.UI.Auth.Enabled {
		a := cluster.Spec.UI.Auth
		base := field.NewPath("spec", "ui", "auth")
		if a.ClientID == "" {
			errs = append(errs, field.Required(base.Child("clientID"), "clientID is required when ui.auth.enabled"))
		}
		if a.ClientSecretRef == nil || a.ClientSecretRef.Name == "" {
			errs = append(errs, field.Required(base.Child("clientSecretRef"), "clientSecretRef is required when ui.auth.enabled"))
		}
		if a.CallbackURL == "" {
			errs = append(errs, field.Required(base.Child("callbackURL"), "callbackURL is required when ui.auth.enabled"))
		}
		if a.Entra == nil && a.ProviderURL == "" {
			errs = append(errs, field.Required(base.Child("providerURL"), "set ui.auth.entra.tenantID or ui.auth.providerURL"))
		}
	}
```

> Confirm the local error slice is named `errs` and the `field` package is imported (it is — `field.ErrorList` is the return type of `validateSpec`). Match the existing append style used for `mtls` validation in the same function.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/webhook/... -run TestValidate_UIAuth -v`
Expected: PASS.

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/webhook/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/webhook/v1alpha1/
git commit -s -m "$(printf 'feat(webhook): validate ui.auth OIDC required fields\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>')"
```

---

### Task 4: Regenerate manifests, CRD docs, and propagate to the Helm chart

**Files:**
- Regenerate: `config/crd/bases/temporal.bmor10.com_temporalclusters.yaml`, `docs/api/v1alpha1.md`, `docs/content/reference/_index.md`
- Modify (by hand): `dist/chart/templates/crd/temporalclusters.temporal.bmor10.com.yaml`

**Interfaces:** none (generated artifacts).

- [ ] **Step 1: Regenerate**

Run: `make manifests api-docs docs-crd-reference`
Expected: `config/crd/...`, `docs/api/v1alpha1.md`, `docs/content/reference/_index.md` now contain `authorization.entra`, `jwtKeyProvider`, `permissionsClaimName`, and the typed `ui.auth` fields.

- [ ] **Step 2: Propagate the CRD schema into the Helm chart**

The kind/CI e2e installs CRDs from `dist/chart`. Copy the new `spec.authorization` and `spec.ui.auth` schema sub-trees from `config/crd/bases/temporal.bmor10.com_temporalclusters.yaml` into `dist/chart/templates/crd/temporalclusters.temporal.bmor10.com.yaml`, matching that file's existing indentation (4-space) and ordering. Do NOT run `make helm-chart`.

Verify the two files agree on the auth schema:

```bash
diff <(sed -n '/authorization:/,/clusterMetadata:/p' config/crd/bases/temporal.bmor10.com_temporalclusters.yaml) \
     <(sed -n '/authorization:/,/clusterMetadata:/p' dist/chart/templates/crd/temporalclusters.temporal.bmor10.com.yaml) || true
```

Resolve differences so the auth/ui-auth fields match (indentation may legitimately differ; field presence must not).

- [ ] **Step 3: Confirm no generation drift**

Run: `make generate manifests && git status --porcelain`
Expected: no unexpected modified files beyond what you've staged (clean generate).

- [ ] **Step 4: Commit**

```bash
git add config/crd dist/chart docs/api/v1alpha1.md docs/content/reference/_index.md
git commit -s -m "$(printf 'docs(crd): regenerate manifests and reference for Entra auth\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>')"
```

---

### Task 5: Example — `examples/cluster-entra-auth`

**Files:**
- Create: `examples/cluster-entra-auth/temporalcluster.yaml`
- Create: `examples/cluster-entra-auth/onepassworditem.yaml`
- Create: `examples/cluster-entra-auth/README.md`

**Interfaces:** Consumes the CRD fields from Tasks 1-2.

- [ ] **Step 1: Create the TemporalCluster example**

`examples/cluster-entra-auth/temporalcluster.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: entra-auth
spec:
  version: "1.31.1"
  numHistoryShards: 512
  persistence:
    defaultStore:
      sql:
        pluginName: postgres12
        databaseName: temporal
        connectAddr: temporal-pg-rw:5432
        user: temporal
        passwordSecretRef:
          name: temporal-pg-app
          key: password
    visibilityStore:
      sql:
        pluginName: postgres12
        databaseName: temporal_visibility
        connectAddr: temporal-pg-rw:5432
        user: temporal
        passwordSecretRef:
          name: temporal-pg-app
          key: password
  # Service-to-service: validate Entra-issued JWTs and enforce per-namespace
  # roles from the "roles" claim (Entra app roles).
  authorization:
    entra:
      tenantID: "00000000-0000-0000-0000-000000000000"
  ui:
    enabled: true
    ingress:
      enabled: true
      host: temporal.example.com
    # Human login via Entra OIDC.
    auth:
      enabled: true
      entra:
        tenantID: "00000000-0000-0000-0000-000000000000"
      clientID: "11111111-1111-1111-1111-111111111111"
      clientSecretRef:
        name: temporal-ui-oidc
        key: client-secret
      callbackURL: https://temporal.example.com/auth/sso/callback
```

- [ ] **Step 2: Create the OnePasswordItem for the UI client secret**

`examples/cluster-entra-auth/onepassworditem.yaml` (mirror the structure in `dist/chart/templates/onepassworditem.yaml` / existing examples):

```yaml
apiVersion: onepassword.com/v1
kind: OnePasswordItem
metadata:
  name: temporal-ui-oidc
spec:
  itemPath: "vaults/Kubernetes/items/temporal-ui-oidc"
```

> The synced Secret name matches `metadata.name` (`temporal-ui-oidc`), and the 1Password item must contain a `client-secret` field.

- [ ] **Step 3: Create the README**

`examples/cluster-entra-auth/README.md` — a short doc: prerequisites (an Entra app registration with app roles + a client secret in 1Password), `kubectl apply` steps, and a pointer to `docs/content/operations/authentication.md`. Keep it consistent in tone/length with `examples/cluster-azure-workload-identity/README.md`.

- [ ] **Step 4: Validate the YAML parses against the CRD (dry-run if a cluster is handy, else yaml lint)**

Run: `python -c "import yaml,sys; [list(yaml.safe_load_all(open(f))) for f in ['examples/cluster-entra-auth/temporalcluster.yaml','examples/cluster-entra-auth/onepassworditem.yaml']]; print('ok')"`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add examples/cluster-entra-auth/
git commit -s -m "$(printf 'docs(examples): add cluster-entra-auth example\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>')"
```

---

### Task 6: Hermetic JWKS server-JWT e2e suite

**Files:**
- Create: `test/e2e/entra-auth/chainsaw-test.yaml`
- Create: `test/e2e/entra-auth/01-jwks.yaml` (nginx serving a static JWKS from a ConfigMap)
- Create: `test/e2e/entra-auth/02-temporalcluster.yaml`
- Create: `test/e2e/entra-auth/02-assert.yaml`
- Create: `test/e2e/entra-auth/03-authz-checks.yaml` (CLI pods: valid token accepted, missing token rejected)
- Create: `test/e2e/entra-auth/fixtures/README.md` (how the keypair/tokens were generated)
- Create: `test/e2e/entra-auth/fixtures/gen.go` or `gen.sh` (deterministic generator) and the generated `jwks.json`, `token-valid.txt`, `token-invalid.txt`

**Interfaces:** Consumes the rendered `authorization.jwtKeyProvider` from Task 1. Mirrors the kind-runnable pattern of `test/e2e/ui/` and the postgres fixtures.

- [ ] **Step 1: Generate the offline keypair, JWKS, and tokens**

Create `test/e2e/entra-auth/fixtures/gen.sh` that, using a committed RSA private key, produces `jwks.json` (public key in JWK form, `kid: e2e`), `token-valid.txt` (header `{"alg":"RS256","kid":"e2e"}`, claims `{"sub":"e2e","exp":4102444800,"roles":["default:read","default:write","temporal-system:admin"]}`), and `token-invalid.txt` (same claims signed with a *different* key). Use a small Go program (`go run gen.go`) since Go is available and deterministic.

Provide the generator as `test/e2e/entra-auth/fixtures/gen.go` (a `//go:build ignore` program using `crypto/rsa`, `crypto/rand` seeded deterministically is not required — generate once and commit the outputs). Commit the resulting `jwks.json`, `token-valid.txt`, `token-invalid.txt`, and the private key PEM used (clearly marked test-only).

Run: `cd test/e2e/entra-auth/fixtures && go run gen.go && cat jwks.json`
Expected: a JWKS JSON with one RSA key, `kid: e2e`; `token-valid.txt` and `token-invalid.txt` written.

> `exp: 4102444800` is 2100-01-01, so tokens never expire during CI. This avoids running a token minter in-cluster.

- [ ] **Step 2: JWKS server manifest**

`test/e2e/entra-auth/01-jwks.yaml`: a `ConfigMap` named `jwks` with `jwks.json` as a key (content pasted from the fixture), an nginx `Deployment` mounting it at `/usr/share/nginx/html/jwks.json`, and a `Service` named `jwks` exposing port 80. Assert readiness in the suite.

- [ ] **Step 3: TemporalCluster manifest pointing at the in-cluster JWKS**

`test/e2e/entra-auth/02-temporalcluster.yaml`: a cluster (reuse `../postgres/01-fixtures-cnpg.yaml` + `../postgres/02-secrets.yaml` like the UI suite) with:

```yaml
  authorization:
    authorizer: "default"
    claimMapper: "default"
    permissionsClaimName: "roles"
    jwtKeyProvider:
      keySourceURIs:
        - http://jwks.default.svc/jwks.json
      refreshInterval: "1m"
```

`02-assert.yaml`: assert the cluster reaches `Ready` (mirror `test/e2e/ui/01-assert.yaml`).

- [ ] **Step 4: Authorization check pods**

`test/e2e/entra-auth/03-authz-checks.yaml`: two `Job`s using the `temporalio/admin-tools:1.31.1` image:
- `authz-allow`: runs `temporal operator namespace list --address <frontend>:7233 --grpc-meta "authorization=Bearer $(cat /tokens/token-valid.txt)"` and exits 0. Mount the valid token via a ConfigMap.
- `authz-deny`: runs the same command with **no** authorization metadata and is expected to FAIL (the Job asserts non-zero by wrapping: `! temporal operator namespace list ...`).

Assert `authz-allow` `succeeded: 1` and `authz-deny` `succeeded: 1` (the wrapper inverts the expected failure).

> Confirm the exact `temporal` CLI auth-metadata flag against the admin-tools image during implementation (`--grpc-meta` vs `--grpc_meta`); adjust the manifest accordingly. The principle — valid token authorized, missing token rejected — is the assertion that must hold.

- [ ] **Step 5: Chainsaw suite wiring**

`test/e2e/entra-auth/chainsaw-test.yaml`: steps `provision-postgres` (reuse postgres fixtures), `deploy-jwks`, `cluster-ready`, `authz-checks` — following the structure of `test/e2e/ui/chainsaw-test.yaml`, with `catch` blocks (`describe`, `events`, `podLogs`).

- [ ] **Step 6: Run the suite on kind**

Run: `make test-e2e` (or `make chainsaw-test` against a kind context with CNPG installed, per the existing e2e setup).
Expected: the `entra-auth` suite passes — cluster Ready, `authz-allow` succeeds, `authz-deny` (inverted) succeeds.

> If `make test-e2e` runs all suites and another suite needs infra you can't provision, run just this suite: `chainsaw test --test-dir test/e2e/entra-auth --config .chainsaw.yaml` against a kind cluster with the operator + CNPG installed.

- [ ] **Step 7: Commit**

```bash
git add test/e2e/entra-auth/
git commit -s -m "$(printf 'test(e2e): hermetic JWKS server-JWT authorization suite\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>')"
```

---

### Task 7: Longstanding Entra app script + make targets

**Files:**
- Create: `hack/entra-e2e.sh`
- Modify: `Makefile` (add `entra-e2e-up`, `entra-e2e-token`, `entra-e2e-down` targets near the `azure-e2e-*` targets ~line 296)

**Interfaces:** Standalone operational tooling; documented in Task 8.

- [ ] **Step 1: Write the script**

`hack/entra-e2e.sh` (bash, `set -euo pipefail`, mirroring `hack/azure-e2e.sh` structure: a `usage`, a `cmd_<name>` dispatch, and `az` CLI calls). Subcommands:

- `up`: `az ad app create` an app registration; define **app roles** with `value` `default:read`, `default:write`, `temporal-system:admin` (via `--app-roles @roles.json` or `az ad app update`); `az ad sp create`; `az ad app credential reset` for a client secret; print `TENANT_ID`, `CLIENT_ID`, `CLIENT_SECRET`, and the derived JWKS URL.
- `token`: acquire an access token via client-credentials (`az account get-access-token --resource api://<clientId>` or MSAL) for gRPC testing; print the JWT.
- `down`: `az ad app delete --id <appId>`.

Include an inline `roles.json` heredoc with the three app-role definitions (each with a unique `id` GUID, `allowedMemberTypes: ["Application","User"]`, `isEnabled: true`).

- [ ] **Step 2: Add make targets**

In `Makefile`, after the `azure-e2e-down` target, add:

```makefile
.PHONY: entra-e2e-up
entra-e2e-up: ## Create the longstanding Entra app registration (app roles + client secret).
	./hack/entra-e2e.sh up

.PHONY: entra-e2e-token
entra-e2e-token: ## Print a client-credentials access token for gRPC auth testing.
	./hack/entra-e2e.sh token

.PHONY: entra-e2e-down
entra-e2e-down: ## Delete the Entra app registration created by entra-e2e-up.
	./hack/entra-e2e.sh down
```

- [ ] **Step 3: Lint the script**

Run: `bash -n hack/entra-e2e.sh && chmod +x hack/entra-e2e.sh`
Expected: no syntax errors. (Do NOT run `up` here — it mutates a real tenant; it is exercised manually.)

- [ ] **Step 4: Commit**

```bash
git add hack/entra-e2e.sh Makefile
git commit -s -m "$(printf 'ci(entra): script the longstanding Entra app registration\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>')"
```

---

### Task 8: Authentication documentation guide

**Files:**
- Create: `docs/content/operations/authentication.md`
- Modify: `docs/content/installation/azure.md` (add a cross-link)

**Interfaces:** Documents all fields from Tasks 1-2 and the workflow from Tasks 5-7.

- [ ] **Step 1: Write the guide**

`docs/content/operations/authentication.md` with Hugo front matter (`+++ title = "Authentication & Authorization" weight = ... +++`, matching sibling pages under `operations/`). Sections:

1. **Concepts** — authentication vs authorization, JWKS/JWT key provider, claim mapper, the `<namespace>:<role>` permission format, and that stock images are used.
2. **Server JWT — authenticate-only** — `spec.authorization.entra.tenantID` (or explicit `jwtKeyProvider.keySourceURIs`); note `authorizer: ""` for allow-all-authenticated; how callers attach `authorization: Bearer <token>` gRPC metadata. **Validate the exact no-op authorizer key against the target Temporal version before stating it.**
3. **Server JWT — full RBAC via Entra app roles** — create app roles whose `value` is exactly `default:write`, `temporal-system:admin`, etc.; assign them to apps/users; Entra emits them in the `roles` claim → `permissionsClaimName: roles` (the operator default when Entra is set).
4. **UI OIDC login** — `spec.ui.auth` typed fields, `clientSecretRef` (+ the 1Password `OnePasswordItem` pattern), `callbackURL` must match the Entra app redirect URI.
5. **The longstanding Entra app** — purpose, ownership, and recreation via `make entra-e2e-up` / `hack/entra-e2e.sh`; the manual UI-OIDC verification checklist (browser login, role-gated operation).
6. **Security caveats** — audience/issuer validation, token lifetime, role-value format pitfalls, secrets never inlined.
7. Link to `examples/cluster-entra-auth`.

- [ ] **Step 2: Cross-link from the Azure guide**

In `docs/content/installation/azure.md`, under the existing Entra/passwordless section, add a sentence linking to the new guide:

```markdown
For authenticating **clients and the UI** to Temporal with Microsoft Entra
(JWT + OIDC), see [Authentication & Authorization](../operations/authentication.md).
```

- [ ] **Step 3: Lint the docs (markdownlint, per repo config)**

Run: `npx --yes markdownlint-cli2 "docs/content/operations/authentication.md" "docs/content/installation/azure.md" 2>/dev/null || true`
Expected: no errors (the repo has `.markdownlint.yaml`). Fix any reported issues.

- [ ] **Step 4: Commit**

```bash
git add docs/content/operations/authentication.md docs/content/installation/azure.md
git commit -s -m "$(printf 'docs: Microsoft Entra authentication & authorization guide\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>')"
```

---

### Task 9: Final validation & PR

**Files:** none (verification).

- [ ] **Step 1: Build, test, lint**

Run: `make build && make test && make lint`
Expected: all pass, lint 0 issues.

- [ ] **Step 2: Confirm no generation drift**

Run: `make generate manifests api-docs docs-crd-reference && git status --porcelain`
Expected: clean (no uncommitted regenerated files).

- [ ] **Step 3: Open the PR**

```bash
git push -u origin feat/entra-authentication
gh pr create --fill --base main
```

Expected: PR created from `feat/entra-authentication`. Body references this plan and the design doc, and notes the breaking `spec.ui.auth` change.

---

## Self-Review Notes

- **Spec coverage:** server JWT (Task 1), UI OIDC (Task 2), webhook validation (Task 3, supports decision C: authn-only via `authorizer: ""`, RBAC via app roles), generated artifacts incl. `dist/chart` (Task 4), example with 1Password (Task 5), hermetic JWKS e2e (Task 6), longstanding Entra app (Task 7), docs guide + cross-link (Task 8), validation/PR (Task 9). All spec sections mapped.
- **Type consistency:** `AuthConfig` fields (`Authorizer`, `ClaimMapper`, `PermissionsClaimName`, `KeySourceURIs`, `RefreshInterval`, `ExtraConfig`) are defined in Task 1 and used by the template in the same task. `UIAuthSpec` fields defined in Task 2 are consumed identically by `uiEnv` (Task 2) and the webhook (Task 3). `SecretKeyReference{Name,Key}` reused everywhere.
- **Known verification points flagged inline for the implementer:** exact no-op authorizer key for Temporal, the `temporal` CLI gRPC-metadata flag name, and `baseCluster()`/`validBaseCluster()`/`metaObjectMeta` test-helper availability (fall back to inline literals copied from existing tests).
