// SPDX-License-Identifier: AGPL-3.0-only
//
// Command anonseed seeds a local-service-using tool's config into an anonymized
// identity. This is the thin entrypoint; all argv parsing and seed dispatch live
// in internal/cli so the dispatch seam is testable without spawning a binary.
package main

import (
	"os"

	"github.com/wighawag/anonseed/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
