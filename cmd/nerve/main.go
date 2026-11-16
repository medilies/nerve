// Command nerve carries a Brain to every Claude Code config on this machine
// and to claude.ai, and checks it.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const usage = `usage: nerve <command>

  init <brain> <config>[=<profile>]...  write this machine's file: the Brain's checkout, and each config with its profile
  sync                 carry the machine's Brain to each config, build dist/*.zip, report drift
  update <repo> [sha]  show the upstream diff of a source's skills, then move its pin
  check                validate the Brain (run by its CI)

check and update work on the Brain around the working directory, else on the machine's.
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
	switch {
	case args[0] == "init":
		return runInit(args[1:], out)
	case args[0] == "sync" && len(args) == 1:
		m, err := loadMachine()
		if err != nil {
			return err
		}
		return runSync(m, out)
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	switch {
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
// that holds the manifest, else the machine's Brain.
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
			break
		}
		dir = parent
	}
	m, err := loadMachine()
	if err != nil {
		return "", fmt.Errorf("no %s in this directory or above, and %w", manifestFile, err)
	}
	return m.Brain, nil
}
