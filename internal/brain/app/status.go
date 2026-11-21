package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/medilies/nerve/internal/brain/domain"
)

// Status says where the Brain is and what it holds that is not pushed, and
// how the target project stands: what waits for a sync, links, the team's
// skills, and drift. Outside a listed checkout, it lists the projects
// instead. It changes nothing.
func (a App) Status(ref ProjectRef, r Reporter) error {
	machine, err := a.Machine.Load()
	if err != nil {
		return err
	}
	m, err := a.Brain.Load()
	if err != nil {
		return err
	}
	if m.Projects == nil {
		m.Projects = map[string]*domain.Project{}
	}
	r.Add(Event{Kind: EvBrain, Path: a.Brain.Root(), Detail: a.Brain.State()})
	l := loaded{machine: machine, m: m}
	tree, name, err := a.here(machine, ref.Dir)
	dirs := []string{tree.Top}
	if ref.Project != "" {
		if _, err := m.Project(ref.Project); err != nil {
			return err
		}
		name, err, dirs = ref.Project, nil, nil
		for _, c := range machine.Checkouts {
			if c.Project == name {
				dirs = append(dirs, c.Path)
			}
		}
	}
	if err != nil {
		a.listProjects(l, r)
		return nil
	}
	p := m.Projects[name]
	if p == nil {
		r.Add(Event{Kind: EvProblem, Path: tree.Top, Detail: fmt.Sprintf("project %s left the Brain: sync cleans this checkout", name)})
		return nil
	}
	r.Add(Event{Kind: EvProject, Project: name, Detail: m.Describe(p)})
	for _, dir := range dirs {
		if err := a.treeStatus(m, name, p, dir, r); err != nil {
			return err
		}
	}
	return nil
}

// treeStatus says how one checkout of project name stands: its sync state
// and what waits for a sync, links, the team's skills and agents, and drift.
func (a App) treeStatus(m domain.Manifest, name string, p *domain.Project, dir string, r Reporter) error {
	st, err := a.Machine.State(dir)
	if errors.Is(err, ErrUnreadable) {
		r.Add(Event{Kind: EvProblem, Path: dir, Detail: err.Error() + ": nerve sync replaces it"})
		st, err = domain.TreeState{Path: dir, Links: map[string]domain.Link{}}, nil
	}
	if err != nil {
		return err
	}
	var events []Event
	pending := 0 // links and settings a sync would change
	wanted := map[string]bool{"skill nerve": true}
	for _, kind := range []string{"skill", "agent"} {
		for _, n := range *p.List(kind) {
			wanted[kind+" "+n] = true
		}
	}
	linked := map[string]bool{}
	for _, rel := range domain.SortedKeys(st.Links) {
		l := st.Links[rel]
		linked[l.Item()] = true
		if !wanted[l.Item()] {
			pending++
		}
		events = append(events, Event{Kind: EvItem, Project: name, Item: l.Item(), Detail: "linked as " + rel})
	}
	// A listed item with nothing in its place waits for a sync, which links
	// it or says why not.
	for _, kind := range []string{"skill", "agent"} {
		for _, n := range *p.List(kind) {
			l := domain.Link{Kind: kind, Name: n}
			if !linked[l.Item()] && !a.Trees.Present(filepath.Join(dir, l.Rel())) {
				pending++
			}
		}
	}
	for _, kind := range []string{"skill", "agent"} {
		for _, e := range a.othersItems(dir, kind, st) {
			e.Project = name
			events = append(events, e)
		}
	}
	cmd, err := a.Self.Command()
	if err != nil {
		return err
	}
	overlay, _, err := a.Machine.Overlay(name)
	if err != nil {
		return err
	}
	path := settingsPath(dir)
	current, err := a.Trees.ReadSettings(path)
	if err != nil {
		return err
	}
	rc := domain.Reconcile(current, st.Settings, domain.Want(m, p, cmd, overlay, a.Brain.Root()), false)
	pending += len(rc.Changes)
	detail := syncState(st, a.Brain.Head())
	switch {
	case pending == 1:
		detail += "; 1 change waits for nerve sync"
	case pending > 1:
		detail += fmt.Sprintf("; %d changes wait for nerve sync", pending)
	}
	r.Add(Event{Kind: EvCheckout, Path: dir, Project: name, Detail: detail})
	for _, e := range events {
		r.Add(e)
	}
	for _, c := range rc.Drift {
		e := driftEvent(path, c)
		if domain.FromOverlay(c, overlay) {
			e.Detail = "local overlay"
		}
		r.Add(e)
	}
	return nil
}
