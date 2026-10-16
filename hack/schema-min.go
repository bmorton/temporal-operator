//go:build ignore

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

// Command schema-min prints the schema version the operator requires for a
// store at a given Temporal server version, as recorded in
// hack/version-matrix.yaml, without the "v" prefix (e.g. "1.19"). The upgrade
// e2e uses it to assert that migrations actually ran.
//
// Usage: go run hack/schema-min.go <server-version> <sql|visibility|cassandra>
package main

import (
	"fmt"
	"os"

	"github.com/bmorton/temporal-operator/internal/persistence"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: schema-min <server-version> <sql|visibility|cassandra>")
		os.Exit(2)
	}
	info, err := temporal.LookupVersion(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "schema-min:", err)
		os.Exit(1)
	}
	var v string
	switch os.Args[2] {
	case "sql":
		v = info.MinSchemaSQL
	case "visibility":
		v = info.MinSchemaSQLVisibility
	case "cassandra":
		v = info.MinSchemaCassandra
	default:
		fmt.Fprintf(os.Stderr, "schema-min: unknown store %q\n", os.Args[2])
		os.Exit(2)
	}
	fmt.Println(persistence.NormalizeSchemaVersion(v))
}
