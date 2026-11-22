package app

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/medilies/nerve/internal/brain/domain"
)

// AddItem adds skills, agents, hooks or plugins to the target project, in
// one push, then syncs the listed checkout around the working directory
// when the project is its own. One name refused, it adds none.
func (a App) AddItem(ref ProjectRef, kind string, names []string, r Reporter) error {
	here, err := a.reachedHere(ref, false)
	if err != nil {
		return err
	}
	l, err := a.fresh(r)
	if err != nil {
		return err
	}
	project, p, err := a.project(l, ref)
	if err != nil {
		return err
	}
	list := p.List(kind)
	var added []string
	for _, name := range names {
		if err := a.addable(l.m, kind, name); err != nil {
			return err
		}
		if slices.Contains(*list, name) || slices.Contains(added, name) {
			r.Add(Event{Kind: EvItem, Project: project, Item: kind + " " + name, Detail: "already in project " + project})
			continue
		}
		if err := a.onRemote(kind, name); err != nil {
			return err
		}
		added = append(added, name)
	}
	if len(added) == 0 {
		return a.syncHere(here, r, pluginSettings(kind, names)...)
	}
	*list = append(*list, added...)
	if err := a.pushManifest(l.m, fmt.Sprintf("add %s %s to project %s", kind, strings.Join(added, ", "), project), r); err != nil {
		return err
	}
	for _, name := range added {
		r.Add(Event{Kind: EvAdded, Project: project, Item: kind + " " + name})
	}
	return a.syncHere(here, r, pluginSettings(kind, names)...)
}

// addable says why the Brain cannot give a project the item, or nil.
func (a App) addable(m domain.Manifest, kind, name string) error {
	if why := a.offers(m, kind, name); why != "" {
		return errors.New(why)
	}
	if kind == "skill" {
		if _, own, err := a.Brain.OwnSkill(name); own && err != nil {
			return err // one sync would only report
		}
	}
	return nil
}

// onRemote refuses an own item the Brain's remote lacks: what brain.json
// lists, the remote must hold, or other machines' syncs refuse the project.
func (a App) onRemote(kind, name string) error {
	rel := ""
	if _, own, _ := a.Brain.OwnSkill(name); kind == "skill" && own {
		rel = "skills/" + name
	} else if kind == "agent" {
		rel = "agents/" + name + ".md"
	}
	if rel != "" && !a.Brain.OnRemote(rel) {
		return fmt.Errorf("%s %s is not on the Brain's remote yet, as it is here: nerve push it, then add it", kind, name)
	}
	return nil
}

// RemoveItem takes items out of the target project, or with all out of
// every project, in one push, then syncs the listed checkout around the
// working directory when the change reaches it. A name no project it acts
// on lists, it refuses, and removes none.
func (a App) RemoveItem(ref ProjectRef, kind string, names []string, all bool, r Reporter) error {
	if err := onePlace(ref.Project, false, all); err != nil {
		return err
	}
	here, err := a.reachedHere(ref, all)
	if err != nil {
		return err
	}
	names = unique(names)
	l, err := a.fresh(r)
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
	for _, name := range names {
		found := false
		for _, project := range projects {
			list := l.m.Projects[project].List(kind)
			if !slices.Contains(*list, name) {
				continue
			}
			found = true
			*list = slices.DeleteFunc(*list, func(x string) bool { return x == name })
			dropped = append(dropped, Event{Kind: EvDropped, Project: project, Item: kind + " " + name})
		}
		switch {
		case found:
		case all:
			return fmt.Errorf("no project has %s %s", kind, name)
		default:
			return fmt.Errorf("project %s has no %s %s", projects[0], kind, name)
		}
	}
	where := "project " + projects[0]
	if all {
		where = "every project"
	}
	if err := a.pushManifest(l.m, fmt.Sprintf("remove %s %s from %s", kind, strings.Join(names, ", "), where), r); err != nil {
		return err
	}
	for _, e := range dropped {
		r.Add(e)
	}
	return a.syncHere(here, r, pluginSettings(kind, names)...)
}

// unique is names without repeats, in order.
func unique(names []string) []string {
	var u []string
	for _, n := range names {
		if !slices.Contains(u, n) {
			u = append(u, n)
		}
	}
	return u
}

// pluginSettings names the settings nerve writes for plugins of names, or
// none for another kind.
func pluginSettings(kind string, names []string) []domain.Named {
	if kind != "plugin" {
		return nil
	}
	var named []domain.Named
	for _, n := range names {
		named = append(named, domain.Named{Leaf: domain.LeafKey([]string{"enabledPlugins", n})})
	}
	return named
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
// default branch, every entry of a repo together, first showing how each
// of its skills changed, with the whole diff when full, and pushes the new
// pins. Then it syncs the listed checkout around dir, if any, so the
// project here gets them. A source that cannot move is reported, and the
// others still do. A dry run shows the same from the Brain as it is here,
// and moves no pin.
func (a App) Update(dir, repo string, full, dryRun bool, r Reporter) error {
	load := func() (loaded, error) { return a.fresh(r) }
	if dryRun {
		load = a.load
	}
	l, err := load()
	if err != nil {
		return err
	}
	m := l.m
	repo = strings.TrimSuffix(strings.TrimPrefix(repo, "https://github.com/"), ".git")
	if repo != "" && !slices.ContainsFunc(m.Sources, func(s domain.Source) bool { return strings.EqualFold(s.Repo, repo) }) {
		return fmt.Errorf("%s is not a source in %s", repo, domain.ManifestFile)
	}
	if len(m.Sources) == 0 {
		r.Add(Event{Kind: EvOK, Detail: domain.ManifestFile + " has no sources to update"})
		return nil
	}
	failed := 0
	var pinned []Event // reported once pushed
	var moved []string
	for i, src := range m.Sources {
		sameRepo := func(s domain.Source) bool { return strings.EqualFold(s.Repo, src.Repo) }
		if repo != "" && !strings.EqualFold(src.Repo, repo) || slices.ContainsFunc(m.Sources[:i], sameRepo) {
			continue
		}
		// Each entry of a repo is at one pin, and moves with the others.
		var entries []domain.Source
		for _, s := range m.Sources[i:] {
			if sameRepo(s) {
				entries = append(entries, s)
			}
		}
		sha, err := a.pinTip(entries, full, r)
		switch {
		case err != nil:
			r.Add(Event{Kind: EvProblem, Path: domain.ManifestFile, Key: src.Repo, Detail: err.Error()})
			failed++
		case sha == src.SHA:
			r.Add(Event{Kind: EvAlreadyThere, Detail: src.Repo, Target: sha})
		case dryRun:
			moved = append(moved, src.Repo)
		default:
			for j := range m.Sources {
				if sameRepo(m.Sources[j]) {
					m.Sources[j].SHA = sha
				}
			}
			pinned = append(pinned, Event{Kind: EvPinned, Detail: src.Repo, Target: sha})
			moved = append(moved, src.Repo+" at "+Short(sha))
		}
	}
	var errs []error
	switch {
	case dryRun && len(moved) > 0:
		r.Add(Event{Kind: EvOK, Detail: "dry run: no pin moved; nerve update moves them"})
	case len(moved) > 0:
		if err := a.pushManifest(m, "pin "+strings.Join(moved, ", "), r); err != nil {
			return err
		}
		for _, e := range pinned {
			r.Add(e)
		}
		if _, _, err := a.here(l.machine, dir); err == nil {
			// The Brain is its remote's already: a sync of it would only
			// drop the user's work in it.
			errs = append(errs, a.Sync(SyncOptions{Dir: dir, Here: true, brainAsIs: true}, r))
		} else {
			r.Add(Event{Kind: EvOK, Detail: "no listed checkout here: nerve sync carries the new pins to each one"})
		}
	}
	if failed > 0 {
		what := "sources"
		if failed == 1 {
			what = "source"
		}
		errs = append(errs, fmt.Errorf("%d %s did not move", failed, what))
	}
	return errors.Join(errs...)
}

// pinTip finds the tip of a repo's default branch and reports how each
// skill of its entries changed since their pin, with the whole diff when
// full.
func (a App) pinTip(entries []domain.Source, full bool, r Reporter) (string, error) {
	src := entries[0]
	sha, err := a.Sources.Tip(src.Repo)
	if err != nil || sha == src.SHA {
		return sha, err
	}
	from, err := a.Sources.Committed(src.Repo, src.SHA)
	if err != nil {
		return "", err
	}
	to, err := a.Sources.Committed(src.Repo, sha)
	if err != nil {
		return "", err
	}
	events := []Event{{Kind: EvTip, Key: src.Repo, From: src.SHA, FromDate: day(from), Target: sha, TargetDate: day(to)}}
	for _, entry := range entries {
		for _, name := range entry.Skills {
			d, err := a.Sources.Diff(entry, name, sha)
			if err != nil {
				return "", err
			}
			e := Event{Kind: EvSame, Key: src.Repo, Item: "skill " + name}
			if len(d.Files) > 0 {
				e.Kind, e.Files, e.Path = EvChanged, d.Files, d.Dir
				if full {
					e.Detail = d.Text
				}
			}
			events = append(events, e)
		}
	}
	for _, e := range events {
		r.Add(e)
	}
	return sha, nil
}

// day is the date of t, in UTC.
func day(t time.Time) string { return t.UTC().Format(time.DateOnly) }
