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

package persistence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// VisibilityIndexTemplate is the name of the Temporal visibility index template.
const VisibilityIndexTemplate = "temporal_visibility_v1_template"

// esBackend probes and manages an Elasticsearch visibility store over HTTP.
type esBackend struct {
	spec   *temporalv1alpha1.ElasticsearchDatastoreSpec
	cred   ResolvedCredential
	client *http.Client
}

func (b *esBackend) httpClient() *http.Client {
	if b.client != nil {
		return b.client
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (b *esBackend) baseURL() string {
	scheme := "http"
	if b.spec.TLS != nil && b.spec.TLS.Enabled {
		scheme = "https"
	}
	url := b.spec.URL
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return strings.TrimRight(url, "/")
	}
	return scheme + "://" + strings.TrimRight(url, "/")
}

func (b *esBackend) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, b.baseURL()+path, body)
	if err != nil {
		return nil, err
	}
	if b.spec.Username != "" {
		req.SetBasicAuth(b.spec.Username, b.cred.Password)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (b *esBackend) Probe(ctx context.Context) error {
	req, err := b.newRequest(ctx, http.MethodGet, "/_cluster/health", nil)
	if err != nil {
		return err
	}
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("connecting to elasticsearch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("elasticsearch health returned %d", resp.StatusCode)
	}
	return nil
}

// SchemaVersion reports the visibility index template version, or "" when the
// template has not yet been applied.
func (b *esBackend) SchemaVersion(ctx context.Context) (string, error) {
	req, err := b.newRequest(ctx, http.MethodGet, "/_index_template/"+VisibilityIndexTemplate, nil)
	if err != nil {
		return "", err
	}
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("checking elasticsearch index template: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("elasticsearch template check returned %d", resp.StatusCode)
	}
	return b.spec.Version, nil
}

// EnsureSchema applies the visibility index template. Elasticsearch manages
// schema inline (no Kubernetes Job), so this returns inline=true.
func (b *esBackend) EnsureSchema(ctx context.Context, _ string) (bool, error) {
	body := strings.NewReader(visibilityTemplateBody)
	req, err := b.newRequest(ctx, http.MethodPut, "/_index_template/"+VisibilityIndexTemplate, body)
	if err != nil {
		return true, err
	}
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return true, fmt.Errorf("applying elasticsearch index template: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return true, fmt.Errorf("elasticsearch template apply returned %d", resp.StatusCode)
	}
	return true, nil
}

func (b *esBackend) Kind() string { return "elasticsearch" }

// visibilityTemplateBody is a minimal Temporal visibility index template. The
// full mapping is managed by Temporal; this establishes the index pattern and
// core searchable fields so that the visibility index can be created.
const visibilityTemplateBody = `{
  "index_patterns": ["temporal_visibility_v1*"],
  "template": {
    "settings": {
      "number_of_shards": 1,
      "index.sort.field": ["CloseTime", "StartTime"],
      "index.sort.order": ["desc", "desc"]
    },
    "mappings": {
      "dynamic": "false",
      "properties": {
        "NamespaceId": {"type": "keyword"},
        "WorkflowId": {"type": "keyword"},
        "RunId": {"type": "keyword"},
        "WorkflowType": {"type": "keyword"},
        "StartTime": {"type": "date_nanos"},
        "CloseTime": {"type": "date_nanos"},
        "ExecutionStatus": {"type": "keyword"},
        "TaskQueue": {"type": "keyword"}
      }
    }
  }
}`
