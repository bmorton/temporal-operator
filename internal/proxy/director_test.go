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

package proxy

import "testing"

func TestDirectorRoute(t *testing.T) {
	d := Director{Mode: ModePassthrough}
	// Passthrough: everything to source, no fallback.
	for _, c := range []MethodClass{ClassStart, ClassExisting, ClassPoll, ClassPassthrough} {
		primary, secondary := d.Route(c)
		if primary != BackendSource || secondary != BackendNone {
			t.Fatalf("passthrough class %v -> (%v,%v), want (source,none)", c, primary, secondary)
		}
	}

	d.Mode = ModeCutover
	tests := []struct {
		class             MethodClass
		primary, fallback Backend
	}{
		{ClassStart, BackendTarget, BackendNone},
		{ClassExisting, BackendTarget, BackendSource},
		{ClassPoll, BackendSource, BackendNone},
		{ClassPassthrough, BackendTarget, BackendNone},
	}
	for _, tt := range tests {
		primary, secondary := d.Route(tt.class)
		if primary != tt.primary || secondary != tt.fallback {
			t.Errorf("cutover class %v -> (%v,%v), want (%v,%v)", tt.class, primary, secondary, tt.primary, tt.fallback)
		}
	}
}
