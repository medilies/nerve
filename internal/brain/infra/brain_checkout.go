package infra

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
	Nerve          string                  `toml:"nerve,omitempty"`
	NerveChecksums string                  `toml:"nerve-checksums,omitempty"`
	Autosync       *bool                   `toml:"autosync,omitempty"`
	Sources        []sourceTable           `toml:"source,omitempty"`
	Hooks          map[string]hookTable    `toml:"hook,omitempty"`
	Common         *commonTable            `toml:"common,omitempty"`
	Projects       map[string]projectTable `toml:"project,omitempty"`
}

type sourceTable struct {
	Repo   string   `toml:"repo"`
	SHA    string   `toml:"sha"`
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
	Surface  string         `toml:"surface,omitempty"`
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
	m := domain.Manifest{Nerve: f.Nerve, NerveChecksums: f.NerveChecksums, Autosync: f.Autosync}
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
			Surface: p.Surface, Autosync: p.Autosync,
			Skills: p.Skills, Agents: p.Agents, Hooks: p.Hooks, Plugins: p.Plugins,
			Settings: settings,
		}
	}
	return m, m.Validate()
}

// Save writes brain.toml in nerve's own layout. Comments are not kept.
func (b BrainCheckout) Save(m domain.Manifest) error {
	f := manifestFile{Nerve: m.Nerve, NerveChecksums: m.NerveChecksums, Autosync: m.Autosync}
	for _, s := range m.Sources {
		f.Sources = append(f.Sources, sourceTable(s))
	}
	if len(m.Hooks) > 0 {
		f.Hooks = map[string]hookTable{}
		for name, h := range m.Hooks {
			f.Hooks[name] = hookTable(h)
		}
	}
	at := nullAt("common.settings", m.Common)
	for _, name := range domain.SortedKeys(m.Projects) {
		if at == "" {
			at = nullAt("project."+name+".settings", m.Projects[name].Settings)
		}
	}
	if at != "" {
		return fmt.Errorf("%s: %s: settings cannot hold null", domain.ManifestFile, at)
	}
	if len(m.Common) > 0 {
		f.Common = &commonTable{Settings: forTOML(m.Common).(map[string]any)}
	}
	if len(m.Projects) > 0 {
		f.Projects = map[string]projectTable{}
		for name, p := range m.Projects {
			settings, _ := forTOML(p.Settings).(map[string]any)
			f.Projects[name] = projectTable{
				Surface: p.Surface, Autosync: p.Autosync,
				Skills: p.Skills, Agents: p.Agents, Hooks: p.Hooks, Plugins: p.Plugins,
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

// nullAt names the first setting under key that holds null, which TOML
// cannot, or "".
func nullAt(key string, settings map[string]any) string {
	for _, k := range domain.SortedKeys(settings) {
		v := settings[k]
		if sub, ok := v.(map[string]any); ok {
			if at := nullAt(key+"."+k, sub); at != "" {
				return at
			}
		} else if domain.HasNull(v) {
			return key + "." + k
		}
	}
	return ""
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

func (b BrainCheckout) MovePin(old, new string) error {
	path := filepath.Join(b.root, domain.ManifestFile)
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Count(content, []byte(old)) != 1 {
		return fmt.Errorf("%s: pin %s must appear exactly once", domain.ManifestFile, old)
	}
	return writeFile(path, bytes.Replace(content, []byte(old), []byte(new), 1))
}

// pullTimeout bounds the fetch of a pull, so autosync reports a network
// that does not answer before Claude Code stops waiting for it.
const pullTimeout = 30 * time.Second

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
	if _, err := git(b.root, "symbolic-ref", "-q", "HEAD"); err != nil {
		return skip("detached HEAD")
	}
	if _, err := git(b.root, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		return skip("no upstream branch")
	}
	out, err := git(b.root, "status", "--porcelain", "--untracked-files=no")
	switch {
	case err != nil:
		return skip("cannot read its git state: " + firstLine(err))
	case trim(out) != "":
		return skip("uncommitted changes")
	}
	if _, err := gitWithin(pullTimeout, b.root, "fetch", "-q"); err != nil {
		return skip("could not reach its remote")
	}
	before := b.Head()
	if _, err := git(b.root, "merge", "-q", "--ff-only", "@{upstream}"); err != nil {
		return fmt.Errorf("pull %s: %w", b.root, err)
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

func (b BrainCheckout) State() string {
	if _, err := os.Stat(filepath.Join(b.root, ".git")); err != nil {
		return ""
	}
	out, err := git(b.root, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "cannot read its git state: " + firstLine(err)
	}
	var parts []string
	if trim(out) != "" {
		parts = append(parts, "uncommitted changes")
	}
	if _, err := git(b.root, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil {
		out, err := git(b.root, "rev-list", "--count", "@{upstream}..HEAD")
		switch {
		case err != nil:
			parts = append(parts, "cannot count the commits not pushed: "+firstLine(err))
		case trim(out) != "0":
			parts = append(parts, trim(out)+" commits not pushed")
		}
	}
	return strings.Join(parts, ", ")
}

// ownerPattern is what nerve takes as an owner: a name that makes a plain
// folder prefix.
var ownerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

// Owner is the owner of the origin remote: the folder that holds the
// repo in the remote's path, such as medilies in
// git@github.com:medilies/brain.git.
func (b BrainCheckout) Owner() (string, error) {
	out, err := git(b.root, "remote", "get-url", "origin")
	if err != nil {
		return "", errors.New("the Brain has no origin remote to name the prefix nerve links yours under")
	}
	remote := trim(out)
	owner, ok := ownerOf(remote)
	if !ok {
		return "", fmt.Errorf("the Brain's remote %s names no owner/repo to name the prefix nerve links yours under", remote)
	}
	return owner, nil
}

// ownerOf reads the owner from a remote URL, an scp-like one included.
func ownerOf(remote string) (string, bool) {
	path := remote
	if scheme, _, ok := strings.Cut(remote, "://"); ok && !strings.Contains(scheme, "/") {
		u, err := url.Parse(remote)
		if err != nil {
			return "", false
		}
		path = u.Path
	} else if host, rest, ok := strings.Cut(remote, ":"); ok && !strings.Contains(host, "/") {
		path = rest
	}
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	if len(parts) < 2 {
		return "", false
	}
	owner := strings.ToLower(parts[len(parts)-2])
	return owner, ownerPattern.MatchString(owner) && owner != ".."
}

func (b BrainCheckout) OwnSkill(name string) (domain.Skill, bool, error) {
	dir := filepath.Join(b.root, "skills", name)
	if !fileExists(filepath.Join(dir, "SKILL.md")) {
		return domain.Skill{}, false, nil
	}
	s, err := readSkill(name, dir, "own")
	return s, true, err
}

func (b BrainCheckout) OwnSkills() ([]domain.Skill, error) {
	dir := filepath.Join(b.root, "skills")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var skills []domain.Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := readSkill(e.Name(), filepath.Join(dir, e.Name()), "own")
		if err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	return skills, nil
}

func (b BrainCheckout) AgentFile(name string) (string, bool) {
	path := filepath.Join(b.root, "agents", name+".md")
	return path, fileExists(path)
}

func (b BrainCheckout) Agents() ([]app.Agent, error) {
	files, err := filepath.Glob(filepath.Join(b.root, "agents", "*.md"))
	if err != nil {
		return nil, err
	}
	var agents []app.Agent
	for _, f := range files {
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
