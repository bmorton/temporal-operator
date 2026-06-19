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
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// Config is the proxy's runtime configuration, loaded from a mounted file.
type Config struct {
	Mode   Mode          `json:"mode"`
	Listen string        `json:"listen"`
	Source BackendConfig `json:"source"`
	Target BackendConfig `json:"target"`
}

// BackendConfig describes how to dial one upstream cluster frontend.
type BackendConfig struct {
	Address string `json:"address"`
	// TLS, when non-nil, enables TLS to this backend.
	TLS *BackendTLS `json:"tls,omitempty"`
}

// BackendTLS configures TLS/mTLS to a backend. Paths point at mounted secrets.
type BackendTLS struct {
	CAFile     string `json:"caFile,omitempty"`
	CertFile   string `json:"certFile,omitempty"`
	KeyFile    string `json:"keyFile,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	// Insecure skips server cert verification (testing only).
	Insecure bool `json:"insecure,omitempty"`
}

// LoadConfig reads and parses the proxy config file.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading proxy config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parsing proxy config: %w", err)
	}
	if cfg.Listen == "" {
		cfg.Listen = ":7233"
	}
	if cfg.Mode == "" {
		cfg.Mode = ModePassthrough
	}
	return &cfg, nil
}
