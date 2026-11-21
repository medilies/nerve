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

// Update moves the pin of each source, or of repo alone, to the tip of its
// default branch, first showing how each of its skills changed. A source
// that cannot move is reported, and the others still do.
func (a App) Update(repo string, r Reporter) error {
	m, err := a.Brain.Load()
	if err != nil {
		return err
	}
	repo = strings.TrimSuffix(strings.TrimPrefix(repo, "https://github.com/"), ".git")
	if repo != "" && !slices.ContainsFunc(m.Sources, func(s domain.Source) bool { return s.Repo == repo }) {
		return fmt.Errorf("%s is not a source in %s", repo, domain.ManifestFile)
	}
	moved, failed := false, 0
	for i, src := range m.Sources {
		if repo != "" && src.Repo != repo {
			continue
		}
		sha, err := a.pinTip(src, r)
		switch {
		case err != nil:
			r.Add(Event{Kind: EvProblem, Path: domain.ManifestFile, Key: src.Repo, Detail: err.Error()})
			failed++
		case sha == src.SHA:
			r.Add(Event{Kind: EvAlreadyThere, Detail: src.Repo, Target: sha})
		default:
			m.Sources[i].SHA, moved = sha, true
			r.Add(Event{Kind: EvPinned, Detail: src.Repo, Target: sha})
		}
	}
	if moved {
		if err := a.Brain.Save(m); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d sources did not move", failed)
	}
	return nil
}

// pinTip finds the tip of a source's default branch and reports how each of
// its skills changed since the pin.
func (a App) pinTip(src domain.Source, r Reporter) (string, error) {
	sha, err := a.Sources.Head(src.Repo)
	if err != nil || sha == src.SHA {
		return sha, err
	}
	var diffs []Event
	for _, name := range src.Skills {
		d, err := a.Sources.Diff(src, name, sha)
		if err != nil {
			return "", err
		}
		diffs = append(diffs, Event{Kind: EvDiff, Item: "skill " + name, Detail: d})
	}
	for _, e := range diffs {
		r.Add(e)
	}
	return sha, nil
}
