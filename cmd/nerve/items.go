package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// itemKinds are what a project lists by name.
var itemKinds = []string{"skill", "agent", "hook", "plugin"}

func (p *Project) list(kind string) *[]string {
	return map[string]*[]string{"skill": &p.Skills, "agent": &p.Agents, "hook": &p.Hooks, "plugin": &p.Plugins}[kind]
}

func runItems(kind string, args []string, f flags, r *report) error {
	if len(args) == 0 {
		return errUsage
	}
	w, err := openWorkspace()
	if err != nil {
		return err
	}
	switch verb := args[0]; {
	case verb == "add" && len(args) == 2:
		return w.itemAdd(kind, args[1], f, r)
	case verb == "rm" && len(args) == 2:
		return w.itemRm(kind, args[1], f, r)
	case verb == "ls" && len(args) == 1:
		return w.itemLs(kind, f, r)
	}
	return errUsage
}

// offers says why the Brain cannot give a project this item, or "".
func (w workspace) offers(kind, name string) string {
	b := brain{w.root, w.m}
	switch kind {
	case "skill":
		if name == "nerve" {
			return "nerve's own skill reaches every project already"
		}
		if !b.hasSkill(name) {
			return fmt.Sprintf("the Brain has no skill %q: add skills/%s/, or a source that lists it", name, name)
		}
	case "agent":
		if !b.hasAgent(name) {
			return fmt.Sprintf("the Brain has no agent %q: add agents/%s.md", name, name)
		}
	case "hook":
		if _, ok := w.m.Hooks[name]; !ok {
			return fmt.Sprintf("%s defines no hook %q: add a [hook.%s] table", manifestFile, name, name)
		}
	case "plugin":
		if n, mk, ok := strings.Cut(name, "@"); !ok || n == "" || mk == "" || strings.Contains(mk, "@") {
			return fmt.Sprintf("plugin %q must be <plugin>@<marketplace>", name)
		}
	}
	return ""
}

func (w workspace) itemAdd(kind, name string, f flags, r *report) error {
	project, p, err := w.target(f)
	if err != nil {
		return err
	}
	if p.Surface == claudeAI && kind != "skill" {
		return fmt.Errorf("project %s is a claude.ai account, which takes skills only", project)
	}
	if why := w.offers(kind, name); why != "" {
		return errors.New(why)
	}
	list := p.list(kind)
	if slices.Contains(*list, name) {
		r.add(Event{Kind: evItem, Project: project, Item: kind + " " + name, Detail: "already in project " + project})
		return nil
	}
	*list = append(*list, name)
	if err := w.save(); err != nil {
		return err
	}
	r.add(Event{Kind: evAdded, Project: project, Item: kind + " " + name})
	return nil
}

// itemRm takes an item out of the project, or with --all out of every
// project.
func (w workspace) itemRm(kind, name string, f flags, r *report) error {
	var projects []string
	if f.all {
		projects = sortedKeys(w.m.Projects)
	} else {
		project, _, err := w.target(f)
		if err != nil {
			return err
		}
		projects = []string{project}
	}
	dropped := 0
	for _, project := range projects {
		list := w.m.Projects[project].list(kind)
		if !slices.Contains(*list, name) {
			continue
		}
		*list = slices.DeleteFunc(*list, func(x string) bool { return x == name })
		dropped++
		r.add(Event{Kind: evDropped, Project: project, Item: kind + " " + name})
	}
	if dropped == 0 {
		if f.all {
			return fmt.Errorf("no project has %s %s", kind, name)
		}
		return fmt.Errorf("project %s has no %s %s", projects[0], kind, name)
	}
	return w.save()
}

// itemLs lists the project's items of a kind and where each comes from.
// Inside its checkout, skills and agents also show how they are linked, and
// the team's own.
func (w workspace) itemLs(kind string, f flags, r *report) error {
	project, p, err := w.target(f)
	if err != nil {
		return err
	}
	b := brain{w.root, w.m}
	var st state
	dir := ""
	if f.project == "" && (kind == "skill" || kind == "agent") {
		if t, _, err := w.machine.here(); err == nil {
			dir = t.Top
			if st, err = loadState(dir); err != nil {
				return err
			}
		}
	}
	linkedAs := map[string]string{} // kind:name -> link
	for rel, what := range st.Links {
		linkedAs[what] = rel
	}
	for _, name := range *p.list(kind) {
		detail := ""
		switch kind {
		case "skill":
			if src, ok := b.source(name); ok && !fileExists(filepath.Join(b.ownSkillDir(name), "SKILL.md")) {
				detail = "from " + src.Repo + "@" + src.SHA[:7]
			} else {
				detail = "own"
			}
		case "agent":
			detail = "own"
		case "hook":
			h := w.m.Hooks[name]
			detail = strings.TrimSpace(h.Event + " " + h.Matcher)
		case "plugin":
			detail = "enabled"
		}
		if rel, ok := linkedAs[kind+":"+name]; ok {
			detail += ", linked as " + rel
		} else if dir != "" {
			detail += ", not linked yet: sync"
		}
		r.add(Event{Kind: evItem, Project: project, Item: kind + " " + name, Detail: detail})
	}
	if dir == "" {
		return nil
	}
	if kind == "skill" {
		r.add(Event{Kind: evItem, Project: project, Item: "skill nerve", Detail: "nerve's own"})
	}
	for _, name := range teamItems(dir, kind, st) {
		r.add(Event{Kind: evItem, Project: project, Item: kind + " " + name, Detail: "the team's"})
	}
	return nil
}

// teamItems lists the skills or agents in a working tree that nerve did not
// link.
func teamItems(dir, kind string, st state) []string {
	sub := map[string]string{"skill": "skills", "agent": "agents"}[kind]
	entries, err := os.ReadDir(filepath.Join(dir, ".claude", sub))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		rel := filepath.Join(".claude", sub, e.Name())
		if st.Links[rel] != "" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	return names
}

// runUpdate prints how each skill used from repo differs between its pin and
// sha (default: the upstream tip), then moves the pin.
func runUpdate(root, repo, sha string, r *report) error {
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	repo = strings.TrimSuffix(strings.TrimPrefix(repo, "https://github.com/"), ".git")
	i := slices.IndexFunc(m.Sources, func(s Source) bool { return s.Repo == repo })
	if i < 0 {
		return fmt.Errorf("%s is not a source in %s", repo, manifestFile)
	}
	src := m.Sources[i]
	if sha == "" {
		if sha, err = remoteHead(repo); err != nil {
			return err
		}
	}
	if !shaPattern.MatchString(sha) {
		return errors.New("sha must be a full 40-character lowercase commit")
	}
	if sha == src.SHA {
		r.add(Event{Kind: evAlreadyThere, Detail: repo, Target: sha})
		return nil
	}
	oldTree, err := fetch(repo, src.SHA)
	if err != nil {
		return err
	}
	newTree, err := fetch(repo, sha)
	if err != nil {
		return err
	}
	for _, name := range src.Skills {
		oldDir, err := findSkill(oldTree, name)
		if err != nil {
			return err
		}
		newDir, err := findSkill(newTree, name)
		if err != nil {
			return fmt.Errorf("at %s: %w; drop it from %s first", sha[:7], err, manifestFile)
		}
		d, err := diff(oldDir, newDir)
		if err != nil {
			return err
		}
		r.add(Event{Kind: evDiff, Item: "skill " + name, Detail: d})
	}
	path := filepath.Join(root, manifestFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Count(b, []byte(src.SHA)) != 1 {
		return fmt.Errorf("%s: pin %s must appear exactly once", manifestFile, src.SHA)
	}
	if err := writeFile(path, bytes.Replace(b, []byte(src.SHA), []byte(sha), 1)); err != nil {
		return err
	}
	r.add(Event{Kind: evPinned, Detail: repo, Target: sha})
	return nil
}
