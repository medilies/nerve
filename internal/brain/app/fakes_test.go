package app_test

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// copyValue deep-copies decoded JSON, so a fake never shares what a use case
// may change without saving.
func copyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		if t == nil {
			return map[string]any(nil)
		}
		out := map[string]any{}
		for k, x := range t {
			out[k] = copyValue(x)
		}
		return out
	case []any:
		return slices.Clone(t)
	}
	return v
}

func copySettings(s map[string]any) map[string]any {
	c, _ := copyValue(s).(map[string]any)
	return c
}

func copyManifest(m domain.Manifest) domain.Manifest {
	c := m
	c.Sources = slices.Clone(m.Sources)
	c.Common = copySettings(m.Common)
	c.Projects = map[string]*domain.Project{}
	for name, p := range m.Projects {
		q := *p
		q.Skills, q.Agents, q.Hooks, q.Plugins = slices.Clone(p.Skills), slices.Clone(p.Agents), slices.Clone(p.Hooks), slices.Clone(p.Plugins)
		q.Settings = copySettings(p.Settings)
		c.Projects[name] = &q
	}
	return c
}

type fakeBrain struct {
	m         domain.Manifest
	saves     int
	failSaves int // how many saves fail from now on, as a read-only Brain does
	pullErr   error
	own       map[string]domain.Skill
	ownErr    map[string]error // own skills that cannot be read, by name
	agents    map[string]bool
	head      string
	pulls     int
	moved     [][2]string
}

func (b *fakeBrain) Root() string                   { return "/brain" }
func (b *fakeBrain) Load() (domain.Manifest, error) { return copyManifest(b.m), b.m.Validate() }
func (b *fakeBrain) Pull(app.Reporter) error        { b.pulls++; return b.pullErr }

// Save refuses what Load would, as the real Brain does.
func (b *fakeBrain) Save(m domain.Manifest) error {
	if b.failSaves > 0 {
		b.failSaves--
		return errors.New("brain.toml: read-only")
	}
	if err := m.Validate(); err != nil {
		return err
	}
	b.m = copyManifest(m)
	b.saves++
	return nil
}
func (b *fakeBrain) Head() string  { return b.head }
func (b *fakeBrain) State() string { return "" }
func (b *fakeBrain) AgentFile(name string) (string, bool) {
	return "/brain/agents/" + name + ".md", b.agents[name]
}

func (b *fakeBrain) MovePin(repo, old, new string) error {
	b.moved = append(b.moved, [2]string{old, new})
	for i := range b.m.Sources {
		if b.m.Sources[i].Repo == repo && b.m.Sources[i].SHA == old {
			b.m.Sources[i].SHA = new
			return nil
		}
	}
	return fmt.Errorf("no [[source]] %s is pinned at %s", repo, old)
}

func (b *fakeBrain) OwnSkill(name string) (domain.Skill, bool, error) {
	if err := b.ownErr[name]; err != nil {
		return domain.Skill{Name: name, Dir: "/brain/skills/" + name}, true, err
	}
	s, ok := b.own[name]
	return s, ok, nil
}

func (b *fakeBrain) OwnSkills() ([]app.OwnSkill, error) {
	names := slices.Concat(domain.SortedKeys(b.own), domain.SortedKeys(b.ownErr))
	slices.Sort(names)
	var skills []app.OwnSkill
	for _, name := range slices.Compact(names) {
		s, _, err := b.OwnSkill(name)
		skills = append(skills, app.OwnSkill{Skill: s, Err: err})
	}
	return skills, nil
}

func (b *fakeBrain) Agents() ([]app.Agent, error) {
	var agents []app.Agent
	for _, name := range domain.SortedKeys(b.agents) {
		agents = append(agents, app.Agent{Name: name, Rel: "agents/" + name + ".md", Meta: map[string]string{"name": name, "description": "Does it."}})
	}
	return agents, nil
}

// fakeSources serves skills by repo, commit and name.
type fakeSources struct {
	skills map[string]domain.Skill // repo@sha/name
	heads  map[string]string
}

func (s *fakeSources) Skill(src domain.Source, name string) (domain.Skill, error) {
	sk, ok := s.skills[src.Repo+"@"+src.SHA+"/"+name]
	if !ok {
		return sk, fmt.Errorf("%s@%s: no skill %q", src.Repo, src.SHA[:7], name)
	}
	return sk, nil
}

func (s *fakeSources) Head(repo string) (string, error) { return s.heads[repo], nil }

func (s *fakeSources) Diff(repo, name, from, to string) (string, error) {
	if _, ok := s.skills[repo+"@"+to+"/"+name]; !ok {
		return "", fmt.Errorf("at %s: no skill %q; drop it from brain.toml first", to[:7], name)
	}
	return fmt.Sprintf("diff %s %s..%s", name, from[:7], to[:7]), nil
}

type fakeMachine struct {
	machine    domain.Machine
	missing    bool
	fail       map[string]error // by method, such as "SaveOverlay"
	overlays   map[string]map[string]any
	states     map[string]domain.TreeState
	unreadable []string // state files States cannot read
}

func (m *fakeMachine) Load() (domain.Machine, error) {
	if m.missing {
		return domain.Machine{}, errors.New("no machine file at /cfg/nerve/config.toml: run nerve init")
	}
	c := m.machine
	c.Checkouts = slices.Clone(m.machine.Checkouts)
	return c, nil
}

func (m *fakeMachine) Save(machine domain.Machine) error {
	if err := m.fail["Save"]; err != nil {
		return err
	}
	m.machine = machine
	return nil
}

func (m *fakeMachine) Create(machine domain.Machine) (string, error) {
	if !m.missing {
		return "", errors.New("/cfg/nerve/config.toml exists: edit it instead")
	}
	m.machine, m.missing = machine, false
	return "/cfg/nerve/config.toml", nil
}

func (m *fakeMachine) Overlay(project string) (map[string]any, string, error) {
	return copySettings(m.overlays[project]), "/cfg/nerve/overlays/" + project + ".json", nil
}

func (m *fakeMachine) SaveOverlay(project string, s map[string]any) (string, error) {
	if err := m.fail["SaveOverlay"]; err != nil {
		return "", err
	}
	m.overlays[project] = copySettings(s)
	return "/cfg/nerve/overlays/" + project + ".json", nil
}

func (m *fakeMachine) DropOverlay(project string) (string, error) {
	if err := m.fail["DropOverlay"]; err != nil {
		return "", err
	}
	if _, ok := m.overlays[project]; !ok {
		return "", nil
	}
	delete(m.overlays, project)
	return "/cfg/nerve/overlays/" + project + ".json", nil
}

func (m *fakeMachine) State(dir string) (domain.TreeState, error) {
	st, ok := m.states[dir]
	if !ok {
		return domain.TreeState{Path: dir, Links: map[string]domain.Link{}}, nil
	}
	return copyState(st), nil
}

func copyState(st domain.TreeState) domain.TreeState {
	links := map[string]domain.Link{}
	for k, v := range st.Links {
		links[k] = v
	}
	st.Links, st.Settings, st.Before, st.Folders = links, copySettings(st.Settings), copySettings(st.Before), slices.Clone(st.Folders)
	return st
}

func (m *fakeMachine) SaveState(st domain.TreeState) error {
	if err := m.fail["SaveState"]; err != nil {
		return err
	}
	m.states[st.Path] = copyState(st)
	return nil
}

func (m *fakeMachine) DropState(dir string) error {
	if err := m.fail["DropState"]; err != nil {
		return err
	}
	delete(m.states, dir)
	return nil
}

func (m *fakeMachine) States() ([]domain.TreeState, []string, error) {
	var states []domain.TreeState
	for _, dir := range domain.SortedKeys(m.states) {
		states = append(states, copyState(m.states[dir]))
	}
	return states, slices.Clone(m.unreadable), nil
}

// fakeTrees is working trees in memory: folders, links, the team's real
// files, settings files, what git ignores, and one exclude block per repo.
type fakeTrees struct {
	trees       []domain.Tree
	dirs        map[string]bool // folders: checkouts, and what Link and WriteSettings make on the way
	links       map[string]string
	dangling    map[string]bool // link targets that do not exist
	files       map[string]bool
	settings    map[string]map[string]any
	unignored   map[string]bool // trees where git does not ignore settings.local.json
	tracked     map[string]bool // trees where git tracks it
	excludes    map[string][]string
	damaged     map[string]bool              // repos whose exclude block someone broke
	lost        map[string]bool              // trees git can no longer locate, as after git worktree prune
	writeErr    error                        // of WriteSettings
	linkErr     map[string]error             // of Link, at a path
	removeErr   map[string]error             // of Remove, at a path
	readErr     map[string]error             // of ReadSettings, at a path
	frontmatter map[string]map[string]string // of the team's Markdown files
}

func (w *fakeTrees) Locate(dir string) (domain.Tree, error) {
	for _, t := range w.trees {
		if dir == t.Top || strings.HasPrefix(dir, t.Top+"/") {
			if w.lost[t.Top] {
				return domain.Tree{}, errors.New("not in a git repo: fatal: not a git repository: /nowhere")
			}
			return t, nil
		}
	}
	return domain.Tree{}, errors.New("not in a git repo")
}

func (w *fakeTrees) Exists(p string) bool {
	if t, ok := w.links[p]; ok {
		return !w.dangling[t]
	}
	_, isSettings := w.settings[p]
	return w.dirs[p] || w.files[p] || isSettings
}

func (w *fakeTrees) Present(p string) bool {
	_, isLink := w.links[p]
	return isLink || w.Exists(p)
}

func (w *fakeTrees) LinkTarget(p string) (string, bool) {
	t, ok := w.links[p]
	return t, ok
}

// mkdirs makes dir and the folders above it, as os.MkdirAll does.
func (w *fakeTrees) mkdirs(dir string) {
	for ; dir != "/" && dir != "."; dir = path.Dir(dir) {
		w.dirs[dir] = true
	}
}

func (w *fakeTrees) Link(p, target string) error {
	if err := w.linkErr[p]; err != nil {
		return err
	}
	if w.files[p] || w.dirs[p] {
		return fmt.Errorf("%s is a real file", p)
	}
	w.mkdirs(path.Dir(p))
	w.links[p] = target
	return nil
}

// Remove removes a file or link, or a folder that is empty, as os.Remove
// does.
func (w *fakeTrees) Remove(p string) error {
	switch {
	case w.removeErr[p] != nil:
		return w.removeErr[p]
	case !w.Present(p):
		return fmt.Errorf("no %s", p)
	case w.dirs[p] && len(w.Entries(p)) > 0:
		return fmt.Errorf("%s is not empty", p)
	}
	delete(w.links, p)
	delete(w.files, p)
	delete(w.settings, p)
	delete(w.dirs, p)
	return nil
}

func (w *fakeTrees) RemoveEmptyFolder(p string) (bool, error) {
	if !w.dirs[p] || len(w.Entries(p)) > 0 {
		return false, nil
	}
	delete(w.dirs, p)
	return true, nil
}

func (w *fakeTrees) Entries(dir string) []string {
	var names []string
	for _, set := range []map[string]bool{w.files, w.dirs, keySet(w.links), keySet(w.settings)} {
		for p := range set {
			if path.Dir(p) == dir {
				names = append(names, path.Base(p))
			}
		}
	}
	slices.Sort(names)
	return names
}

func keySet[V any](m map[string]V) map[string]bool {
	set := map[string]bool{}
	for p := range m {
		set[p] = true
	}
	return set
}

func (w *fakeTrees) CheckIgnored(dir string) error {
	switch {
	case w.tracked[dir]:
		return errors.New("git tracks .claude/settings.local.json")
	case w.unignored[dir]:
		return errors.New("git does not ignore .claude/settings.local.json")
	}
	return nil
}

// SyncExclude keeps one block per repo, as info/exclude does, under the
// tree that wrote it last.
func (w *fakeTrees) SyncExclude(dir string, lines func() ([]string, error)) error {
	t, err := w.Locate(dir)
	if err != nil {
		return err
	}
	if w.damaged[t.Main] {
		return errors.New("info/exclude: nerve's block is damaged: fix it by hand")
	}
	rels, err := lines()
	if err != nil {
		return err
	}
	for _, other := range w.trees {
		if other.Repo == t.Repo {
			delete(w.excludes, other.Top)
		}
	}
	if len(rels) > 0 {
		w.excludes[t.Main] = slices.Clone(rels)
	}
	return nil
}

// gitSees lists the links in the tree at top that its repo's exclude block
// does not keep out of git.
func (w *fakeTrees) gitSees(top string) []string {
	t, err := w.Locate(top)
	if err != nil {
		return nil
	}
	var block []string
	for _, other := range w.trees {
		if other.Repo == t.Repo && w.excludes[other.Main] != nil {
			block = w.excludes[other.Main]
		}
	}
	var seen []string
	for p := range w.links {
		if rel, ok := strings.CutPrefix(p, top+"/"); ok && !slices.Contains(block, rel) {
			seen = append(seen, rel)
		}
	}
	slices.Sort(seen)
	return seen
}

func (w *fakeTrees) ReadSettings(p string) (map[string]any, error) {
	if err := w.readErr[p]; err != nil {
		return nil, err
	}
	return copySettings(w.settings[p]), nil
}

func (w *fakeTrees) Frontmatter(p string) (map[string]string, error) {
	meta, ok := w.frontmatter[p]
	if !ok {
		return nil, fmt.Errorf("%s has no frontmatter", path.Base(p))
	}
	return meta, nil
}

func (w *fakeTrees) WriteSettings(p string, s map[string]any) error {
	if w.writeErr != nil {
		return w.writeErr
	}
	if s == nil {
		s = map[string]any{}
	}
	w.mkdirs(path.Dir(p))
	w.settings[p] = copySettings(s)
	return nil
}

type fakeSelf struct{ version string }

func (s *fakeSelf) Skill() (domain.Skill, error) {
	return domain.Skill{Name: "nerve", Dir: "/cache/nerve/skill/nerve", Origin: "nerve"}, nil
}
func (s *fakeSelf) Command() (string, error) { return "nerve sync --here --hook", nil }
func (s *fakeSelf) Version() string {
	if s.version == "" {
		return "v0.3.0"
	}
	return s.version
}

// recorder keeps a use case's events.
type recorder struct{ events []app.Event }

func (r *recorder) Add(e app.Event) { r.events = append(r.events, e) }

func (r *recorder) String() string {
	var lines []string
	for _, e := range r.events {
		lines = append(lines, e.String())
	}
	return strings.Join(lines, "\n")
}

// env is nerve on fakes: a Brain at /brain, nothing listed yet.
type env struct {
	t       *testing.T
	brain   *fakeBrain
	sources *fakeSources
	machine *fakeMachine
	trees   *fakeTrees
	self    *fakeSelf
	app     app.App
}

func newEnv(t *testing.T) *env {
	e := &env{
		t:       t,
		brain:   &fakeBrain{own: map[string]domain.Skill{}, agents: map[string]bool{}, head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		sources: &fakeSources{skills: map[string]domain.Skill{}, heads: map[string]string{}},
		machine: &fakeMachine{machine: domain.Machine{Brain: "/brain"}, fail: map[string]error{}, overlays: map[string]map[string]any{}, states: map[string]domain.TreeState{}},
		trees: &fakeTrees{dirs: map[string]bool{}, links: map[string]string{}, dangling: map[string]bool{}, files: map[string]bool{},
			settings: map[string]map[string]any{}, unignored: map[string]bool{}, tracked: map[string]bool{},
			excludes: map[string][]string{}, damaged: map[string]bool{}, lost: map[string]bool{},
			removeErr: map[string]error{}, readErr: map[string]error{}, frontmatter: map[string]map[string]string{}},
		self: &fakeSelf{},
	}
	e.brain.m.Projects = map[string]*domain.Project{}
	e.app = app.App{Brain: e.brain, Sources: e.sources, Machine: e.machine, Trees: e.trees, Self: e.self}
	return e
}

// checkout puts a git working tree at dir.
func (e *env) checkout(dir string) {
	e.trees.trees = append(e.trees.trees, domain.Tree{Top: dir, Main: dir, Repo: dir + "/.git"})
	e.trees.dirs[dir] = true
}

// worktree puts a linked worktree of main at dir.
func (e *env) worktree(dir, main string) {
	e.trees.trees = append([]domain.Tree{{Top: dir, Main: main, Repo: main + "/.git"}}, e.trees.trees...)
	e.trees.dirs[dir] = true
}

// listed makes dir a checkout of project, listed in the Brain and on this
// machine, and returns the project to fill in.
func (e *env) listed(project, dir string) *domain.Project {
	e.checkout(dir)
	p := &domain.Project{}
	e.brain.m.Projects[project] = p
	e.machine.machine.Checkouts = append(e.machine.machine.Checkouts, domain.Checkout{Path: dir, Project: project})
	return p
}

func (e *env) ownSkill(name string) domain.Skill {
	s := domain.Skill{Name: name, Dir: "/brain/skills/" + name, Origin: "own", Meta: map[string]string{"name": name, "description": "Does " + name + "."}}
	e.brain.own[name] = s
	return s
}

func (e *env) project(name string) *domain.Project { return e.brain.m.Projects[name] }

func (e *env) settings(dir string) map[string]any {
	return e.trees.settings[dir+"/.claude/settings.local.json"]
}

// sync runs a sync of the tree around dir, and returns its events.
func (e *env) sync(dir string, o app.SyncOptions) (*recorder, error) {
	r := &recorder{}
	o.Dir = dir
	return r, e.app.Sync(o, r)
}

// must fails the test on an error, showing the events.
func (e *env) must(r *recorder, err error) *recorder {
	e.t.Helper()
	if err != nil {
		e.t.Fatalf("%v\n%s", err, r)
	}
	return r
}

func contains(t *testing.T, r *recorder, lines ...string) {
	t.Helper()
	out := r.String()
	for _, l := range lines {
		if !strings.Contains(out, l) {
			t.Errorf("events lack %q:\n%s", l, out)
		}
	}
}
