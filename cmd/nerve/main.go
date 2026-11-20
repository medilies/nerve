// Command nerve carries a Brain to the projects that list what they need
// from it, and checks it.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
	"github.com/medilies/nerve/internal/brain/infra"
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

  setting set <key> <value>    set a setting in the project (--common: in common, for every project); value is JSON or text
  setting add <key> <value>    add one item to a list setting in the project (--common: in common)
  setting rm <key>[=<value>]   remove a setting, or one item of a list, from the project and its local overlay
                               (--common: from common; --all: from common, every project and their overlays)
  setting ls                   the project's settings, and where each comes from

  help                         print this

A command acts on the project around the working directory; --project <name> names another.
A key that holds a dot is written in double quotes: enabledPlugins."x@y.z".
Commands that change the Brain edit brain.toml and never commit.
--json, with any command, prints one JSON document of its events and error instead of text.
`

// selfSkill is the nerve skill, carried by the binary so the two always
// match.
//
//go:embed skill/SKILL.md
var selfSkill []byte

// version is set at release time.
var version string

// release is where nerve upgrades itself from; tests replace it.
var release = infra.GitHubRelease()

// errUsage is a command line nerve does not know; it prints the usage.
var errUsage = errors.New(strings.TrimSpace(usage))

// flags are the options a command may take.
type flags struct {
	project                    string
	all, common, here, discard bool
	hook                       bool     // autosync: print the report as a hook's output
	given                      []string // each flag given, as written
}

func parseFlags(args []string) ([]string, flags, error) {
	var f flags
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, _, _ := strings.Cut(a, "=")
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
			continue
		}
		if f.project == "" && name == "--project" {
			return nil, f, usageError("--project needs a project name")
		}
		f.given = append(f.given, name)
	}
	return pos, f, nil
}

// only refuses a flag the command does not take.
func (f flags) only(allowed ...string) error {
	for _, g := range f.given {
		if !slices.Contains(allowed, g) {
			return usageError(g + " does not go with this command")
		}
	}
	return nil
}

// usageError is a command line nerve cannot run, and why; it prints the
// usage too.
func usageError(why string) error { return fmt.Errorf("%s\n\n%w", why, errUsage) }

func main() { os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr)) }

// cli runs nerve on args, printing to stdout and stderr, and returns its
// exit code.
func cli(args []string, stdout, stderr io.Writer) int {
	plain := slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--json" })
	mode := infra.AsText
	switch {
	case slices.Contains(plain, "--hook"):
		mode = infra.AsHook
	case len(plain) < len(args):
		mode = infra.AsJSON
	}
	r := infra.NewReport(stdout, mode)
	err := run(plain, r)
	switch {
	case r.Finish(err) || err == nil:
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintln(stderr, err)
	default:
		_, _ = fmt.Fprintln(stderr, "error:", err)
	}
	if err != nil && mode != infra.AsHook { // a hook that fails would only hide the report
		return 1
	}
	return 0
}

// wire builds nerve on the Brain at brainDir.
func wire(brainDir string, machine infra.MachineFiles) (app.App, error) {
	zips, err := infra.NewZipCache()
	if err != nil {
		return app.App{}, err
	}
	return app.App{
		Brain:   infra.NewBrainCheckout(brainDir),
		Sources: infra.SourceCache{},
		Machine: machine,
		Trees:   infra.LocalTrees{},
		Self:    infra.Nerve{SkillMD: selfSkill, Ver: infra.Version(version), Release: release},
		Zips:    zips,
	}, nil
}

// onMachine builds nerve on this machine's Brain. A machine without its
// file still gets an App, whose use cases say so.
func onMachine() (app.App, error) {
	machine, err := infra.NewMachineFiles()
	if err != nil {
		return app.App{}, err
	}
	m, _ := machine.Load()
	return wire(m.Brain, machine)
}

// onBrainHere builds nerve on the Brain around the working directory, else
// this machine's.
func onBrainHere(wd string) (app.App, error) {
	machine, err := infra.NewMachineFiles()
	if err != nil {
		return app.App{}, err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, domain.ManifestFile)); err == nil {
			return wire(dir, machine)
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	m, err := machine.Load()
	if err != nil {
		return app.App{}, fmt.Errorf("no %s in this directory or above, and %w", domain.ManifestFile, err)
	}
	return wire(m.Brain, machine)
}

func run(args []string, r *infra.Report) error {
	pos, f, err := parseFlags(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errUsage
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	ref := app.ProjectRef{Dir: wd, Project: f.project}
	cmd, rest := pos[0], pos[1:]
	item := slices.Contains(domain.ItemKinds, cmd)
	// Each command, the flags it takes, and what it runs.
	var allowed []string
	var do func(app.App) error
	brainHere := false // run on the Brain around the working directory
	switch {
	case slices.Contains([]string{"help", "-h"}, cmd) && len(rest) == 0:
		if err := f.only(); err != nil {
			return err
		}
		r.Add(app.Event{Kind: app.EvUsage, Detail: strings.TrimSpace(usage)})
		return nil
	case cmd == "init" && len(rest) == 1:
		if err := f.only(); err != nil {
			return err
		}
		brain, err := filepath.Abs(rest[0])
		if err != nil {
			return err
		}
		machine, err := infra.NewMachineFiles()
		if err != nil {
			return err
		}
		a, err := wire(brain, machine)
		if err != nil {
			return err
		}
		return a.Init(r)
	case cmd == "check" && len(rest) == 0:
		brainHere, do = true, func(a app.App) error { return a.Check(r) }
	case cmd == "skill" && len(rest) > 1 && rest[0] == "update" && len(rest) <= 3:
		sha := ""
		if len(rest) == 3 {
			sha = rest[2]
		}
		brainHere, do = true, func(a app.App) error { return a.UpdatePin(rest[1], sha, r) }
	case cmd == "status" && len(rest) == 0:
		allowed, do = []string{"--project"}, func(a app.App) error { return a.Status(ref, r) }
	case cmd == "sync" && len(rest) == 0:
		allowed = []string{"--here", "--discard", "--hook"}
		do = func(a app.App) error {
			return a.Sync(app.SyncOptions{Dir: wd, Here: f.here, Discard: f.discard, Hook: f.hook, SkipPull: infra.Resumed()}, r)
		}
	case cmd == "project" && len(rest) == 2 && rest[0] == "add":
		do = func(a app.App) error { return a.AddProject(wd, rest[1], r) }
	case cmd == "project" && len(rest) == 1 && rest[0] == "ls":
		do = func(a app.App) error { return a.ListProjects(r) }
	case cmd == "project" && len(rest) == 2 && rest[0] == "rm":
		do = func(a app.App) error { return a.RemoveProject(rest[1], r) }
	case cmd == "project" && len(rest) == 2 && rest[0] == "autosync" && (rest[1] == "on" || rest[1] == "off"):
		allowed = []string{"--project", "--all"}
		do = func(a app.App) error { return a.SetAutosync(ref, rest[1] == "on", f.all, r) }
	case item && len(rest) == 2 && rest[0] == "add":
		allowed, do = []string{"--project"}, func(a app.App) error { return a.AddItem(ref, cmd, rest[1], r) }
	case item && len(rest) == 2 && rest[0] == "rm":
		allowed, do = []string{"--project", "--all"}, func(a app.App) error { return a.RemoveItem(ref, cmd, rest[1], f.all, r) }
	case item && len(rest) == 1 && rest[0] == "ls":
		allowed, do = []string{"--project"}, func(a app.App) error { return a.ListItems(ref, cmd, r) }
	case cmd == "setting" && len(rest) == 3 && rest[0] == "set":
		allowed = []string{"--project", "--common"}
		do = func(a app.App) error { return a.SetSetting(ref, rest[1], domain.ParseValue(rest[2]), f.common, r) }
	case cmd == "setting" && len(rest) == 3 && rest[0] == "add":
		allowed = []string{"--project", "--common"}
		do = func(a app.App) error { return a.AddSetting(ref, rest[1], domain.ParseValue(rest[2]), f.common, r) }
	case cmd == "setting" && len(rest) == 2 && rest[0] == "rm":
		allowed = []string{"--project", "--common", "--all"}
		do = func(a app.App) error { return a.RemoveSetting(ref, rest[1], f.common, f.all, r) }
	case cmd == "setting" && len(rest) == 1 && rest[0] == "ls":
		allowed, do = []string{"--project"}, func(a app.App) error { return a.ListSettings(ref, r) }
	default:
		return errUsage
	}
	if err := f.only(allowed...); err != nil {
		return err
	}
	var a app.App
	if brainHere {
		a, err = onBrainHere(wd)
	} else {
		a, err = onMachine()
	}
	if err != nil {
		return err
	}
	return do(a)
}
