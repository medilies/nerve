// Command skills installs this repo's skills and pinned third-party skills
// into every Claude Code config, builds claude.ai zips, and checks them.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const usage = `usage: go run ./cmd/skills <command>

  sync              link every skill into each config, build dist/*.zip, report claude.ai drift
  update <repo> [sha]  show the upstream diff of a source's skills, then move its pin
  check             validate every skill and pin (run by CI)
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	switch {
	case args[0] == "sync" && len(args) == 1:
		return runSync(root, out)
	case args[0] == "check" && len(args) == 1:
		return runCheck(root, out)
	case args[0] == "update" && len(args) == 2:
		return runUpdate(root, args[1], "", out)
	case args[0] == "update" && len(args) == 3:
		return runUpdate(root, args[1], args[2], out)
	}
	return errors.New(usage)
}

// findRoot returns the nearest directory, from the working directory up,
// that holds the manifest.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, manifestFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s in this directory or above", manifestFile)
		}
		dir = parent
	}
}
