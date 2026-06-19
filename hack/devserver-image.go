//go:build ignore

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

// devserver-image prints the temporalio/temporal CLI image that the operator
// runs for a given Temporal server version, using the same version-matrix
// mapping as the controller. It exists so CI (the e2e image pre-pull) can
// resolve the dev server image without duplicating the mapping in bash:
//
//	go run hack/devserver-image.go 1.31.1   # -> temporalio/temporal:1.7.2
package main

import (
	"fmt"
	"os"

	"github.com/bmorton/temporal-operator/internal/temporal"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: devserver-image <server-version>")
		os.Exit(2)
	}
	cli := temporal.DevServerCLIVersion(os.Args[1])
	if cli == "" {
		fmt.Fprintf(os.Stderr, "devserver-image: no CLI image mapping for server version %q\n", os.Args[1])
		os.Exit(1)
	}
	fmt.Printf("temporalio/temporal:%s\n", cli)
}
