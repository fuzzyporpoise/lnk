// Package main is the entry point for the lnk CLI.
package main

import (
	_ "embed"
	"strings"

	"go.fuzzyporpoise.dev/lnk/v2/cmd"
)

//go:embed VERSION
var versionFile string

// version and buildTime are set by GoReleaser via ldflags at build time.
var (
	version   string
	buildTime = "local"
)

// use embedded VERSION file for local `go install`d version
func init() {
	if version == "" {
		version = strings.TrimSpace(versionFile)
	}
}

func main() {
	cmd.SetVersion(version, buildTime)
	cmd.Execute()
}
