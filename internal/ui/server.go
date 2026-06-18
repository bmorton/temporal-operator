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
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-logr/logr"
)

// Server serves the operator UI. It implements manager.Runnable and
// manager.LeaderElectionRunnable (so it runs on every replica).
type Server struct {
	opts Options
	data DataSource
	log  logr.Logger
}

// NewServer builds a UI server.
func NewServer(opts Options, data DataSource, log logr.Logger) *Server {
	return &Server{opts: opts.Normalize(), data: data, log: log}
}

// NeedLeaderElection returns false so the UI runs regardless of leadership.
func (s *Server) NeedLeaderElection() bool { return false }

// Start runs the HTTP server until ctx is cancelled (manager.Runnable).
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.opts.BindAddress,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("starting UI server", "address", s.opts.BindAddress, "basePath", s.opts.BasePath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
