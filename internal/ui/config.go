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
	"strings"
	"time"
)

// Options configures the UI server.
type Options struct {
	// BindAddress is the listen address; empty disables the UI.
	BindAddress string
	// RefreshInterval is the htmx poll interval surfaced to templates.
	RefreshInterval time.Duration
	// BasePath is the URL prefix the UI is served under (no trailing slash).
	BasePath string
	// RequireAuth makes the server return 401 when no trusted user header is present.
	RequireAuth bool
	// UserHeader, GroupsHeader and EmailHeader are the trusted forward-auth headers.
	UserHeader   string
	GroupsHeader string
	EmailHeader  string
}

// DefaultOptions returns the UI options used when the UI is enabled.
//
// Note: BindAddress here (":8082") is the suggested address once the UI is
// turned on; it is NOT the runtime default. The CLI flag --ui-bind-address
// defaults to empty (disabled), and Normalize never fills BindAddress from
// these defaults, so an empty BindAddress keeps the UI off.
func DefaultOptions() Options {
	return Options{
		BindAddress:     ":8082",
		RefreshInterval: 5 * time.Second,
		BasePath:        "/",
		UserHeader:      "Remote-User",
		GroupsHeader:    "Remote-Groups",
		EmailHeader:     "Remote-Email",
	}
}

// Enabled reports whether the UI should run.
func (o Options) Enabled() bool { return o.BindAddress != "" }

// Normalize fills blank fields with defaults and tidies the base path.
func (o Options) Normalize() Options {
	d := DefaultOptions()
	if o.RefreshInterval <= 0 {
		o.RefreshInterval = d.RefreshInterval
	}
	if o.RefreshInterval < time.Second {
		// htmx hx-trigger uses whole seconds; clamp so sub-second
		// intervals never render as "every 0s".
		o.RefreshInterval = time.Second
	}
	if o.UserHeader == "" {
		o.UserHeader = d.UserHeader
	}
	if o.GroupsHeader == "" {
		o.GroupsHeader = d.GroupsHeader
	}
	if o.EmailHeader == "" {
		o.EmailHeader = d.EmailHeader
	}
	if o.BasePath == "" {
		o.BasePath = "/"
	}
	if o.BasePath != "/" {
		o.BasePath = "/" + strings.Trim(o.BasePath, "/")
	}
	return o
}
