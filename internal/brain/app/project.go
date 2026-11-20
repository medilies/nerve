package app

import (
	"fmt"
	"slices"

	"github.com/medilies/nerve/internal/brain/domain"
)

// AddProject lists the checkout around dir as project name. A new project's
// entry records the checkout's existing settings, so it behaves as before;
// an existing one is only joined. Nothing is written in the project.
func (a App) AddProject(dir, name string, r Reporter) error {
	if !domain.NamePattern.MatchString(name) {
		return fmt.Errorf("project %q must be lowercase words joined by hyphens", name)
	}
	l, err := a.load()
	if err != nil {
		return err
	}
	t, err := a.Trees.Locate(dir)
	if err != nil {
		return err
	}
	for _, d := range []string{t.Top, t.Main} {
		if c, ok := l.machine.CheckoutAt(d); ok {
			if c.Project == name {
				r.Add(Event{Kind: EvCheckout, Path: c.Path, Project: name, Detail: "already listed"})
				return nil
			}
			return fmt.Errorf("%s is listed as project %s", c.Path, c.Project)
		}
	}
	p := l.m.Projects[name]
	switch {
	case p != nil && p.Surface == domain.ClaudeAI:
		return fmt.Errorf("project %s is a claude.ai account, not a checkout", name)
	case p != nil:
		r.Add(Event{Kind: EvProject, Project: name, Detail: "already in the Brain: this checkout joins it"})
	default:
		existing, err := a.Trees.ReadSettings(settingsPath(t.Main))
		if err != nil {
			return err
		}
		entry, overlay, fates := domain.ImportSettings(existing, l.m.Common)
		// The overlay first: a run that fails after it and is run again
		// imports again, and merges the same values.
		if overlay != nil {
			old, _, err := a.Machine.Overlay(name)
			if err != nil {
				return err
			}
			path, err := a.Machine.SaveOverlay(name, domain.Merge(old, overlay))
			if err != nil {
				return err
			}
			r.Add(Event{Kind: EvWrote, Path: path})
		}
		l.m.Projects[name] = &domain.Project{Settings: entry}
		if err := a.Brain.Save(l.m); err != nil {
			return err
		}
		for _, f := range fates {
			r.Add(Event{Kind: EvImported, Project: name, Key: f.Key, Detail: f.What})
		}
		r.Add(Event{Kind: EvProject, Project: name, Detail: "added to " + domain.ManifestFile})
	}
	l.machine.Checkouts = append(l.machine.Checkouts, domain.Checkout{Path: t.Main, Project: name})
	if err := a.Machine.Save(l.machine); err != nil {
		return err
	}
	r.Add(Event{Kind: EvCheckout, Path: t.Main, Project: name, Detail: "listed: review brain.toml, then nerve sync --here"})
	return nil
}

// ListProjects lists the Brain's projects and their checkouts on this
// machine.
func (a App) ListProjects(r Reporter) error {
	l, err := a.load()
	if err != nil {
		return err
	}
	a.listProjects(l, r)
	return nil
}

func (a App) listProjects(l loaded, r Reporter) {
	head := a.Brain.Head()
	for _, name := range domain.SortedKeys(l.m.Projects) {
		r.Add(Event{Kind: EvProject, Project: name, Detail: l.m.Describe(l.m.Projects[name])})
		for _, c := range l.machine.Checkouts {
			if c.Project == name {
				r.Add(Event{Kind: EvCheckout, Path: c.Path, Project: name, Detail: a.syncState(c.Path, head)})
			}
		}
	}
	for _, c := range l.machine.Checkouts {
		if l.m.Projects[c.Project] == nil {
			r.Add(Event{Kind: EvCheckout, Path: c.Path, Project: c.Project, Detail: "its project left the Brain: sync cleans it"})
		}
	}
}

// syncState says where a working tree stands against the Brain's commit.
func (a App) syncState(dir, head string) string {
	st, err := a.Machine.State(dir)
	switch {
	case err != nil:
		return err.Error()
	case st.Commit == "" && len(st.Links) == 0:
		return "never synced"
	case st.Commit != head:
		return fmt.Sprintf("synced at %s, behind the Brain at %s", Short(st.Commit), Short(head))
	}
	return "synced at " + Short(head)
}

// RemoveProject cleans the project's working trees on this machine, drops
// it from the Brain, then unlists its checkouts and deletes its local overlay
// here; other machines clean theirs at their next sync. The overlay goes
// last: it may hold what exists nowhere else, and a run that fails before it
// is run again.
func (a App) RemoveProject(name string, r Reporter) error {
	if !domain.NamePattern.MatchString(name) {
		return fmt.Errorf("no project %q: a project's name is lowercase words joined by hyphens", name)
	}
	l, err := a.load()
	if err != nil {
		return err
	}
	_, inBrain := l.m.Projects[name]
	states, err := a.Machine.States()
	if err != nil {
		return err
	}
	var trees []string
	for _, st := range states {
		if st.Project == name {
			trees = append(trees, st.Path)
		}
	}
	listed := slices.ContainsFunc(l.machine.Checkouts, func(c domain.Checkout) bool { return c.Project == name })
	overlay, _, err := a.Machine.Overlay(name)
	if err != nil {
		return err
	}
	if !inBrain && !listed && len(trees) == 0 && overlay == nil {
		return fmt.Errorf("no project %q here or in the Brain", name)
	}
	for _, c := range l.machine.Checkouts {
		if c.Project == name && !slices.Contains(trees, c.Path) {
			trees = append(trees, c.Path)
		}
	}
	for _, dir := range trees {
		if err := a.clean(dir, "project "+name+" removed", r); err != nil {
			return err
		}
	}
	if inBrain {
		delete(l.m.Projects, name)
		if err := a.Brain.Save(l.m); err != nil {
			return err
		}
		r.Add(Event{Kind: EvProject, Project: name, Detail: "dropped from " + domain.ManifestFile})
	}
	if listed {
		l.machine.Checkouts = slices.DeleteFunc(l.machine.Checkouts, func(c domain.Checkout) bool { return c.Project == name })
		if err := a.Machine.Save(l.machine); err != nil {
			return err
		}
	}
	if path, err := a.Machine.DropOverlay(name); err != nil {
		return err
	} else if path != "" {
		r.Add(Event{Kind: EvRemoved, Path: path})
	}
	return nil
}

// SetAutosync turns autosync on or off for the target project, or with all
// for the Brain's default.
func (a App) SetAutosync(ref ProjectRef, on, all bool, r Reporter) error {
	if err := onePlace(ref.Project, false, all); err != nil {
		return err
	}
	l, err := a.load()
	if err != nil {
		return err
	}
	word := map[bool]string{true: "on", false: "off"}[on]
	if all {
		l.m.Autosync = &on
		if err := a.Brain.Save(l.m); err != nil {
			return err
		}
		r.Add(Event{Kind: EvAutosync, Detail: word})
		return nil
	}
	name, p, err := a.project(l, ref)
	if err != nil {
		return err
	}
	if p.Surface == domain.ClaudeAI {
		return fmt.Errorf("project %s is a claude.ai account, which has no sessions to sync", name)
	}
	p.Autosync = &on
	if err := a.Brain.Save(l.m); err != nil {
		return err
	}
	r.Add(Event{Kind: EvAutosync, Project: name, Detail: word})
	return nil
}

// Init writes this machine's file, naming the Brain's checkout, once the
// Brain reads well.
func (a App) Init(r Reporter) error {
	if _, err := a.Brain.Load(); err != nil {
		return err
	}
	path, err := a.Machine.Create(domain.Machine{Brain: a.Brain.Root()})
	if err != nil {
		return err
	}
	r.Add(Event{Kind: EvWrote, Path: path})
	return nil
}
