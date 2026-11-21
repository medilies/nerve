package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/medilies/nerve/internal/brain/domain"
)

// SyncOptions say which checkouts a sync carries the Brain to, and how.
type SyncOptions struct {
	Dir     string // the working directory
	Here    bool   // only the checkout around Dir
	Discard bool   // overwrite drift
}

// syncer carries one Brain to checkouts.
type syncer struct {
	App
	m       domain.Manifest
	r       Reporter
	self    domain.Skill
	cmd     string
	discard bool
}

// Sync makes the Brain equal to its remote, then carries it to every
// checkout this machine lists, or only to the one around the working
// directory. A checkout that fails is reported, and the others still sync;
// what the run changed on this machine is saved either way.
func (a App) Sync(o SyncOptions, r Reporter) error {
	// This machine's file names the Brain, so nothing is pulled without it.
	if _, err := a.Machine.Load(); err != nil {
		return err
	}
	if err := a.Brain.Pull(r); err != nil {
		return err
	}
	l, err := a.load()
	if err != nil {
		return err
	}
	if err := refused(l.m, r); err != nil {
		return err
	}
	self, err := a.Self.Skill()
	if err != nil {
		return err
	}
	cmd, err := a.Self.Command()
	if err != nil {
		return err
	}
	s := &syncer{App: a, m: l.m, r: r, self: self, cmd: cmd, discard: o.Discard}
	var checkouts []domain.Checkout
	var gone []Event // checkouts unlisted, reported once the machine file is saved
	if o.Here {
		t, name, err := a.here(l.machine, o.Dir)
		if err != nil {
			return err
		}
		checkouts = []domain.Checkout{{Path: t.Top, Project: name}}
	} else if checkouts, gone, err = a.present(&l.machine); err != nil {
		return err
	}
	failed, drifted := 0, 0
	for _, c := range checkouts {
		p := l.m.Projects[c.Project]
		if p == nil {
			why := fmt.Sprintf("project %s left the Brain", c.Project)
			if err := a.clean(c.Path, why, r); err != nil {
				r.Add(Event{Kind: EvProblem, Path: c.Path, Detail: err.Error()})
				failed++
				continue
			}
			l.machine.Checkouts = slices.DeleteFunc(l.machine.Checkouts, func(x domain.Checkout) bool { return x.Path == c.Path })
			gone = append(gone, Event{Kind: EvUnlisted, Project: c.Project, Path: c.Path, Detail: why})
			continue
		}
		held, err := s.syncTree(c.Project, p, c.Path)
		if err != nil {
			r.Add(Event{Kind: EvProblem, Path: c.Path, Detail: err.Error()})
			failed++
			continue
		}
		if held {
			drifted++
		}
	}
	var errs []error
	if len(gone) > 0 {
		err := a.Machine.Save(l.machine)
		if err == nil {
			for _, e := range gone {
				r.Add(e)
			}
		}
		errs = append(errs, err)
	}
	if failed > 0 {
		errs = append(errs, fmt.Errorf("%d of %d checkouts did not sync", failed, len(checkouts)))
	}
	if drifted > 0 {
		discard := "nerve sync --discard"
		if o.Here {
			discard = "nerve sync --here --discard"
		}
		errs = append(errs, fmt.Errorf("settings drifted in %d of %d checkouts: keep each change with nerve setting set or add, or drop it with %s", drifted, len(checkouts), discard))
	}
	return errors.Join(errs...)
}

// Push commits everything in the Brain with message, and force-pushes it to
// its upstream, once the Brain holds nothing a sync would refuse.
func (a App) Push(message string, r Reporter) error {
	m, err := a.Brain.Load()
	if err != nil {
		return err
	}
	if err := refused(m, r); err != nil {
		return err
	}
	return a.Brain.Push(message, r)
}

// refused reports each setting of m no list allows, and fails when there
// are any.
func refused(m domain.Manifest, r Reporter) error {
	p := m.Refusals()
	for _, e := range p {
		r.Add(problemEvent(e))
	}
	if len(p) > 0 {
		return fmt.Errorf("%d values in %s are refused: take each out as its problem says", len(p), domain.ManifestFile)
	}
	return nil
}

// present lists the checkouts machine lists that are still there. It
// unlists those that are gone, returning their events.
func (a App) present(machine *domain.Machine) ([]domain.Checkout, []Event, error) {
	var kept []domain.Checkout
	var gone []Event
	for _, c := range machine.Checkouts {
		if a.Trees.Exists(c.Path) {
			kept = append(kept, c)
			continue
		}
		if err := a.Machine.DropState(c.Path); err != nil {
			return nil, nil, err
		}
		gone = append(gone, Event{Kind: EvUnlisted, Project: c.Project, Path: c.Path})
	}
	machine.Checkouts = kept
	return slices.Clone(kept), gone, nil
}

func problemEvent(p domain.Problem) Event {
	return Event{Kind: EvProblem, Path: p.Path, Key: p.Key, Detail: p.Detail}
}

// syncTree carries one project to one working tree and records what it did,
// even when a step fails part way. It returns whether settings drifted.
func (s *syncer) syncTree(name string, p *domain.Project, dir string) (held bool, err error) {
	if err := s.refusals(s.m, p); err != nil {
		return false, err
	}
	tree, err := s.Trees.Locate(dir)
	if err != nil {
		return false, fmt.Errorf("git cannot locate it, so nerve leaves it as it is: %w; make it a git repo again, or move the folder away for nerve to unlist it", err)
	}
	if filepath.Clean(tree.Top) != filepath.Clean(dir) { // its links would sit below the exclude block's
		return false, fmt.Errorf("it is inside the git repo at %s, not its top: list %s in the machine file instead", tree.Top, tree.Top)
	}
	st, err := s.state(dir, s.r)
	if err != nil {
		return false, err
	}
	var missing []string // folders nerve may make
	for _, f := range domain.TreeFolders {
		if !s.Trees.Present(filepath.Join(dir, f)) {
			missing = append(missing, f)
		}
	}
	saved := false
	save := func() error {
		saved = true
		for _, f := range missing {
			if s.Trees.Exists(filepath.Join(dir, f)) && !slices.Contains(st.Folders, f) {
				st.Folders = append(st.Folders, f)
			}
		}
		return s.Machine.SaveState(st)
	}
	defer func() {
		if !saved {
			if serr := save(); err == nil {
				err = serr
			}
		}
	}()
	st.Project, st.Repo = name, tree.Repo
	r := s.r
	c := &changes{Reporter: r}
	s.r = c
	defer func() { s.r = r }()
	if err := s.throughLink(dir); err != nil {
		return false, err
	}
	if err := s.Trees.CheckIgnored(dir); err != nil {
		return false, err
	}
	// A damaged block refuses the tree before any link is made.
	if err := s.exclude(tree, st.Links); err != nil {
		return false, err
	}
	links := []domain.Link{{Kind: "skill", Name: "nerve", Target: s.self.Dir}}
	for _, n := range p.Skills {
		l := domain.Link{Kind: "skill", Name: n}
		sk, err := s.skill(s.m, n)
		if err != nil {
			// One skill that cannot be had, say from a source offline,
			// costs only that skill, which keeps the link it had.
			why := "not linked: "
			if old, ok := st.Links[l.Rel()]; ok {
				links = append(links, old)
				why = "left as last linked: "
			}
			s.r.Add(Event{Kind: EvProblem, Path: filepath.Join(dir, l.Rel()), Detail: why + err.Error()})
			continue
		}
		l.Target = sk.Dir
		links = append(links, l)
	}
	for _, n := range p.Agents {
		file, _ := s.Brain.AgentFile(n)
		links = append(links, domain.Link{Kind: "agent", Name: n, Target: file})
	}
	// Whatever link did, even part way, git must not see it.
	excluded := false
	defer func() {
		if !excluded {
			if xerr := s.exclude(tree, st.Links); err == nil {
				err = xerr
			}
		}
	}()
	// A tree nerve has no state for, that already holds its links, is a
	// checkout that moved: what it holds as the Brain wants it is nerve's,
	// as adopt takes it, so that cleaning it takes that out.
	fresh := len(st.Links) == 0 && st.Settings == nil
	adopted, err := s.link(dir, links, &st)
	if err != nil {
		return false, err
	}
	excluded = true
	if err := s.exclude(tree, st.Links); err != nil {
		return false, err
	}
	overlay, _, err := s.Machine.Overlay(name)
	if err != nil {
		return false, err
	}
	want := domain.Want(s.m, p, s.cmd, overlay, s.Brain.Root())
	if held, err = s.reconcile(settingsPath(dir), want, overlay, &st, fresh && adopted); err != nil {
		return held, err
	}
	st.Commit = s.Brain.Head()
	if err := save(); err != nil {
		return held, err
	}
	kind := EvSynced
	if c.n == 0 {
		kind = EvUpToDate
	}
	s.r.Add(Event{Kind: kind, Project: name, Path: dir, Target: st.Commit})
	return held, nil
}

// changes passes events on, counting those that say something changed.
type changes struct {
	Reporter
	n int
}

func (c *changes) Add(e Event) {
	if e.Kind != EvProblem && e.Kind != EvDrift && e.Kind != EvPersonal {
		c.n++
	}
	c.Reporter.Add(e)
}

// refusals says what a project lists that the Brain cannot give it, as
// check does, or nil.
func (a App) refusals(m domain.Manifest, p *domain.Project) error {
	var why []string
	for _, kind := range domain.ItemKinds {
		for _, item := range *p.List(kind) {
			if w := a.offers(m, kind, item); w != "" {
				why = append(why, w)
			}
		}
	}
	if len(why) == 0 {
		return nil
	}
	return errors.New(strings.Join(why, "; "))
}

// exclude keeps out of git the links of every working tree that shares
// tree's info/exclude: links, this tree's, and those nerve made in the
// repo's other worktrees.
func (a App) exclude(tree domain.Tree, links map[string]domain.Link) error {
	return a.Trees.SyncExclude(tree.Top, func() ([]string, error) {
		rels := map[string]bool{}
		for rel := range links {
			rels[rel] = true
		}
		states, err := a.Machine.States()
		if err != nil {
			return nil, err
		}
		for _, st := range states {
			if filepath.Clean(st.Path) == filepath.Clean(tree.Top) || st.Repo != tree.Repo || !a.Trees.Exists(st.Path) {
				continue
			}
			for rel := range st.Links {
				rels[rel] = true
			}
		}
		return domain.SortedKeys(rels), nil
	})
}

// reconcile writes a settings file as the Brain wants it, except for drift
// and personal settings, and reports each key it changes or holds back, and
// each personal setting the first time it sees it. In a checkout that
// moved, nothing the file holds counts as there before nerve.
func (s *syncer) reconcile(path string, want, overlay map[string]any, st *domain.TreeState, moved bool) (bool, error) {
	current, err := s.Trees.ReadSettings(path)
	if err != nil {
		return false, err
	}
	rc := domain.Reconcile(current, st.Settings, want, s.discard)
	if st.Settings == nil || current == nil { // nerve's first write to this file
		st.Before = rc.Before
		if moved {
			st.Before = nil
		}
	}
	for _, c := range rc.Drift {
		e := driftEvent(path, c)
		if domain.FromOverlay(c, overlay) {
			e.Detail = "local overlay"
		}
		s.r.Add(e)
	}
	if rc.Write {
		if err := s.Trees.WriteSettings(path, rc.Result); err != nil {
			return false, err
		}
	}
	for _, c := range rc.Changes {
		s.r.Add(Event{Kind: EvSetting, Path: path, Key: c.Key, Local: c.Before, Brain: c.After})
	}
	names, shown := domain.PersonalKeys(rc.Personal)
	for _, n := range names {
		if slices.Contains(st.Personal, n) {
			continue
		}
		e := Event{Kind: EvPersonal, Path: path, Key: shown[n].Key, Local: shown[n].After}
		if shown[n].Item {
			e.Item = shown[n].After
		}
		s.r.Add(e)
	}
	st.Settings, st.Personal = rc.Next, names
	return len(rc.Drift) > 0, nil
}

func driftEvent(path string, c domain.Change) Event {
	e := Event{Kind: EvDrift, Path: path, Key: c.Key, Local: c.After, Brain: c.Before}
	if c.Item {
		e.Item = c.After
		if c.After == "unset" {
			e.Item = c.Before
		}
	}
	return e
}

// ours says whether the link at rel in dir is still the one nerve made: a
// link that points where nerve pointed it.
func (a App) ours(dir, rel string, l domain.Link) bool {
	target, ok := a.Trees.LinkTarget(filepath.Join(dir, rel))
	return ok && target == l.Target
}

// link makes dir hold each link, and removes the links nerve made that are
// no longer wanted. A link that is gone is made again. Where the team
// already has a skill or agent of that name, it leaves it, skips that item,
// and says so; a link someone pointed elsewhere is theirs, and left alone. A link that already
// points where nerve would point it, as one left by a checkout that moved,
// becomes nerve's. It returns whether it took any link that way.
func (s *syncer) link(dir string, links []domain.Link, st *domain.TreeState) (adopted bool, err error) {
	ours := func(rel string) bool {
		l, ok := st.Links[rel]
		return ok && s.ours(dir, rel, l)
	}
	same := func(rel string, l domain.Link) bool {
		target, ok := s.Trees.LinkTarget(filepath.Join(dir, rel))
		return ok && target == l.Target
	}
	taken := func(rel string, l domain.Link) bool {
		return s.Trees.Present(filepath.Join(dir, rel)) && !ours(rel) && !same(rel, l)
	}
	next := map[string]domain.Link{}
	for _, l := range links {
		rel := l.Rel()
		path := filepath.Join(dir, rel)
		if taken(rel, l) {
			_, isLink := s.Trees.LinkTarget(path)
			switch {
			case isLink: // a link someone pointed elsewhere: theirs, left alone
			case l.Kind == "skill" && l.Name == "nerve":
				s.r.Add(Event{Kind: EvProblem, Path: path, Detail: "the team has a skill named nerve, so nerve's own is not linked here"})
			default:
				s.r.Add(Event{Kind: EvProblem, Path: path, Detail: fmt.Sprintf("the team has it, so %s %s is not linked here: rename yours in the Brain to use it", l.Kind, l.Name)})
			}
			continue
		}
		switch {
		case !same(rel, l):
			if err := s.Trees.Link(path, l.Target); err != nil {
				return adopted, err
			}
			s.r.Add(Event{Kind: EvLinked, Path: path, Target: l.Target})
		case !ours(rel): // already as nerve would make it: adopted
			adopted = true
		}
		next[rel] = l
		st.Links[rel] = l // ours from here on, should a later link collide
	}
	for _, rel := range domain.SortedKeys(st.Links) {
		if _, ok := next[rel]; ok || !ours(rel) {
			continue
		}
		path := filepath.Join(dir, rel)
		if err := s.Trees.Remove(path); err != nil {
			return adopted, err
		}
		s.r.Add(Event{Kind: EvRemoved, Path: path})
	}
	st.Links = next
	return adopted, nil
}

// throughLink refuses a tree where a folder or file nerve writes is a link:
// nerve would write outside the tree, and git would see what it made.
func (a App) throughLink(dir string) error {
	for _, rel := range append(slices.Clone(domain.TreeFolders), domain.LocalSettings) {
		if _, ok := a.Trees.LinkTarget(filepath.Join(dir, rel)); ok {
			what := "folder"
			if rel == domain.LocalSettings {
				what = "file"
			}
			return fmt.Errorf("%s is a link, so nerve would write outside the tree: make it a plain %s", rel, what)
		}
	}
	return nil
}

// clean takes out of a working tree what nerve put there: its links that
// still point where it pointed them, its share of the exclude block, the
// settings it wrote that were not changed since, and the folders it made
// that are left empty.
func (a App) clean(dir, why string, r Reporter) error {
	st, err := a.state(dir, r)
	if err != nil {
		return err
	}
	if a.Trees.Exists(dir) {
		if err := a.throughLink(dir); err != nil {
			return err
		}
		for _, rel := range domain.SortedKeys(st.Links) {
			if !a.ours(dir, rel, st.Links[rel]) {
				continue
			}
			path := filepath.Join(dir, rel)
			if err := a.Trees.Remove(path); err != nil {
				return err
			}
			r.Add(Event{Kind: EvRemoved, Path: path})
		}
		tree, err := a.Trees.Locate(dir)
		switch {
		case err == nil:
			if err := a.exclude(tree, nil); err != nil {
				return err
			}
		case st.Repo != "" && a.Trees.Exists(st.Repo): // its block is still there
			r.Add(Event{Kind: EvProblem, Path: dir, Detail: fmt.Sprintf("git cannot locate it, so nerve's lines for it stay in %s: %v", filepath.Join(st.Repo, "info", "exclude"), err)})
		}
		path := settingsPath(dir)
		current, err := a.Trees.ReadSettings(path)
		if err != nil {
			return err
		}
		// The file stays, if only as {}: nerve never deletes it.
		if rest, changed := domain.Strip(current, st.Settings, st.Before); current != nil && changed {
			if rest == nil {
				rest = map[string]any{}
			}
			if err := a.Trees.WriteSettings(path, rest); err != nil {
				return err
			}
			r.Add(Event{Kind: EvWrote, Path: path})
		}
		for _, f := range domain.TreeFolders {
			if !slices.Contains(st.Folders, f) {
				continue
			}
			path := filepath.Join(dir, f)
			if removed, err := a.Trees.RemoveEmptyFolder(path); err != nil {
				return err
			} else if removed {
				r.Add(Event{Kind: EvRemoved, Path: path})
			}
		}
	}
	if err := a.Machine.DropState(dir); err != nil {
		return err
	}
	r.Add(Event{Kind: EvCleaned, Path: dir, Detail: why})
	return nil
}
