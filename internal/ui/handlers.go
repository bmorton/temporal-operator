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
	"html"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/bmorton/temporal-operator/internal/ui/layouts"
	"github.com/bmorton/temporal-operator/internal/ui/pages"
)

// Handler builds the UI's HTTP handler (router + middleware).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	base := s.opts.BasePath
	if base == "/" {
		base = ""
	}

	mux.HandleFunc("GET "+base+"/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET "+base+"/static/", StaticHandler(JoinPath(s.opts.BasePath, "/static/")))

	mux.HandleFunc("GET "+base+"/{$}", s.handleOverview)
	if base != "" {
		// A non-root BasePath only registers "<base>/"; redirect the bare
		// "<base>" (a common entry URL / ingress rewrite) to it.
		target := base + "/"
		mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		})
	}
	mux.HandleFunc("GET "+base+"/partials/clusters", s.handleClustersPartial)
	mux.HandleFunc("GET "+base+"/clusters/{namespace}/{name}", s.handleClusterDetail)
	mux.HandleFunc("GET "+base+"/partials/clusters/{namespace}/{name}", s.handleClusterDetailPartial)

	return s.authMiddleware(mux)
}

// authMiddleware enforces RequireAuth (fails closed) but always allows /healthz.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.RequireAuth && r.URL.Path != JoinPath(s.basePath(), "/healthz") {
			if !s.opts.IdentityFrom(r).Authenticated {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) view(r *http.Request, title string) layouts.View {
	return layouts.View{
		Title:       title,
		BasePath:    strings.TrimSuffix(s.opts.BasePath, "/"),
		AssetVer:    AssetVersion(),
		User:        s.opts.IdentityFrom(r).User,
		RefreshSecs: int(s.opts.RefreshInterval.Seconds()),
	}
}

func (s *Server) basePath() string { return strings.TrimSuffix(s.opts.BasePath, "/") }

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	clusters, err := s.data.ListClusters(r.Context())
	if err != nil {
		s.renderError(w, r, "load clusters", err)
		return
	}
	s.render(w, r, pages.Overview(s.view(r, "Clusters"), clusters))
}

func (s *Server) handleClustersPartial(w http.ResponseWriter, r *http.Request) {
	clusters, err := s.data.ListClusters(r.Context())
	if err != nil {
		s.renderError(w, r, "load clusters", err)
		return
	}
	s.render(w, r, pages.OverviewGrid(s.basePath(), clusters))
}

func (s *Server) handleClusterDetail(w http.ResponseWriter, r *http.Request) {
	d, err := s.data.GetCluster(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		if apierrors.IsNotFound(err) {
			http.Error(w, "cluster not found", http.StatusNotFound)
			return
		}
		s.renderError(w, r, "load cluster", err)
		return
	}
	s.render(w, r, pages.ClusterDetailPage(s.view(r, d.Name), *d))
}

func (s *Server) handleClusterDetailPartial(w http.ResponseWriter, r *http.Request) {
	d, err := s.data.GetCluster(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		if apierrors.IsNotFound(err) {
			http.Error(w, "cluster not found", http.StatusNotFound)
			return
		}
		s.renderError(w, r, "load cluster", err)
		return
	}
	s.render(w, r, pages.DetailBody(*d))
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		s.log.Error(err, "rendering UI")
	}
}

func (s *Server) renderError(w http.ResponseWriter, _ *http.Request, action string, err error) {
	s.log.Error(err, "ui handler error", "action", action)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`<div class="empty">Failed to ` + html.EscapeString(action) + `. Check operator logs.</div>`))
}
