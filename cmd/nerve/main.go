// Command nerve carries a Brain to the projects that list what they need
// from it, and checks it.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const usage = `usage: nerve <command>

  init <brain>                 write this machine's file, naming the Brain's checkout
  sync [--here] [--discard]    pull the Brain and carry it to each checkout this machine lists
                               (--here: only the one around the working directory);
                               --discard overwrites settings changed outside the Brain
  status                       the Brain, and the project around the working directory
  check                        validate the Brain (run by its CI)

  project add <name>           list this checkout as project <name>, importing its settings.local.json
  project ls                   the projects, and their checkouts on this machine
  project rm <name>            clean its checkouts here, and drop it from the Brain
  project autosync on|off      sync at each session start in the project (--all: the Brain's default)

  skill add|rm <name>          add a skill to the project, or take it out (rm --all: out of every project)
  skill ls                     the project's skills, and the team's
  skill update <repo> [sha]    show the upstream diff of a source's skills, then move its pin
  agent add|rm <name>, agent ls                      the same for own agents
  hook add|rm <name>, hook ls                        the same for hooks defined in the Brain
  plugin add|rm <plugin>@<marketplace>, plugin ls    the same for Claude Code plugins

  setting set <key> <value>    set a setting in the project (--common: in every project); value is JSON or text
  setting rm <key>[=<value>]   remove a setting, or one item of a list (--all: from common and every project)
  setting ls                   the project's settings, and where each comes from

  help                         print this

A command acts on the project around the working directory; --project <name> names another.
Commands that change the Brain edit brain.toml and never commit.
--json, with any command, prints one JSON document of its events and error instead of text.
`

// errUsage is a command line nerve does not know; it prints the usage.
var errUsage = errors.New(strings.TrimSpace(usage))

// flags are the options any command may take.
type flags struct {
	project                    string
	all, common, here, discard bool
	hook                       bool // autosync: print the report as a hook's output
}

func parseFlags(args []string) ([]string, flags, error) {
	var f flags
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--all":
			f.all = true
		case a == "--common":
			f.common = true
		case a == "--here":
			f.here = true
		case a == "--discard":
			f.discard = true
		case a == "--hook":
			f.hook = true
		case a == "--project" && i+1 < len(args):
			i++
			f.project = args[i]
		case strings.HasPrefix(a, "--project="):
			f.project = strings.TrimPrefix(a, "--project=")
		case strings.HasPrefix(a, "--"):
			return nil, f, errUsage
		default:
			pos = append(pos, a)
		}
	}
	return pos, f, nil
}

func main() {
	args := slices.DeleteFunc(slices.Clone(os.Args[1:]), func(a string) bool { return a == "--json" })
	mode := asText
	switch {
	case slices.Contains(args, "--hook"):
		mode = asHook
	case len(args) < len(os.Args)-1:
		mode = asJSON
	}
	r := newReport(os.Stdout, mode)
	err := run(args, r)
	switch {
	case r.finish(err) || err == nil:
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, err)
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	if err != nil && mode != asHook { // a hook that fails would only hide the report
		os.Exit(1)
	}
}

func run(args []string, r *report) error {
	pos, f, err := parseFlags(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errUsage
	}
	switch cmd, rest := pos[0], pos[1:]; {
	case slices.Contains([]string{"help", "-h"}, cmd) && len(rest) == 0:
		r.add(Event{Kind: evUsage, Detail: strings.TrimSpace(usage)})
		return nil
	case cmd == "init":
		return runInit(rest, r)
	case cmd == "status" && len(rest) == 0:
		return runStatus(f, r)
	case cmd == "sync" && len(rest) == 0:
		m, err := loadMachine()
		if err != nil {
			return err
		}
		return runSync(m, f, r)
	case cmd == "check" && len(rest) == 0:
		root, err := findRoot()
		if err != nil {
			return err
		}
		return runCheck(root, r)
	case cmd == "skill" && len(rest) > 0 && rest[0] == "update" && (len(rest) == 2 || len(rest) == 3):
		root, err := findRoot()
		if err != nil {
			return err
		}
		sha := ""
		if len(rest) == 3 {
			sha = rest[2]
		}
		return runUpdate(root, rest[1], sha, r)
	case cmd == "project":
		return runProject(rest, f, r)
	case slices.Contains(itemKinds, cmd):
		return runItems(cmd, rest, f, r)
	case cmd == "setting":
		return runSetting(rest, f, r)
	}
	return errUsage
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
