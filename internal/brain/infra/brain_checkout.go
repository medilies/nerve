package infra

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// BrainCheckout is a Brain checked out on disk, its manifest in brain.toml.
type BrainCheckout struct {
	root string
}

// NewBrainCheckout is the Brain checked out at root.
func NewBrainCheckout(root string) BrainCheckout { return BrainCheckout{root: root} }

var _ app.Brain = BrainCheckout{}

func (b BrainCheckout) Root() string { return b.root }

// manifestFile is brain.toml as written.
type manifestFile struct {
	Nerve    string                  `toml:"nerve,omitempty"`
	Autosync *bool                   `toml:"autosync,omitempty"`
	Allowed  []string                `toml:"allowed-settings,omitempty"`
	Sources  []sourceTable           `toml:"source,omitempty"`
	Hooks    map[string]hookTable    `toml:"hook,omitempty"`
	Common   *commonTable            `toml:"common,omitempty"`
	Projects map[string]projectTable `toml:"project,omitempty"`
}

type sourceTable struct {
	Repo   string   `toml:"repo"`
	SHA    string   `toml:"sha"`
	Path   string   `toml:"path,omitempty"`
	Skills []string `toml:"skills"`
}

type hookTable struct {
	Event   string `toml:"event"`
	Matcher string `toml:"matcher,omitempty"`
	Command string `toml:"command"`
}

type commonTable struct {
	Settings map[string]any `toml:"settings,omitempty"`
}

type projectTable struct {
	Autosync *bool          `toml:"autosync,omitempty"`
	Skills   []string       `toml:"skills,omitempty"`
	Agents   []string       `toml:"agents,omitempty"`
	Hooks    []string       `toml:"hooks,omitempty"`
	Plugins  []string       `toml:"plugins,omitempty"`
	Settings map[string]any `toml:"settings,omitempty"`
}

func (b BrainCheckout) Load() (domain.Manifest, error) {
	var f manifestFile
	meta, err := toml.DecodeFile(filepath.Join(b.root, domain.ManifestFile), &f)
	if err != nil {
		return domain.Manifest{}, fmt.Errorf("%s: %w", domain.ManifestFile, err)
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
		return domain.Manifest{}, fmt.Errorf("%s: unknown keys %v", domain.ManifestFile, unknown)
	}
	m := domain.Manifest{Nerve: f.Nerve, Autosync: f.Autosync, AllowedSettings: f.Allowed}
	for _, s := range f.Sources {
		m.Sources = append(m.Sources, domain.Source(s))
	}
	if len(f.Hooks) > 0 {
		m.Hooks = map[string]domain.Hook{}
		for name, h := range f.Hooks {
			m.Hooks[name] = domain.Hook(h)
		}
	}
	if f.Common != nil {
		if m.Common, err = normalize("common.settings", f.Common.Settings); err != nil {
			return domain.Manifest{}, err
		}
	}
	m.Projects = map[string]*domain.Project{}
	for name, p := range f.Projects {
		settings, err := normalize("project."+name+".settings", p.Settings)
		if err != nil {
			return domain.Manifest{}, err
		}
		m.Projects[name] = &domain.Project{
			Autosync: p.Autosync,
			Skills:   p.Skills, Agents: p.Agents, Hooks: p.Hooks, Plugins: p.Plugins,
			Settings: settings,
		}
	}
	return m, m.Validate()
}

// Save writes brain.toml in nerve's own layout, refusing what Load would.
// Comments are not kept.
func (b BrainCheckout) Save(m domain.Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	f := manifestFile{Nerve: m.Nerve, Autosync: m.Autosync, Allowed: m.AllowedSettings}
	for _, s := range m.Sources {
		f.Sources = append(f.Sources, sourceTable(s))
	}
	if len(m.Hooks) > 0 {
		f.Hooks = map[string]hookTable{}
		for name, h := range m.Hooks {
			f.Hooks[name] = hookTable(h)
		}
	}
	at := unfitAt("common.settings", m.Common)
	for _, name := range domain.SortedKeys(m.Projects) {
		if at == "" {
			at = unfitAt("project."+name+".settings", m.Projects[name].Settings)
		}
	}
	if at != "" {
		return fmt.Errorf("%s: %s: settings cannot hold null, or a whole number beyond 64 bits", domain.ManifestFile, at)
	}
	if len(m.Common) > 0 {
		f.Common = &commonTable{Settings: forTOML(m.Common).(map[string]any)}
	}
	if len(m.Projects) > 0 {
		f.Projects = map[string]projectTable{}
		for name, p := range m.Projects {
			settings, _ := forTOML(p.Settings).(map[string]any)
			f.Projects[name] = projectTable{
				Autosync: p.Autosync,
				Skills:   p.Skills, Agents: p.Agents, Hooks: p.Hooks, Plugins: p.Plugins,
				Settings: settings,
			}
		}
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("%s: %w", domain.ManifestFile, err)
	}
	return writeFile(filepath.Join(b.root, domain.ManifestFile), dropBareHeaders(buf.Bytes()))
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
// from both compare equal. It refuses what JSON cannot hold: inf, nan and
// dates.
func normalize(where string, v map[string]any) (map[string]any, error) {
	if len(v) == 0 {
		return nil, nil
	}
	if key := unfit(where, v); key != "" {
		return nil, fmt.Errorf("%s: %s: settings cannot hold inf, nan or dates", domain.ManifestFile, key)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", domain.ManifestFile, where, err)
	}
	var out map[string]any
	if err := decodeJSON(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %s: %w", domain.ManifestFile, where, err)
	}
	domain.Numbers(out)
	return out, nil
}

// unfit names the first value under key that JSON cannot hold, or "".
func unfit(key string, v any) string {
	switch t := v.(type) {
	case float64:
		if math.IsInf(t, 0) || math.IsNaN(t) {
			return key
		}
	case time.Time:
		return key
	case []any:
		for _, x := range t {
			if k := unfit(key, x); k != "" {
				return k
			}
		}
	case []map[string]any:
		for _, x := range t {
			if k := unfit(key, x); k != "" {
				return k
			}
		}
	case map[string]any:
		for _, k := range domain.SortedKeys(t) {
			if k := unfit(key+"."+k, t[k]); k != "" {
				return k
			}
		}
	}
	return ""
}

// unfitAt names the first setting under key that TOML cannot hold: null,
// or a whole number beyond int64. "" when there is none.
func unfitAt(key string, settings map[string]any) string {
	for _, k := range domain.SortedKeys(settings) {
		v := settings[k]
		if sub, ok := v.(map[string]any); ok {
			if at := unfitAt(key+"."+k, sub); at != "" {
				return at
			}
		} else if domain.HasNull(v) || holdsNumber(v) {
			return key + "." + k
		}
	}
	return ""
}

// holdsNumber says whether a value is or holds a json.Number: a whole
// number beyond int64, which TOML cannot hold.
func holdsNumber(v any) bool {
	switch t := v.(type) {
	case json.Number:
		return true
	case []any:
		return slices.ContainsFunc(t, holdsNumber)
	case map[string]any:
		for _, x := range t {
			if holdsNumber(x) {
				return true
			}
		}
	}
	return false
}

// forTOML turns decoded JSON into values TOML writes as people would: whole
// numbers as integers.
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

var (
	sourceHeader = regexp.MustCompile(`^\s*\[\[\s*source\s*\]\]\s*(#.*)?$`)
	anyHeader    = regexp.MustCompile(`^\s*\[`)
)

// MovePin replaces old on the sha line of repo's [[source]] table, and
// leaves every other line, such as a comment naming it, as it is.
func (b BrainCheckout) MovePin(repo, old, new string) error {
	path := filepath.Join(b.root, domain.ManifestFile)
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	value := func(v string) string { q := regexp.QuoteMeta(v); return `("` + q + `"|'` + q + `')` }
	shaLine := regexp.MustCompile(`^\s*sha\s*=\s*` + value(old) + `\s*(#.*)?$`)
	repoLine := regexp.MustCompile(`^\s*repo\s*=\s*` + value(repo) + `\s*(#.*)?$`)
	lines := strings.SplitAfter(string(content), "\n")
	pin, inSource := -1, false
	sha, isRepo := -1, false // in the current [[source]] table
	end := func() {
		if inSource && isRepo && sha >= 0 {
			pin = sha
		}
		sha, isRepo = -1, false
	}
	for i, line := range lines {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case anyHeader.MatchString(line):
			end()
			inSource = sourceHeader.MatchString(line)
		case inSource && shaLine.MatchString(line):
			sha = i
		case inSource && repoLine.MatchString(line):
			isRepo = true
		}
	}
	end()
	if pin < 0 {
		return fmt.Errorf("%s: no [[source]] %s is pinned at %s", domain.ManifestFile, repo, old)
	}
	lines[pin] = strings.Replace(lines[pin], old, new, 1)
	return writeFile(path, []byte(strings.Join(lines, "")))
}

// pullTimeout bounds the fetch of a pull, so autosync reports a network
// that does not answer before Claude Code stops waiting for it.
var pullTimeout = 30 * time.Second

// Pull fast-forwards the checkout from its upstream. It skips a checkout
// that is not a git repo, has a detached HEAD, no upstream, or uncommitted
// changes to tracked files, or whose remote it cannot reach. It fails when
// the checkout has diverged. Untracked files never block a fast-forward;
// git refuses on its own if one would be overwritten.
func (b BrainCheckout) Pull(r app.Reporter) error {
	if _, err := os.Stat(filepath.Join(b.root, ".git")); err != nil {
		return nil
	}
	skip := func(why string) error {
		r.Add(app.Event{Kind: app.EvPullSkipped, Path: b.root, Detail: why})
		return nil
	}
	branch, err := git(b.root, "symbolic-ref", "-q", "--short", "HEAD")
	switch {
	case err == nil:
	case exitCode(err) == 1: // 1: HEAD is not a branch
		return skip("detached HEAD")
	default:
		return skip("cannot read its git state: " + firstLine(err))
	}
	// The branch's own config, not its tracking ref, which only a fetch
	// may make.
	switch _, err := git(b.root, "config", "branch."+trim(branch)+".merge"); {
	case exitCode(err) == 1: // 1: no such key
		return skip("no upstream branch")
	case err != nil:
		return skip("cannot read its git state: " + firstLine(err))
	}
	out, err := git(b.root, "status", "--porcelain", "--untracked-files=no")
	switch {
	case err != nil:
		return skip("cannot read its git state: " + firstLine(err))
	case trim(out) != "":
		return skip("uncommitted changes")
	}
	if _, err := gitWithin(pullTimeout, b.root, "fetch", "-q"); err != nil {
		return skip("could not fetch: " + firstLine(err))
	}
	if _, err := git(b.root, "rev-parse", "-q", "--verify", "@{upstream}"); err != nil {
		return skip("its upstream branch is not on the remote")
	}
	switch _, err := git(b.root, "merge-base", "--is-ancestor", "HEAD", "@{upstream}"); {
	case exitCode(err) == 1: // 1: HEAD is not behind the upstream
		if _, err := git(b.root, "merge-base", "--is-ancestor", "@{upstream}", "HEAD"); err == nil {
			return nil // ahead or level: nothing to pull
		}
		return fmt.Errorf("%s has diverged from its upstream: rebase it on its upstream, then sync again", b.root)
	case err != nil:
		return skip("cannot read its git state: " + firstLine(err))
	}
	before := b.Head()
	if _, err := git(b.root, "merge", "-q", "--ff-only", "@{upstream}"); err != nil {
		return fmt.Errorf("pull %s: %s", b.root, firstLine(err))
	}
	if after := b.Head(); after != before {
		r.Add(app.Event{Kind: app.EvPulled, Path: b.root, Target: after})
	}
	return nil
}

func (b BrainCheckout) Head() string {
	out, err := git(b.root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return trim(out)
}

// State says what is not yet committed or pushed in the Brain, new files
// included: a new skill is one.
func (b BrainCheckout) State() string {
	if _, err := os.Stat(filepath.Join(b.root, ".git")); err != nil {
		return ""
	}
	out, err := git(b.root, "status", "--porcelain")
	if err != nil {
		return "cannot read its git state: " + firstLine(err)
	}
	var parts []string
	if trim(out) != "" {
		parts = append(parts, "uncommitted changes")
	}
	// Against the upstream, else, on a branch with none, against every
	// remote branch. A Brain with no remote has nowhere to push.
	notPushed := []string{"@{upstream}..HEAD"}
	if _, err := git(b.root, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		notPushed = nil
		if remotes, err := git(b.root, "remote"); err == nil && trim(remotes) != "" {
			notPushed = []string{"HEAD", "--not", "--remotes"}
		}
	}
	if notPushed != nil {
		out, err := git(b.root, append([]string{"rev-list", "--count"}, notPushed...)...)
		switch n := trim(out); {
		case err != nil:
			parts = append(parts, "cannot count the commits not pushed: "+firstLine(err))
		case n == "1":
			parts = append(parts, "1 commit not pushed")
		case n != "0":
			parts = append(parts, n+" commits not pushed")
		}
	}
	return strings.Join(parts, ", ")
}

func (b BrainCheckout) OwnSkill(name string) (domain.Skill, bool, error) {
	dir := filepath.Join(b.root, "skills", name)
	if !fileExists(filepath.Join(dir, "SKILL.md")) {
		return domain.Skill{}, false, nil
	}
	s, err := readSkill(name, dir, "own")
	return s, true, err
}

// OwnSkills reads every folder in skills/, linked ones too, each with the
// error reading it.
func (b BrainCheckout) OwnSkills() ([]app.OwnSkill, error) {
	dir := filepath.Join(b.root, "skills")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var skills []app.OwnSkill
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			continue
		}
		s, err := readSkill(e.Name(), path, "own")
		s.Name, s.Dir = e.Name(), path
		skills = append(skills, app.OwnSkill{Skill: s, Err: err})
	}
	return skills, nil
}

func (b BrainCheckout) AgentFile(name string) (string, bool) {
	path := filepath.Join(b.root, "agents", name+".md")
	return path, fileExists(path)
}

func (b BrainCheckout) Agents() ([]app.Agent, error) {
	entries, err := os.ReadDir(filepath.Join(b.root, "agents"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var agents []app.Agent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		f := filepath.Join(b.root, "agents", e.Name())
		rel, _ := filepath.Rel(b.root, f)
		ag := app.Agent{Name: strings.TrimSuffix(filepath.Base(f), ".md"), Rel: rel}
		ag.Meta, ag.Err = readFrontmatter(f)
		agents = append(agents, ag)
	}
	return agents, nil
}

// readSkill reads a skill folder's frontmatter.
func readSkill(name, dir, origin string) (domain.Skill, error) {
	meta, err := readFrontmatter(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return domain.Skill{}, fmt.Errorf("skill %s: %w", name, err)
	}
	return domain.Skill{Name: name, Dir: dir, Origin: origin, Meta: meta}, nil
}

func readFrontmatter(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return domain.ParseFrontmatter(filepath.Base(path), string(b))
}
