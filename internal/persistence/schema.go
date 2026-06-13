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
	"strconv"
	"strings"
)

// NormalizeSchemaVersion strips an optional leading "v" from a schema version
// string (e.g. "v1.12" -> "1.12").
func NormalizeSchemaVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// CompareSchemaVersions compares two dotted numeric schema versions. It returns
// -1 if a < b, 0 if equal, and 1 if a > b. Non-numeric or missing components are
// treated as zero. An empty string is treated as the lowest possible version.
func CompareSchemaVersions(a, b string) int {
	as := strings.Split(NormalizeSchemaVersion(a), ".")
	bs := strings.Split(NormalizeSchemaVersion(b), ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai := componentAt(as, i)
		bi := componentAt(bs, i)
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

func componentAt(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}

// SchemaSatisfies reports whether a current schema version meets or exceeds the
// required minimum. An empty current version never satisfies the requirement.
func SchemaSatisfies(current, required string) bool {
	if strings.TrimSpace(current) == "" {
		return false
	}
	return CompareSchemaVersions(current, required) >= 0
}
