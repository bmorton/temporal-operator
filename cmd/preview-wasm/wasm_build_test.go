//go:build !js

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

package main

import (
	"os/exec"
	"testing"
)

// TestWASMCompiles guards against changes that break the js/wasm build of the
// preview shim, which would silently break the docs site.
func TestWASMCompiles(t *testing.T) {
	cmd := exec.Command("go", "build", "-o", t.TempDir()+"/preview.wasm", ".")
	cmd.Env = append(cmd.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("js/wasm build failed: %v\n%s", err, out)
	}
}
