package temporal

import (
	"testing"
	"time"
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
