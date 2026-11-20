package app

import (
	"fmt"
	"path/filepath"

	"github.com/medilies/nerve/internal/brain/domain"
)

// ProjectRef names the project a use case acts on: the one given, else the one
// whose checkout holds the working directory.
type ProjectRef struct {
	Dir     string
	Project string
}

// loaded is this machine and the Brain's manifest, read once per use case.
type loaded struct {
	machine domain.Machine
	m       domain.Manifest
}

func (a App) load() (loaded, error) {
	machine, err := a.Machine.Load()
	if err != nil {
		return loaded{}, err
	}
	m, err := a.Brain.Load()
	if err != nil {
		return loaded{}, err
	}
	if m.Projects == nil {
		m.Projects = map[string]*domain.Project{}
	}
	return loaded{machine: machine, m: m}, nil
}

// here returns the working tree around dir and the project its checkout is
// listed under.
func (a App) here(machine domain.Machine, dir string) (domain.Tree, string, error) {
	t, err := a.Trees.Locate(dir)
	if err != nil {
		return t, "", err
	}
	for _, p := range []string{t.Top, t.Main} {
		if c, ok := machine.CheckoutAt(p); ok {
			return t, c.Project, nil
		}
	}
	return t, "", fmt.Errorf("%s is not a listed checkout: run nerve project add <name> here, or pass --project", t.Top)
}

// project returns the project ref names.
func (a App) project(l loaded, ref ProjectRef) (string, *domain.Project, error) {
	name := ref.Project
	if name == "" {
		var err error
		if _, name, err = a.here(l.machine, ref.Dir); err != nil {
			return "", nil, err
		}
	}
	p, err := l.m.Project(name)
	return name, p, err
}

// skill returns a skill by name: an own one, else a third-party one at its
// pin.
func (a App) skill(m domain.Manifest, name string) (domain.Skill, error) {
	if s, ok, err := a.Brain.OwnSkill(name); ok || err != nil {
		return s, err
	}
	src, ok := m.SourceOf(name)
	if !ok {
		return domain.Skill{}, fmt.Errorf("the Brain has no skill %q", name)
	}
	return a.Sources.Skill(src, name)
}

// offers says why the Brain cannot give a project an item, or "".
func (a App) offers(m domain.Manifest, kind, name string) string {
	switch kind {
	case "skill":
		if name == "nerve" {
			return "nerve's own skill reaches every project already"
		}
		_, own, _ := a.Brain.OwnSkill(name)
		if _, listed := m.SourceOf(name); !own && !listed {
			return fmt.Sprintf("the Brain has no skill %q: add skills/%s/, or a source that lists it", name, name)
		}
	case "agent":
		if _, ok := a.Brain.AgentFile(name); !ok {
			return fmt.Sprintf("the Brain has no agent %q: add agents/%s.md", name, name)
		}
	case "hook":
		if _, ok := m.Hooks[name]; !ok {
			return fmt.Sprintf("%s defines no hook %q: add a [hook.%s] table", domain.ManifestFile, name, name)
		}
	case "plugin":
		if !pluginID(name) {
			return fmt.Sprintf("plugin %q must be <plugin>@<marketplace>", name)
		}
	}
	return ""
}

func pluginID(s string) bool {
	at := 0
	for i, c := range s {
		if c == '@' {
			if at != 0 || i == 0 {
				return false
			}
			at = i
		}
	}
	return at > 0 && at < len(s)-1
}

// settingsPath is a tree's settings.local.json.
func settingsPath(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(domain.LocalSettings))
}
