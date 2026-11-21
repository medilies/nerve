package app

import (
	"errors"
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

// fresh brings the remote's brain.json to the Brain, then loads it with
// this machine: what a command that may change brain.json starts from.
func (a App) fresh(r Reporter) (loaded, error) {
	if _, err := a.Machine.Load(); err != nil {
		return loaded{}, err
	}
	if err := a.Brain.Refresh(r); err != nil {
		return loaded{}, err
	}
	return a.load()
}

// pushManifest writes m as brain.json, then commits it alone with message and
// pushes it, so the Brain and its remote never hold two brain.json.
func (a App) pushManifest(m domain.Manifest, message string, r Reporter) error {
	if err := a.Brain.Save(m); err != nil {
		return err
	}
	if err := a.Brain.PushManifest("nerve: "+message, r); err != nil {
		if errors.Is(err, ErrRemoteMoved) {
			return fmt.Errorf("%w: nothing changed: run the command again, which takes its commits first", err)
		}
		return fmt.Errorf("%w: nothing changed", err)
	}
	return nil
}

// reachedHere is the working directory when a change to the project ref
// names, or with every to every project, reaches the listed checkout
// around it; "" when it reaches none. A checkout a sync would refuse, it
// refuses before anything changes.
func (a App) reachedHere(ref ProjectRef, every bool) (string, error) {
	machine, err := a.Machine.Load()
	if err != nil {
		return "", err
	}
	t, project, err := a.here(machine, ref.Dir)
	if err != nil || !every && ref.Project != "" && ref.Project != project {
		return "", nil // no listed checkout here, or not its project: the change reaches none
	}
	if err := a.precheck(t); err != nil {
		return "", fmt.Errorf("%s: %w", t.Top, err)
	}
	return ref.Dir, nil
}

// syncHere carries the Brain just pushed to the listed checkout around dir,
// if any. The Brain is its remote's already: a sync of it would only drop
// the user's work in it.
func (a App) syncHere(dir string, r Reporter) error {
	if dir == "" {
		return nil
	}
	return a.Sync(SyncOptions{Dir: dir, Here: true, brainAsIs: true}, r)
}

// here returns the working tree around dir and the project its checkout is
// listed under.
func (a App) here(machine domain.Machine, dir string) (domain.Tree, string, error) {
	t, err := a.Trees.Locate(dir)
	if err != nil {
		return t, "", err
	}
	if c, ok := machine.CheckoutAt(t.Top); ok {
		return t, c.Project, nil
	}
	return t, "", notListed{t.Top}
}

// state is what nerve last did to the checkout at dir. A state it cannot
// read is reported and dropped, so the checkout is as one that moved.
func (a App) state(dir string, r Reporter) (domain.TreeState, error) {
	st, err := a.Machine.State(dir)
	if !errors.Is(err, ErrUnreadable) {
		return st, err
	}
	if err := a.Machine.DropState(dir); err != nil {
		return st, err
	}
	r.Add(Event{Kind: EvProblem, Path: dir, Detail: err.Error() + ": dropped, so nerve takes the links and settings it would make there as its own"})
	return a.Machine.State(dir)
}

// notListed is a working tree no checkout this machine lists holds.
type notListed struct{ top string }

func (e notListed) Error() string {
	return e.top + " is not a listed checkout: run nerve project add <name> in it"
}

// project returns the project ref names.
func (a App) project(l loaded, ref ProjectRef) (string, *domain.Project, error) {
	name := ref.Project
	if name == "" {
		var err error
		if _, name, err = a.here(l.machine, ref.Dir); err != nil {
			if errors.As(err, new(notListed)) {
				return "", nil, fmt.Errorf("%w, or pass --project", err)
			}
			return "", nil, err
		}
	}
	p, err := l.m.Project(name)
	if err != nil && ref.Project == "" { // the checkout here is listed as a project that left the Brain
		err = fmt.Errorf("%w: nerve sync cleans this checkout, and nerve project add lists it again", err)
	}
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
	if (kind == "skill" || kind == "agent") && !domain.NamePattern.MatchString(name) {
		return fmt.Sprintf("%s %q must be lowercase words joined by hyphens", kind, name) // it names a file or folder
	}
	switch kind {
	case "skill":
		if name == "nerve" {
			return "nerve's own skill reaches every project already"
		}
		_, own, _ := a.Brain.OwnSkill(name)
		src, listed := m.SourceOf(name)
		switch {
		case !own && !listed:
			return fmt.Sprintf("the Brain has no skill %q: add skills/%s/, or a source that lists it", name, name)
		case own && listed:
			return fmt.Sprintf("skill %s comes from both skills/ and %s: drop one of them", name, src.Repo)
		}
	case "agent":
		if _, ok := a.Brain.AgentFile(name); !ok {
			return fmt.Sprintf("the Brain has no agent %q: add agents/%s.md", name, name)
		}
	case "hook":
		if _, ok := m.Hooks[name]; !ok {
			return fmt.Sprintf("%s defines no hook %q: add %s to hooks", domain.ManifestFile, name, name)
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
