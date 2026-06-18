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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetVersionDeterministic(t *testing.T) {
	version := computeAssetVersion()
	if version == "" {
		t.Fatal("asset version empty")
	}
	if version != computeAssetVersion() {
		t.Fatal("asset version not deterministic")
	}
	if len(version) != 12 {
		t.Errorf("asset version length = %d, want 12", len(version))
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

func TestStaticHandlerSetsImmutableOn200(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	StaticHandler("/static/").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestStaticHandlerNoImmutableOn404(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/does-not-exist.js", nil)
	StaticHandler("/static/").ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
	}
	if cc := rr.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("404 must not be immutable-cached, got %q", cc)
	}
}
