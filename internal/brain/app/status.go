package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"

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
	brain := Event{Kind: EvBrain, Path: a.Brain.Root(), Detail: a.Brain.State()}
	if unpushed.MatchString(brain.Detail) {
		brain.Fix = "nerve push"
	}
	r.Add(brain)
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
		if err := a.treeStatus(m, name, p, dir, dir == tree.Top, r); err != nil {
			return err
		}
	}
	return nil
}

// unpushed matches a Brain state that holds work push sends.
var unpushed = regexp.MustCompile(`(^|, )(uncommitted changes|\d+ commits? not pushed)`)

// treeStatus says how one checkout of project name stands: its sync state
// and what waits for sync, the command that syncs it, links that are not
// as they should be, the team's skills and agents, and drift. here says
// whether it is the checkout around the working directory.
func (a App) treeStatus(m domain.Manifest, name string, p *domain.Project, dir string, here bool, r Reporter) error {
	st, err := a.Machine.State(dir)
	if errors.Is(err, ErrUnreadable) {
		// Whose links and settings are there is unknown until a sync
		// starts the state again.
		r.Add(Event{Kind: EvCheckout, Path: dir, Project: name, Detail: "state unknown until nerve sync"})
		r.Add(Event{Kind: EvProblem, Path: dir, Detail: err.Error() + ": nerve sync replaces it"})
		return nil
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
		path := filepath.Join(dir, rel)
		switch {
		case !a.Trees.Present(path) && !wanted[l.Item()]: // gone, and nerve would take it out anyway
		case !a.Trees.Present(path):
			pending++
			events = append(events, Event{Kind: EvItem, Project: name, Item: l.Item(), Detail: "gone from " + rel + ": nerve sync makes it again"})
		case a.ours(dir, rel, l) && !a.Trees.Exists(path):
			pending++
			events = append(events, Event{Kind: EvProblem, Path: path, Detail: fmt.Sprintf("it points at %s, which is gone: nerve sync makes it again, or says why not", l.Target)})
		case !wanted[l.Item()]:
			pending++
		}
	}
	// A listed item with nothing in its place waits for a sync, which links
	// it or says why not. One the team has in its place is a problem, as
	// sync reports it.
	for _, kind := range []string{"skill", "agent"} {
		for _, n := range append(slices.Clone(*p.List(kind)), "nerve") {
			l := domain.Link{Kind: kind, Name: n}
			if n == "nerve" && kind == "agent" || linked[l.Item()] {
				continue
			}
			path := filepath.Join(dir, l.Rel())
			_, isLink := a.Trees.LinkTarget(path)
			switch {
			case !a.Trees.Present(path):
				pending++
			case !isLink:
				events = append(events, teamHas(path, l))
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
	e := Event{Kind: EvCheckout, Path: dir, Project: name, Detail: syncState(st, a.Brain.Head())}
	switch {
	case pending == 0 && st.Commit != "": // behind only by pushes that change nothing here
		e.Detail = "synced at " + Short(st.Commit) + ", in step with the Brain"
	case pending == 1:
		e.Detail += "; 1 change waits"
	case pending > 1:
		e.Detail += fmt.Sprintf("; %d changes wait", pending)
	}
	if pending > 0 {
		e.Fix = "nerve sync"
		if here {
			e.Fix += " --here"
		}
	}
	r.Add(e)
	for _, e := range events {
		r.Add(e)
	}
	site := driftSite{path: path, project: name, here: here, common: m.Common, settings: p.Settings, overlay: overlay}
	for _, c := range rc.Drift {
		r.Add(site.event(c))
	}
	return nil
}
