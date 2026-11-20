package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// runCheck validates a Brain: its manifest, own skills and agents, pins, and
// what each project lists.
func runCheck(root string, r *report) error {
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	b := brain{root, m}
	var p []Event
	problem := func(path, key, detail string) {
		p = append(p, Event{Kind: evProblem, Path: path, Key: key, Detail: detail})
	}
	own, err := b.ownSkills()
	if err != nil {
		return err
	}
	ownNames := map[string]bool{}
	for _, s := range own {
		ownNames[s.Name] = true
		rel, _ := filepath.Rel(root, s.Dir)
		for _, msg := range s.problems() {
			problem(rel, "", msg)
		}
		if s.Name == "nerve" {
			problem(rel, "", "nerve is nerve's own skill: rename this one")
		}
	}
	agents, _ := filepath.Glob(filepath.Join(root, "agents", "*.md"))
	for _, a := range agents {
		rel, _ := filepath.Rel(root, a)
		meta, err := readFrontmatter(a)
		switch {
		case err != nil:
			problem(rel, "", err.Error())
		case meta["name"] != strings.TrimSuffix(filepath.Base(a), ".md"):
			problem(rel, "", fmt.Sprintf("frontmatter name %q differs from file name", meta["name"]))
		case meta["description"] == "":
			problem(rel, "", "no description")
		}
	}
	from := map[string]string{} // third-party skill -> its repo
	for _, src := range m.Sources {
		tree, err := fetch(src.Repo, src.SHA)
		if err != nil {
			problem(manifestFile, "source", err.Error())
			continue
		}
		for _, name := range src.Skills {
			if other, ok := from[name]; ok || ownNames[name] {
				if !ok {
					other = "skills/"
				}
				problem(manifestFile, "source", fmt.Sprintf("skill %s comes from both %s and %s", name, other, src.Repo))
			}
			from[name] = src.Repo
			if _, err := findSkill(tree, name); err != nil {
				problem(manifestFile, "source", fmt.Sprintf("%s@%s: %v", src.Repo, src.SHA[:7], err))
			}
		}
	}
	w := workspace{root: root, m: m}
	common := leaves(m.Common.Settings)
	for _, name := range sortedKeys(m.Projects) {
		pr := m.Projects[name]
		key := "project." + name
		for _, kind := range itemKinds {
			for _, item := range *pr.list(kind) {
				if why := w.offers(kind, item); why != "" {
					problem(manifestFile, key, why)
				}
			}
		}
		if pr.Surface == claudeAI && (len(pr.Agents)+len(pr.Hooks)+len(pr.Plugins)+len(pr.Settings) > 0 || pr.Autosync != nil) {
			problem(manifestFile, key, "a claude.ai account takes skills only")
		}
		l := leaves(pr.Settings)
		for _, k := range sortedKeys(l) {
			if v, ok := common[k]; ok && reflect.DeepEqual(v, l[k]) {
				problem(manifestFile, key+".settings."+dotted(k), "equals common: drop it from the project")
			}
		}
	}
	p = append(p, manifestProblems(m)...)
	for _, e := range p {
		r.add(e)
	}
	if len(p) > 0 {
		return fmt.Errorf("%d problems", len(p))
	}
	r.add(Event{Kind: evOK, Detail: "no problems"})
	return nil
}
