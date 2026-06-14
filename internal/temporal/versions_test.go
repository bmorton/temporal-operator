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

import "testing"

func TestIsSupported(t *testing.T) {
	if !IsSupported("1.31.1") {
		t.Errorf("expected 1.31.1 to be supported")
	}
	if IsSupported("9.9.9") {
		t.Errorf("expected 9.9.9 to be unsupported")
	}
}

func TestCanUpgrade(t *testing.T) {
	cases := []struct {
		name    string
		from    string
		to      string
		want    bool
		wantErr bool
	}{
		{"patch bump", "1.31.0", "1.31.1", true, false},
		{"same version", "1.31.1", "1.31.1", true, false},
		{"adjacent minor", "1.30.0", "1.31.0", true, false},
		{"minor skip", "1.29.0", "1.31.0", false, false},
		{"downgrade minor", "1.31.0", "1.30.0", false, false},
		{"downgrade patch", "1.31.1", "1.31.0", false, false},
		{"unsupported target", "1.31.0", "9.9.9", false, true},
		{"bad from", "garbage", "1.31.1", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanUpgrade(tc.from, tc.to)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CanUpgrade(%q,%q) err = %v, wantErr %v", tc.from, tc.to, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("CanUpgrade(%q,%q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestDefaultUIVersion(t *testing.T) {
	if got := DefaultUIVersion("1.31.1"); got == "" {
		t.Errorf("expected a default UI version for 1.31.1")
	}
	if got := DefaultUIVersion("9.9.9"); got != "" {
		t.Errorf("expected empty default UI version for unknown server version, got %q", got)
	}
}

func TestSupportedVersionsAndGet(t *testing.T) {
	if len(SupportedVersions()) == 0 {
		t.Fatalf("expected at least one supported version")
	}
	if _, ok := Get("1.31.1"); !ok {
		t.Errorf("expected Get(1.31.1) to return info")
	}
	if _, ok := Get("nope"); ok {
		t.Errorf("expected Get(nope) to return false")
	}
}
