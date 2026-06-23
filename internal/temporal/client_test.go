package temporal

import (
	"testing"
	"time"

	operatorservice "go.temporal.io/api/operatorservice/v1"
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
