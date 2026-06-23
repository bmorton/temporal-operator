package temporal

import (
	"testing"
	"time"

	namespacepb "go.temporal.io/api/namespace/v1"
	operatorservice "go.temporal.io/api/operatorservice/v1"
	replicationpb "go.temporal.io/api/replication/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
)

func TestNamespaceParamsIsGlobal(t *testing.T) {
	params := NamespaceParams{
		Name:            "test",
		Description:     "desc",
		OwnerEmail:      "owner@example.com",
		RetentionPeriod: 72 * time.Hour,
		IsGlobal:        true,
	}
	if !params.IsGlobal {
		t.Error("IsGlobal should be true")
	}
}

func TestRemoteClusterInfoMapping(t *testing.T) {
	in := &operatorservice.ClusterMetadata{
		ClusterName:            "clusterB",
		Address:                "b.example.com:7233",
		InitialFailoverVersion: 2,
		IsConnectionEnabled:    true,
		HistoryShardCount:      512,
	}
	got := remoteClusterInfoFromProto(in)
	if got.Name != "clusterB" || got.Address != "b.example.com:7233" ||
		got.InitialFailoverVersion != 2 || !got.ConnectionEnabled || got.HistoryShardCount != 512 {
		t.Fatalf("unexpected mapping: %+v", got)
	}
}

func TestNamespaceReplicationRegisterRequest(t *testing.T) {
	params := NamespaceParams{
		Name:            "global-ns",
		IsGlobal:        true,
		ActiveCluster:   "a",
		Clusters:        []string{"a", "b"},
		RetentionPeriod: 72 * time.Hour,
	}
	req := registerNamespaceRequest(params)
	if !req.GetIsGlobalNamespace() {
		t.Error("expected IsGlobalNamespace=true")
	}
	if req.GetActiveClusterName() != "a" {
		t.Errorf("expected ActiveClusterName=a, got %q", req.GetActiveClusterName())
	}
	got := []string{}
	for _, c := range req.GetClusters() {
		got = append(got, c.GetClusterName())
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("expected clusters [a b], got %v", got)
	}
}

func TestNamespaceReplicationUpdateRequestFailover(t *testing.T) {
	params := NamespaceParams{
		Name:            "global-ns",
		IsGlobal:        true,
		ActiveCluster:   "b",
		Clusters:        []string{"a", "b"},
		RetentionPeriod: 72 * time.Hour,
	}
	req := updateNamespaceRequest(params)
	if req.GetReplicationConfig() == nil {
		t.Fatal("expected ReplicationConfig to be set for failover")
	}
	if req.GetReplicationConfig().GetActiveClusterName() != "b" {
		t.Errorf("expected failover ActiveClusterName=b, got %q", req.GetReplicationConfig().GetActiveClusterName())
	}
}

func TestNamespaceReplicationUpdateRequestLocal(t *testing.T) {
	params := NamespaceParams{
		Name:            "local-ns",
		RetentionPeriod: 72 * time.Hour,
	}
	req := updateNamespaceRequest(params)
	if req.GetReplicationConfig() != nil {
		t.Error("expected no ReplicationConfig for a non-global namespace")
	}
}

func TestNamespaceInfoFromProto(t *testing.T) {
	resp := &workflowservice.DescribeNamespaceResponse{
		NamespaceInfo: &namespacepb.NamespaceInfo{
			Id:          "uuid-1",
			Description: "desc",
			OwnerEmail:  "owner@example.com",
		},
		IsGlobalNamespace: true,
		ReplicationConfig: &replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: "b",
			Clusters: []*replicationpb.ClusterReplicationConfig{
				{ClusterName: "a"},
				{ClusterName: "b"},
			},
		},
	}
	info := namespaceInfoFromProto(resp)
	if !info.IsGlobal {
		t.Error("expected IsGlobal=true")
	}
	if info.ActiveCluster != "b" {
		t.Errorf("expected ActiveCluster=b, got %q", info.ActiveCluster)
	}
	if len(info.Clusters) != 2 || info.Clusters[0] != "a" || info.Clusters[1] != "b" {
		t.Errorf("expected clusters [a b], got %v", info.Clusters)
	}
	if info.ID != "uuid-1" || info.Description != "desc" || info.OwnerEmail != "owner@example.com" {
		t.Errorf("unexpected base info mapping: %+v", info)
	}
}
