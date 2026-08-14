// Command gendoc generates deterministic English CLI reference pages.
// It writes to docs/cli by default; pass -out <directory> to override it.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/est7/skills-switch-tui/internal/cli"
	"github.com/spf13/cobra/doc"
)

func main() {
	outputDir := flag.String("out", "docs/cli", "directory for generated CLI reference pages")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "gendoc does not accept positional arguments")
		os.Exit(2)
	}
	if err := generate(*outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create CLI reference directory %s: %w", outputDir, err)
	}
	if err := os.Setenv("SKILLS_SWITCH_LANG", "en"); err != nil {
		return fmt.Errorf("select English CLI reference language: %w", err)
	}
	command := cli.NewRootCommand("dev")
	command.DisableAutoGenTag = true
	if err := doc.GenMarkdownTree(command, outputDir); err != nil {
		return fmt.Errorf("generate CLI reference in %s: %w", outputDir, err)
	}
	return nil
}
