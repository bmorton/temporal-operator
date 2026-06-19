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

package main

import (
	"flag"
	"log"

	"github.com/bmorton/temporal-operator/internal/proxy"
)

func main() {
	configPath := flag.String("config", "/etc/migration-proxy/config.yaml", "path to proxy config file")
	flag.Parse()

	cfg, err := proxy.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	srv, err := proxy.NewServer(cfg)
	if err != nil {
		log.Fatalf("starting server: %v", err)
	}
	log.Printf("migration-proxy listening on %s (mode=%s, source=%s, target=%s)",
		cfg.Listen, cfg.Mode, cfg.Source.Address, cfg.Target.Address)
	if err := srv.Serve(); err != nil {
		log.Fatalf("serving: %v", err)
	}
}
