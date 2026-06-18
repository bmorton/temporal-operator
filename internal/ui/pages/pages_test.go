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

package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/bmorton/temporal-operator/internal/ui/layouts"
	ui "github.com/bmorton/temporal-operator/internal/ui/model"
)

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return b.String()
}

func TestOverviewRendersRootPartialURL(t *testing.T) {
	html := renderComponent(t, Overview(layouts.View{
		Title:       "Clusters",
		BasePath:    "/",
		AssetVer:    "abc123",
		RefreshSecs: 5,
	}, nil))

	if !strings.Contains(html, `hx-get="/partials/clusters"`) {
		t.Errorf("rendered HTML missing root partial URL:\n%s", html)
	}
	if strings.Contains(html, `hx-get="//`) {
		t.Errorf("rendered HTML contains protocol-relative htmx URL:\n%s", html)
	}
}

func TestOverviewRendersPrefixedPartialURL(t *testing.T) {
	html := renderComponent(t, Overview(layouts.View{
		Title:       "Clusters",
		BasePath:    "/ops",
		AssetVer:    "abc123",
		RefreshSecs: 5,
	}, nil))

	if !strings.Contains(html, `hx-get="/ops/partials/clusters"`) {
		t.Errorf("rendered HTML missing prefixed partial URL:\n%s", html)
	}
}

func TestClusterDetailPageRendersRootURLs(t *testing.T) {
	html := renderComponent(t, ClusterDetailPage(layouts.View{
		Title:       "demo",
		BasePath:    "/",
		AssetVer:    "abc123",
		RefreshSecs: 5,
	}, ui.ClusterDetail{
		ClusterSummary: ui.ClusterSummary{
			Namespace: "default",
			Name:      "demo",
		},
	}))

	for _, want := range []string{
		`href="/"`,
		`hx-get="/partials/clusters/default/demo"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, `href="//`) || strings.Contains(html, `hx-get="//`) {
		t.Errorf("rendered HTML contains protocol-relative URL:\n%s", html)
	}
}

func TestClusterDetailPageRendersPrefixedURLs(t *testing.T) {
	html := renderComponent(t, ClusterDetailPage(layouts.View{
		Title:       "demo",
		BasePath:    "/ops",
		AssetVer:    "abc123",
		RefreshSecs: 5,
	}, ui.ClusterDetail{
		ClusterSummary: ui.ClusterSummary{
			Namespace: "default",
			Name:      "demo",
		},
	}))

	for _, want := range []string{
		`href="/ops/"`,
		`hx-get="/ops/partials/clusters/default/demo"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q:\n%s", want, html)
		}
	}
}
