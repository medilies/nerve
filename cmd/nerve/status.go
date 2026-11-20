package main

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// version is set at release time; builds without it fall back to what the
// Go toolchain recorded, such as a go install tag or "(devel)".
var version string

func currentVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(devel)"
}

// runStatus says which nerve runs, where the Brain is and what it holds that
// is not pushed, and how the project around the working directory stands:
// autosync, links, the team's skills, and drift. Outside a listed checkout,
// it lists the projects instead.
func runStatus(f flags, r *report) error {
	machine, err := loadMachine()
	if err != nil {
		r.add(Event{Kind: evVersion, Detail: currentVersion()})
		return err
	}
	m, err := loadManifest(machine.Brain)
	r.add(Event{Kind: evVersion, Detail: currentVersion(), Target: m.Nerve})
	if err != nil {
		return err
	}
	r.add(Event{Kind: evBrain, Path: machine.Brain, Detail: brainState(machine.Brain)})
	w := workspace{machine: machine, root: machine.Brain, m: m}
	if w.m.Projects == nil {
		w.m.Projects = map[string]*Project{}
	}
	t, name, err := machine.here()
	if f.project != "" {
		name, err = f.project, nil
		t = tree{}
		for _, c := range machine.Checkouts {
			if c.Project == name {
				t = tree{Top: c.Path, Main: c.Path}
			}
		}
	}
	if err != nil {
		return w.projectLs(r)
	}
	p := m.Projects[name]
	if p == nil {
		r.add(Event{Kind: evProblem, Path: t.Top, Detail: fmt.Sprintf("project %s left the Brain: sync cleans this checkout", name)})
		return nil
	}
	r.add(Event{Kind: evProject, Project: name, Detail: describe(m, p)})
	if t.Top == "" {
		return nil
	}
	r.add(Event{Kind: evCheckout, Path: t.Top, Project: name, Detail: syncState(t.Top, brainHead(machine.Brain))})
	st, err := loadState(t.Top)
	if err != nil {
		return err
	}
	for _, rel := range sortedKeys(st.Links) {
		kind, item, _ := strings.Cut(st.Links[rel], ":")
		r.add(Event{Kind: evItem, Project: name, Item: kind + " " + item, Detail: "linked as " + rel})
	}
	for _, kind := range []string{"skill", "agent"} {
		for _, item := range teamItems(t.Top, kind, st) {
			r.add(Event{Kind: evItem, Project: name, Item: kind + " " + item, Detail: "the team's"})
		}
	}
	cmd, err := autosyncCommand()
	if err != nil {
		return err
	}
	want, err := wantSettings(machine.Brain, m, name, p, cmd)
	if err != nil {
		return err
	}
	_, err = syncFile(filepath.Join(t.Top, localSettings), want, &st.Settings, false, true, r)
	return err
}

// brainState says what the Brain holds that others cannot see yet.
func brainState(root string) string {
	var parts []string
	if out, err := git(root, "status", "--porcelain", "--untracked-files=no"); err == nil && trim(out) != "" {
		parts = append(parts, "uncommitted changes")
	}
	if out, err := git(root, "rev-list", "--count", "@{upstream}..HEAD"); err == nil && trim(out) != "0" {
		parts = append(parts, trim(out)+" commits not pushed")
	}
	return strings.Join(parts, ", ")
}
