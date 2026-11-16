// Command nerve carries a Brain to every Claude Code config on this machine
// and to claude.ai, and checks it.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

const usage = `usage: nerve <command>

  init <brain> <config>[=<profile>]...  write this machine's file: the Brain's checkout, and each config with its profile
  sync [--discard]     carry the machine's Brain to each config, build dist/*.zip, report drift;
                       --discard overwrites settings changed outside the Brain
  update <repo> [sha]  show the upstream diff of a source's skills, then move its pin
  check                validate the Brain (run by its CI)

check and update work on the Brain around the working directory, else on the machine's.
--json, with any command, prints one JSON document of its events and error instead of text.
`

func main() {
	args := slices.DeleteFunc(slices.Clone(os.Args[1:]), func(a string) bool { return a == "--json" })
	r := newReport(os.Stdout, len(args) < len(os.Args)-1)
	err := run(args, r)
	if !r.finish(err) && err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	if err != nil {
		os.Exit(1)
	}
}

func run(args []string, r *report) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch {
	case args[0] == "init":
		return runInit(args[1:], r)
	case args[0] == "sync" && (len(args) == 1 || len(args) == 2 && args[1] == "--discard"):
		m, err := loadMachine()
		if err != nil {
			return err
		}
		return runSync(m, len(args) == 2, r)
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	switch {
	case args[0] == "check" && len(args) == 1:
		return runCheck(root, r)
	case args[0] == "update" && len(args) == 2:
		return runUpdate(root, args[1], "", r)
	case args[0] == "update" && len(args) == 3:
		return runUpdate(root, args[1], args[2], r)
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
