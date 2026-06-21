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
	"encoding/json"
	"testing"
	"time"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func TestInspectResultJSONRoundTrip(t *testing.T) {
	r := InspectResult{Reachable: true, Version: "1.13"}
	var got InspectResult
	if err := json.Unmarshal([]byte(r.JSON()), &got); err != nil {
		t.Fatal(err)
	}
	if got != r {
		t.Fatalf("round-trip = %+v, want %+v", got, r)
	}
}

func TestInspectSQLUnreachable(t *testing.T) {
	// Test with an unreachable host - use a private network IP that won't route
	spec := &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12",
		Host:       "10.255.255.1", // Unreachable private IP
		Port:       5432,
		User:       "test",
		Database:   "test",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := InspectSQL(ctx, spec, "testpass", "test")

	if result.Reachable {
		t.Error("Expected Reachable=false for unreachable host")
	}
	if result.Error == "" {
		t.Error("Expected non-empty Error for unreachable host")
	}
	if result.Version != "" {
		t.Errorf("Expected empty Version, got %q", result.Version)
	}
}
