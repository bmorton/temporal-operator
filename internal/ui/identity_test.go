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

package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityFromHeaders(t *testing.T) {
	o := DefaultOptions()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Remote-User", "alice")
	r.Header.Set("Remote-Email", "alice@example.com")
	r.Header.Set("Remote-Groups", "admins, ops")

	id := o.IdentityFrom(r)
	if !id.Authenticated {
		t.Fatal("expected authenticated")
	}
	if id.User != "alice" || id.Email != "alice@example.com" {
		t.Errorf("identity = %+v", id)
	}
	if len(id.Groups) != 2 || id.Groups[0] != "admins" || id.Groups[1] != "ops" {
		t.Errorf("groups = %v", id.Groups)
	}
}

func TestIdentityAnonymous(t *testing.T) {
	o := DefaultOptions()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if o.IdentityFrom(r).Authenticated {
		t.Error("expected anonymous")
	}
}

func TestIdentityWhitespaceUserIsAnonymous(t *testing.T) {
	o := DefaultOptions()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Remote-User", "   ")
	if o.IdentityFrom(r).Authenticated {
		t.Error("expected anonymous")
	}
}
