package app

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/medilies/nerve/internal/brain/domain"
)

// AddItem adds a skill, agent, hook or plugin to the target project.
func (a App) AddItem(t Target, kind, name string, r Reporter) error {
	l, err := a.load()
	if err != nil {
		return err
	}
	project, p, err := a.project(l, t)
	if err != nil {
		return err
	}
	if p.Surface == domain.ClaudeAI && kind != "skill" {
		return fmt.Errorf("project %s is a claude.ai account, which takes skills only", project)
	}
	if why := a.offers(l.m, kind, name); why != "" {
		return errors.New(why)
	}
	list := p.List(kind)
	if slices.Contains(*list, name) {
		r.Add(Event{Kind: EvItem, Project: project, Item: kind + " " + name, Detail: "already in project " + project})
		return nil
	}
	*list = append(*list, name)
	if err := a.Brain.Save(l.m); err != nil {
		return err
	}
	r.Add(Event{Kind: EvAdded, Project: project, Item: kind + " " + name})
	return nil
}

// RemoveItem takes an item out of the target project, or with all out of
// every project.
func (a App) RemoveItem(t Target, kind, name string, all bool, r Reporter) error {
	l, err := a.load()
	if err != nil {
		return err
	}
	var projects []string
	if all {
		projects = domain.SortedKeys(l.m.Projects)
	} else {
		project, _, err := a.project(l, t)
		if err != nil {
			return err
		}
		projects = []string{project}
	}
	dropped := 0
	for _, project := range projects {
		list := l.m.Projects[project].List(kind)
		if !slices.Contains(*list, name) {
			continue
		}
		*list = slices.DeleteFunc(*list, func(x string) bool { return x == name })
		dropped++
		r.Add(Event{Kind: EvDropped, Project: project, Item: kind + " " + name})
	}
	if dropped == 0 {
		if all {
			return fmt.Errorf("no project has %s %s", kind, name)
		}
		return fmt.Errorf("project %s has no %s %s", projects[0], kind, name)
	}
	return a.Brain.Save(l.m)
}

// ListItems lists the target project's items of a kind and where each comes
// from. Inside its checkout, skills and agents also show how they are
// linked, and the team's own.
func (a App) ListItems(t Target, kind string, r Reporter) error {
	l, err := a.load()
	if err != nil {
		return err
	}
	project, p, err := a.project(l, t)
	if err != nil {
		return err
	}
	var st domain.TreeState
	dir := ""
	if t.Project == "" && (kind == "skill" || kind == "agent") {
		if tree, _, err := a.here(l.machine, t.Dir); err == nil {
			dir = tree.Top
			if st, err = a.Machine.State(dir); err != nil {
				return err
			}
		}
	}
	linkedAs := map[string]string{} // kind:name -> link
	for rel, what := range st.Links {
		linkedAs[what] = rel
	}
	for _, name := range *p.List(kind) {
		detail := ""
		switch kind {
		case "skill":
			if _, own, _ := a.Brain.OwnSkill(name); own {
				detail = "own"
			} else if src, ok := l.m.SourceOf(name); ok {
				detail = "from " + src.Repo + "@" + src.SHA[:7]
			}
		case "agent":
			detail = "own"
		case "hook":
			h := l.m.Hooks[name]
			detail = strings.TrimSpace(h.Event + " " + h.Matcher)
		case "plugin":
			detail = "enabled"
		}
		if rel, ok := linkedAs[kind+":"+name]; ok {
			detail += ", linked as " + rel
		} else if dir != "" {
			detail += ", not linked yet: sync"
		}
		r.Add(Event{Kind: EvItem, Project: project, Item: kind + " " + name, Detail: detail})
	}
	if dir == "" {
		return nil
	}
	if kind == "skill" {
		r.Add(Event{Kind: EvItem, Project: project, Item: "skill nerve", Detail: "nerve's own"})
	}
	for _, name := range a.teamItems(dir, kind, st) {
		r.Add(Event{Kind: EvItem, Project: project, Item: kind + " " + name, Detail: "the team's"})
	}
	return nil
}

// teamItems lists the skills or agents in a working tree that nerve did not
// link.
func (a App) teamItems(dir, kind string, st domain.TreeState) []string {
	sub := map[string]string{"skill": "skills", "agent": "agents"}[kind]
	var names []string
	for _, e := range a.Workspace.Entries(filepath.Join(dir, ".claude", sub)) {
		if st.Links[path.Join(".claude", sub, e)] != "" || strings.HasPrefix(e, ".") {
			continue
		}
		names = append(names, strings.TrimSuffix(e, ".md"))
	}
	return names
}

// UpdatePin prints how each skill used from repo differs between its pin and
// sha (default: the upstream tip), then moves the pin.
func (a App) UpdatePin(repo, sha string, r Reporter) error {
	m, err := a.Brain.Load()
	if err != nil {
		return err
	}
	repo = strings.TrimSuffix(strings.TrimPrefix(repo, "https://github.com/"), ".git")
	i := slices.IndexFunc(m.Sources, func(s domain.Source) bool { return s.Repo == repo })
	if i < 0 {
		return fmt.Errorf("%s is not a source in %s", repo, domain.ManifestFile)
	}
	src := m.Sources[i]
	if sha == "" {
		if sha, err = a.Sources.Head(repo); err != nil {
			return err
		}
	}
	if !domain.SHAPattern.MatchString(sha) {
		return errors.New("sha must be a full 40-character lowercase commit")
	}
	if sha == src.SHA {
		r.Add(Event{Kind: EvAlreadyThere, Detail: repo, Target: sha})
		return nil
	}
	for _, name := range src.Skills {
		d, err := a.Sources.Diff(repo, name, src.SHA, sha)
		if err != nil {
			return err
		}
		r.Add(Event{Kind: EvDiff, Item: "skill " + name, Detail: d})
	}
	if err := a.Brain.MovePin(src.SHA, sha); err != nil {
		return err
	}
	r.Add(Event{Kind: EvPinned, Detail: repo, Target: sha})
	return nil
}
