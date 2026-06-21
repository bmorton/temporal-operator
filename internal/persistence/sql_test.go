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
	"strings"
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func TestSQLBackendProbeUsesStaticPassword(t *testing.T) {
	b := &sqlBackend{spec: &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12", Host: "h", Port: 5432, User: "u",
	}, cred: ResolvedCredential{Password: "secret"}, dbName: "temporal"}
	got := b.dsn(b.cred.Password)
	if !strings.Contains(got, "u:secret@") {
		t.Fatalf("dsn = %q, want static password", got)
	}
}
