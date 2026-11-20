// Package domain is the Brain and the rules for carrying it to projects:
// what a project gets, how settings are laid and reconciled, and what must
// never enter the Brain. It knows no file format, git or filesystem.
package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// ManifestFile is the Brain's manifest, as users know it.
const ManifestFile = "brain.toml"

// ClaudeAI is the surface of a project that stands for a claude.ai account.
const ClaudeAI = "claude.ai"

var (
	TagPattern  = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	RepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	SHAPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	SumPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	NamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Manifest is a Brain's brain.toml: the nerve release that carries it, the
// third-party sources it pins, the hooks it defines, the settings common to
// its projects, and the projects themselves.
type Manifest struct {
	Nerve          string
	NerveChecksums string
	Autosync       *bool
	Sources        []Source
	Hooks          map[string]Hook
	Common         map[string]any // settings every listed project gets
	Projects       map[string]*Project
}

// Source is one third-party repo, pinned to a commit.
type Source struct {
	Repo   string
	SHA    string
	Skills []string
}

// Hook is a Claude Code hook a project can take by name.
type Hook struct {
	Event   string
	Matcher string
	Command string
}

// Project is one project's entry: what reaches it, and nothing else does.
type Project struct {
	Surface  string // "" for a Claude Code project, or ClaudeAI
	Autosync *bool
	Skills   []string
	Agents   []string
	Hooks    []string
	Plugins  []string
	Settings map[string]any
}

// ItemKinds are what a project lists by name.
var ItemKinds = []string{"skill", "agent", "hook", "plugin"}

// List is the project's list of one item kind.
func (p *Project) List(kind string) *[]string {
	return map[string]*[]string{"skill": &p.Skills, "agent": &p.Agents, "hook": &p.Hooks, "plugin": &p.Plugins}[kind]
}

// Validate checks what a manifest can be wrong about on its own.
func (m Manifest) Validate() error {
	if m.Nerve != "" && !TagPattern.MatchString(m.Nerve) {
		return fmt.Errorf("%s: nerve %q must be a release tag such as v1.2.3", ManifestFile, m.Nerve)
	}
	if m.NerveChecksums != "" && (m.Nerve == "" || !SumPattern.MatchString(m.NerveChecksums)) {
		return fmt.Errorf("%s: nerve-checksums must be the lowercase SHA-256 of the pinned nerve release's checksums.txt", ManifestFile)
	}
	for _, s := range m.Sources {
		if !RepoPattern.MatchString(s.Repo) {
			return fmt.Errorf("%s: repo %q is not owner/name", ManifestFile, s.Repo)
		}
		if !SHAPattern.MatchString(s.SHA) {
			return fmt.Errorf("%s: %s: sha must be a full 40-character lowercase commit", ManifestFile, s.Repo)
		}
		if len(s.Skills) == 0 {
			return fmt.Errorf("%s: %s: no skills", ManifestFile, s.Repo)
		}
	}
	for name, h := range m.Hooks {
		if !NamePattern.MatchString(name) {
			return fmt.Errorf("%s: hook %q must be lowercase words joined by hyphens", ManifestFile, name)
		}
		if h.Event == "" || h.Command == "" {
			return fmt.Errorf("%s: hook %s needs an event and a command", ManifestFile, name)
		}
	}
	for name, p := range m.Projects {
		if !NamePattern.MatchString(name) {
			return fmt.Errorf("%s: project %q must be lowercase words joined by hyphens", ManifestFile, name)
		}
		if p.Surface != "" && p.Surface != ClaudeAI {
			return fmt.Errorf("%s: project %s: surface must be %q or left out", ManifestFile, name, ClaudeAI)
		}
	}
	return nil
}

// AutosyncFor says whether a project syncs at each session start: its own
// choice, else the Brain's, else on.
func (m Manifest) AutosyncFor(p *Project) bool {
	switch {
	case p != nil && p.Autosync != nil:
		return *p.Autosync
	case m.Autosync != nil:
		return *m.Autosync
	}
	return true
}

// Project returns the named project, or an error naming the manifest.
func (m Manifest) Project(name string) (*Project, error) {
	p := m.Projects[name]
	if p == nil {
		return nil, fmt.Errorf("%s lists no project %q", ManifestFile, name)
	}
	return p, nil
}

// SourceOf returns the source that lists a skill, if any.
func (m Manifest) SourceOf(skill string) (Source, bool) {
	for _, s := range m.Sources {
		for _, n := range s.Skills {
			if n == skill {
				return s, true
			}
		}
	}
	return Source{}, false
}

// Describe sums up a project in a line.
func (m Manifest) Describe(p *Project) string {
	var parts []string
	switch {
	case p.Surface == ClaudeAI:
		parts = append(parts, "claude.ai account")
	case m.AutosyncFor(p):
		parts = append(parts, "autosync on")
	default:
		parts = append(parts, "autosync off")
	}
	for _, x := range []struct {
		n    int
		what string
	}{{len(p.Skills), "skills"}, {len(p.Agents), "agents"}, {len(p.Hooks), "hooks"}, {len(p.Plugins), "plugins"}, {len(Leaves(p.Settings)), "settings"}} {
		switch {
		case x.n == 1:
			parts = append(parts, "1 "+strings.TrimSuffix(x.what, "s"))
		case x.n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", x.n, x.what))
		}
	}
	return strings.Join(parts, ", ")
}

// Problem is something wrong with the Brain, at a path and possibly a key.
type Problem struct {
	Path   string
	Key    string
	Detail string
}

// SensitiveValues lists the values in the manifest that must never be in a
// Brain, which may be public.
func (m Manifest) SensitiveValues() []Problem {
	var p []Problem
	scan := func(where string, settings map[string]any) {
		l := Leaves(settings)
		for _, k := range SortedKeys(l) {
			if why := Sensitive(k, l[k]); why != "" {
				p = append(p, Problem{Path: ManifestFile, Key: where + Dotted(k), Detail: why + ": move it to a local overlay"})
			}
		}
	}
	scan("common.settings.", m.Common)
	for _, name := range SortedKeys(m.Projects) {
		scan("project."+name+".settings.", m.Projects[name].Settings)
	}
	for _, name := range SortedKeys(m.Hooks) {
		if why := SensitiveText(m.Hooks[name].Command); why != "" {
			p = append(p, Problem{Path: ManifestFile, Key: "hook." + name + ".command", Detail: why})
		}
	}
	return p
}
