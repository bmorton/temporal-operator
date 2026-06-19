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
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner executes a shell command and returns its trimmed stdout. It is
// used to resolve short-lived datastore credentials emitted by a user-supplied
// passwordCommand (e.g. an Entra access token).
type CommandRunner func(ctx context.Context, command string) (string, error)

// DefaultCommandRunner runs the command with "sh -c" and returns trimmed stdout.
func DefaultCommandRunner(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("executing password command: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("executing password command: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
