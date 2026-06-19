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
