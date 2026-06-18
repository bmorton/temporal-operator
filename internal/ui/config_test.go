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
	"testing"
	"time"
)

const opsBasePath = "/ops"

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if o.BindAddress != ":8082" {
		t.Errorf("BindAddress = %q, want :8082", o.BindAddress)
	}
	if o.RefreshInterval != 5*time.Second {
		t.Errorf("RefreshInterval = %v, want 5s", o.RefreshInterval)
	}
	if o.UserHeader != "Remote-User" {
		t.Errorf("UserHeader = %q, want Remote-User", o.UserHeader)
	}
}

func TestNormalizeFillsBlanks(t *testing.T) {
	o := Options{}.Normalize()
	if o.BasePath != "/" {
		t.Errorf("BasePath = %q, want /", o.BasePath)
	}
	if o.UserHeader != "Remote-User" {
		t.Errorf("UserHeader = %q, want Remote-User", o.UserHeader)
	}
	if o.RefreshInterval != 5*time.Second {
		t.Errorf("RefreshInterval = %v, want 5s", o.RefreshInterval)
	}
}

func TestNormalizePreservesExplicitValues(t *testing.T) {
	o := Options{
		BindAddress:     ":9000",
		RefreshInterval: 30 * time.Second,
		BasePath:        opsBasePath,
		UserHeader:      "X-User",
		GroupsHeader:    "X-Groups",
		EmailHeader:     "X-Email",
	}.Normalize()
	if o.BindAddress != ":9000" {
		t.Errorf("BindAddress = %q, want :9000", o.BindAddress)
	}
	if o.RefreshInterval != 30*time.Second {
		t.Errorf("RefreshInterval = %v, want 30s", o.RefreshInterval)
	}
	if o.BasePath != opsBasePath {
		t.Errorf("BasePath = %q, want %s", o.BasePath, opsBasePath)
	}
	if o.UserHeader != "X-User" || o.GroupsHeader != "X-Groups" || o.EmailHeader != "X-Email" {
		t.Errorf("headers not preserved: %+v", o)
	}
}

func TestNormalizeAddsLeadingSlash(t *testing.T) {
	o := Options{BasePath: "ops"}.Normalize()
	if o.BasePath != opsBasePath {
		t.Errorf("BasePath = %q, want %s", o.BasePath, opsBasePath)
	}
	d := DefaultOptions()
	def := Options{}.Normalize()
	if def.GroupsHeader != d.GroupsHeader || def.EmailHeader != d.EmailHeader {
		t.Errorf("group/email defaults wrong: %+v", def)
	}
}

func TestNormalizeTrimsTrailingSlash(t *testing.T) {
	o := Options{BasePath: "/ops/"}.Normalize()
	if o.BasePath != opsBasePath {
		t.Errorf("BasePath = %q, want %s", o.BasePath, opsBasePath)
	}
}

func TestEnabled(t *testing.T) {
	if (Options{BindAddress: ""}).Enabled() {
		t.Error("empty BindAddress should be disabled")
	}
	if !(Options{BindAddress: ":8082"}).Enabled() {
		t.Error(":8082 should be enabled")
	}
}
