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

package temporal

import (
	"context"
	"crypto/tls"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	workflowservice "go.temporal.io/api/workflowservice/v1"
)

// RunningWorkflowsQuery is the visibility query selecting open workflows.
func RunningWorkflowsQuery() string { return `ExecutionStatus="Running"` }

// MigrationClient inspects a cluster for migration drain detection.
type MigrationClient interface {
	// ListNamespaces returns non-system namespace names.
	ListNamespaces(ctx context.Context) ([]string, error)
	// CountRunningWorkflows returns the count of open workflows in a namespace.
	CountRunningWorkflows(ctx context.Context, namespace string) (int64, error)
	Close() error
}

// MigrationClientFactory builds a MigrationClient for a frontend address.
type MigrationClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (MigrationClient, error)

type grpcMigrationClient struct {
	conn     *grpc.ClientConn
	workflow workflowservice.WorkflowServiceClient
}

// NewMigrationClient dials a frontend and returns a MigrationClient.
func NewMigrationClient(_ context.Context, address string, tlsConfig *tls.Config) (MigrationClient, error) {
	creds := insecure.NewCredentials()
	if tlsConfig != nil {
		creds = credentials.NewTLS(tlsConfig)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &grpcMigrationClient{conn: conn, workflow: workflowservice.NewWorkflowServiceClient(conn)}, nil
}

func (c *grpcMigrationClient) ListNamespaces(ctx context.Context) ([]string, error) {
	var out []string
	var token []byte
	for {
		resp, err := c.workflow.ListNamespaces(ctx, &workflowservice.ListNamespacesRequest{
			PageSize:      100,
			NextPageToken: token,
		})
		if err != nil {
			return nil, err
		}
		for _, ns := range resp.GetNamespaces() {
			name := ns.GetNamespaceInfo().GetName()
			if name == "temporal-system" {
				continue
			}
			out = append(out, name)
		}
		token = resp.GetNextPageToken()
		if len(token) == 0 {
			break
		}
	}
	return out, nil
}

func (c *grpcMigrationClient) CountRunningWorkflows(ctx context.Context, namespace string) (int64, error) {
	resp, err := c.workflow.CountWorkflowExecutions(ctx, &workflowservice.CountWorkflowExecutionsRequest{
		Namespace: namespace,
		Query:     RunningWorkflowsQuery(),
	})
	if err != nil {
		return 0, err
	}
	return resp.GetCount(), nil
}

func (c *grpcMigrationClient) Close() error { return c.conn.Close() }
