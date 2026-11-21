package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/medilies/nerve/internal/brain/domain"
)

// Check validates a Brain: its manifest, own skills and agents, pins, and
// what each project lists.
func (a App) Check(r Reporter) error {
	m, err := a.Brain.Load()
	if err != nil {
		return err
	}
	p, err := a.problems(m)
	if err != nil {
		return err
	}
	for _, e := range p {
		r.Add(problemEvent(e))
	}
	for _, key := range m.AllowedSettings {
		if path, err := domain.ParseKey(key); err == nil && domain.NerveAllows(path) {
			r.Add(Event{Kind: EvNote, Path: domain.ManifestFile, Detail: fmt.Sprintf("allowedSettings lists %s, which nerve allows itself: nerve allow rm %s", key, domain.ShellWord(key))})
		}
	}
	if len(p) > 0 {
		if len(p) == 1 {
			return errors.New("1 problem")
		}
		return fmt.Errorf("%d problems", len(p))
	}
	r.Add(Event{Kind: EvOK, Detail: "no problems"})
	return nil
}

// problems lists what is wrong with a Brain whose manifest is m.
func (a App) problems(m domain.Manifest) ([]domain.Problem, error) {
	var p []domain.Problem
	problem := func(path, key, detail string) {
		p = append(p, domain.Problem{Path: path, Key: key, Detail: detail})
	}
	own, err := a.Brain.OwnSkills()
	if err != nil {
		return nil, err
	}
	ownNames := map[string]bool{}
	for _, s := range own {
		ownNames[s.Name] = true
		rel, _ := filepath.Rel(a.Brain.Root(), s.Dir)
		if s.Err != nil {
			problem(rel, "", s.Err.Error())
			continue
		}
		for _, msg := range s.Problems() {
			problem(rel, "", msg)
		}
		if s.Name == "nerve" {
			problem(rel, "", "nerve is nerve's own skill: rename this one")
		}
	}
	agents, err := a.Brain.Agents()
	if err != nil {
		return nil, err
	}
	for _, ag := range agents {
		if ag.Err != nil {
			problem(ag.Rel, "", ag.Err.Error())
		} else if err := domain.AgentProblem(ag.Name, ag.Meta); err != nil {
			problem(ag.Rel, "", err.Error())
		}
	}
	from := map[string]string{} // third-party skill -> its repo, and folder if any
	for _, src := range m.Sources {
		where := src.Repo
		if src.Path != "" {
			where += "/" + src.Path
		}
		for _, name := range src.Skills {
			if other, ok := from[name]; ok || ownNames[name] {
				if !ok {
					other = "skills/"
				}
				problem(domain.ManifestFile, "source", fmt.Sprintf("skill %s comes from both %s and %s", name, other, where))
			}
			from[name] = where
			if _, err := a.Sources.Skill(src, name); err != nil {
				problem(domain.ManifestFile, "source", err.Error())
			}
		}
	}
	for _, name := range domain.SortedKeys(m.Projects) {
		pr := m.Projects[name]
		key := "projects." + name
		for _, kind := range domain.ItemKinds {
			for _, item := range *pr.List(kind) {
				if why := a.offers(m, kind, item); why != "" {
					problem(domain.ManifestFile, key, why)
				}
			}
		}
		for _, k := range domain.OverridesOfCommon(pr, m.Common) {
			problem(domain.ManifestFile, key+".settings."+k, "equals common: drop it from the project")
		}
	}
	return append(p, m.Refusals()...), nil
}
