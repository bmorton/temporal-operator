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

package layouts

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return b.String()
}

func TestBaseRendersRootBasePathURLs(t *testing.T) {
	html := renderComponent(t, Base(View{
		Title:    "Clusters",
		BasePath: "/",
		AssetVer: "abc123",
	}))

	for _, want := range []string{
		`href="/static/app.css?v=abc123"`,
		`src="/static/htmx.min.js?v=abc123"`,
		`src="/static/alpine.min.js?v=abc123"`,
		`src="/static/app.js?v=abc123"`,
		`href="/"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, `href="//`) || strings.Contains(html, `src="//`) {
		t.Errorf("rendered HTML contains protocol-relative URL:\n%s", html)
	}
}

func TestBaseRendersLiveControls(t *testing.T) {
	html := renderComponent(t, Base(View{Title: "Clusters", BasePath: "/", AssetVer: "abc123"}))
	for _, want := range []string{
		`$store.live.toggle()`,
		`$store.live.paused`,
		`x-text="$store.live.last`,
		`>Pause</button>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing live control %q:\n%s", want, html)
		}
	}
}

func TestBaseRendersPrefixedBasePathURLs(t *testing.T) {
	html := renderComponent(t, Base(View{
		Title:    "Clusters",
		BasePath: "/ops",
		AssetVer: "abc123",
	}))

	for _, want := range []string{
		`href="/ops/static/app.css?v=abc123"`,
		`src="/ops/static/htmx.min.js?v=abc123"`,
		`src="/ops/static/alpine.min.js?v=abc123"`,
		`src="/ops/static/app.js?v=abc123"`,
		`href="/ops/"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q:\n%s", want, html)
		}
	}
}
