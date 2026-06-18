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

package v1alpha1

import "testing"

func TestNamespaceMappingTargetOrSource(t *testing.T) {
	m := NamespaceMapping{Source: "orders"}
	if got := m.TargetOrSource(); got != "orders" {
		t.Fatalf("default target = %q, want orders", got)
	}
	m.Target = "orders-new"
	if got := m.TargetOrSource(); got != "orders-new" {
		t.Fatalf("explicit target = %q, want orders-new", got)
	}
}
