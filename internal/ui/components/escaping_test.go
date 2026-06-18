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

package components

import (
	"strings"
	"testing"

	ui "github.com/bmorton/temporal-operator/internal/ui/model"
)

func TestConditionListEscapesInput(t *testing.T) {
	rows := []ui.ConditionRow{{
		Type:    "Ready",
		Status:  "False",
		Reason:  "x",
		Message: `<script>alert(1)</script>`,
		State:   ui.BadgeError,
	}}

	out := renderComponent(t, ConditionList(rows))
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Error("message was not escaped")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped output, got: %s", out)
	}
}
