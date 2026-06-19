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

// Mode is the proxy's current routing mode.
type Mode string

const (
	// ModePassthrough forwards all traffic to the source cluster.
	ModePassthrough Mode = "passthrough"
	// ModeCutover routes new workflows to the target with source fallback.
	ModeCutover Mode = "cutover"
)

// Backend identifies a routing destination.
type Backend int

const (
	// BackendNone means no (fallback) backend.
	BackendNone Backend = iota
	// BackendSource is the external source cluster.
	BackendSource
	// BackendTarget is the operator-managed target cluster.
	BackendTarget
)

// Director decides routing based on the current mode.
type Director struct {
	Mode Mode
}

// Route returns the primary backend and an optional fallback backend
// (BackendNone when there is no fallback) for a method class.
func (d Director) Route(class MethodClass) (primary, fallback Backend) {
	if d.Mode == ModePassthrough {
		return BackendSource, BackendNone
	}
	// Cutover mode.
	switch class {
	case ClassStart:
		return BackendTarget, BackendNone
	case ClassExisting:
		return BackendTarget, BackendSource
	case ClassPoll:
		return BackendSource, BackendNone
	default: // ClassPassthrough
		return BackendTarget, BackendNone
	}
}
