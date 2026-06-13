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

// Package temporal contains Temporal-version-aware helpers, including the
// supported-version compatibility matrix used by webhooks and controllers.
//
// This is an intentionally minimal seed of the matrix; Milestone 4 expands it
// with config-template-affecting details (e.g. per-version visibility backend
// support and default UI versions).
package temporal

import (
	"fmt"
	"strconv"
	"strings"
)

// VersionInfo describes a single supported Temporal server version.
type VersionInfo struct {
	// Version is the exact semantic version, e.g. "1.31.2".
	Version string
	// DefaultUIVersion is the known-good temporal-ui version to pair with this
	// server version.
	DefaultUIVersion string
	// CassandraVisibilitySupported reports whether Cassandra may be used as a
	// visibility store on this server version.
	CassandraVisibilitySupported bool
}

// supportedVersions is the compatibility matrix keyed by exact version.
var supportedVersions = map[string]VersionInfo{
	"1.27.0": {Version: "1.27.0", DefaultUIVersion: "2.31.2", CassandraVisibilitySupported: false},
	"1.28.0": {Version: "1.28.0", DefaultUIVersion: "2.32.0", CassandraVisibilitySupported: false},
	"1.29.0": {Version: "1.29.0", DefaultUIVersion: "2.33.1", CassandraVisibilitySupported: false},
	"1.30.0": {Version: "1.30.0", DefaultUIVersion: "2.34.0", CassandraVisibilitySupported: false},
	"1.31.0": {Version: "1.31.0", DefaultUIVersion: "2.34.0", CassandraVisibilitySupported: false},
	"1.31.2": {Version: "1.31.2", DefaultUIVersion: "2.34.0", CassandraVisibilitySupported: false},
}

// semver is a parsed major.minor.patch version.
type semver struct {
	major, minor, patch int
}

func parseSemver(v string) (semver, error) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("invalid version %q: expected major.minor.patch", v)
	}
	out := semver{}
	for i, dst := range []*int{&out.major, &out.minor, &out.patch} {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return semver{}, fmt.Errorf("invalid version %q: %w", v, err)
		}
		*dst = n
	}
	return out, nil
}

// IsSupported reports whether the exact version is in the support matrix.
func IsSupported(version string) bool {
	_, ok := supportedVersions[version]
	return ok
}

// Get returns the VersionInfo for an exact version.
func Get(version string) (VersionInfo, bool) {
	info, ok := supportedVersions[version]
	return info, ok
}

// SupportedVersions returns the (unsorted) list of supported exact versions.
func SupportedVersions() []string {
	out := make([]string, 0, len(supportedVersions))
	for v := range supportedVersions {
		out = append(out, v)
	}
	return out
}

// CanUpgrade reports whether a cluster may move from version `from` to version
// `to`. Upgrades may only stay within the same minor (patch bump) or advance to
// the immediately following minor; downgrades and minor skips are disallowed.
func CanUpgrade(from, to string) (bool, error) {
	if !IsSupported(to) {
		return false, fmt.Errorf("version %q is not supported", to)
	}
	f, err := parseSemver(from)
	if err != nil {
		return false, err
	}
	t, err := parseSemver(to)
	if err != nil {
		return false, err
	}
	if t.major != f.major {
		return false, nil
	}
	switch t.minor {
	case f.minor:
		return t.patch >= f.patch, nil
	case f.minor + 1:
		return true, nil
	default:
		return false, nil
	}
}

// DefaultUIVersion returns the known-good UI version for a server version, or
// an empty string when the server version is unknown.
func DefaultUIVersion(serverVersion string) string {
	if info, ok := supportedVersions[serverVersion]; ok {
		return info.DefaultUIVersion
	}
	return ""
}
