// Package domain is the Brain and the rules for carrying it to projects:
// what a project gets, how settings are laid and reconciled, and which
// settings a Brain may hold. It knows no file format, git or filesystem.
package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ManifestFile is the Brain's manifest, as users know it.
const ManifestFile = "brain.toml"

var (
	RepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	SHAPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	NamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	pathPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)
)

// Manifest is a Brain's brain.toml: the settings it allows, the
// third-party sources it pins, the hooks it defines, the settings common to
// its projects, and the projects themselves.
type Manifest struct {
	AllowedSettings []string // settings keys, as users write them, the Brain may hold beyond nerve's own
	Sources         []Source
	Hooks           map[string]Hook
	Common          map[string]any // settings every listed project gets
	Projects        map[string]*Project
}

// nerveAllows are the settings a Brain may hold without listing them in
// allowed-settings: they shape how a project works.
var nerveAllows = []string{
	"alwaysThinkingEnabled", "attribution", "cleanupPeriodDays", "disabledMcpjsonServers", "enableAllProjectMcpServers",
	"enabledMcpjsonServers", "model", "outputStyle", "permissions.allow",
	"permissions.ask", "permissions.defaultMode", "permissions.deny", "spinnerTipsEnabled", "syncClaudeAiSkills",
}

// nerveLists are the settings nerve allows that hold a list of text.
var nerveLists = []string{"disabledMcpjsonServers", "enabledMcpjsonServers", "permissions.allow", "permissions.ask", "permissions.deny"}

// ListOfText is what a setting of nerveLists holds.
const ListOfText = `holds a list of text, such as ["Read"]`

// MisTyped names the settings of nerveLists that s holds as anything but a
// list of text.
func MisTyped(s map[string]any) []string {
	whole := map[string]bool{}
	for _, k := range nerveLists {
		path, _ := ParseKey(k)
		whole[LeafKey(path)] = true
	}
	l := leavesUpTo(s, whole)
	var keys []string
	for _, k := range nerveLists {
		path, _ := ParseKey(k)
		v, ok := l[LeafKey(path)]
		if !ok {
			continue
		}
		list, isList := v.([]any)
		if !isList || slices.ContainsFunc(list, func(x any) bool { _, text := x.(string); return !text }) {
			keys = append(keys, k)
		}
	}
	return keys
}

// nerveWrites are the settings nerve writes from a Brain's hooks and
// plugins, which the Brain never sets itself.
var nerveWrites = []string{"enabledPlugins", "hooks"}

// NotAllowed is why a setting no list allows stays out of the Brain.
const NotAllowed = "not an allowed setting"

// Source is one third-party repo, pinned to a commit.
type Source struct {
	Repo   string
	SHA    string
	Path   string // the folder holding its skills, "" for the usual places
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
	for _, a := range m.AllowedSettings {
		path, err := ParseKey(a)
		switch {
		case err != nil:
			return fmt.Errorf("%s: allowed-settings: %q is not a settings key", ManifestFile, a)
		case slices.Contains(nerveWrites, path[0]):
			return fmt.Errorf("%s: allowed-settings: %q is written by nerve from hooks and plugins", ManifestFile, a)
		}
	}
	for i, s := range m.Sources {
		if slices.ContainsFunc(m.Sources[:i], func(o Source) bool { return strings.EqualFold(o.Repo, s.Repo) }) {
			return fmt.Errorf("%s: %s is in two [[source]] tables: list all its skills in one", ManifestFile, s.Repo)
		}
		if !RepoPattern.MatchString(s.Repo) || slices.ContainsFunc(strings.Split(s.Repo, "/"), func(p string) bool { return p == "." || p == ".." }) {
			return fmt.Errorf("%s: repo %q is not owner/name", ManifestFile, s.Repo)
		}
		if !SHAPattern.MatchString(s.SHA) {
			return fmt.Errorf("%s: %s: sha must be a full 40-character lowercase commit", ManifestFile, s.Repo)
		}
		if s.Path != "" && (!pathPattern.MatchString(s.Path) || slices.ContainsFunc(strings.Split(s.Path, "/"), func(p string) bool { return p == "." || p == ".." })) {
			return fmt.Errorf("%s: %s: path %q must be a folder in the repo, such as skills/eng", ManifestFile, s.Repo, s.Path)
		}
		if len(s.Skills) == 0 {
			return fmt.Errorf("%s: %s: no skills", ManifestFile, s.Repo)
		}
		for _, name := range s.Skills {
			if !NamePattern.MatchString(name) {
				return fmt.Errorf("%s: %s: skill %q must be lowercase words joined by hyphens", ManifestFile, s.Repo, name)
			}
		}
	}
	for _, name := range SortedKeys(m.Hooks) {
		h := m.Hooks[name]
		if !NamePattern.MatchString(name) {
			return fmt.Errorf("%s: hook %q must be lowercase words joined by hyphens", ManifestFile, name)
		}
		if h.Event == "" || h.Command == "" {
			return fmt.Errorf("%s: hook %s needs an event and a command", ManifestFile, name)
		}
	}
	for _, name := range SortedKeys(m.Projects) {
		p := m.Projects[name]
		if !NamePattern.MatchString(name) {
			return fmt.Errorf("%s: project %q must be lowercase words joined by hyphens", ManifestFile, name)
		}
		// Skills, agents and hooks name files and folders: a name must not
		// lead out of them.
		for _, kind := range []string{"skill", "agent", "hook"} {
			for _, item := range *p.List(kind) {
				if !NamePattern.MatchString(item) {
					return fmt.Errorf("%s: project %s: %s %q must be lowercase words joined by hyphens", ManifestFile, name, kind, item)
				}
			}
		}
	}
	return nil
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
	for _, x := range []struct {
		n    int
		what string
	}{{len(p.Skills), "skills"}, {len(p.Agents), "agents"}, {len(p.Hooks), "hooks"}, {len(p.Plugins), "plugins"}, {len(settingLeaves(p.Settings, nil)), "settings"}} {
		switch {
		case x.n == 1:
			parts = append(parts, "1 "+strings.TrimSuffix(x.what, "s"))
		case x.n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", x.n, x.what))
		}
	}
	if len(parts) == 0 {
		return "lists nothing yet"
	}
	return strings.Join(parts, ", ")
}

// Problem is something wrong with the Brain, at a path and possibly a key.
type Problem struct {
	Path   string
	Key    string
	Detail string
}

// allows says whether a leaf key is an allowed setting, or under one.
func (m Manifest) allows(key string) bool {
	for _, a := range slices.Concat(nerveAllows, m.AllowedSettings) {
		path, err := ParseKey(a)
		if err != nil {
			continue
		}
		if k := LeafKey(path); key == k || strings.HasPrefix(key, k+Sep) {
			return true
		}
	}
	return false
}

// SettingRefusal says why a setting stays out of the Brain, or "": it is
// not an allowed setting. key is a leaf path; an object value is judged by
// each of its leaves.
func (m Manifest) SettingRefusal(key string, v any) string {
	obj, isObj := v.(map[string]any)
	if isObj && len(obj) > 0 {
		l := Leaves(obj)
		for _, k := range SortedKeys(l) {
			if why := m.SettingRefusal(key+Sep+k, l[k]); why != "" {
				return why
			}
		}
		return ""
	}
	if !isObj && !m.allows(key) { // an empty object holds no setting
		return NotAllowed
	}
	return ""
}

// Refusals lists the settings in the manifest no list allows, and those of
// nerveLists that are not a list of text.
func (m Manifest) Refusals() []Problem {
	var p []Problem
	add := func(key, why string) {
		p = append(p, Problem{Path: ManifestFile, Key: key, Detail: why})
	}
	settings := func(where, fix string, s map[string]any) {
		l := Leaves(s)
		for _, k := range SortedKeys(l) {
			why := m.SettingRefusal(k, l[k])
			if why == "" {
				continue
			}
			if WrittenByNerve(k) {
				add(where+Dotted(k), why+": "+ShareNerves)
				continue
			}
			add(where+Dotted(k), why+": list it in allowed-settings, or "+fix)
		}
		for _, k := range MisTyped(s) {
			add(where+k, ListOfText+": write it as one")
		}
	}
	settings("common.settings.", "run nerve setting rm --common on it, then nerve setting set per project, which keeps it in the local overlay", m.Common)
	for _, name := range SortedKeys(m.Projects) {
		pr := m.Projects[name]
		settings("project."+name+".settings.", "run nerve setting rm --project "+name+" on it, then nerve setting set --project "+name+", which keeps it in the local overlay", pr.Settings)
	}
	return p
}

// ShareNerves says how to share a setting nerve writes from a Brain's
// plugins and hooks.
const ShareNerves = "share a plugin with nerve plugin add, a hook with [hook.<name>]"

// WrittenByNerve says whether a leaf key is one nerve writes from a Brain's
// hooks and plugins, which no allowed-settings can let the Brain set.
func WrittenByNerve(key string) bool {
	first, _, _ := strings.Cut(key, Sep)
	return slices.Contains(nerveWrites, first)
}
