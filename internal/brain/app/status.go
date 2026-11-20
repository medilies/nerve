package app

import (
	"fmt"

	"github.com/medilies/nerve/internal/brain/domain"
)

// Status says which nerve runs, where the Brain is and what it holds that is
// not pushed, and how the target project stands: autosync, links, the
// team's skills, and drift. Outside a listed checkout, it lists the projects
// instead.
func (a App) Status(ref ProjectRef, r Reporter) error {
	machine, err := a.Machine.Load()
	if err != nil {
		r.Add(Event{Kind: EvVersion, Detail: a.Self.Version()})
		return err
	}
	m, err := a.Brain.Load()
	r.Add(Event{Kind: EvVersion, Detail: a.Self.Version(), Target: m.Nerve})
	if err != nil {
		return err
	}
	if m.Projects == nil {
		m.Projects = map[string]*domain.Project{}
	}
	r.Add(Event{Kind: EvBrain, Path: a.Brain.Root(), Detail: a.Brain.State()})
	l := loaded{machine: machine, m: m}
	tree, name, err := a.here(machine, ref.Dir)
	if ref.Project != "" {
		if _, err := m.Project(ref.Project); err != nil {
			return err
		}
		name, err, tree = ref.Project, nil, domain.Tree{}
		for _, c := range machine.Checkouts {
			if c.Project == name {
				tree = domain.Tree{Top: c.Path, Main: c.Path}
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
	if tree.Top == "" {
		return nil
	}
	r.Add(Event{Kind: EvCheckout, Path: tree.Top, Project: name, Detail: a.syncState(tree.Top, a.Brain.Head())})
	st, err := a.Machine.State(tree.Top)
	if err != nil {
		return err
	}
	for _, rel := range domain.SortedKeys(st.Links) {
		r.Add(Event{Kind: EvItem, Project: name, Item: st.Links[rel].Item(), Detail: "linked as " + rel})
	}
	for _, kind := range []string{"skill", "agent"} {
		for _, item := range a.teamItems(tree.Top, kind, st) {
			r.Add(Event{Kind: EvItem, Project: name, Item: kind + " " + item, Detail: "the team's"})
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
	path := settingsPath(tree.Top)
	current, err := a.Trees.ReadSettings(path)
	if err != nil {
		return err
	}
	rc := domain.Reconcile(current, st.Settings, domain.Want(m, p, cmd, overlay, a.Brain.Root()), false)
	for _, c := range rc.Drift {
		r.Add(driftEvent(path, c))
	}
	return nil
}
