package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// workspace is what most commands need: this machine, its Brain, and the
// Brain's manifest.
type workspace struct {
	machine Machine
	root    string
	m       Manifest
}

func openWorkspace() (workspace, error) {
	machine, err := loadMachine()
	if err != nil {
		return workspace{}, err
	}
	m, err := loadManifest(machine.Brain)
	if err != nil {
		return workspace{}, err
	}
	if m.Projects == nil {
		m.Projects = map[string]*Project{}
	}
	return workspace{machine: machine, root: machine.Brain, m: m}, nil
}

func (w workspace) save() error { return saveManifest(w.root, w.m) }

// target is the project a command acts on.
func (w workspace) target(f flags) (string, *Project, error) {
	name, err := w.machine.projectFor(f)
	if err != nil {
		return "", nil, err
	}
	p, err := w.m.project(name)
	return name, p, err
}

func runProject(args []string, f flags, r *report) error {
	if len(args) == 0 {
		return errUsage
	}
	w, err := openWorkspace()
	if err != nil {
		return err
	}
	switch verb := args[0]; {
	case verb == "add" && len(args) == 2:
		return w.projectAdd(args[1], r)
	case verb == "ls" && len(args) == 1:
		return w.projectLs(r)
	case verb == "rm" && len(args) == 2:
		return w.projectRm(args[1], r)
	case verb == "autosync" && len(args) == 2 && (args[1] == "on" || args[1] == "off"):
		return w.projectAutosync(args[1] == "on", f, r)
	}
	return errUsage
}

// projectAdd lists the checkout around the working directory as project
// name. A new project's entry records the checkout's existing settings, so
// it behaves as before; an existing one is only joined.
func (w workspace) projectAdd(name string, r *report) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("project %q must be lowercase words joined by hyphens", name)
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	t, err := locateTree(wd)
	if err != nil {
		return err
	}
	for _, dir := range []string{t.Top, t.Main} {
		if c, ok := w.machine.checkout(dir); ok {
			if c.Project == name {
				r.add(Event{Kind: evCheckout, Path: c.Path, Project: name, Detail: "already listed"})
				return nil
			}
			return fmt.Errorf("%s is listed as project %s", c.Path, c.Project)
		}
	}
	p := w.m.Projects[name]
	switch {
	case p != nil && p.Surface == claudeAI:
		return fmt.Errorf("project %s is a claude.ai account, not a checkout", name)
	case p != nil:
		r.add(Event{Kind: evProject, Project: name, Detail: "already in the Brain: this checkout joins it"})
	default:
		existing, err := readObject(filepath.Join(t.Top, localSettings))
		if err != nil {
			return err
		}
		entry, overlay := importSettings(name, existing, w.m.Common.Settings, r)
		w.m.Projects[name] = &Project{Settings: entry}
		if err := w.save(); err != nil {
			return err
		}
		r.add(Event{Kind: evProject, Project: name, Detail: "added to " + manifestFile})
		if overlay != nil {
			path, err := overlayPath(name)
			if err != nil {
				return err
			}
			old, err := readObject(path)
			if err != nil {
				return err
			}
			if err := writeObject(path, merge(old, overlay)); err != nil {
				return err
			}
			r.add(Event{Kind: evWrote, Path: path})
		}
	}
	w.machine.Checkouts = append(w.machine.Checkouts, Checkout{Path: t.Main, Project: name})
	if err := saveMachine(w.machine); err != nil {
		return err
	}
	r.add(Event{Kind: evCheckout, Path: t.Main, Project: name, Detail: "listed: review brain.toml, then nerve sync --here"})
	return nil
}

func (w workspace) projectLs(r *report) error {
	head := brainHead(w.root)
	for _, name := range sortedKeys(w.m.Projects) {
		p := w.m.Projects[name]
		r.add(Event{Kind: evProject, Project: name, Detail: describe(w.m, p)})
		for _, c := range w.machine.Checkouts {
			if c.Project == name {
				r.add(Event{Kind: evCheckout, Path: c.Path, Project: name, Detail: syncState(c.Path, head)})
			}
		}
	}
	for _, c := range w.machine.Checkouts {
		if w.m.Projects[c.Project] == nil {
			r.add(Event{Kind: evCheckout, Path: c.Path, Project: c.Project, Detail: "its project left the Brain: sync cleans it"})
		}
	}
	return nil
}

// describe sums up a project in a line.
func describe(m Manifest, p *Project) string {
	var parts []string
	if p.Surface == claudeAI {
		parts = append(parts, "claude.ai account")
	} else if m.autosync(p) {
		parts = append(parts, "autosync on")
	} else {
		parts = append(parts, "autosync off")
	}
	for _, x := range []struct {
		n    int
		what string
	}{{len(p.Skills), "skills"}, {len(p.Agents), "agents"}, {len(p.Hooks), "hooks"}, {len(p.Plugins), "plugins"}, {len(leaves(p.Settings)), "settings"}} {
		switch {
		case x.n == 1:
			parts = append(parts, "1 "+strings.TrimSuffix(x.what, "s"))
		case x.n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", x.n, x.what))
		}
	}
	return strings.Join(parts, ", ")
}

// syncState says where a working tree stands against the Brain's commit.
func syncState(dir, head string) string {
	st, err := loadState(dir)
	switch {
	case err != nil:
		return err.Error()
	case st.Commit == "" && len(st.Links) == 0:
		return "never synced"
	case st.Commit != head:
		return fmt.Sprintf("synced at %s, behind the Brain at %s", short(st.Commit), short(head))
	}
	return "synced at " + short(head)
}

// projectRm cleans the project's working trees on this machine and drops it
// from the Brain; other machines clean theirs at their next sync.
func (w workspace) projectRm(name string, r *report) error {
	_, inBrain := w.m.Projects[name]
	trees, err := treesOf(name)
	if err != nil {
		return err
	}
	listed := slices.ContainsFunc(w.machine.Checkouts, func(c Checkout) bool { return c.Project == name })
	if !inBrain && !listed && len(trees) == 0 {
		return fmt.Errorf("no project %q here or in the Brain", name)
	}
	for _, c := range w.machine.Checkouts {
		if c.Project == name && !slices.Contains(trees, c.Path) {
			trees = append(trees, c.Path)
		}
	}
	for _, dir := range trees {
		if err := cleanTree(dir, "project "+name+" removed", r); err != nil {
			return err
		}
	}
	if listed {
		w.machine.Checkouts = slices.DeleteFunc(w.machine.Checkouts, func(c Checkout) bool { return c.Project == name })
		if err := saveMachine(w.machine); err != nil {
			return err
		}
	}
	if inBrain {
		delete(w.m.Projects, name)
		if err := w.save(); err != nil {
			return err
		}
		r.add(Event{Kind: evProject, Project: name, Detail: "dropped from " + manifestFile})
	}
	return nil
}

// treesOf lists the working trees nerve synced for a project on this
// machine, worktrees included.
func treesOf(project string) ([]string, error) {
	dir, err := machineDir()
	if err != nil {
		return nil, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "state", "*.json"))
	var trees []string
	for _, f := range files {
		var st state
		if err := readJSON(f, &st); err == nil && st.Project == project && st.Path != "" {
			trees = append(trees, st.Path)
		}
	}
	return trees, nil
}

func (w workspace) projectAutosync(on bool, f flags, r *report) error {
	word := map[bool]string{true: "on", false: "off"}[on]
	if f.all {
		w.m.Autosync = &on
		if err := w.save(); err != nil {
			return err
		}
		r.add(Event{Kind: evAutosync, Detail: word})
		return nil
	}
	name, p, err := w.target(f)
	if err != nil {
		return err
	}
	p.Autosync = &on
	if err := w.save(); err != nil {
		return err
	}
	r.add(Event{Kind: evAutosync, Project: name, Detail: word})
	return nil
}
