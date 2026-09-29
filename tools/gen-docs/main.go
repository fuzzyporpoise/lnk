// Command gen-docs generates lnk man pages from the Cobra command tree.
package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra/doc"

	"go.fuzzyporpoise.dev/lnk/v2/cmd"
)

func main() {
	outDir := "man"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("create output directory: %v", err)
	}

	// Match the local-build version behavior of the main binary so the
	// generated man pages reflect the current source tree.
	version := "dev"
	if data, err := os.ReadFile("VERSION"); err == nil {
		version = strings.TrimSpace(string(data))
	}
	cmd.SetVersion(version, "")

	root := cmd.NewRootCommand()
	root.DisableAutoGenTag = true
	root.CompletionOptions.DisableDefaultCmd = true

	header := &doc.GenManHeader{
		Title:   "LNK",
		Section: "1",
	}

	if err := doc.GenManTree(root, header, outDir); err != nil {
		log.Fatalf("generate man pages: %v", err)
	}

	fmt.Printf("man pages written to %s/\n", outDir)
}
