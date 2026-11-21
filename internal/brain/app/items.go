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
func (a App) AddItem(ref ProjectRef, kind, name string, r Reporter) error {
	l, err := a.load()
	if err != nil {
		return err
	}
	project, p, err := a.project(l, ref)
	if err != nil {
		return err
	}
	if why := a.offers(l.m, kind, name); why != "" {
		return errors.New(why)
	}
	if kind == "skill" {
		if _, own, err := a.Brain.OwnSkill(name); own && err != nil {
			return err // one sync would only report
		}
	}
	if why := domain.SensitiveText(name); why != "" {
		msg := fmt.Sprintf("%s %q %s, so it cannot go in the Brain", kind, name, why)
		if kind == "plugin" {
			msg += fmt.Sprintf(`: enable it on this machine only with nerve setting set 'enabledPlugins."%s"' true`, name)
		}
		return errors.New(msg)
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
func (a App) RemoveItem(ref ProjectRef, kind, name string, all bool, r Reporter) error {
	if err := onePlace(ref.Project, false, all); err != nil {
		return err
	}
	l, err := a.load()
	if err != nil {
		return err
	}
	var projects []string
	if all {
		projects = domain.SortedKeys(l.m.Projects)
	} else {
		project, _, err := a.project(l, ref)
		if err != nil {
			return err
		}
		projects = []string{project}
	}
	var dropped []Event
	for _, project := range projects {
		list := l.m.Projects[project].List(kind)
		if !slices.Contains(*list, name) {
			continue
		}
		*list = slices.DeleteFunc(*list, func(x string) bool { return x == name })
		dropped = append(dropped, Event{Kind: EvDropped, Project: project, Item: kind + " " + name})
	}
	if len(dropped) == 0 {
		if all {
			return fmt.Errorf("no project has %s %s", kind, name)
		}
		return fmt.Errorf("project %s has no %s %s", projects[0], kind, name)
	}
	if err := a.Brain.Save(l.m); err != nil {
		return err
	}
	for _, e := range dropped {
		r.Add(e)
	}
	return nil
}

// ListItems lists the target project's items of a kind and where each comes
// from. Inside its checkout, skills and agents also show how they are
// linked, and the team's own.
func (a App) ListItems(ref ProjectRef, kind string, r Reporter) error {
	l, err := a.load()
	if err != nil {
		return err
	}
	project, p, err := a.project(l, ref)
	if err != nil {
		return err
	}
	var st domain.TreeState
	dir := ""
	if ref.Project == "" && (kind == "skill" || kind == "agent") {
		if tree, _, err := a.here(l.machine, ref.Dir); err == nil {
			dir = tree.Top
			if st, err = a.Machine.State(dir); err != nil {
				return err
			}
		}
	}
	linkedAs := map[string]string{} // item -> link
	for rel, l := range st.Links {
		linkedAs[l.Item()] = rel
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
		link := domain.Link{Kind: kind, Name: name}
		if rel, ok := linkedAs[link.Item()]; ok && a.ours(dir, rel, st.Links[rel]) {
			detail += ", linked as " + rel
		} else if _, isLink := a.Trees.LinkTarget(filepath.Join(dir, link.Rel())); dir != "" && isLink {
			detail += ", not linked: yours at " + link.Rel()
		} else if dir != "" && (kind == "skill" || kind == "agent") && a.Trees.Present(filepath.Join(dir, link.Rel())) {
			detail += ", not linked: the team has " + link.Rel()
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
	for _, e := range a.othersItems(dir, kind, st) {
		e.Project = project
		r.Add(e)
	}
	return nil
}

// othersItems lists the skills or agents in a working tree that nerve did
// not link, each as the team's or, for a link someone made, as yours. A
// skill is a folder holding a SKILL.md, and an agent a .md file whose
// frontmatter names and describes it, as Claude Code reads agents.
func (a App) othersItems(dir, kind string, st domain.TreeState) []Event {
	sub := map[string]string{"skill": "skills", "agent": "agents"}[kind]
	var events []Event
	for _, e := range a.Trees.Entries(filepath.Join(dir, ".claude", sub)) {
		rel := path.Join(".claude", sub, e)
		l, known := st.Links[rel]
		_, isLink := a.Trees.LinkTarget(filepath.Join(dir, rel))
		isItem := kind == "agent" && strings.HasSuffix(e, ".md") && (isLink || a.agentFile(filepath.Join(dir, rel))) ||
			kind == "skill" && (isLink || a.Trees.Exists(filepath.Join(dir, rel, "SKILL.md")))
		if known && a.ours(dir, rel, l) || strings.HasPrefix(e, ".") || !isItem {
			continue
		}
		whose := "the team's"
		if isLink {
			whose = "yours"
		}
		events = append(events, Event{Kind: EvItem, Project: st.Project, Item: kind + " " + strings.TrimSuffix(e, ".md"), Detail: whose})
	}
	return events
}

// agentFile says whether the Markdown file at path is an agent Claude Code
// reads: its frontmatter gives a name and a description.
func (a App) agentFile(path string) bool {
	meta, err := a.Trees.Frontmatter(path)
	return err == nil && meta["name"] != "" && meta["description"] != ""
}

// UpdatePin prints how each skill used from repo differs between its pin and
// sha (default: the tip of the source's default branch), then moves the pin.
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
	if err := a.Brain.MovePin(repo, src.SHA, sha); err != nil {
		return err
	}
	r.Add(Event{Kind: EvPinned, Detail: repo, Target: sha})
	return nil
}
