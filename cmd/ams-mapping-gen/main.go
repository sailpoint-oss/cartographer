// Command ams-mapping-gen builds a right→scope mapping JSON from authorization-model-service.
//
// Output is intended for consumer pipelines (e.g. Meridian), not for committing
// the full production table into the public cartographer repo. See docs/ANONYMIZATION.md.
//
// Usage:
//
//	AMS_REPO=../authorization-model-service ams-mapping-gen -out ../meridian/reports/ams-mapping.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sailpoint-oss/cartographer/extract/authscope"
)

func main() {
	out := flag.String("out", "", "output JSON path (default: stdout)")
	repo := flag.String("repo", os.Getenv("AMS_REPO"), "authorization-model-service repo root")
	flag.Parse()

	if *repo == "" {
		*repo = "../authorization-model-service"
	}
	abs, err := filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "repo path: %v\n", err)
		os.Exit(1)
	}

	m, err := authscope.BuildFromAMSRepo(abs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build mapping: %v\n", err)
		os.Exit(1)
	}

	if *out == "" {
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "encode: %v\n", err)
			os.Exit(1)
		}
		os.Stdout.Write(append(data, '\n'))
		return
	}
	if err := authscope.WriteJSON(*out, m); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d rights, %d scopes)\n", *out, len(m.RightToScopes), len(m.ScopeToRights))
}
