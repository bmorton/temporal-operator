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
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/persistence"
)

// runInspect implements: manager inspect --host H --port P --db D --user U
//
//	--plugin postgres12 --tls --password-file /azure/pgpass
//	[--termination-path /dev/termination-log]
func runInspect(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	host := fs.String("host", "", "")
	port := fs.Int("port", 5432, "")
	db := fs.String("db", "", "")
	user := fs.String("user", "", "")
	plugin := fs.String("plugin", "postgres12", "")
	tls := fs.Bool("tls", false, "")
	pwFile := fs.String("password-file", "", "file holding the DB password/token")
	termPath := fs.String("termination-path", "/dev/termination-log", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pw, err := os.ReadFile(*pwFile)
	if err != nil {
		emit(*termPath, persistence.InspectResult{Reachable: false, Error: fmt.Sprintf("reading password file: %v", err)})
		return 1
	}
	spec := &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: *plugin, Host: *host, Port: int32(*port), User: *user, Database: *db,
	}
	if *tls {
		spec.TLS = &temporalv1alpha1.DatastoreTLSSpec{Enabled: true}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res := persistence.InspectSQL(ctx, spec, strings.TrimSpace(string(pw)), *db)
	emit(*termPath, res)
	return 0 // always 0: the result (not the exit code) carries the outcome
}

func emit(termPath string, res persistence.InspectResult) {
	out := res.JSON()
	fmt.Println(out)
	_ = os.WriteFile(termPath, []byte(out), 0o644)
}
