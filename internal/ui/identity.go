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
	"strings"
)

// Identity is the request's authenticated user, derived from forward-auth headers.
type Identity struct {
	User          string
	Email         string
	Groups        []string
	Authenticated bool
}

// IdentityFrom extracts the Identity from the configured trusted headers.
func (o Options) IdentityFrom(r *http.Request) Identity {
	o = o.Normalize()
	user := strings.TrimSpace(r.Header.Get(o.UserHeader))
	id := Identity{
		User:          user,
		Email:         strings.TrimSpace(r.Header.Get(o.EmailHeader)),
		Authenticated: user != "",
	}
	if g := r.Header.Get(o.GroupsHeader); g != "" {
		for _, part := range strings.Split(g, ",") {
			if p := strings.TrimSpace(part); p != "" {
				id.Groups = append(id.Groups, p)
			}
		}
	}
	return id
}
