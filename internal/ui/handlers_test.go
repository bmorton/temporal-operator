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

package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func TestUnknownPathNotFound(t *testing.T) {
	s := newTestServer(&fakeDS{}, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/not-a-route", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
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

func TestStaticRouteServesRootBasePath(t *testing.T) {
	s := newTestServer(&fakeDS{}, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/static/htmx.min.js", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
}

func TestStaticRouteServesAppJS(t *testing.T) {
	s := newTestServer(&fakeDS{}, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "__uiLivePaused") {
		t.Error("app.js missing live-control logic")
	}
}

func TestStaticRouteServesPrefixedBasePath(t *testing.T) {
	opts := DefaultOptions()
	opts.BasePath = "/ops"
	s := newTestServer(&fakeDS{}, opts)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ops/static/htmx.min.js", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
}

func TestBareBasePathRedirectsToTrailingSlash(t *testing.T) {
	opts := DefaultOptions()
	opts.BasePath = "/ops"
	s := newTestServer(&fakeDS{}, opts)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ops", nil))
	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("code = %d, want 301", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/ops/" {
		t.Errorf("Location = %q, want /ops/", loc)
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

func TestClusterDetailGenericErrorIs500(t *testing.T) {
	s := newTestServer(&fakeDS{err: errors.New("boom")}, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/clusters/team-a/demo", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rr.Code)
	}
}

func TestClusterDetailNotFoundIs404(t *testing.T) {
	nf := apierrors.NewNotFound(schema.GroupResource{Group: "temporal.bmor10.com", Resource: "temporalclusters"}, "demo")
	s := newTestServer(&fakeDS{err: nf}, DefaultOptions())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/clusters/team-a/demo", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
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

func TestRequireAuthDoesNotAllowHealthzSuffixBypass(t *testing.T) {
	opts := DefaultOptions()
	opts.RequireAuth = true
	s := newTestServer(&fakeDS{}, opts)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/evil/healthz", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rr.Code)
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

func TestRenderErrorEscapesAction(t *testing.T) {
	s := newTestServer(&fakeDS{}, DefaultOptions())
	rr := httptest.NewRecorder()
	s.renderError(rr, httptest.NewRequest(http.MethodGet, "/", nil), `<script>alert("x")</script>`, errors.New("boom"))
	body := rr.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("action was not escaped: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("escaped action not rendered: %s", body)
	}
}
