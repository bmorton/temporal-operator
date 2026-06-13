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
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	namespacepb "go.temporal.io/api/namespace/v1"
	operatorservice "go.temporal.io/api/operatorservice/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
)

// ErrNamespaceNotFound is returned by Describe when the namespace does not exist.
var ErrNamespaceNotFound = errors.New("namespace not found")

// NamespaceParams describes the desired state of a Temporal namespace.
type NamespaceParams struct {
	Name            string
	Description     string
	OwnerEmail      string
	RetentionPeriod time.Duration
}

// NamespaceInfo is the observed state of a Temporal namespace.
type NamespaceInfo struct {
	ID              string
	Description     string
	OwnerEmail      string
	RetentionPeriod time.Duration
}

// NamespaceClient manages namespaces in a Temporal cluster.
type NamespaceClient interface {
	Describe(ctx context.Context, name string) (*NamespaceInfo, error)
	Register(ctx context.Context, params NamespaceParams) error
	Update(ctx context.Context, params NamespaceParams) error
	Delete(ctx context.Context, name string) error
	Close() error
}

// NamespaceClientFactory builds a NamespaceClient connected to a frontend
// address. A nil tlsConfig means an insecure connection.
type NamespaceClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (NamespaceClient, error)

// grpcNamespaceClient is the default gRPC-backed NamespaceClient.
type grpcNamespaceClient struct {
	conn     *grpc.ClientConn
	workflow workflowservice.WorkflowServiceClient
	operator operatorservice.OperatorServiceClient
}

// NewNamespaceClient dials the frontend and returns a NamespaceClient.
func NewNamespaceClient(_ context.Context, address string, tlsConfig *tls.Config) (NamespaceClient, error) {
	creds := insecure.NewCredentials()
	if tlsConfig != nil {
		creds = credentials.NewTLS(tlsConfig)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &grpcNamespaceClient{
		conn:     conn,
		workflow: workflowservice.NewWorkflowServiceClient(conn),
		operator: operatorservice.NewOperatorServiceClient(conn),
	}, nil
}

func (c *grpcNamespaceClient) Describe(ctx context.Context, name string) (*NamespaceInfo, error) {
	resp, err := c.workflow.DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: name})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrNamespaceNotFound
		}
		return nil, err
	}
	info := &NamespaceInfo{}
	if resp.GetNamespaceInfo() != nil {
		info.ID = resp.GetNamespaceInfo().GetId()
		info.Description = resp.GetNamespaceInfo().GetDescription()
		info.OwnerEmail = resp.GetNamespaceInfo().GetOwnerEmail()
	}
	if resp.GetConfig().GetWorkflowExecutionRetentionTtl() != nil {
		info.RetentionPeriod = resp.GetConfig().GetWorkflowExecutionRetentionTtl().AsDuration()
	}
	return info, nil
}

func (c *grpcNamespaceClient) Register(ctx context.Context, params NamespaceParams) error {
	_, err := c.workflow.RegisterNamespace(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        params.Name,
		Description:                      params.Description,
		OwnerEmail:                       params.OwnerEmail,
		WorkflowExecutionRetentionPeriod: durationpb.New(params.RetentionPeriod),
	})
	return err
}

func (c *grpcNamespaceClient) Update(ctx context.Context, params NamespaceParams) error {
	_, err := c.workflow.UpdateNamespace(ctx, &workflowservice.UpdateNamespaceRequest{
		Namespace: params.Name,
		UpdateInfo: &namespacepb.UpdateNamespaceInfo{
			Description: params.Description,
			OwnerEmail:  params.OwnerEmail,
		},
		Config: &namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionTtl: durationpb.New(params.RetentionPeriod),
		},
	})
	return err
}

func (c *grpcNamespaceClient) Delete(ctx context.Context, name string) error {
	_, err := c.operator.DeleteNamespace(ctx, &operatorservice.DeleteNamespaceRequest{Namespace: name})
	return err
}

func (c *grpcNamespaceClient) Close() error {
	return c.conn.Close()
}
