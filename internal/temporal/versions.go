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

// Package temporal contains Temporal-version-aware helpers: the supported
// version compatibility matrix and the server config-template renderer.
//
// The matrix data lives in versions_gen.go, which is generated from
// hack/version-matrix.yaml by `make gen-version-matrix`. Edit the YAML, not the
// generated Go.
package temporal

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// VersionInfo describes a single supported Temporal minor version.
type VersionInfo struct {
	// Version is the minor version, e.g. "1.31".
	Version string
	// PatchVersions are the exact supported patch releases, e.g. "1.31.1".
	PatchVersions []string
	// MinSchemaSQL is the minimum SQL schema version required.
	MinSchemaSQL string
	// MinSchemaCassandra is the minimum Cassandra schema version required.
	MinSchemaCassandra string
	// MinSchemaES is the minimum Elasticsearch schema/index template version.
	MinSchemaES string
	// AllowedFromVersions are the minor versions a cluster may upgrade from.
	AllowedFromVersions []string
	// UISeries is the compatible temporal-ui minor series, e.g. "2.34".
	UISeries string
	// DefaultUIVersion is the known-good exact temporal-ui version to pair with.
	DefaultUIVersion string
	// CassandraVisibilitySupported reports whether Cassandra may be used as a
	// visibility store on this version.
	CassandraVisibilitySupported bool
	// RemovedDynamicConfig lists dynamic config keys removed in this version.
	RemovedDynamicConfig []string
	// AddedDynamicConfig lists dynamic config keys added in this version.
	AddedDynamicConfig []string
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

// minorOf returns the "major.minor" prefix of a version string. The input may
// already be a minor ("1.31") or a full patch version ("1.31.1").
func minorOf(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// LookupVersion returns the VersionInfo for a version, which may be a minor
// ("1.31") or an exact patch ("1.31.1").
func LookupVersion(version string) (*VersionInfo, error) {
	minor := minorOf(version)
	for i := range supportedVersions {
		if supportedVersions[i].Version == minor {
			return &supportedVersions[i], nil
		}
	}
	return nil, fmt.Errorf("version %q is not supported", version)
}

// IsSupported reports whether the exact patch version is in the support matrix.
func IsSupported(version string) bool {
	minor := minorOf(version)
	for i := range supportedVersions {
		if supportedVersions[i].Version != minor {
			continue
		}
		for _, p := range supportedVersions[i].PatchVersions {
			if p == version {
				return true
			}
		}
	}
	return false
}

// Get returns the VersionInfo covering an exact patch version.
func Get(version string) (VersionInfo, bool) {
	if !IsSupported(version) {
		return VersionInfo{}, false
	}
	info, err := LookupVersion(version)
	if err != nil {
		return VersionInfo{}, false
	}
	return *info, true
}

// SupportedVersions returns the sorted list of supported exact patch versions.
func SupportedVersions() []string {
	var out []string
	for i := range supportedVersions {
		out = append(out, supportedVersions[i].PatchVersions...)
	}
	sort.Slice(out, func(i, j int) bool {
		a, errA := parseSemver(out[i])
		b, errB := parseSemver(out[j])
		if errA != nil || errB != nil {
			return out[i] < out[j]
		}
		if a.major != b.major {
			return a.major < b.major
		}
		if a.minor != b.minor {
			return a.minor < b.minor
		}
		return a.patch < b.patch
	})
	return out
}

// ResolveLatestPatch returns the highest supported patch version for a minor.
func ResolveLatestPatch(minor string) (string, error) {
	info, err := LookupVersion(minor)
	if err != nil {
		return "", err
	}
	if len(info.PatchVersions) == 0 {
		return "", fmt.Errorf("no patch versions registered for %q", minor)
	}
	latest := info.PatchVersions[0]
	latestVer, _ := parseSemver(latest)
	for _, p := range info.PatchVersions[1:] {
		v, err := parseSemver(p)
		if err != nil {
			continue
		}
		if v.patch > latestVer.patch {
			latest, latestVer = p, v
		}
	}
	return latest, nil
}

// ValidateUpgradePath returns nil if a cluster may move from version `from` to
// version `to` (both may be minor or exact).
func ValidateUpgradePath(from, to string) error {
	ok, err := CanUpgrade(from, to)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("upgrade from %s to %s is not allowed", from, to)
	}
	return nil
}

// CanUpgrade reports whether a cluster may move from version `from` to version `to`.
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
	if t.major == f.major && t.minor == f.minor {
		// Same minor: only forward (or equal) patch moves are allowed.
		return t.patch >= f.patch, nil
	}
	// Cross-minor: the target minor must explicitly allow the source minor.
	toInfo, err := LookupVersion(to)
	if err != nil {
		return false, err
	}
	fromMinor := minorOf(from)
	for _, allowed := range toInfo.AllowedFromVersions {
		if allowed == fromMinor {
			return true, nil
		}
	}
	return false, nil
}

// DefaultUIVersion returns the known-good UI version for a server version, or an
// empty string when the server version is unknown.
func DefaultUIVersion(serverVersion string) string {
	info, err := LookupVersion(serverVersion)
	if err != nil {
		return ""
	}
	return info.DefaultUIVersion
}

// ServerImage returns the default Temporal server image for a version.
func ServerImage(version string) string {
	return "temporalio/server:" + version
}

// AdminToolsImage returns the Temporal admin-tools image for a version, used to
// run schema setup and migrations.
func AdminToolsImage(version string) string {
	return "temporalio/admin-tools:" + version
}
