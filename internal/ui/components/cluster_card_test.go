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

package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
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

func TestClusterCardRendersRootBasePathURL(t *testing.T) {
	html := renderComponent(t, ClusterCard("/", ui.ClusterSummary{
		Namespace: "default",
		Name:      "demo",
	}))

	if !strings.Contains(html, `href="/clusters/default/demo"`) {
		t.Errorf("rendered HTML missing cluster link:\n%s", html)
	}
	if strings.Contains(html, `href="//`) {
		t.Errorf("rendered HTML contains protocol-relative URL:\n%s", html)
	}
}

func TestClusterCardRendersPrefixedBasePathURL(t *testing.T) {
	html := renderComponent(t, ClusterCard("/ops", ui.ClusterSummary{
		Namespace: "default",
		Name:      "demo",
	}))

	if !strings.Contains(html, `href="/ops/clusters/default/demo"`) {
		t.Errorf("rendered HTML missing prefixed cluster link:\n%s", html)
	}
}
