// Command nerve carries a Brain to the projects that list what they need
// from it, and checks it.
package main

import (
	_ "embed"
	"errors"
	"fmt"
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

  setting set <key> <value>    set a setting in the project (--common: in every project); value is JSON or text
  setting rm <key>[=<value>]   remove a setting, or one item of a list (--all: from common and every project)
  setting ls                   the project's settings, and where each comes from

  help                         print this

A command acts on the project around the working directory; --project <name> names another.
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
	mode := infra.AsText
	switch {
	case slices.Contains(args, "--hook"):
		mode = infra.AsHook
	case len(args) < len(os.Args)-1:
		mode = infra.AsJSON
	}
	r := infra.NewReport(os.Stdout, mode)
	err := run(args, r)
	switch {
	case r.Finish(err) || err == nil:
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, err)
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	if err != nil && mode != infra.AsHook { // a hook that fails would only hide the report
		os.Exit(1)
	}
}

// wire builds nerve on the Brain at brainDir.
func wire(brainDir string, machine infra.MachineFiles) (app.App, error) {
	zips, err := infra.NewZipCache()
	if err != nil {
		return app.App{}, err
	}
	return app.App{
		Brain:     infra.NewBrainCheckout(brainDir),
		Sources:   infra.SourceCache{},
		Machine:   machine,
		Workspace: infra.LocalWorkspace{},
		Self:      infra.Nerve{SkillMD: selfSkill, Ver: infra.Version(version), Release: release},
		Zips:      zips,
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
	t := app.Target{Dir: wd, Project: f.project}
	cmd, rest := pos[0], pos[1:]
	switch {
	case slices.Contains([]string{"help", "-h"}, cmd) && len(rest) == 0:
		r.Add(app.Event{Kind: app.EvUsage, Detail: strings.TrimSpace(usage)})
		return nil
	case cmd == "init" && len(rest) == 1:
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
	case (cmd == "check" && len(rest) == 0) || (cmd == "skill" && len(rest) > 1 && rest[0] == "update" && len(rest) <= 3):
		a, err := onBrainHere(wd)
		if err != nil {
			return err
		}
		if cmd == "check" {
			return a.Check(r)
		}
		sha := ""
		if len(rest) == 3 {
			sha = rest[2]
		}
		return a.UpdatePin(rest[1], sha, r)
	}
	a, err := onMachine()
	if err != nil {
		return err
	}
	switch {
	case cmd == "status" && len(rest) == 0:
		return a.Status(t, r)
	case cmd == "sync" && len(rest) == 0:
		return a.Sync(app.SyncOptions{Dir: wd, Here: f.here, Discard: f.discard, Hook: f.hook, SkipPull: infra.Resumed()}, r)
	case cmd == "project" && len(rest) == 2 && rest[0] == "add":
		return a.AddProject(wd, rest[1], r)
	case cmd == "project" && len(rest) == 1 && rest[0] == "ls":
		return a.ListProjects(r)
	case cmd == "project" && len(rest) == 2 && rest[0] == "rm":
		return a.RemoveProject(rest[1], r)
	case cmd == "project" && len(rest) == 2 && rest[0] == "autosync" && (rest[1] == "on" || rest[1] == "off"):
		return a.SetAutosync(t, rest[1] == "on", f.all, r)
	case slices.Contains(domain.ItemKinds, cmd) && len(rest) == 2 && rest[0] == "add":
		return a.AddItem(t, cmd, rest[1], r)
	case slices.Contains(domain.ItemKinds, cmd) && len(rest) == 2 && rest[0] == "rm":
		return a.RemoveItem(t, cmd, rest[1], f.all, r)
	case slices.Contains(domain.ItemKinds, cmd) && len(rest) == 1 && rest[0] == "ls":
		return a.ListItems(t, cmd, r)
	case cmd == "setting" && len(rest) == 3 && rest[0] == "set":
		return a.SetSetting(t, rest[1], domain.ParseValue(rest[2]), f.common, r)
	case cmd == "setting" && len(rest) == 2 && rest[0] == "rm":
		return a.RemoveSetting(t, rest[1], f.common, f.all, r)
	case cmd == "setting" && len(rest) == 1 && rest[0] == "ls":
		return a.ListSettings(t, r)
	}
	return errUsage
}
