package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/medilies/nerve/internal/brain/domain"
)

// AddProject lists the checkout around dir as project name, adding an empty
// entry for a new project. Nothing is written in the project: settings it
// already has are drift at its first sync.
func (a App) AddProject(dir, name string, r Reporter) error {
	if !domain.NamePattern.MatchString(name) {
		return fmt.Errorf("project %q must be lowercase words joined by hyphens", name)
	}
	t, err := a.Trees.Locate(dir)
	if err != nil {
		return err
	}
	if filepath.Clean(t.Top) == filepath.Clean(a.Brain.Root()) {
		return fmt.Errorf("%s is the Brain's checkout, not a project's: run nerve project add in a project", t.Top)
	}
	l, err := a.fresh(r)
	if err != nil {
		return err
	}
	if c, ok := l.machine.CheckoutAt(t.Top); ok {
		if c.Project == name {
			r.Add(Event{Kind: EvCheckout, Path: c.Path, Project: name, Detail: "already listed"})
			return nil
		}
		return fmt.Errorf("%s is listed as project %s", c.Path, c.Project)
	}
	p := l.m.Projects[name]
	switch {
	case p != nil:
		r.Add(Event{Kind: EvProject, Project: name, Detail: "already in the Brain: this checkout joins it"})
	default:
		l.m.Projects[name] = &domain.Project{}
		if err := a.pushManifest(l.m, "add project "+name, r); err != nil {
			return err
		}
		r.Add(Event{Kind: EvProject, Project: name, Detail: "added to " + domain.ManifestFile})
	}
	l.machine.Checkouts = append(l.machine.Checkouts, domain.Checkout{Path: t.Top, Project: name})
	if err := a.Machine.Save(l.machine); err != nil {
		return err
	}
	r.Add(Event{Kind: EvCheckout, Path: t.Top, Project: name, Detail: "listed: nerve sync --here carries the Brain to it"})
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
				detail := ""
				if st, err := a.Machine.State(c.Path); err != nil {
					detail = err.Error()
				} else {
					detail = syncState(st, head)
				}
				r.Add(Event{Kind: EvCheckout, Path: c.Path, Project: name, Detail: detail})
			}
		}
	}
	for _, c := range l.machine.Checkouts {
		if l.m.Projects[c.Project] == nil {
			r.Add(Event{Kind: EvCheckout, Path: c.Path, Project: c.Project, Detail: "its project left the Brain: sync cleans it"})
		}
	}
}

// syncState says where a checkout stands against the Brain's commit.
func syncState(st domain.TreeState, head string) string {
	switch {
	case st.Commit == "" && len(st.Links) == 0:
		return "never synced"
	case st.Commit != head:
		return fmt.Sprintf("synced at %s, behind the Brain at %s", Short(st.Commit), Short(head))
	}
	return "synced at " + Short(head)
}

// RemoveProject cleans the project's working trees on this machine, drops
// it from the Brain, then unlists its checkouts here; other machines clean
// theirs at their next sync. Its local overlay is kept, and said where: it
// may hold what exists nowhere else, such as a secret.
func (a App) RemoveProject(name string, r Reporter) error {
	if !domain.NamePattern.MatchString(name) {
		return fmt.Errorf("no project %q: a project's name is lowercase words joined by hyphens", name)
	}
	l, err := a.fresh(r)
	if err != nil {
		return err
	}
	_, inBrain := l.m.Projects[name]
	states, _, err := a.Machine.States()
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
	overlay, overlayPath, err := a.Machine.Overlay(name)
	if err != nil {
		return err
	}
	if !inBrain && !listed && len(trees) == 0 {
		return fmt.Errorf("no project %q here or in the Brain", name)
	}
	for _, c := range l.machine.Checkouts {
		if c.Project == name && !slices.Contains(trees, c.Path) {
			trees = append(trees, c.Path)
		}
	}
	// A checkout nerve would refuse halfway refuses them all, before any
	// is cleaned.
	for _, dir := range trees {
		if !a.Trees.Exists(dir) {
			continue
		}
		if err := a.throughLink(dir); err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		if _, err := a.Trees.ReadSettings(settingsPath(dir)); err != nil {
			return err
		}
		if t, err := a.Trees.Locate(dir); err == nil { // one git cannot locate is cleaned all the same
			if err := a.Trees.SyncExclude(t.Top, func(current []string) ([]string, error) { return current, nil }); err != nil {
				return fmt.Errorf("%s: %w", dir, err)
			}
		}
	}
	p := l.m.Projects[name]
	if p == nil {
		p = &domain.Project{}
	}
	// Dropped from the remote first: a checkout left half-cleaned is one
	// whose project left the Brain, which the next sync cleans.
	if inBrain {
		delete(l.m.Projects, name)
		if err := a.pushManifest(l.m, "remove project "+name, r); err != nil {
			return err
		}
		r.Add(Event{Kind: EvProject, Project: name, Detail: "dropped from " + domain.ManifestFile})
	}
	for _, dir := range trees {
		if err := a.adopt(l.m, p, overlay, dir, r); err != nil {
			return fmt.Errorf("%s: %w: nerve sync cleans it", dir, err)
		}
		if err := a.clean(dir, "project "+name+" removed", r); err != nil {
			return fmt.Errorf("%s: %w: nerve sync cleans it", dir, err)
		}
	}
	if listed {
		l.machine.Checkouts = slices.DeleteFunc(l.machine.Checkouts, func(c domain.Checkout) bool { return c.Project == name })
		if err := a.Machine.Save(l.machine); err != nil {
			return err
		}
	}
	if overlay != nil {
		r.Add(Event{Kind: EvKept, Path: overlayPath, Detail: "the local overlay of project " + name + " may hold what is nowhere else: delete it once you no longer need it"})
	}
	return nil
}

// adopt takes as nerve's, in a tree it has no state for, what a checkout
// that moved still holds, so that cleaning it takes that out: the links
// that point where nerve points its own, into the Brain's skills/ or
// agents/ or at its own skill, and the settings it would write for project
// p, common and nerve's own included. Only a tree where nerve's own skill
// is linked was synced: in any other, the links and settings are the
// user's. A third-party skill's link is left, since its folder cannot be
// told from another one.
func (a App) adopt(m domain.Manifest, p *domain.Project, overlay map[string]any, dir string, r Reporter) error {
	st, err := a.state(dir, r)
	if err != nil || len(st.Links) > 0 || st.Settings != nil || !a.Trees.Exists(dir) {
		return err
	}
	links, err := a.leftLinks(dir)
	if err != nil || len(links) == 0 {
		return err
	}
	cmd, err := a.Self.Command()
	if err != nil {
		return err
	}
	st.Links = links
	st.Settings = domain.Want(m, p, cmd, overlay, a.Brain.Root())
	return a.Machine.SaveState(st)
}

// leftLinks are the links in dir that point where nerve points its own,
// into the Brain's skills/ or agents/, or at its own skill: what a checkout
// that moved holds. Only a tree where nerve's own skill is linked was
// synced: in any other, the links are the user's, and it finds none.
func (a App) leftLinks(dir string) (map[string]domain.Link, error) {
	self, err := a.Self.Skill()
	if err != nil {
		return nil, err
	}
	links := map[string]domain.Link{}
	brain := filepath.Clean(a.Brain.Root())
	for _, kind := range []string{"skill", "agent"} {
		sub := map[string]string{"skill": "skills", "agent": "agents"}[kind]
		for _, e := range a.Trees.Entries(filepath.Join(dir, ".claude", sub)) {
			l := domain.Link{Kind: kind, Name: strings.TrimSuffix(e, ".md")}
			target, ok := a.Trees.LinkTarget(filepath.Join(dir, l.Rel()))
			if !ok || filepath.Join(dir, l.Rel()) != filepath.Join(dir, ".claude", sub, e) {
				continue
			}
			if target == self.Dir && l == (domain.Link{Kind: "skill", Name: "nerve"}) ||
				filepath.Dir(filepath.Clean(target)) == filepath.Join(brain, sub) {
				l.Target = target
				links[l.Rel()] = l
			}
		}
	}
	if _, synced := links[domain.Link{Kind: "skill", Name: "nerve"}.Rel()]; !synced {
		return nil, nil
	}
	return links, nil
}

// Init writes this machine's file, naming the Brain's checkout, once the
// Brain reads well.
func (a App) Init(r Reporter) error {
	if _, err := a.Brain.Load(); err != nil {
		return err
	}
	if root := a.Brain.Root(); !a.Trees.Exists(filepath.Join(root, ".git")) {
		return fmt.Errorf("%s is not a git repo, so it is not a Brain: run git init there, or nerve init on the folder that holds yours", root)
	}
	path, err := a.Machine.Create(domain.Machine{Brain: a.Brain.Root()})
	if err != nil {
		return err
	}
	r.Add(Event{Kind: EvWrote, Path: path})
	return nil
}
