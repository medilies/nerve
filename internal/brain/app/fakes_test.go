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
	m       domain.Manifest
	saves   int
	saveErr error
	pullErr error
	own     map[string]domain.Skill
	agents  map[string]bool
	owner   string
	head    string
	pulls   int
	moved   [][2]string
}

func (b *fakeBrain) Root() string                   { return "/brain" }
func (b *fakeBrain) Load() (domain.Manifest, error) { return copyManifest(b.m), b.m.Validate() }
func (b *fakeBrain) Pull(app.Reporter) error        { b.pulls++; return b.pullErr }

func (b *fakeBrain) Save(m domain.Manifest) error {
	if b.saveErr != nil {
		return b.saveErr
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

func (b *fakeBrain) MovePin(old, new string) error {
	b.moved = append(b.moved, [2]string{old, new})
	for i := range b.m.Sources {
		if b.m.Sources[i].SHA == old {
			b.m.Sources[i].SHA = new
		}
	}
	return nil
}

func (b *fakeBrain) Owner() (string, error) {
	if b.owner == "" {
		return "", errors.New("the Brain has no origin remote")
	}
	return b.owner, nil
}

func (b *fakeBrain) OwnSkill(name string) (domain.Skill, bool, error) {
	s, ok := b.own[name]
	return s, ok, nil
}

func (b *fakeBrain) OwnSkills() ([]domain.Skill, error) {
	var skills []domain.Skill
	for _, name := range domain.SortedKeys(b.own) {
		skills = append(skills, b.own[name])
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
	machine  domain.Machine
	missing  bool
	saveErr  error // of the machine file and overlays
	overlays map[string]map[string]any
	states   map[string]domain.TreeState
	built    map[string]map[string]string
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
	if m.saveErr != nil {
		return m.saveErr
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
	if m.saveErr != nil {
		return "", m.saveErr
	}
	m.overlays[project] = copySettings(s)
	return "/cfg/nerve/overlays/" + project + ".json", nil
}

func (m *fakeMachine) DropOverlay(project string) (string, error) {
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
	st.Links, st.Settings = links, copySettings(st.Settings)
	return st
}

func (m *fakeMachine) SaveState(st domain.TreeState) error {
	m.states[st.Path] = copyState(st)
	return nil
}
func (m *fakeMachine) DropState(dir string) error { delete(m.states, dir); return nil }

func (m *fakeMachine) States() ([]domain.TreeState, error) {
	var states []domain.TreeState
	for _, dir := range domain.SortedKeys(m.states) {
		states = append(states, copyState(m.states[dir]))
	}
	return states, nil
}

func (m *fakeMachine) Built() (map[string]map[string]string, error) {
	c := map[string]map[string]string{}
	for k, v := range m.built {
		c[k] = v
	}
	return c, nil
}

func (m *fakeMachine) SaveBuilt(b map[string]map[string]string) error { m.built = b; return nil }

// fakeTrees is working trees in memory: links, the team's real files,
// settings files, what git ignores, and one exclude block per repo.
type fakeTrees struct {
	trees     []domain.Tree
	dirs      map[string]bool // checkouts on disk
	links     map[string]string
	dangling  map[string]bool // link targets that do not exist
	files     map[string]bool
	settings  map[string]map[string]any
	unignored map[string]bool // trees where git does not ignore settings.local.json
	tracked   map[string]bool // trees where git tracks it
	excludes  map[string][]string
	damaged   map[string]bool // repos whose exclude block someone broke
}

func (w *fakeTrees) Locate(dir string) (domain.Tree, error) {
	for _, t := range w.trees {
		if dir == t.Top || strings.HasPrefix(dir, t.Top+"/") {
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

func (w *fakeTrees) Link(p, target string) error {
	if w.files[p] {
		return fmt.Errorf("%s is a real file", p)
	}
	w.links[p] = target
	return nil
}

func (w *fakeTrees) Remove(p string) error {
	if !w.Present(p) {
		return fmt.Errorf("no %s", p)
	}
	delete(w.links, p)
	delete(w.files, p)
	delete(w.settings, p)
	return nil
}

func (w *fakeTrees) Entries(dir string) []string {
	var names []string
	for _, set := range []map[string]bool{w.files, linkSet(w.links)} {
		for p := range set {
			if path.Dir(p) == dir {
				names = append(names, path.Base(p))
			}
		}
	}
	slices.Sort(names)
	return names
}

func linkSet(links map[string]string) map[string]bool {
	set := map[string]bool{}
	for p := range links {
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
func (w *fakeTrees) SyncExclude(dir string, rels []string) error {
	t, err := w.Locate(dir)
	if err != nil {
		return err
	}
	if w.damaged[t.Main] {
		return errors.New("info/exclude: nerve's block is damaged: fix it by hand")
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

func (w *fakeTrees) ReadSettings(p string) (map[string]any, error) {
	return copySettings(w.settings[p]), nil
}

func (w *fakeTrees) WriteSettings(p string, s map[string]any) error {
	if s == nil {
		s = map[string]any{}
	}
	w.settings[p] = copySettings(s)
	return nil
}

type fakeSelf struct{ upgrades int }

func (s *fakeSelf) Skill() (domain.Skill, error) {
	return domain.Skill{Name: "nerve", Dir: "/cache/nerve/skill/nerve", Origin: "nerve"}, nil
}
func (s *fakeSelf) Command() (string, error)                    { return "nerve sync --here --hook", nil }
func (s *fakeSelf) Version() string                             { return "v0.3.0" }
func (s *fakeSelf) Upgrade(domain.Manifest, app.Reporter) error { s.upgrades++; return nil }

// fakeZips zips a skill as its name and description, so a changed
// description is a changed zip.
type fakeZips struct {
	written     map[string][]byte // project/name
	folderLinks map[string]string // skill dir -> link
}

func (z *fakeZips) Zip(s domain.Skill) ([]byte, error) {
	return []byte(s.Name + ":" + s.Meta["description"]), nil
}

func (z *fakeZips) Write(project, name string, b []byte) (string, error) {
	z.written[project+"/"+name] = b
	return "/cache/nerve/claude.ai/" + project + "/" + name + ".zip", nil
}

func (z *fakeZips) Has(project, name string) bool { return z.written[project+"/"+name] != nil }
func (z *fakeZips) Remove(project, name string)   { delete(z.written, project+"/"+name) }
func (z *fakeZips) FolderLink(dir string) string  { return z.folderLinks[dir] }

// recorder keeps a use case's events.
type recorder struct{ events []app.Event }

func (r *recorder) Add(e app.Event) { r.events = append(r.events, e) }
func (r *recorder) Count() int      { return len(r.events) }

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
	zips    *fakeZips
	app     app.App
}

func newEnv(t *testing.T) *env {
	e := &env{
		t:       t,
		brain:   &fakeBrain{own: map[string]domain.Skill{}, agents: map[string]bool{}, owner: "medilies", head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		sources: &fakeSources{skills: map[string]domain.Skill{}, heads: map[string]string{}},
		machine: &fakeMachine{machine: domain.Machine{Brain: "/brain"}, overlays: map[string]map[string]any{}, states: map[string]domain.TreeState{}, built: map[string]map[string]string{}},
		trees: &fakeTrees{dirs: map[string]bool{}, links: map[string]string{}, dangling: map[string]bool{}, files: map[string]bool{},
			settings: map[string]map[string]any{}, unignored: map[string]bool{}, tracked: map[string]bool{},
			excludes: map[string][]string{}, damaged: map[string]bool{}},
		self: &fakeSelf{},
		zips: &fakeZips{written: map[string][]byte{}, folderLinks: map[string]string{}},
	}
	e.brain.m.Projects = map[string]*domain.Project{}
	e.app = app.App{Brain: e.brain, Sources: e.sources, Machine: e.machine, Trees: e.trees, Self: e.self, Zips: e.zips}
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
