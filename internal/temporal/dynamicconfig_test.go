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
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func raw(s string) runtime.RawExtension { return runtime.RawExtension{Raw: []byte(s)} }

func TestRenderDynamicConfigEmpty(t *testing.T) {
	out, warnings, err := RenderDynamicConfig(nil, "1.31.2")
	if err != nil || out != "" || warnings != nil {
		t.Fatalf("expected empty render, got out=%q warnings=%v err=%v", out, warnings, err)
	}
}

func TestRenderDynamicConfig(t *testing.T) {
	spec := &temporalv1alpha1.DynamicConfigSpec{
		Values: map[string][]temporalv1alpha1.DynamicConfigValue{
			"frontend.enableClientVersionCheck": {
				{Value: raw("true")},
			},
			"history.persistenceMaxQPS": {
				{
					Value: raw("3000"),
					Constraints: &temporalv1alpha1.DynamicConfigConstraints{
						Namespace: "default",
					},
				},
			},
		},
	}

	out, warnings, err := RenderDynamicConfig(spec, "1.31.2")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_ = warnings

	// Output must be valid YAML and round-trip to the expected structure.
	var parsed map[string][]map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, out)
	}
	if _, ok := parsed["frontend.enableClientVersionCheck"]; !ok {
		t.Errorf("missing frontend key in output:\n%s", out)
	}
	if got := parsed["history.persistenceMaxQPS"][0]["constraints"]; got == nil {
		t.Errorf("expected constraints on history key:\n%s", out)
	}

	// Keys must be emitted in sorted order for determinism.
	if strings.Index(out, "frontend.enableClientVersionCheck") > strings.Index(out, "history.persistenceMaxQPS") {
		t.Errorf("expected sorted keys:\n%s", out)
	}
}

func TestRenderDynamicConfigRemovedKey(t *testing.T) {
	// Inject a removed key into the in-memory matrix for the test.
	info, err := LookupVersion("1.31")
	if err != nil {
		t.Fatal(err)
	}
	orig := info.RemovedDynamicConfig
	info.RemovedDynamicConfig = []string{"some.removedKey"}
	defer func() { info.RemovedDynamicConfig = orig }()

	spec := &temporalv1alpha1.DynamicConfigSpec{
		Values: map[string][]temporalv1alpha1.DynamicConfigValue{
			"some.removedKey": {{Value: raw("1")}},
		},
	}
	if _, _, err := RenderDynamicConfig(spec, "1.31.2"); err == nil {
		t.Errorf("expected error for removed dynamic config key")
	}
}
