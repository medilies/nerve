// Package domain is the Brain and the rules for carrying it to projects:
// what a project gets, how settings are laid and reconciled, and which
// settings a Brain may hold. It knows no file format, git or filesystem.
package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// ManifestFile is the Brain's manifest, as users know it.
const ManifestFile = "brain.json"

var (
	RepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	SHAPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	NamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	pathPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)
)

// Manifest is a Brain's brain.json: the settings it allows, the
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
// allowedSettings, as they shape how a project works, each with what it
// holds as Claude Code's settings reference gives it: why a value does not
// fit, or "".
var nerveAllows = map[string]func(v any) string{
	"alwaysThinkingEnabled":      boolean,
	"attribution":                attribution,
	"cleanupPeriodDays":          days,
	"disableBundledSkills":       boolean,
	"disableWorkflows":           boolean,
	"disabledMcpjsonServers":     listOfText,
	"effortLevel":                effort,
	"enableAllProjectMcpServers": boolean,
	"enableArtifact":             boolean,
	"enabledMcpjsonServers":      listOfText,
	"modelSettings":              modelSettings,
	"model":                      text,
	"outputStyle":                text,
	"permissions.allow":          listOfText,
	"permissions.ask":            listOfText,
	"permissions.defaultMode":    mode,
	"permissions.deny":           listOfText,
	"spinnerTipsEnabled":         boolean,
	"syncClaudeAiSkills":         boolean,
}

// ListOfText is why a value does not fit a setting that holds a list of
// text.
const ListOfText = `must be a list of text, such as ["Read"]`

func boolean(v any) string {
	if _, ok := v.(bool); !ok {
		return "must be true or false"
	}
	return ""
}

func text(v any) string {
	if _, ok := v.(string); !ok {
		return "must be text"
	}
	return ""
}

func listOfText(v any) string {
	list, ok := v.([]any)
	if !ok || slices.ContainsFunc(list, func(x any) bool { return text(x) != "" }) {
		return ListOfText
	}
	return ""
}

// days fits a whole number of at least 1, however it is written.
func days(v any) string {
	switch t := v.(type) {
	case int64:
		if t >= 1 {
			return ""
		}
	case float64:
		if t >= 1 && t == math.Trunc(t) {
			return ""
		}
	case json.Number: // a whole number beyond int64
		if !strings.HasPrefix(string(t), "-") {
			return ""
		}
	}
	return "must be a whole number of days, at least 1"
}

// mode fits the permission modes a project's settings can set: Claude Code
// takes auto and bypassPermissions only from the user's own settings.
func mode(v any) string {
	switch v {
	case "default", "acceptEdits", "plan", "dontAsk", "manual":
		return ""
	case "auto", "bypassPermissions":
		return fmt.Sprintf("%q does not take effect from a project's settings: set it in ~/.claude/settings.json", v)
	}
	return `must be one of "default", "acceptEdits", "plan", "dontAsk" or "manual"`
}

// effort fits the effort levels Claude Code takes in settings.
func effort(v any) string {
	switch v {
	case "low", "medium", "high", "xhigh":
		return ""
	}
	return `must be one of "low", "medium", "high" or "xhigh"`
}

// modelSettings fits an object per model, checking only its effortLevel.
func modelSettings(v any) string {
	const why = `must hold, for each model, an object whose effortLevel is one of "low", "medium", "high" or "xhigh"`
	models, ok := v.(map[string]any)
	if !ok {
		return why
	}
	for _, m := range models {
		obj, ok := m.(map[string]any)
		if !ok {
			return why
		}
		if level, ok := obj["effortLevel"]; ok && effort(level) != "" {
			return why
		}
	}
	return ""
}

func attribution(v any) string {
	const why = "must be false, or hold commit and pr as text and sessionUrl as true or false"
	if v == false {
		return ""
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return why
	}
	for k, x := range obj {
		fit := map[string]func(any) string{"commit": text, "pr": text, "sessionUrl": boolean}[k]
		if fit == nil || fit(x) != "" {
			return why
		}
	}
	return ""
}

// Misfits names each setting of nerveAllows that s holds as Claude Code
// would refuse it, and why.
func Misfits(s map[string]any) []Problem {
	whole := map[string]bool{}
	for k := range nerveAllows {
		path, _ := ParseKey(k)
		whole[LeafKey(path)] = true
	}
	l := leavesUpTo(s, whole)
	var p []Problem
	for _, k := range SortedKeys(nerveAllows) {
		path, _ := ParseKey(k)
		v, ok := l[LeafKey(path)]
		if !ok {
			continue
		}
		if why := nerveAllows[k](v); why != "" {
			p = append(p, Problem{Key: k, Detail: why})
		}
	}
	return p
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
			return fmt.Errorf("%s: allowedSettings: %q is not a settings key", ManifestFile, a)
		case slices.Contains(nerveWrites, path[0]):
			return fmt.Errorf("%s: allowedSettings: %q is written by nerve from hooks and plugins", ManifestFile, a)
		}
	}
	for i, s := range m.Sources {
		if slices.ContainsFunc(m.Sources[:i], func(o Source) bool { return strings.EqualFold(o.Repo, s.Repo) }) {
			return fmt.Errorf("%s: %s is in sources twice: list all its skills in one entry", ManifestFile, s.Repo)
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
	if at := nullAt("common.settings.", m.Common); at != "" {
		return fmt.Errorf("%s: %s: settings cannot hold null: nerve setting rm removes a setting", ManifestFile, at)
	}
	for _, name := range SortedKeys(m.Projects) {
		p := m.Projects[name]
		if !NamePattern.MatchString(name) {
			return fmt.Errorf("%s: project %q must be lowercase words joined by hyphens", ManifestFile, name)
		}
		if at := nullAt("projects."+name+".settings.", p.Settings); at != "" {
			return fmt.Errorf("%s: %s: settings cannot hold null: nerve setting rm removes a setting", ManifestFile, at)
		}
		for _, kind := range ItemKinds {
			list := *p.List(kind)
			for i, item := range list {
				// Skills, agents and hooks name files and folders: a name
				// must not lead out of them.
				if kind != "plugin" && !NamePattern.MatchString(item) {
					return fmt.Errorf("%s: project %s: %s %q must be lowercase words joined by hyphens", ManifestFile, name, kind, item)
				}
				if slices.Contains(list[:i], item) {
					return fmt.Errorf("%s: project %s: %s %q is listed twice: list it once", ManifestFile, name, kind, item)
				}
			}
		}
	}
	return nil
}

// nullAt names the first setting in s that is or holds null, after where,
// or "".
func nullAt(where string, s map[string]any) string {
	l := Leaves(s)
	for _, k := range SortedKeys(l) {
		if HasNull(l[k]) {
			return where + Dotted(k)
		}
	}
	return ""
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

// NerveAllows says whether nerve allows the settings key at path on its
// own: it is one of nerve's, or under one.
func NerveAllows(path []string) bool { return covers(SortedKeys(nerveAllows), LeafKey(path)) }

// allows says whether a leaf key is an allowed setting, or under one.
func (m Manifest) allows(key string) bool {
	return covers(SortedKeys(nerveAllows), key) || covers(m.AllowedSettings, key)
}

// covers says whether a leaf key is one of keys, as users write them, or
// under one.
func covers(keys []string, key string) bool {
	for _, a := range keys {
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

// Refusals lists the settings in the manifest no list allows, and the
// misfits among those nerve allows.
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
			add(where+Dotted(k), why+": run nerve allow add "+ShellWord(Dotted(k))+", or "+fix)
		}
		for _, f := range Misfits(s) {
			add(where+f.Key, f.Detail)
		}
	}
	settings("common.settings.", "run nerve setting rm --common on it, then nerve setting set per project, which keeps it in the local overlay", m.Common)
	for _, name := range SortedKeys(m.Projects) {
		pr := m.Projects[name]
		settings("projects."+name+".settings.", "run nerve setting rm --project "+name+" on it, then nerve setting set --project "+name+", which keeps it in the local overlay", pr.Settings)
	}
	return p
}

// ShareNerves says how to share a setting nerve writes from a Brain's
// plugins and hooks.
const ShareNerves = "share a plugin with nerve plugin add, a hook in hooks"

// WrittenByNerve says whether a leaf key is one nerve writes from a Brain's
// hooks and plugins, which no allowedSettings can let the Brain set.
func WrittenByNerve(key string) bool {
	first, _, _ := strings.Cut(key, Sep)
	return slices.Contains(nerveWrites, first)
}
