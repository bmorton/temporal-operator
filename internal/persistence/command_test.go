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
	"context"
	"strings"
	"testing"
)

func TestDefaultCommandRunnerTrimsOutput(t *testing.T) {
	out, err := DefaultCommandRunner(context.Background(), "printf 'tok123\\n'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "tok123" {
		t.Errorf("expected trimmed %q, got %q", "tok123", out)
	}
}

func TestDefaultCommandRunnerError(t *testing.T) {
	if _, err := DefaultCommandRunner(context.Background(), "exit 3"); err == nil {
		t.Fatal("expected error from failing command")
	}
}

func TestDefaultCommandRunnerIncludesStderr(t *testing.T) {
	_, err := DefaultCommandRunner(context.Background(), "echo boom-detail >&2; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom-detail") {
		t.Errorf("expected stderr in error, got %q", err.Error())
	}
}
