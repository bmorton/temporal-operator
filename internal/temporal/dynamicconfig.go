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

package temporal

import (
	"encoding/json"
	"fmt"
	"sort"

	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// dynamicConfigEntry mirrors a single Temporal dynamic config constrained value.
type dynamicConfigEntry struct {
	Value       interface{}            `json:"value"`
	Constraints map[string]interface{} `json:"constraints,omitempty"`
}

// RenderDynamicConfig renders a Temporal dynamicconfig.yaml document from the
// spec's dynamic config values. Keys are emitted in sorted order for
// deterministic output. Unknown keys (relative to the target version's matrix)
// are returned as warnings; keys removed in the target version cause an error.
func RenderDynamicConfig(spec *temporalv1alpha1.DynamicConfigSpec, version string) (rendered string, warnings []string, err error) {
	if spec == nil || len(spec.Values) == 0 {
		return "", nil, nil
	}

	var removed map[string]struct{}
	if info, lookupErr := LookupVersion(version); lookupErr == nil {
		removed = make(map[string]struct{}, len(info.RemovedDynamicConfig))
		for _, k := range info.RemovedDynamicConfig {
			removed[k] = struct{}{}
		}
	}

	keys := make([]string, 0, len(spec.Values))
	for k := range spec.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	doc := make(map[string][]dynamicConfigEntry, len(keys))
	for _, key := range keys {
		if removed != nil {
			if _, isRemoved := removed[key]; isRemoved {
				return "", warnings, fmt.Errorf("dynamic config key %q was removed in Temporal %s", key, version)
			}
		}
		if !isKnownDynamicConfigKey(key, version) {
			warnings = append(warnings, fmt.Sprintf("dynamic config key %q is not known for Temporal %s", key, version))
		}

		entries := make([]dynamicConfigEntry, 0, len(spec.Values[key]))
		for _, v := range spec.Values[key] {
			value, decodeErr := decodeRawValue(v.Value.Raw)
			if decodeErr != nil {
				return "", warnings, fmt.Errorf("dynamic config key %q: %w", key, decodeErr)
			}
			entry := dynamicConfigEntry{Value: value}
			if v.Constraints != nil {
				entry.Constraints = constraintsToMap(v.Constraints)
			}
			entries = append(entries, entry)
		}
		doc[key] = entries
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", warnings, fmt.Errorf("marshaling dynamic config: %w", err)
	}
	return string(out), warnings, nil
}

func decodeRawValue(raw []byte) (interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("invalid JSON value: %w", err)
	}
	return value, nil
}

func constraintsToMap(c *temporalv1alpha1.DynamicConfigConstraints) map[string]interface{} {
	m := map[string]interface{}{}
	if c.Namespace != "" {
		m["namespace"] = c.Namespace
	}
	if c.TaskQueueName != "" {
		m["taskQueueName"] = c.TaskQueueName
	}
	if c.TaskQueueType != "" {
		m["taskQueueType"] = c.TaskQueueType
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// isKnownDynamicConfigKey reports whether a dynamic config key is recognized for
// the given version. The current matrix does not enumerate the full key set, so
// this conservatively treats all keys as known unless explicitly removed; it is
// a hook for richer validation as the matrix grows.
func isKnownDynamicConfigKey(_ string, _ string) bool {
	return true
}
