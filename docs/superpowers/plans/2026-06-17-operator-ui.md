# Operator UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only, operator-embedded web UI that shows an overview of every TemporalCluster the operator manages and a per-cluster detail drill-down, served from the controller-runtime manager's cache and protected by forward-auth.

**Architecture:** A new `internal/ui` package exposes a `ui.Server` implementing `manager.Runnable`. It reads CRDs from the manager's cached `client.Reader` through a `DataSource` interface (the seam for future SSE), maps them into plain view-model structs, and renders them with templ. htmx polls partial endpoints for auto-refresh; Alpine.js handles tiny client-side state. The UI is opt-in via `--ui-bind-address` and unaffected when disabled.

**Tech Stack:** Go, templ (`github.com/a-h/templ` v0.3.1020), htmx 2.x, Alpine.js 3.x, hand-written CSS, controller-runtime, plain `testing` + `httptest` + controller-runtime fake client.

---

## Spec

See `docs/superpowers/specs/2026-06-17-operator-ui-design.md`. Key locked decisions: operator-managed CRDs only, served by the operator sharing the manager cache, forward-auth (no login in operator), strictly read-only v1, plain templ + htmx + Alpine (no Tailwind/TemplUI), htmx polling structured so SSE can replace it later.

## Conventions to follow

- License header on every Go file (copy the 16-line Apache header from any existing file, e.g. `api/v1alpha1/conditions.go`, owner "Brian Morton").
- Conventional Commits + DCO sign-off: `git commit -s -m "feat(ui): ..."`. Include the trailer `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>`.
- Tools install into `./bin` via the `go-install-tool` Makefile macro with a pinned `*_VERSION` variable.
- golangci-lint excludes generated files (`exclusions.generated: lax`); templ output carries the standard generated header, so `*_templ.go` is auto-excluded.
- Condition type constants live in `api/v1alpha1/conditions.go` (`ConditionReady`, `ConditionPersistenceReachable`, `ConditionSchemaReady`, `ConditionMTLSReady`, etc.).
- Satellite CRDs reference their cluster via `Spec.ClusterRef` (a `corev1.LocalObjectReference`, same namespace): `TemporalNamespace`, `TemporalClusterClient`, `TemporalSearchAttribute`.

## File structure

```
internal/ui/
  config.go            # Options + defaults/normalization
  config_test.go
  viewmodel.go         # plain view-model structs + BadgeState enum
  mapping.go           # k8s types -> view-models (the logic-heavy part)
  mapping_test.go
  datasource.go        # DataSource interface + CachedDataSource impl
  datasource_test.go
  identity.go          # forward-auth header parsing + Identity
  identity_test.go
  server.go            # ui.Server: Runnable, router, graceful shutdown
  handlers.go          # HTTP handlers (methods on *Server): pages + partials
  handlers_test.go
  static.go            # //go:embed static + versioned asset serving
  static/
    app.css            # hand-written CSS
    htmx.min.js        # vendored, pinned
    alpine.min.js      # vendored, pinned
  layouts/
    base.templ         # html shell, nav, embedded asset links
  components/
    badge.templ        # status badge
    cluster_card.templ # one cluster on the overview grid
    service_table.templ
    condition_list.templ
    persistence_panel.templ
    endpoints_panel.templ
    related_resources.templ
  pages/
    overview.templ     # full page + grid fragment
    cluster_detail.templ
config/ui/             # optional Service + example Ingress/Authelia (kustomize, not in default)
examples/ui/           # runnable example manifests
```

Import DAG (no cycles): `ui` → `pages` → {`components`, `layouts`}; `ui` → {`components` for fragments}. Handlers live in the `ui` package as methods on `*Server` (deviation from the spec's `handlers/` subdir) to avoid a `handlers ↔ pages ↔ server` import knot.

---

## Task 1: Tooling — add templ to the build

**Files:**
- Modify: `Makefile` (Tool Binaries, Tool Versions, new targets, `install-tools`, `generate`)
- Modify: `go.mod` / `go.sum` (add `github.com/a-h/templ`)

- [ ] **Step 1: Add the templ runtime dependency**

Run:
```bash
cd /workspaces/temporal-operator
go get github.com/a-h/templ@v0.3.1020
```
Expected: `go.mod` now requires `github.com/a-h/templ v0.3.1020`.

- [ ] **Step 2: Add templ tool binary + version to the Makefile**

In `Makefile`, under `## Tool Binaries` add:
```makefile
TEMPL ?= $(LOCALBIN)/templ
```
Under `## Tool Versions` add:
```makefile
TEMPL_VERSION ?= v0.3.1020
```

- [ ] **Step 3: Add the templ install target + ui-generate target**

In `Makefile`, near the other tool targets (e.g. after the `crd-ref-docs` target), add:
```makefile
.PHONY: templ
templ: $(TEMPL) ## Download templ locally if necessary.
$(TEMPL): $(LOCALBIN)
	$(call go-install-tool,$(TEMPL),github.com/a-h/templ/cmd/templ,$(TEMPL_VERSION))

.PHONY: ui-generate
ui-generate: templ ## Generate Go code from .templ files.
	"$(TEMPL)" generate
```

- [ ] **Step 4: Wire ui-generate into generate and install-tools**

In `Makefile`, change the `generate` target to also run templ:
```makefile
.PHONY: generate
generate: controller-gen ui-generate ## Generate DeepCopy methods and templ Go code.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt" paths="./..."
```
And add `templ` to `install-tools`:
```makefile
.PHONY: install-tools
install-tools: controller-gen kustomize envtest golangci-lint chainsaw crd-ref-docs kind templ ## Install all pinned developer tooling into ./bin.
	@echo "Developer tooling installed in $(LOCALBIN)."
```

- [ ] **Step 5: Verify templ installs and runs**

Run:
```bash
make templ && ./bin/templ --help | head -3
```
Expected: templ binary downloaded to `./bin/templ-v0.3.1020` and help text prints. (`ui-generate` is a no-op until `.templ` files exist — that's fine.)

- [ ] **Step 6: Commit**

```bash
git add Makefile go.mod go.sum
git commit -s -m "build(ui): add templ tooling to the build" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Config (Options)

**Files:**
- Create: `internal/ui/config.go`
- Test: `internal/ui/config_test.go`

- [ ] **Step 1: Write the failing test**

`internal/ui/config_test.go`:
```go
// <Apache header>

package ui

import (
	"testing"
	"time"
)

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if o.BindAddress != ":8082" {
		t.Errorf("BindAddress = %q, want :8082", o.BindAddress)
	}
	if o.RefreshInterval != 5*time.Second {
		t.Errorf("RefreshInterval = %v, want 5s", o.RefreshInterval)
	}
	if o.UserHeader != "Remote-User" {
		t.Errorf("UserHeader = %q, want Remote-User", o.UserHeader)
	}
}

func TestNormalizeFillsBlanks(t *testing.T) {
	o := Options{}.Normalize()
	if o.BasePath != "/" {
		t.Errorf("BasePath = %q, want /", o.BasePath)
	}
	if o.UserHeader != "Remote-User" {
		t.Errorf("UserHeader = %q, want Remote-User", o.UserHeader)
	}
	if o.RefreshInterval != 5*time.Second {
		t.Errorf("RefreshInterval = %v, want 5s", o.RefreshInterval)
	}
}

func TestNormalizeTrimsTrailingSlash(t *testing.T) {
	o := Options{BasePath: "/ops/"}.Normalize()
	if o.BasePath != "/ops" {
		t.Errorf("BasePath = %q, want /ops", o.BasePath)
	}
}

func TestEnabled(t *testing.T) {
	if (Options{BindAddress: ""}).Enabled() {
		t.Error("empty BindAddress should be disabled")
	}
	if !(Options{BindAddress: ":8082"}).Enabled() {
		t.Error(":8082 should be enabled")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestDefaultOptions -v`
Expected: FAIL — `undefined: DefaultOptions`.

- [ ] **Step 3: Write minimal implementation**

`internal/ui/config.go`:
```go
// <Apache header>

package ui

import (
	"strings"
	"time"
)

// Options configures the UI server.
type Options struct {
	// BindAddress is the listen address; empty disables the UI.
	BindAddress string
	// RefreshInterval is the htmx poll interval surfaced to templates.
	RefreshInterval time.Duration
	// BasePath is the URL prefix the UI is served under (no trailing slash).
	BasePath string
	// RequireAuth makes the server return 401 when no trusted user header is present.
	RequireAuth bool
	// UserHeader, GroupsHeader and EmailHeader are the trusted forward-auth headers.
	UserHeader   string
	GroupsHeader string
	EmailHeader  string
}

// DefaultOptions returns the default UI options.
func DefaultOptions() Options {
	return Options{
		BindAddress:     ":8082",
		RefreshInterval: 5 * time.Second,
		BasePath:        "/",
		UserHeader:      "Remote-User",
		GroupsHeader:    "Remote-Groups",
		EmailHeader:     "Remote-Email",
	}
}

// Enabled reports whether the UI should run.
func (o Options) Enabled() bool { return o.BindAddress != "" }

// Normalize fills blank fields with defaults and tidies the base path.
func (o Options) Normalize() Options {
	d := DefaultOptions()
	if o.RefreshInterval <= 0 {
		o.RefreshInterval = d.RefreshInterval
	}
	if o.UserHeader == "" {
		o.UserHeader = d.UserHeader
	}
	if o.GroupsHeader == "" {
		o.GroupsHeader = d.GroupsHeader
	}
	if o.EmailHeader == "" {
		o.EmailHeader = d.EmailHeader
	}
	if o.BasePath == "" {
		o.BasePath = "/"
	}
	if o.BasePath != "/" {
		o.BasePath = "/" + strings.Trim(o.BasePath, "/")
	}
	return o
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/config.go internal/ui/config_test.go
git commit -s -m "feat(ui): add UI server options" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: View models + mapping (the logic-heavy part)

**Files:**
- Create: `internal/ui/viewmodel.go`
- Create: `internal/ui/mapping.go`
- Test: `internal/ui/mapping_test.go`

- [ ] **Step 1: Create the view-model structs (no logic)**

`internal/ui/viewmodel.go`:
```go
// <Apache header>

package ui

// BadgeState is the visual state of a status badge.
type BadgeState string

const (
	BadgeOK      BadgeState = "ok"
	BadgeWarn    BadgeState = "warn"
	BadgeError   BadgeState = "error"
	BadgePending BadgeState = "pending"
	BadgeUnknown BadgeState = "unknown"
)

// ClusterSummary is one row/card on the overview.
type ClusterSummary struct {
	Namespace   string
	Name        string
	Version     string
	Shards      int32
	Phase       string
	Ready       BadgeState
	Persistence BadgeState
	MTLSEnabled bool
	MTLS        BadgeState
	Upgrading   bool
	Age         string
}

// ServiceRow reports readiness of one Temporal service.
type ServiceRow struct {
	Name    string
	Ready   int32
	Desired int32
	Version string
	State   BadgeState
}

// ConditionRow is one status condition.
type ConditionRow struct {
	Type    string
	Status  string
	Reason  string
	Message string
	State   BadgeState
}

// PersistenceInfo summarizes datastore state.
type PersistenceInfo struct {
	Reachable   BadgeState
	SchemaReady BadgeState
}

// EndpointsInfo holds resolved endpoints.
type EndpointsInfo struct {
	Frontend string
	UI       string
	Metrics  string
}

// UpgradeInfo describes an in-flight upgrade.
type UpgradeInfo struct {
	Active       bool
	FromVersion  string
	ToVersion    string
	Phase        string
	Rollbackable bool
}

// RelatedResource is a satellite CRD tied to a cluster.
type RelatedResource struct {
	Kind   string
	Name   string
	Ready  BadgeState
	Detail string
}

// ClusterDetail is the full per-cluster view.
type ClusterDetail struct {
	ClusterSummary
	Conditions  []ConditionRow
	Services    []ServiceRow
	Persistence PersistenceInfo
	Endpoints   EndpointsInfo
	Upgrade     UpgradeInfo
	Related     []RelatedResource
}
```

- [ ] **Step 2: Write the failing mapping test**

`internal/ui/mapping_test.go`:
```go
// <Apache header>

package ui

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func cond(t, status, reason string) metav1.Condition {
	return metav1.Condition{Type: t, Status: metav1.ConditionStatus(status), Reason: reason}
}

func TestBadgeForCondition(t *testing.T) {
	conds := []metav1.Condition{
		cond("Ready", "True", "AllServicesReady"),
		cond("PersistenceReachable", "False", "PersistenceUnreachable"),
		cond("SchemaReady", "Unknown", "SchemaMigrating"),
	}
	if got := badgeForCondition(conds, "Ready"); got != BadgeOK {
		t.Errorf("Ready badge = %v, want ok", got)
	}
	if got := badgeForCondition(conds, "PersistenceReachable"); got != BadgeError {
		t.Errorf("Persistence badge = %v, want error", got)
	}
	if got := badgeForCondition(conds, "SchemaReady"); got != BadgePending {
		t.Errorf("Schema badge = %v, want pending", got)
	}
	if got := badgeForCondition(conds, "MTLSReady"); got != BadgeUnknown {
		t.Errorf("missing badge = %v, want unknown", got)
	}
}

func TestSummaryFromCluster(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "team-a"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version:          "1.31.1",
			NumHistoryShards: 512,
			MTLS:             &temporalv1alpha1.MTLSSpec{},
		},
		Status: temporalv1alpha1.TemporalClusterStatus{
			Phase: "Running",
			Conditions: []metav1.Condition{
				cond("Ready", "True", "AllServicesReady"),
				cond("PersistenceReachable", "True", ""),
				cond("MTLSReady", "True", ""),
			},
		},
	}
	s := SummaryFromCluster(c)
	if s.Name != "demo" || s.Namespace != "team-a" {
		t.Fatalf("identity wrong: %+v", s)
	}
	if s.Version != "1.31.1" || s.Shards != 512 {
		t.Errorf("spec fields wrong: %+v", s)
	}
	if s.Ready != BadgeOK || s.Persistence != BadgeOK {
		t.Errorf("badges wrong: %+v", s)
	}
	if !s.MTLSEnabled || s.MTLS != BadgeOK {
		t.Errorf("mtls wrong: %+v", s)
	}
	if s.Upgrading {
		t.Errorf("should not be upgrading: %+v", s)
	}
}

func TestServiceRowsOrdered(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		Status: temporalv1alpha1.TemporalClusterStatus{
			Services: map[string]temporalv1alpha1.ServiceStatus{
				"worker":   {Ready: 1, Desired: 1},
				"frontend": {Ready: 0, Desired: 2},
				"history":  {Ready: 3, Desired: 3},
			},
		},
	}
	rows := serviceRows(c)
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
	}
	if rows[0].Name != "frontend" || rows[1].Name != "history" || rows[2].Name != "worker" {
		t.Errorf("order wrong: %+v", rows)
	}
	if rows[0].State != BadgeError { // 0/2 ready
		t.Errorf("frontend state = %v, want error", rows[0].State)
	}
	if rows[1].State != BadgeOK { // 3/3 ready
		t.Errorf("history state = %v, want ok", rows[1].State)
	}
}

func TestUpgradeInfo(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		Status: temporalv1alpha1.TemporalClusterStatus{
			Upgrade: &temporalv1alpha1.UpgradeStatus{
				FromVersion: "1.30.4", ToVersion: "1.31.1", Phase: "Migrating", Rollbackable: false,
			},
		},
	}
	u := upgradeInfo(c)
	if !u.Active || u.FromVersion != "1.30.4" || u.ToVersion != "1.31.1" {
		t.Errorf("upgrade wrong: %+v", u)
	}
	if SummaryFromCluster(c).Upgrading != true {
		t.Error("summary should report upgrading")
	}
}

func TestRelatedFromSatellites(t *testing.T) {
	ns := []temporalv1alpha1.TemporalNamespace{{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "team-a"},
		Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "demo"}},
		Status: temporalv1alpha1.TemporalNamespaceStatus{
			Conditions: []metav1.Condition{cond("Ready", "True", "")},
		},
	}}
	other := []temporalv1alpha1.TemporalNamespace{{
		Spec: temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "elsewhere"}},
	}}
	got := relatedNamespaces(append(ns, other...), "demo")
	if len(got) != 1 || got[0].Name != "orders" || got[0].Ready != BadgeOK {
		t.Errorf("related wrong: %+v", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestSummaryFromCluster -v`
Expected: FAIL — `undefined: SummaryFromCluster`.

- [ ] **Step 4: Write the mapping implementation**

`internal/ui/mapping.go`:
```go
// <Apache header>

package ui

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// serviceOrder is the stable display order for Temporal services.
var serviceOrder = []string{"frontend", "history", "matching", "worker", "internal-frontend"}

func badgeForCondition(conds []metav1.Condition, condType string) BadgeState {
	c := meta(conds, condType)
	if c == nil {
		return BadgeUnknown
	}
	switch c.Status {
	case metav1.ConditionTrue:
		return BadgeOK
	case metav1.ConditionFalse:
		return BadgeError
	default:
		return BadgePending
	}
}

func meta(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

func age(t metav1.Time) string {
	if t.IsZero() {
		return ""
	}
	return duration(time.Since(t.Time))
}

func duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

// SummaryFromCluster maps a TemporalCluster to its overview view-model.
func SummaryFromCluster(c *temporalv1alpha1.TemporalCluster) ClusterSummary {
	return ClusterSummary{
		Namespace:   c.Namespace,
		Name:        c.Name,
		Version:     c.Spec.Version,
		Shards:      c.Spec.NumHistoryShards,
		Phase:       c.Status.Phase,
		Ready:       badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionReady),
		Persistence: badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionPersistenceReachable),
		MTLSEnabled: c.Spec.MTLS != nil,
		MTLS:        mtlsBadge(c),
		Upgrading:   c.Status.Upgrade != nil,
		Age:         age(c.CreationTimestamp),
	}
}

func mtlsBadge(c *temporalv1alpha1.TemporalCluster) BadgeState {
	if c.Spec.MTLS == nil {
		return BadgeUnknown
	}
	return badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionMTLSReady)
}

func serviceRows(c *temporalv1alpha1.TemporalCluster) []ServiceRow {
	rows := make([]ServiceRow, 0, len(c.Status.Services))
	for _, name := range serviceOrder {
		st, ok := c.Status.Services[name]
		if !ok {
			continue
		}
		rows = append(rows, ServiceRow{
			Name:    name,
			Ready:   st.Ready,
			Desired: st.Desired,
			Version: st.Version,
			State:   serviceState(st),
		})
	}
	return rows
}

func serviceState(st temporalv1alpha1.ServiceStatus) BadgeState {
	switch {
	case st.Desired == 0:
		return BadgeUnknown
	case st.Ready == 0:
		return BadgeError
	case st.Ready < st.Desired:
		return BadgeWarn
	default:
		return BadgeOK
	}
}

func conditionRows(conds []metav1.Condition) []ConditionRow {
	rows := make([]ConditionRow, 0, len(conds))
	for i := range conds {
		c := conds[i]
		rows = append(rows, ConditionRow{
			Type:    c.Type,
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
			State:   badgeForCondition(conds, c.Type),
		})
	}
	return rows
}

func upgradeInfo(c *temporalv1alpha1.TemporalCluster) UpgradeInfo {
	u := c.Status.Upgrade
	if u == nil {
		return UpgradeInfo{}
	}
	return UpgradeInfo{
		Active:       true,
		FromVersion:  u.FromVersion,
		ToVersion:    u.ToVersion,
		Phase:        u.Phase,
		Rollbackable: u.Rollbackable,
	}
}

func persistenceInfo(c *temporalv1alpha1.TemporalCluster) PersistenceInfo {
	return PersistenceInfo{
		Reachable:   badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionPersistenceReachable),
		SchemaReady: badgeForCondition(c.Status.Conditions, temporalv1alpha1.ConditionSchemaReady),
	}
}

func endpointsInfo(c *temporalv1alpha1.TemporalCluster) EndpointsInfo {
	return EndpointsInfo{
		Frontend: c.Status.Endpoints.Frontend,
		UI:       c.Status.Endpoints.UI,
		Metrics:  c.Status.Endpoints.Metrics,
	}
}

func relatedNamespaces(items []temporalv1alpha1.TemporalNamespace, cluster string) []RelatedResource {
	out := make([]RelatedResource, 0)
	for i := range items {
		if items[i].Spec.ClusterRef.Name != cluster {
			continue
		}
		detail := ""
		if items[i].Status.Registered {
			detail = "registered"
		}
		out = append(out, RelatedResource{
			Kind:   "TemporalNamespace",
			Name:   items[i].Name,
			Ready:  badgeForCondition(items[i].Status.Conditions, temporalv1alpha1.ConditionReady),
			Detail: detail,
		})
	}
	return out
}

func relatedClients(items []temporalv1alpha1.TemporalClusterClient, cluster string) []RelatedResource {
	out := make([]RelatedResource, 0)
	for i := range items {
		if items[i].Spec.ClusterRef.Name != cluster {
			continue
		}
		detail := ""
		if items[i].Status.SecretRef != nil {
			detail = "secret: " + items[i].Status.SecretRef.Name
		}
		out = append(out, RelatedResource{
			Kind:   "TemporalClusterClient",
			Name:   items[i].Name,
			Ready:  badgeForCondition(items[i].Status.Conditions, temporalv1alpha1.ConditionReady),
			Detail: detail,
		})
	}
	return out
}

func relatedSearchAttributes(items []temporalv1alpha1.TemporalSearchAttribute, cluster string) []RelatedResource {
	out := make([]RelatedResource, 0)
	for i := range items {
		if items[i].Spec.ClusterRef.Name != cluster {
			continue
		}
		out = append(out, RelatedResource{
			Kind:  "TemporalSearchAttribute",
			Name:  items[i].Name,
			Ready: badgeForCondition(items[i].Status.Conditions, temporalv1alpha1.ConditionReady),
		})
	}
	return out
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (config + mapping tests). If `TemporalSearchAttributeStatus` lacks a `Conditions` field, open `api/v1alpha1/temporalsearchattribute_types.go` and adjust `relatedSearchAttributes` to read whatever readiness field exists (do not invent fields).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/viewmodel.go internal/ui/mapping.go internal/ui/mapping_test.go
git commit -s -m "feat(ui): add view-models and k8s mapping" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: DataSource (cached client → view-models)

**Files:**
- Create: `internal/ui/datasource.go`
- Test: `internal/ui/datasource_test.go`

- [ ] **Step 1: Write the failing test**

`internal/ui/datasource_test.go`:
```go
// <Apache header>

package ui

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := temporalv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestListClusters(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns2"},
			Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512},
		},
		&temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns1"},
			Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512},
		},
	).Build()

	ds := &CachedDataSource{Reader: c}
	got, err := ds.ListClusters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Namespace != "ns1" || got[1].Namespace != "ns2" {
		t.Errorf("not sorted by ns/name: %+v", got)
	}
}

func TestGetClusterWithRelated(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512},
		},
		&temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "demo"}},
		},
		&temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "nope"}},
		},
	).Build()

	ds := &CachedDataSource{Reader: c}
	got, err := ds.GetCluster(context.Background(), "team-a", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" {
		t.Fatalf("name = %q", got.Name)
	}
	if len(got.Related) != 1 || got.Related[0].Name != "orders" {
		t.Errorf("related wrong: %+v", got.Related)
	}
}

func TestGetClusterNotFound(t *testing.T) {
	ds := &CachedDataSource{Reader: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}
	if _, err := ds.GetCluster(context.Background(), "x", "y"); err == nil {
		t.Error("expected error for missing cluster")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestListClusters -v`
Expected: FAIL — `undefined: CachedDataSource`.

- [ ] **Step 3: Write the implementation**

`internal/ui/datasource.go`:
```go
// <Apache header>

package ui

import (
	"context"
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// DataSource provides view-models to the handlers. It is the seam that lets a
// future SSE/watch-push implementation replace cache polling.
type DataSource interface {
	ListClusters(ctx context.Context) ([]ClusterSummary, error)
	GetCluster(ctx context.Context, namespace, name string) (*ClusterDetail, error)
}

// CachedDataSource reads from a controller-runtime cached client.Reader.
type CachedDataSource struct {
	Reader client.Reader
}

var _ DataSource = (*CachedDataSource)(nil)

// ListClusters returns every TemporalCluster sorted by namespace then name.
func (d *CachedDataSource) ListClusters(ctx context.Context) ([]ClusterSummary, error) {
	var list temporalv1alpha1.TemporalClusterList
	if err := d.Reader.List(ctx, &list); err != nil {
		return nil, err
	}
	out := make([]ClusterSummary, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, SummaryFromCluster(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// GetCluster returns the detail view for one cluster plus its related satellites.
func (d *CachedDataSource) GetCluster(ctx context.Context, namespace, name string) (*ClusterDetail, error) {
	var c temporalv1alpha1.TemporalCluster
	if err := d.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &c); err != nil {
		return nil, err
	}

	detail := &ClusterDetail{
		ClusterSummary: SummaryFromCluster(&c),
		Conditions:     conditionRows(c.Status.Conditions),
		Services:       serviceRows(&c),
		Persistence:    persistenceInfo(&c),
		Endpoints:      endpointsInfo(&c),
		Upgrade:        upgradeInfo(&c),
	}

	inNS := client.InNamespace(namespace)

	var namespaces temporalv1alpha1.TemporalNamespaceList
	if err := d.Reader.List(ctx, &namespaces, inNS); err != nil {
		return nil, err
	}
	var clients temporalv1alpha1.TemporalClusterClientList
	if err := d.Reader.List(ctx, &clients, inNS); err != nil {
		return nil, err
	}
	var attrs temporalv1alpha1.TemporalSearchAttributeList
	if err := d.Reader.List(ctx, &attrs, inNS); err != nil {
		return nil, err
	}

	related := make([]RelatedResource, 0)
	related = append(related, relatedNamespaces(namespaces.Items, name)...)
	related = append(related, relatedClients(clients.Items, name)...)
	related = append(related, relatedSearchAttributes(attrs.Items, name)...)
	detail.Related = related

	return detail, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS. (If the list types differ, confirm names in `api/v1alpha1/*_types.go` — they are `TemporalNamespaceList`, `TemporalClusterClientList`, `TemporalSearchAttributeList`.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/datasource.go internal/ui/datasource_test.go
git commit -s -m "feat(ui): add cached-client DataSource" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Identity (forward-auth headers)

**Files:**
- Create: `internal/ui/identity.go`
- Test: `internal/ui/identity_test.go`

- [ ] **Step 1: Write the failing test**

`internal/ui/identity_test.go`:
```go
// <Apache header>

package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityFromHeaders(t *testing.T) {
	o := DefaultOptions()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Remote-User", "alice")
	r.Header.Set("Remote-Email", "alice@example.com")
	r.Header.Set("Remote-Groups", "admins, ops")

	id := o.IdentityFrom(r)
	if !id.Authenticated {
		t.Fatal("expected authenticated")
	}
	if id.User != "alice" || id.Email != "alice@example.com" {
		t.Errorf("identity = %+v", id)
	}
	if len(id.Groups) != 2 || id.Groups[0] != "admins" || id.Groups[1] != "ops" {
		t.Errorf("groups = %v", id.Groups)
	}
}

func TestIdentityAnonymous(t *testing.T) {
	o := DefaultOptions()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if o.IdentityFrom(r).Authenticated {
		t.Error("expected anonymous")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestIdentityFromHeaders -v`
Expected: FAIL — `o.IdentityFrom undefined`.

- [ ] **Step 3: Write the implementation**

`internal/ui/identity.go`:
```go
// <Apache header>

package ui

import (
	"net/http"
	"strings"
)

// Identity is the request's authenticated user, derived from forward-auth headers.
type Identity struct {
	User          string
	Email         string
	Groups        []string
	Authenticated bool
}

// IdentityFrom extracts the Identity from the configured trusted headers.
func (o Options) IdentityFrom(r *http.Request) Identity {
	o = o.Normalize()
	user := strings.TrimSpace(r.Header.Get(o.UserHeader))
	id := Identity{
		User:          user,
		Email:         strings.TrimSpace(r.Header.Get(o.EmailHeader)),
		Authenticated: user != "",
	}
	if g := r.Header.Get(o.GroupsHeader); g != "" {
		for _, part := range strings.Split(g, ",") {
			if p := strings.TrimSpace(part); p != "" {
				id.Groups = append(id.Groups, p)
			}
		}
	}
	return id
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/identity.go internal/ui/identity_test.go
git commit -s -m "feat(ui): parse forward-auth identity headers" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Static assets (vendored JS/CSS + embed)

**Files:**
- Create: `internal/ui/static/app.css`
- Create: `internal/ui/static/htmx.min.js` (downloaded)
- Create: `internal/ui/static/alpine.min.js` (downloaded)
- Create: `internal/ui/static.go`
- Test: `internal/ui/static_test.go`

- [ ] **Step 1: Vendor the pinned libraries**

Run:
```bash
cd /workspaces/temporal-operator
mkdir -p internal/ui/static
curl -fsSL https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js -o internal/ui/static/htmx.min.js
curl -fsSL https://cdn.jsdelivr.net/npm/alpinejs@3.14.8/dist/cdn.min.js -o internal/ui/static/alpine.min.js
ls -l internal/ui/static
```
Expected: both files non-empty (htmx ~50KB, alpine ~45KB).

- [ ] **Step 2: Write minimal CSS**

`internal/ui/static/app.css`:
```css
:root{--ok:#1a7f37;--warn:#9a6700;--error:#cf222e;--pending:#0969da;--unknown:#6e7781;--bg:#f6f8fa;--fg:#1f2328;--card:#fff;--border:#d0d7de}
*{box-sizing:border-box}
body{margin:0;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;color:var(--fg);background:var(--bg)}
a{color:var(--pending);text-decoration:none}a:hover{text-decoration:underline}
header.app{display:flex;align-items:center;justify-content:space-between;padding:12px 20px;background:var(--card);border-bottom:1px solid var(--border)}
header.app .brand{font-weight:600}
main{max-width:1100px;margin:0 auto;padding:20px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:16px}
.card{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:16px}
.card h3{margin:0 0 8px}
.badge{display:inline-block;padding:1px 8px;border-radius:999px;font-size:12px;font-weight:600;color:#fff}
.badge.ok{background:var(--ok)}.badge.warn{background:var(--warn)}.badge.error{background:var(--error)}
.badge.pending{background:var(--pending)}.badge.unknown{background:var(--unknown)}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--border)}
.muted{color:var(--unknown)}
.empty{padding:40px;text-align:center;color:var(--unknown)}
.banner{padding:10px 14px;border-radius:6px;background:#fff8c5;border:1px solid #d4a72c;margin-bottom:16px}
.kv{display:grid;grid-template-columns:160px 1fr;gap:4px 12px}
```

- [ ] **Step 3: Write the failing test**

`internal/ui/static_test.go`:
```go
// <Apache header>

package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetVersionStable(t *testing.T) {
	if AssetVersion() == "" {
		t.Fatal("asset version empty")
	}
	if AssetVersion() != AssetVersion() {
		t.Fatal("asset version not stable")
	}
}

func TestStaticHandlerServesCSS(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	StaticHandler("/static/").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), ".badge") {
		t.Error("css body missing")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestStaticHandlerServesCSS -v`
Expected: FAIL — `undefined: StaticHandler`.

- [ ] **Step 5: Write the implementation**

`internal/ui/static.go`:
```go
// <Apache header>

package ui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"sort"
	"sync"
)

//go:embed static
var staticFS embed.FS

var (
	assetVersionOnce sync.Once
	assetVersion     string
)

// AssetVersion returns a short content hash of all embedded assets, used as a
// cache-busting query string.
func AssetVersion() string {
	assetVersionOnce.Do(func() {
		sub, _ := fs.Sub(staticFS, "static")
		var names []string
		_ = fs.WalkDir(sub, ".", func(p string, de fs.DirEntry, err error) error {
			if err == nil && !de.IsDir() {
				names = append(names, p)
			}
			return nil
		})
		sort.Strings(names)
		h := sha256.New()
		for _, n := range names {
			b, _ := fs.ReadFile(sub, n)
			h.Write([]byte(n))
			h.Write(b)
		}
		assetVersion = hex.EncodeToString(h.Sum(nil))[:12]
	})
	return assetVersion
}

// StaticHandler serves the embedded assets under the given prefix with a long
// cache lifetime (assets are content-versioned via AssetVersion()).
func StaticHandler(prefix string) http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.StripPrefix(prefix, cacheHeaders(fileServer))
}

func cacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/static internal/ui/static.go internal/ui/static_test.go
git commit -s -m "feat(ui): embed vendored htmx, alpine and css" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 7: templ templates (layout, components, pages)

**Files:**
- Create: `internal/ui/layouts/base.templ`
- Create: `internal/ui/components/badge.templ`
- Create: `internal/ui/components/cluster_card.templ`
- Create: `internal/ui/components/service_table.templ`
- Create: `internal/ui/components/condition_list.templ`
- Create: `internal/ui/components/persistence_panel.templ`
- Create: `internal/ui/components/endpoints_panel.templ`
- Create: `internal/ui/components/related_resources.templ`
- Create: `internal/ui/pages/overview.templ`
- Create: `internal/ui/pages/cluster_detail.templ`

> templ note: each directory is its own Go package (`package layouts`, `package components`, `package pages`). View-model types live in package `ui`; import them as `github.com/bmorton/temporal-operator/internal/ui`. To avoid stuttering, alias the import as `ui`. Components take `ui.ClusterSummary` etc. Generated `*_templ.go` files are committed.

- [ ] **Step 1: Base layout**

`internal/ui/layouts/base.templ`:
```templ
// <Apache header>

package layouts

// View carries per-request layout data.
type View struct {
	Title       string
	BasePath    string
	AssetVer    string
	User        string
	RefreshSecs int
}

func (v View) asset(name string) string {
	return v.BasePath + "/static/" + name + "?v=" + v.AssetVer
}

templ Base(v View) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{ v.Title } · temporal-operator</title>
			<link rel="stylesheet" href={ v.asset("app.css") }/>
			<script src={ v.asset("htmx.min.js") } defer></script>
			<script src={ v.asset("alpine.min.js") } defer></script>
		</head>
		<body>
			<header class="app">
				<a class="brand" href={ templ.SafeURL(v.BasePath + "/") }>temporal-operator</a>
				<span class="muted">
					if v.User != "" {
						Signed in as { v.User }
					} else {
						Not authenticated
					}
				</span>
			</header>
			<main>
				{ children... }
			</main>
		</body>
	</html>
}
```

- [ ] **Step 2: Badge component**

`internal/ui/components/badge.templ`:
```templ
// <Apache header>

package components

import ui "github.com/bmorton/temporal-operator/internal/ui"

func badgeClass(s ui.BadgeState) string {
	return "badge " + string(s)
}

func badgeText(s ui.BadgeState) string {
	switch s {
	case ui.BadgeOK:
		return "OK"
	case ui.BadgeWarn:
		return "Degraded"
	case ui.BadgeError:
		return "Error"
	case ui.BadgePending:
		return "Pending"
	default:
		return "Unknown"
	}
}

templ Badge(s ui.BadgeState) {
	<span class={ badgeClass(s) }>{ badgeText(s) }</span>
}

templ BadgeLabel(s ui.BadgeState, label string) {
	<span class={ badgeClass(s) }>{ label }</span>
}
```

- [ ] **Step 3: Cluster card**

`internal/ui/components/cluster_card.templ`:
```templ
// <Apache header>

package components

import (
	"strconv"

	ui "github.com/bmorton/temporal-operator/internal/ui"
)

templ ClusterCard(basePath string, c ui.ClusterSummary) {
	<div class="card">
		<h3>
			<a href={ templ.SafeURL(basePath + "/clusters/" + c.Namespace + "/" + c.Name) }>
				{ c.Namespace }/{ c.Name }
			</a>
		</h3>
		<div class="kv">
			<div>Status</div><div>@Badge(c.Ready) <span class="muted">{ c.Phase }</span></div>
			<div>Version</div><div>{ c.Version }</div>
			<div>Shards</div><div>{ strconv.Itoa(int(c.Shards)) }</div>
			<div>Persistence</div><div>@Badge(c.Persistence)</div>
			if c.MTLSEnabled {
				<div>mTLS</div><div>@Badge(c.MTLS)</div>
			}
			if c.Upgrading {
				<div>Upgrade</div><div>@BadgeLabel(ui.BadgePending, "in progress")</div>
			}
			<div>Age</div><div class="muted">{ c.Age }</div>
		</div>
	</div>
}
```

- [ ] **Step 4: Service table**

`internal/ui/components/service_table.templ`:
```templ
// <Apache header>

package components

import (
	"strconv"

	ui "github.com/bmorton/temporal-operator/internal/ui"
)

func replicaText(r ui.ServiceRow) string {
	return strconv.Itoa(int(r.Ready)) + "/" + strconv.Itoa(int(r.Desired))
}

templ ServiceTable(rows []ui.ServiceRow) {
	if len(rows) == 0 {
		<p class="muted">No service status reported yet.</p>
	} else {
		<table>
			<thead><tr><th>Service</th><th>Ready</th><th>State</th></tr></thead>
			<tbody>
				for _, r := range rows {
					<tr>
						<td>{ r.Name }</td>
						<td>{ replicaText(r) }</td>
						<td>@Badge(r.State)</td>
					</tr>
				}
			</tbody>
		</table>
	}
}
```

- [ ] **Step 5: Condition list**

`internal/ui/components/condition_list.templ`:
```templ
// <Apache header>

package components

import ui "github.com/bmorton/temporal-operator/internal/ui"

templ ConditionList(rows []ui.ConditionRow) {
	if len(rows) == 0 {
		<p class="muted">No conditions reported.</p>
	} else {
		<table>
			<thead><tr><th>Type</th><th>State</th><th>Reason</th><th>Message</th></tr></thead>
			<tbody>
				for _, r := range rows {
					<tr>
						<td>{ r.Type }</td>
						<td>@Badge(r.State)</td>
						<td class="muted">{ r.Reason }</td>
						<td class="muted">{ r.Message }</td>
					</tr>
				}
			</tbody>
		</table>
	}
}
```

- [ ] **Step 6: Persistence + endpoints + related components**

`internal/ui/components/persistence_panel.templ`:
```templ
// <Apache header>

package components

import ui "github.com/bmorton/temporal-operator/internal/ui"

templ PersistencePanel(p ui.PersistenceInfo) {
	<div class="kv">
		<div>Reachable</div><div>@Badge(p.Reachable)</div>
		<div>Schema</div><div>@Badge(p.SchemaReady)</div>
	</div>
}
```

`internal/ui/components/endpoints_panel.templ`:
```templ
// <Apache header>

package components

import ui "github.com/bmorton/temporal-operator/internal/ui"

templ EndpointsPanel(e ui.EndpointsInfo) {
	<div class="kv">
		<div>Frontend</div><div class="muted">{ orDash(e.Frontend) }</div>
		<div>UI</div><div class="muted">{ orDash(e.UI) }</div>
		<div>Metrics</div><div class="muted">{ orDash(e.Metrics) }</div>
	</div>
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
```

`internal/ui/components/related_resources.templ`:
```templ
// <Apache header>

package components

import ui "github.com/bmorton/temporal-operator/internal/ui"

templ RelatedResources(items []ui.RelatedResource) {
	if len(items) == 0 {
		<p class="muted">No related resources.</p>
	} else {
		<table>
			<thead><tr><th>Kind</th><th>Name</th><th>Ready</th><th>Detail</th></tr></thead>
			<tbody>
				for _, r := range items {
					<tr>
						<td>{ r.Kind }</td>
						<td>{ r.Name }</td>
						<td>@Badge(r.Ready)</td>
						<td class="muted">{ r.Detail }</td>
					</tr>
				}
			</tbody>
		</table>
	}
}
```

- [ ] **Step 7: Overview page (full + fragment)**

`internal/ui/pages/overview.templ`:
```templ
// <Apache header>

package pages

import (
	"strconv"

	"github.com/bmorton/temporal-operator/internal/ui/components"
	"github.com/bmorton/temporal-operator/internal/ui/layouts"
	ui "github.com/bmorton/temporal-operator/internal/ui"
)

// OverviewGrid is the htmx-polled fragment.
templ OverviewGrid(basePath string, clusters []ui.ClusterSummary) {
	if len(clusters) == 0 {
		<div class="empty">No TemporalClusters found.</div>
	} else {
		<div class="grid">
			for _, c := range clusters {
				@components.ClusterCard(basePath, c)
			}
		</div>
	}
}

templ Overview(v layouts.View, clusters []ui.ClusterSummary) {
	@layouts.Base(v) {
		<div
			hx-get={ v.BasePath + "/partials/clusters" }
			hx-trigger={ "every " + strconv.Itoa(v.RefreshSecs) + "s" }
			hx-swap="innerHTML"
		>
			@OverviewGrid(v.BasePath, clusters)
		</div>
	}
}
```

- [ ] **Step 8: Cluster detail page (full + fragment)**

`internal/ui/pages/cluster_detail.templ`:
```templ
// <Apache header>

package pages

import (
	"strconv"

	"github.com/bmorton/temporal-operator/internal/ui/components"
	"github.com/bmorton/temporal-operator/internal/ui/layouts"
	ui "github.com/bmorton/temporal-operator/internal/ui"
)

// DetailBody is the htmx-polled fragment.
templ DetailBody(d ui.ClusterDetail) {
	if d.Upgrade.Active {
		<div class="banner">
			Upgrading { d.Upgrade.FromVersion } → { d.Upgrade.ToVersion } ({ d.Upgrade.Phase }).
			if d.Upgrade.Rollbackable {
				<span class="muted">Rollback still possible.</span>
			} else {
				<span class="muted">Rollback no longer possible.</span>
			}
		</div>
	}
	<div class="card">
		<h3>Overview</h3>
		<div class="kv">
			<div>Status</div><div>@components.Badge(d.Ready) <span class="muted">{ d.Phase }</span></div>
			<div>Version</div><div>{ d.Version }</div>
			<div>Shards</div><div>{ strconv.Itoa(int(d.Shards)) }</div>
			if d.MTLSEnabled {
				<div>mTLS</div><div>@components.Badge(d.MTLS)</div>
			}
		</div>
	</div>
	<div class="card"><h3>Services</h3>@components.ServiceTable(d.Services)</div>
	<div class="card"><h3>Persistence</h3>@components.PersistencePanel(d.Persistence)</div>
	<div class="card"><h3>Endpoints</h3>@components.EndpointsPanel(d.Endpoints)</div>
	<div class="card"><h3>Conditions</h3>@components.ConditionList(d.Conditions)</div>
	<div class="card"><h3>Related resources</h3>@components.RelatedResources(d.Related)</div>
}

templ ClusterDetailPage(v layouts.View, d ui.ClusterDetail) {
	@layouts.Base(v) {
		<p><a href={ templ.SafeURL(v.BasePath + "/") }>← All clusters</a></p>
		<h2>{ d.Namespace }/{ d.Name }</h2>
		<div
			hx-get={ v.BasePath + "/partials/clusters/" + d.Namespace + "/" + d.Name }
			hx-trigger={ "every " + strconv.Itoa(v.RefreshSecs) + "s" }
			hx-swap="innerHTML"
		>
			@DetailBody(d)
		</div>
	}
}
```

- [ ] **Step 9: Generate templ Go code**

Run:
```bash
make ui-generate
```
Expected: `*_templ.go` files generated in `layouts/`, `components/`, `pages/`. Then build the packages:
```bash
go build ./internal/ui/...
```
Expected: compiles. If templ reports an `@Component` vs `{{ }}` syntax error, fix the offending `.templ` and re-run. (Components are invoked with `@Name(args)`.)

- [ ] **Step 10: Commit**

```bash
git add internal/ui/layouts internal/ui/components internal/ui/pages
git commit -s -m "feat(ui): add templ layout, components and pages" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 8: Server + handlers

**Files:**
- Create: `internal/ui/server.go`
- Create: `internal/ui/handlers.go`
- Test: `internal/ui/handlers_test.go`

- [ ] **Step 1: Write the failing handler test**

`internal/ui/handlers_test.go`:
```go
// <Apache header>

package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
)

type fakeDS struct {
	clusters []ClusterSummary
	detail   *ClusterDetail
	err      error
}

func (f *fakeDS) ListClusters(context.Context) ([]ClusterSummary, error) {
	return f.clusters, f.err
}

func (f *fakeDS) GetCluster(_ context.Context, ns, name string) (*ClusterDetail, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.detail, nil
}

func newTestServer(ds DataSource, opts Options) *Server {
	return NewServer(opts, ds, logr.Discard())
}

func TestOverviewRendersClusters(t *testing.T) {
	ds := &fakeDS{clusters: []ClusterSummary{
		{Namespace: "team-a", Name: "demo", Version: "1.31.1", Ready: BadgeOK, Phase: "Running"},
	}}
	s := newTestServer(ds, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "team-a/demo") {
		t.Error("cluster name not rendered")
	}
	if !strings.Contains(body, "htmx.min.js") {
		t.Error("layout not rendered")
	}
}

func TestPartialClustersFragment(t *testing.T) {
	ds := &fakeDS{clusters: []ClusterSummary{{Namespace: "n", Name: "c"}}}
	s := newTestServer(ds, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/partials/clusters", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "<html") {
		t.Error("fragment should not include full page shell")
	}
}

func TestClusterDetailRoute(t *testing.T) {
	ds := &fakeDS{detail: &ClusterDetail{
		ClusterSummary: ClusterSummary{Namespace: "team-a", Name: "demo", Ready: BadgeOK},
	}}
	s := newTestServer(ds, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/clusters/team-a/demo", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "team-a/demo") {
		t.Error("detail not rendered")
	}
}

func TestRequireAuthBlocksAnonymous(t *testing.T) {
	opts := DefaultOptions()
	opts.RequireAuth = true
	s := newTestServer(&fakeDS{}, opts)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rr.Code)
	}
}

func TestRequireAuthAllowsHeader(t *testing.T) {
	opts := DefaultOptions()
	opts.RequireAuth = true
	s := newTestServer(&fakeDS{}, opts)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Remote-User", "alice")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
}

func TestHealthz(t *testing.T) {
	s := newTestServer(&fakeDS{}, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestOverviewRendersClusters -v`
Expected: FAIL — `undefined: NewServer`.

- [ ] **Step 3: Write the server**

`internal/ui/server.go`:
```go
// <Apache header>

package ui

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-logr/logr"
)

// Server serves the operator UI. It implements manager.Runnable and
// manager.LeaderElectionRunnable (so it runs on every replica).
type Server struct {
	opts Options
	data DataSource
	log  logr.Logger
}

// NewServer builds a UI server.
func NewServer(opts Options, data DataSource, log logr.Logger) *Server {
	return &Server{opts: opts.Normalize(), data: data, log: log}
}

// NeedLeaderElection returns false so the UI runs regardless of leadership.
func (s *Server) NeedLeaderElection() bool { return false }

// Start runs the HTTP server until ctx is cancelled (manager.Runnable).
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.opts.BindAddress,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("starting UI server", "address", s.opts.BindAddress, "basePath", s.opts.BasePath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
```

- [ ] **Step 4: Write the handlers**

`internal/ui/handlers.go`:
```go
// <Apache header>

package ui

import (
	"net/http"
	"strings"

	"github.com/a-h/templ"

	"github.com/bmorton/temporal-operator/internal/ui/layouts"
	"github.com/bmorton/temporal-operator/internal/ui/pages"
)

// Handler builds the UI's HTTP handler (router + middleware).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	base := s.opts.BasePath
	if base == "/" {
		base = ""
	}

	mux.HandleFunc("GET "+base+"/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET "+base+"/static/", StaticHandler(s.opts.BasePath+"/static/"))

	mux.HandleFunc("GET "+base+"/", s.handleOverview)
	mux.HandleFunc("GET "+base+"/partials/clusters", s.handleClustersPartial)
	mux.HandleFunc("GET "+base+"/clusters/{namespace}/{name}", s.handleClusterDetail)
	mux.HandleFunc("GET "+base+"/partials/clusters/{namespace}/{name}", s.handleClusterDetailPartial)

	return s.authMiddleware(mux)
}

// authMiddleware enforces RequireAuth (fails closed) but always allows /healthz.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.RequireAuth && !strings.HasSuffix(r.URL.Path, "/healthz") {
			if !s.opts.IdentityFrom(r).Authenticated {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) view(r *http.Request, title string) layouts.View {
	return layouts.View{
		Title:       title,
		BasePath:    strings.TrimSuffix(s.opts.BasePath, "/"),
		AssetVer:    AssetVersion(),
		User:        s.opts.IdentityFrom(r).User,
		RefreshSecs: int(s.opts.RefreshInterval.Seconds()),
	}
}

func (s *Server) basePath() string { return strings.TrimSuffix(s.opts.BasePath, "/") }

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	clusters, err := s.data.ListClusters(r.Context())
	if err != nil {
		s.renderError(w, r, "load clusters", err)
		return
	}
	s.render(w, r, pages.Overview(s.view(r, "Clusters"), clusters))
}

func (s *Server) handleClustersPartial(w http.ResponseWriter, r *http.Request) {
	clusters, err := s.data.ListClusters(r.Context())
	if err != nil {
		s.renderError(w, r, "load clusters", err)
		return
	}
	s.render(w, r, pages.OverviewGrid(s.basePath(), clusters))
}

func (s *Server) handleClusterDetail(w http.ResponseWriter, r *http.Request) {
	d, err := s.data.GetCluster(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}
	s.render(w, r, pages.ClusterDetailPage(s.view(r, d.Name), *d))
}

func (s *Server) handleClusterDetailPartial(w http.ResponseWriter, r *http.Request) {
	d, err := s.data.GetCluster(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}
	s.render(w, r, pages.DetailBody(*d))
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		s.log.Error(err, "rendering UI")
	}
}

func (s *Server) renderError(w http.ResponseWriter, _ *http.Request, action string, err error) {
	s.log.Error(err, "ui handler error", "action", action)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`<div class="empty">Failed to ` + action + `. Check operator logs.</div>`))
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/... -v`
Expected: PASS (all handler + earlier tests). If `go-logr/logr` is not already a dependency, it is transitively present via controller-runtime; `go test` will resolve it. If the router rejects `GET /` patterns on your Go version, confirm Go ≥ 1.22 (the repo uses 1.26) — method+path patterns require it.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/server.go internal/ui/handlers.go internal/ui/handlers_test.go
git commit -s -m "feat(ui): add HTTP server and handlers" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 9: Wire the UI into the operator entrypoint

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Add UI flags**

In `cmd/main.go`, add imports:
```go
	"time"

	"github.com/bmorton/temporal-operator/internal/ui"
```
In `main()`, alongside the other `flag.*` declarations, add:
```go
	var uiBindAddress string
	var uiRefreshInterval time.Duration
	var uiBasePath string
	var uiRequireAuth bool
	flag.StringVar(&uiBindAddress, "ui-bind-address", ":8082",
		"Address the read-only operator UI binds to. Empty string disables the UI.")
	flag.DurationVar(&uiRefreshInterval, "ui-refresh-interval", 5*time.Second,
		"How often the UI auto-refreshes via htmx polling.")
	flag.StringVar(&uiBasePath, "ui-base-path", "/", "URL base path the UI is served under.")
	flag.BoolVar(&uiRequireAuth, "ui-require-auth", false,
		"Require a trusted forward-auth user header; return 401 when absent.")
```

- [ ] **Step 2: Register the UI as a manager Runnable**

In `cmd/main.go`, after the webhook setup block and before `mgr.AddHealthzCheck`, add:
```go
	uiOpts := ui.Options{
		BindAddress:     uiBindAddress,
		RefreshInterval: uiRefreshInterval,
		BasePath:        uiBasePath,
		RequireAuth:     uiRequireAuth,
	}
	if uiOpts.Enabled() {
		uiServer := ui.NewServer(uiOpts, &ui.CachedDataSource{Reader: mgr.GetClient()}, ctrl.Log.WithName("ui"))
		if err := mgr.Add(uiServer); err != nil {
			setupLog.Error(err, "unable to register UI server")
			os.Exit(1)
		}
		setupLog.Info("operator UI enabled", "address", uiBindAddress)
	}
```

- [ ] **Step 3: Build the operator**

Run:
```bash
go build -o bin/manager cmd/main.go
```
Expected: compiles cleanly.

- [ ] **Step 4: Smoke-test the server boots (no cluster needed)**

Run:
```bash
go test ./internal/ui/... && go vet ./internal/ui/... ./cmd/...
```
Expected: PASS / no vet errors.

- [ ] **Step 5: Commit**

```bash
git add cmd/main.go
git commit -s -m "feat(ui): serve the UI from the operator manager" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 10: Deployment manifests + docs

**Files:**
- Create: `config/ui/service.yaml`
- Create: `config/ui/kustomization.yaml`
- Create: `examples/ui/ingress-authelia.yaml`
- Create: `examples/ui/README.md`
- Modify: `README.md` (add a "Operator UI" section)

- [ ] **Step 1: UI Service (opt-in, not in default kustomization)**

`config/ui/service.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: operator-ui
  namespace: system
  labels:
    app.kubernetes.io/name: temporal-operator
    app.kubernetes.io/component: ui
spec:
  selector:
    control-plane: controller-manager
  ports:
    - name: http-ui
      port: 8082
      targetPort: 8082
```

`config/ui/kustomization.yaml`:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - service.yaml
```

- [ ] **Step 2: Example Ingress with Authelia forward-auth**

`examples/ui/ingress-authelia.yaml`:
```yaml
# Example: front the operator UI with Authelia forward-auth (nginx ingress).
# Adjust host, namespace, and Authelia URLs for your environment.
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: temporal-operator-ui
  namespace: temporal-operator-system
  annotations:
    nginx.ingress.kubernetes.io/auth-url: "https://auth.example.com/api/verify"
    nginx.ingress.kubernetes.io/auth-signin: "https://auth.example.com/?rd=$target_url"
    nginx.ingress.kubernetes.io/auth-response-headers: "Remote-User,Remote-Groups,Remote-Email,Remote-Name"
spec:
  ingressClassName: nginx
  rules:
    - host: temporal-operator.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: operator-ui
                port:
                  number: 8082
```

`examples/ui/README.md`:
```markdown
# Operator UI example

The operator serves a read-only UI on `--ui-bind-address` (default `:8082`) when
enabled. It performs no authentication itself; front it with a forward-auth proxy
such as Authelia and pass the trusted identity headers through.

1. Enable the UI on the manager (default port 8082).
2. Apply `config/ui` to expose the `operator-ui` Service.
3. Apply `ingress-authelia.yaml` (edit hosts/URLs) so Authelia authenticates
   users and injects `Remote-User` / `Remote-Groups` / `Remote-Email`.
4. Optionally set `--ui-require-auth` so the operator returns 401 if the proxy
   is misconfigured (fail closed).
```

- [ ] **Step 3: README section**

In `README.md`, add a section:
```markdown
## Operator UI

The operator ships an optional, read-only web UI that shows an overview of every
TemporalCluster it manages and a per-cluster detail view (services, persistence,
mTLS, endpoints, conditions, in-flight upgrades, and related namespaces, clients
and search attributes).

It is **disabled by default**. Enable it with `--ui-bind-address` (default
`:8082`) and front it with a forward-auth proxy (e.g. Authelia) — the operator
does not authenticate users itself. See `examples/ui/` for a worked example.

| Flag | Default | Description |
| --- | --- | --- |
| `--ui-bind-address` | `:8082` | UI listen address; empty disables the UI. |
| `--ui-refresh-interval` | `5s` | htmx auto-refresh interval. |
| `--ui-base-path` | `/` | Serve the UI under a sub-path. |
| `--ui-require-auth` | `false` | Return 401 when no trusted user header is present. |
```

- [ ] **Step 4: Verify manifests parse**

Run:
```bash
./bin/kustomize build config/ui >/dev/null && echo OK
```
Expected: `OK` (run `make kustomize` first if the binary is missing).

- [ ] **Step 5: Commit**

```bash
git add config/ui examples/ui README.md
git commit -s -m "docs(ui): add deployment example and README section" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 11: Final verification

- [ ] **Step 1: Full generate + build + vet + test**

Run:
```bash
make generate && make build && go vet ./... && go test $(go list ./... | grep -v /e2e)
```
Expected: clean build; all unit tests pass. (`make test` also runs envtest suites; the UI adds none, so the existing suites should be unaffected.)

- [ ] **Step 2: Lint**

Run:
```bash
make lint
```
Expected: no new findings in `internal/ui` (generated `*_templ.go` is excluded automatically). Fix any `lll`/`revive` issues in hand-written files.

- [ ] **Step 3: Confirm the WASM build is still intact**

Run:
```bash
GOOS=js GOARCH=wasm go build ./internal/resources/ ./internal/temporal/ ./api/...
go test ./cmd/preview-wasm/...
```
Expected: unaffected (the UI is server-only and not part of the wasm build).

- [ ] **Step 4: Final commit (if lint produced fixes)**

```bash
git add -A
git commit -s -m "chore(ui): lint and final cleanup" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Self-review notes

- **Spec coverage:** runtime wiring (Task 9), cached DataSource seam (Task 4), view-models + mapping (Task 3), routes + partials (Tasks 7–8), forward-auth + require-auth (Tasks 5, 8), config flags (Tasks 2, 9), build/templ tooling (Task 1), embedded assets (Task 6), testing (Tasks 2–8), deployment/RBAC/docs (Task 10). No new RBAC needed (reuses controller watches). SSE/Temporal-gRPC/actions explicitly deferred.
- **Type consistency:** `BadgeState` constants, `ClusterSummary`/`ClusterDetail`, and mapping function names (`SummaryFromCluster`, `serviceRows`, `upgradeInfo`, `relatedNamespaces`/`relatedClients`/`relatedSearchAttributes`) are defined in Task 3 and used unchanged in Tasks 4, 7, 8. `Options` fields/methods (`Enabled`, `Normalize`, `IdentityFrom`) defined in Tasks 2 & 5 and used in Tasks 8–9.
- **Known verification point:** `relatedSearchAttributes` assumes `TemporalSearchAttributeStatus` has a `Conditions` field; Step 5 of Task 3 says to confirm against `api/v1alpha1/temporalsearchattribute_types.go` and adapt if the readiness field differs (do not invent fields).
