// Command nerve carries a Brain to the projects that list what they need
// from it, and checks it.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
	"github.com/medilies/nerve/internal/brain/infra"
)

const usage = `usage: nerve <command>

  init <brain>                 write this machine's file, naming the Brain's checkout
  sync [--here] [-f]           make the Brain its remote's, then carry it to each checkout this
                               machine lists (--here: only the one around the working directory);
                               -f, --force drops the Brain's commits and changes not pushed, and
                               overwrites drift: settings the Brain sets, changed here
  push [<message>] [-f]        commit everything in the Brain, then push it to its remote;
                               -f, --force overwrites the remote's commits this Brain lacks
  update [<repo>]              show how each source's skills changed, pin and push its latest
                               commit, then sync the checkout around the working directory
  status                       the Brain, and how the project around the working directory
                               stands; outside a listed checkout, every project
  check                        validate the Brain (run by its CI)

  project add <name>           list this checkout as project <name>
  project ls                   the projects, and their checkouts on this machine
  project rm <name>            clean its checkouts here, and drop it from the Brain

  skill add|rm <name>          add a skill to the project, or take it out (rm --all: out of every project)
  agent add|rm <name>          the same for own agents
  hook add|rm <name>           the same for hooks defined in the Brain
  plugin add|rm <plugin>@<marketplace>   the same for Claude Code plugins

  setting set <key> <value>    set a setting in the project (--common: in common, for every project); value is JSON or text
  setting add <key> <value>    add one item to a list setting in the project (--common: in common)
  setting rm <key>[=<value>]   remove a setting, or one item of a list, from the project and its local overlay
                               (--common: from common; --all: from common, every project and their overlays)

  version, --version, -v       print nerve's version
  help, --help, -h             print this

status, and the skill, agent, hook, plugin and setting commands act on the project around the
working directory; --project <name> names another.
A key that holds a dot is written in double quotes: enabledPlugins."x@y.z".
-- ends the flags, so a value after it may start with --.
Commands that change brain.json first bring the remote's, then commit and push the change.
--json, with any command, prints one JSON document of its events and error instead of text.
`

// selfSkill is the nerve skill, carried by the binary so the two always
// match.
//
//go:embed skill/SKILL.md
var selfSkill []byte

// errUsage is a command line nerve does not know; it prints the usage.
var errUsage = errors.New("nerve does not know this command line")

// usageErr is a command line nerve cannot run, and why; it prints the usage
// too.
type usageErr string

func (e usageErr) Error() string        { return string(e) }
func (e usageErr) Is(target error) bool { return target == errUsage }

// usageError is a command line nerve cannot run, and why.
func usageError(why string) error { return usageErr(why) }

// helpFor is the usage for a command line: the lines of the command its
// first word names, or the whole usage. The item kinds share their lines.
func helpFor(words []string) string {
	if len(words) == 0 || words[0] == "help" {
		return strings.TrimSpace(usage)
	}
	names := []string{words[0]}
	if slices.Contains(domain.ItemKinds, words[0]) {
		names = domain.ItemKinds
	}
	var lines []string
	in := false
	for _, l := range strings.Split(usage, "\n") {
		switch {
		case strings.HasPrefix(l, "   "): // a command's line goes on
		case strings.HasPrefix(l, "  "):
			in = slices.Contains(names, strings.FieldsFunc(l, func(r rune) bool { return r == ' ' || r == ',' })[0])
		default:
			in = false
		}
		if in {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return strings.TrimSpace(usage)
	}
	return "usage:\n" + strings.Join(lines, "\n") + "\n\nnerve help lists every command, and the flags they share."
}

// flags are the options a command may take.
type flags struct {
	project                  string
	all, common, here, force bool
	hook                     bool     // the session report: print status as a hook's output
	json                     bool     // print one JSON document
	help                     bool     // print the usage
	version                  bool     // print nerve's version
	given                    []string // each flag given, as written
}

// parseFlags splits args into words and flags. -- ends the flags.
func parseFlags(args []string) ([]string, flags, error) {
	var f flags
	var pos []string
	noProject := false // --project with no name, refused unless help is asked
words:
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, _, _ := strings.Cut(a, "=")
		switch {
		case a == "--":
			pos = append(pos, args[i+1:]...)
			break words
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--version" || a == "-v":
			f.version = true
		case a == "--json":
			f.json = true
		case a == "--all":
			f.all = true
		case a == "--common":
			f.common = true
		case a == "--here":
			f.here = true
		case a == "-f" || a == "--force":
			f.force = true
		case a == "--hook":
			f.hook = true
		case a == "--project" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
			i++
			f.project = args[i]
		case name == "--project": // with no name, or --project=<name>
			f.project = strings.TrimPrefix(strings.TrimPrefix(a, "--project"), "=")
		case strings.HasPrefix(a, "--"):
			return nil, f, usageError(name + " is not a flag nerve knows: put -- before a word that starts with -")
		case len(a) > 1 && a[0] == '-' && !isNumber(a): // a typo such as -all
			return nil, f, usageError(a + " is not a flag nerve knows: put -- before a word that starts with -")
		default:
			pos = append(pos, a)
			continue
		}
		if name == "--project" {
			noProject = f.project == ""
		}
		f.given = append(f.given, name)
	}
	if noProject && !f.help {
		return nil, f, usageError("--project needs a project name")
	}
	if f.hook && f.json {
		return nil, f, usageError("--hook and --json each say how to print: pass one")
	}
	return pos, f, nil
}

// isNumber says whether a word is a number, such as -1, which is a value:
// not -inf or -nan, which no setting holds.
func isNumber(s string) bool {
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}

// only refuses a flag the command does not take; any command takes --json.
func (f flags) only(allowed ...string) error {
	for _, g := range f.given {
		long := g
		switch g {
		case "-f":
			long = "--force"
		case "-v":
			long = "--version"
		}
		if g != "--json" && !slices.Contains(allowed, long) {
			return usageError(g + " does not go with this command")
		}
	}
	return nil
}

// printVersion reports nerve's version: the module version Go stamps in
// the binary, "(devel)" when it has none.
func printVersion(f flags, r *infra.Report) error {
	if err := f.only("--version"); err != nil {
		return err
	}
	v := "(devel)"
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		v = bi.Main.Version
	}
	r.Add(app.Event{Kind: app.EvVersion, Detail: "nerve version " + v})
	return nil
}

func main() { os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr)) }

// cli runs nerve on args, printing to stdout and stderr, and returns its
// exit code.
func cli(args []string, stdout, stderr io.Writer) int {
	mode := infra.AsText
	flagArgs := args
	if i := slices.Index(args, "--"); i >= 0 {
		flagArgs = args[:i]
	}
	words, _, err := parseFlags(flagArgs) // the command, wherever its flags are
	if err != nil {                       // a command line nerve refuses: its first word
		words = nil
		if i := slices.IndexFunc(flagArgs, func(a string) bool { return !strings.HasPrefix(a, "-") }); i >= 0 {
			words = flagArgs[i:]
		}
	}
	switch {
	case len(words) > 0 && words[0] == "status" && slices.Contains(flagArgs, "--hook"): // the session report
		mode = infra.AsHook
	case slices.Contains(flagArgs, "--json"): // even on a command line nerve refuses
		mode = infra.AsJSON
	}
	r := infra.NewReport(stdout, mode)
	err = run(args, r)
	if err == errUsage { // the command line is the reason
		shown := args
		if mode == infra.AsJSON {
			shown = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--json" })
		}
		err = usageError(errUsage.Error() + ": " + strings.Join(shown, " "))
	}
	switch {
	case mode == infra.AsJSON && errors.Is(err, errUsage): // the reason, not the usage
		err = errors.New(err.Error() + ": nerve help lists the commands")
	case mode == infra.AsHook && errors.Is(err, errUsage): // one line, not the usage
		err = errors.New(err.Error() + "; run nerve help")
	}
	finished := r.Finish(err)
	if werr := r.Err(); werr != nil { // nothing reached the user: say so where it can
		_, _ = fmt.Fprintln(stderr, "error:", infra.Visible(werr.Error()))
		return 1
	}
	switch {
	case finished || err == nil:
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "%s\n\n%s\n", infra.Visible(err.Error()), helpFor(words))
	default:
		_, _ = fmt.Fprintln(stderr, "error:", infra.Visible(err.Error()))
	}
	if err != nil && mode != infra.AsHook { // a hook that fails would only hide the report
		return 1
	}
	return 0
}

// wire builds nerve on the Brain at brainDir.
func wire(brainDir string, machine infra.MachineFiles) app.App {
	return app.App{
		Brain:   infra.NewBrainCheckout(brainDir),
		Sources: infra.SourceCache{},
		Machine: machine,
		Trees:   infra.LocalTrees{},
		Self:    infra.Nerve{SkillMD: selfSkill, Path: infra.RanAs},
	}
}

// onMachine builds nerve on this machine's Brain. A machine without its
// file still gets an App, whose use cases say so.
func onMachine() (app.App, error) {
	machine, err := infra.NewMachineFiles()
	if err != nil {
		return app.App{}, err
	}
	m, _ := machine.Load() // a missing or broken file is the use cases' to report
	return wire(m.Brain, machine), nil
}

// onBrainHere builds nerve on the Brain around the working directory, else
// this machine's. Inside a checkout this machine lists, a brain.json is the
// project's own, so it is this machine's Brain.
func onBrainHere(wd string) (app.App, error) {
	machine, err := infra.NewMachineFiles()
	if err != nil {
		return app.App{}, err
	}
	m, merr := machine.Load()
	dir, stray := brainAround(wd)
	if dir != "" && (merr != nil || !inCheckout(m, wd)) {
		return wire(dir, machine), nil
	}
	switch {
	case merr != nil && stray != "":
		return app.App{}, fmt.Errorf("%s in %s is not at the top of a git repo, so it is not a Brain, and %w", domain.ManifestFile, stray, merr)
	case merr != nil:
		return app.App{}, fmt.Errorf("no %s in this directory or above, and %w", domain.ManifestFile, merr)
	}
	return wire(m.Brain, machine), nil
}

// brainAround finds the Brain around dir: from dir up, the top of a git
// repo holding a brain.json. A brain.json elsewhere is not a Brain: stray
// names the first such folder, when no Brain is found.
func brainAround(dir string) (brain, stray string) {
	for ; ; dir = filepath.Dir(dir) {
		_, manifest := os.Stat(filepath.Join(dir, domain.ManifestFile))
		_, repo := os.Lstat(filepath.Join(dir, ".git"))
		switch {
		case manifest == nil && repo == nil:
			return dir, ""
		case manifest == nil && stray == "":
			stray = dir
		}
		if filepath.Dir(dir) == dir {
			return "", stray
		}
	}
}

// inCheckout says whether dir is in a checkout machine lists.
func inCheckout(m domain.Machine, dir string) bool {
	t, err := infra.LocalTrees{}.Locate(dir)
	if err != nil {
		return false
	}
	_, ok := m.CheckoutAt(t.Top)
	return ok
}

func run(args []string, r *infra.Report) error {
	pos, f, err := parseFlags(args)
	switch {
	case err != nil:
		return err
	case f.help:
		r.Add(app.Event{Kind: app.EvUsage, Detail: helpFor(pos)})
		return nil
	case f.version && len(pos) == 0:
		return printVersion(f, r)
	case len(pos) == 0:
		if err := f.only(); err != nil {
			return err
		}
		r.Add(app.Event{Kind: app.EvUsage, Detail: strings.TrimSpace(usage)})
		return nil
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
	case cmd == "help" && len(rest) == 0:
		if err := f.only(); err != nil {
			return err
		}
		r.Add(app.Event{Kind: app.EvUsage, Detail: strings.TrimSpace(usage)})
		return nil
	case cmd == "version" && len(rest) == 0:
		return printVersion(f, r)
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
		return wire(brain, machine).Init(r)
	case cmd == "check" && len(rest) == 0:
		brainHere, do = true, func(a app.App) error { return a.Check(r) }
	case cmd == "update" && len(rest) <= 1:
		brainHere, do = true, func(a app.App) error { return a.Update(wd, strings.Join(rest, ""), r) }
	case cmd == "push" && len(rest) <= 1:
		allowed = []string{"--force"}
		brainHere, do = true, func(a app.App) error { return a.Push(strings.Join(rest, ""), f.force, r) }
	case cmd == "push":
		return usageError("push takes one message: quote it, as in nerve push \"add go-stack\"")
	case cmd == "status" && len(rest) == 0:
		allowed, do = []string{"--project", "--hook"}, func(a app.App) error { return a.Status(ref, r) }
	case cmd == "sync" && len(rest) == 0:
		allowed = []string{"--here", "--force"}
		do = func(a app.App) error {
			return a.Sync(app.SyncOptions{Dir: wd, Here: f.here, Force: f.force}, r)
		}
	case cmd == "project" && len(rest) == 2 && rest[0] == "add":
		do = func(a app.App) error { return a.AddProject(wd, rest[1], r) }
	case cmd == "project" && len(rest) == 1 && rest[0] == "ls":
		do = func(a app.App) error { return a.ListProjects(r) }
	case cmd == "project" && len(rest) == 2 && rest[0] == "rm":
		do = func(a app.App) error { return a.RemoveProject(rest[1], r) }
	case item && len(rest) == 2 && rest[0] == "add":
		allowed, do = []string{"--project"}, func(a app.App) error { return a.AddItem(ref, cmd, rest[1], r) }
	case item && len(rest) == 2 && rest[0] == "rm":
		allowed, do = []string{"--project", "--all"}, func(a app.App) error { return a.RemoveItem(ref, cmd, rest[1], f.all, r) }
	case cmd == "setting" && len(rest) == 3 && rest[0] == "set":
		allowed = []string{"--project", "--common"}
		do = func(a app.App) error { return a.SetSetting(ref, rest[1], domain.ParseValue(rest[2]), f.common, r) }
	case cmd == "setting" && len(rest) == 3 && rest[0] == "add":
		allowed = []string{"--project", "--common"}
		do = func(a app.App) error { return a.AddSetting(ref, rest[1], domain.ParseValue(rest[2]), f.common, r) }
	case cmd == "setting" && len(rest) == 2 && rest[0] == "rm":
		allowed = []string{"--project", "--common", "--all"}
		do = func(a app.App) error { return a.RemoveSetting(ref, rest[1], f.common, f.all, r) }
	case len(rest) == 0 && (item || cmd == "project" || cmd == "setting"): // a group alone
		if err := f.only(); err != nil {
			return err
		}
		r.Add(app.Event{Kind: app.EvUsage, Detail: helpFor(pos)})
		return nil
	default:
		if slices.Contains(f.given, "-f") { // a value -f, read as a flag
			return usageError("nerve does not know this command line: a value that starts with - goes after --, as in nerve setting set key -- -f")
		}
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
