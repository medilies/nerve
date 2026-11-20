package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const manifestFile = "brain.toml"

// Manifest is a Brain's brain.toml: the nerve release that carries it, the
// third-party sources it pins, the hooks it defines, the settings common to
// its projects, and the projects themselves.
type Manifest struct {
	Nerve          string              `toml:"nerve,omitempty"`
	NerveChecksums string              `toml:"nerve-checksums,omitempty"`
	Autosync       *bool               `toml:"autosync,omitempty"`
	Sources        []Source            `toml:"source,omitempty"`
	Hooks          map[string]Hook     `toml:"hook,omitempty"`
	Common         *Common             `toml:"common,omitempty"`
	Projects       map[string]*Project `toml:"project,omitempty"`
}

// Source is one third-party repo, pinned to a commit.
type Source struct {
	Repo   string   `toml:"repo"`
	SHA    string   `toml:"sha"`
	Skills []string `toml:"skills"`
}

// Hook is a Claude Code hook a project can take by name.
type Hook struct {
	Event   string `toml:"event"`
	Matcher string `toml:"matcher,omitempty"`
	Command string `toml:"command"`
}

// Common holds the settings every listed project gets.
type Common struct {
	Settings map[string]any `toml:"settings,omitempty"`
}

// Project is one project's entry: what reaches it, and nothing else does.
type Project struct {
	Surface  string         `toml:"surface,omitempty"` // "" for a Claude Code project, or claudeAI
	Autosync *bool          `toml:"autosync,omitempty"`
	Skills   []string       `toml:"skills,omitempty"`
	Agents   []string       `toml:"agents,omitempty"`
	Hooks    []string       `toml:"hooks,omitempty"`
	Plugins  []string       `toml:"plugins,omitempty"`
	Settings map[string]any `toml:"settings,omitempty"`
}

// claudeAI is the surface of a project that stands for a claude.ai account.
const claudeAI = "claude.ai"

var (
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sumPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

func loadManifest(root string) (Manifest, error) {
	var m Manifest
	path := filepath.Join(root, manifestFile)
	meta, err := toml.DecodeFile(path, &m)
	if err != nil {
		return m, fmt.Errorf("%s: %w", manifestFile, err)
	}
	var unknown []toml.Key
	for _, k := range meta.Undecoded() {
		// Settings are free-form, but tables inside them count as undecoded.
		inSettings := len(k) > 2 && k[0] == "common" && k[1] == "settings" ||
			len(k) > 3 && k[0] == "project" && k[2] == "settings"
		if !inSettings {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return m, fmt.Errorf("%s: unknown keys %v", manifestFile, unknown)
	}
	if m.Common == nil {
		m.Common = &Common{}
	}
	m.Common.Settings = normalize(m.Common.Settings)
	for _, p := range m.Projects {
		p.Settings = normalize(p.Settings)
	}
	return m, m.validate()
}

func (m Manifest) validate() error {
	if m.Nerve != "" && !tagPattern.MatchString(m.Nerve) {
		return fmt.Errorf("%s: nerve %q must be a release tag such as v1.2.3", manifestFile, m.Nerve)
	}
	if m.NerveChecksums != "" && (m.Nerve == "" || !sumPattern.MatchString(m.NerveChecksums)) {
		return fmt.Errorf("%s: nerve-checksums must be the lowercase SHA-256 of the pinned nerve release's checksums.txt", manifestFile)
	}
	for _, s := range m.Sources {
		if !repoPattern.MatchString(s.Repo) {
			return fmt.Errorf("%s: repo %q is not owner/name", manifestFile, s.Repo)
		}
		if !shaPattern.MatchString(s.SHA) {
			return fmt.Errorf("%s: %s: sha must be a full 40-character lowercase commit", manifestFile, s.Repo)
		}
		if len(s.Skills) == 0 {
			return fmt.Errorf("%s: %s: no skills", manifestFile, s.Repo)
		}
	}
	for name, h := range m.Hooks {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("%s: hook %q must be lowercase words joined by hyphens", manifestFile, name)
		}
		if h.Event == "" || h.Command == "" {
			return fmt.Errorf("%s: hook %s needs an event and a command", manifestFile, name)
		}
	}
	for name, p := range m.Projects {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("%s: project %q must be lowercase words joined by hyphens", manifestFile, name)
		}
		if p.Surface != "" && p.Surface != claudeAI {
			return fmt.Errorf("%s: project %s: surface must be %q or left out", manifestFile, name, claudeAI)
		}
	}
	return nil
}

// autosync says whether a project syncs at each session start: its own
// choice, else the Brain's, else on.
func (m Manifest) autosync(p *Project) bool {
	switch {
	case p != nil && p.Autosync != nil:
		return *p.Autosync
	case m.Autosync != nil:
		return *m.Autosync
	}
	return true
}

// project returns the named project, or an error that says how to add it.
func (m Manifest) project(name string) (*Project, error) {
	p := m.Projects[name]
	if p == nil {
		return nil, fmt.Errorf("%s lists no project %q", manifestFile, name)
	}
	return p, nil
}

// saveManifest writes brain.toml in nerve's own layout. Comments are not
// kept.
func saveManifest(root string, m Manifest) error {
	out := m
	out.Common = nil
	if m.Common != nil && len(m.Common.Settings) > 0 {
		out.Common = &Common{Settings: forTOML(m.Common.Settings).(map[string]any)}
	}
	out.Projects = map[string]*Project{}
	for name, p := range m.Projects {
		q := *p
		q.Settings, _ = forTOML(p.Settings).(map[string]any)
		out.Projects[name] = &q
	}
	if len(out.Projects) == 0 {
		out.Projects = nil
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("%s: %w", manifestFile, err)
	}
	return writeFile(filepath.Join(root, manifestFile), dropBareHeaders(buf.Bytes()))
}

// dropBareHeaders removes a table header that only opens subtables, such as
// [project] before [project.x], which the encoder writes and nobody would.
func dropBareHeaders(b []byte) []byte {
	lines := strings.Split(string(b), "\n")
	var out []string
	for i, line := range lines {
		if strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[[") && i+1 < len(lines) &&
			strings.HasPrefix(lines[i+1], strings.TrimSuffix(line, "]")+".") {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

// normalize turns decoded TOML into what decoded JSON looks like, so values
// from both compare equal.
func normalize(v map[string]any) map[string]any {
	if len(v) == 0 {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out map[string]any
	json.Unmarshal(b, &out)
	return out
}

// forTOML turns decoded JSON into values TOML writes as the user would:
// whole numbers as integers.
func forTOML(v any) any {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<53 {
			return int64(t)
		}
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = forTOML(x)
		}
		return out
	case map[string]any:
		if t == nil {
			return nil
		}
		out := map[string]any{}
		for k, x := range t {
			out[k] = forTOML(x)
		}
		return out
	}
	return v
}

// expandHome expands a leading ~ in path.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// shortenHome writes a path under the home folder as ~/…, so the machine
// file reads the same for anyone.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		if rel == "." {
			return "~"
		}
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }
