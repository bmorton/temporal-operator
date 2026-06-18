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

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Server is a configured migration proxy ready to serve.
type Server struct {
	grpc    *grpc.Server
	lis     net.Listener
	handler *Handler
}

// NewServer dials both backends and builds the gRPC server from cfg.
func NewServer(cfg *Config) (*Server, error) {
	source, err := dial(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("dialing source: %w", err)
	}
	target, err := dial(cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("dialing target: %w", err)
	}
	h := &Handler{Director: Director{Mode: cfg.Mode}, Source: source, Target: target}

	lis, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Listen, err)
	}
	s := grpc.NewServer(
		grpc.ForceServerCodec(RawCodec{}),
		grpc.UnknownServiceHandler(h.Stream),
	)
	return &Server{grpc: s, lis: lis, handler: h}, nil
}

// Serve blocks serving gRPC until the server is stopped.
func (s *Server) Serve() error { return s.grpc.Serve(s.lis) }

// Stop gracefully stops the server.
func (s *Server) Stop() { s.grpc.GracefulStop() }

func dial(b BackendConfig) (*grpc.ClientConn, error) {
	creds := insecure.NewCredentials()
	if b.TLS != nil {
		tc, err := tlsConfig(b.TLS)
		if err != nil {
			return nil, err
		}
		creds = credentials.NewTLS(tc)
	}
	return grpc.NewClient(b.Address, grpc.WithTransportCredentials(creds))
}

func tlsConfig(t *BackendTLS) (*tls.Config, error) {
	cfg := &tls.Config{
		ServerName:         t.ServerName,
		InsecureSkipVerify: t.Insecure, //nolint:gosec // Insecure is opt-in for testing
		MinVersion:         tls.VersionTLS12,
	}
	if t.CAFile != "" {
		ca, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("invalid CA in %s", t.CAFile)
		}
		cfg.RootCAs = pool
	}
	if t.CertFile != "" && t.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
