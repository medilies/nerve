package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/medilies/nerve/internal/brain/domain"
)

// SyncOptions say which trees a sync carries the Brain to, and how.
type SyncOptions struct {
	Dir     string // the working directory
	Here    bool   // only the tree around Dir
	Discard bool   // overwrite drift
	Hook    bool   // run by autosync, whose report must not fail
}

// syncer carries one Brain to working trees.
type syncer struct {
	App
	m       domain.Manifest
	r       Reporter
	self    domain.Skill
	cmd     string
	discard bool
}

// workingTree is one tree a sync carries the Brain to.
type workingTree struct {
	dir     string
	project string // "" when no checkout this machine lists holds it
	listed  bool   // a checkout in the machine file, not a worktree of one
}

// Sync pulls the Brain and carries it to every checkout this machine lists
// and the worktrees of them a sync reached before, or only to the tree
// around the working directory, then reports which claude.ai zips changed.
// A tree that fails is reported, and the others still sync; what the run
// changed in the Brain and on this machine is saved either way.
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
	// The pinned release is the one that judges the Brain; nerve never
	// replaces itself, so it only says so. A development build is left be.
	if pin, running := l.m.Nerve, a.Self.Version(); pin != "" && running != pin && domain.TagPattern.MatchString(running) {
		r.Add(Event{Kind: EvProblem, Path: domain.ManifestFile, Key: "nerve", Detail: fmt.Sprintf("nerve %s runs, but the Brain pins %s: install %s", running, pin, pin)})
	}
	if p := l.m.Refusals(); len(p) > 0 {
		for _, e := range p {
			r.Add(problemEvent(e))
		}
		return fmt.Errorf("%d values in %s are refused: take each out as its problem says", len(p), domain.ManifestFile)
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
	checkouts := len(l.machine.Checkouts)
	var trees []workingTree
	var gone []Event // checkouts unlisted, reported once the machine file is saved
	if o.Here {
		t, name, err := a.here(l.machine, o.Dir)
		if err != nil {
			return err
		}
		_, listed := l.machine.CheckoutAt(t.Top)
		trees = []workingTree{{dir: t.Top, project: name, listed: listed}}
	} else if trees, gone, err = a.workingTrees(&l.machine); err != nil {
		return err
	}
	unlisted := len(l.machine.Checkouts) != checkouts
	failed, drifted := 0, 0
	for _, t := range trees {
		p := l.m.Projects[t.project]
		if p == nil || p.Surface == domain.ClaudeAI {
			why := fmt.Sprintf("project %s left the Brain", t.project)
			if t.project == "" {
				why = "no checkout this machine lists holds it"
			}
			if err := a.clean(t.dir, why, r); err != nil {
				r.Add(Event{Kind: EvProblem, Path: t.dir, Detail: err.Error()})
				failed++
				continue
			}
			if t.listed {
				l.machine.Checkouts = slices.DeleteFunc(l.machine.Checkouts, func(c domain.Checkout) bool { return c.Path == t.dir })
				unlisted = true
			}
			continue
		}
		held, err := s.syncTree(t.project, p, t.dir)
		if err != nil {
			r.Add(Event{Kind: EvProblem, Path: t.dir, Detail: err.Error()})
			failed++
			continue
		}
		if held {
			drifted++
		}
	}
	var errs []error
	if unlisted {
		err := a.Machine.Save(l.machine)
		if err == nil {
			for _, e := range gone {
				r.Add(e)
			}
		}
		errs = append(errs, err)
	}
	if !o.Here {
		errs = append(errs, s.claudeAI())
	}
	if failed > 0 {
		errs = append(errs, fmt.Errorf("%d of %d working trees did not sync", failed, len(trees)))
	}
	if drifted > 0 && !o.Hook {
		discard := "nerve sync --discard"
		if o.Here {
			discard = "nerve sync --here --discard"
		}
		errs = append(errs, fmt.Errorf("settings drifted in %d of %d working trees: move each change into the Brain, or drop it with %s", drifted, len(trees), discard))
	}
	return errors.Join(errs...)
}

// workingTrees lists what a full sync carries the Brain to: each checkout
// machine lists, and each worktree of one that a sync reached before. It
// unlists a checkout that is gone, returning its event, and forgets a
// worktree that is.
func (a App) workingTrees(machine *domain.Machine) ([]workingTree, []Event, error) {
	var trees []workingTree
	var gone []Event
	var kept []domain.Checkout
	for _, c := range machine.Checkouts {
		if a.Trees.Exists(c.Path) {
			kept = append(kept, c)
			trees = append(trees, workingTree{dir: c.Path, project: c.Project, listed: true})
			continue
		}
		if err := a.Machine.DropState(c.Path); err != nil {
			return nil, nil, err
		}
		gone = append(gone, Event{Kind: EvUnlisted, Project: c.Project, Path: c.Path})
	}
	machine.Checkouts = kept
	states, err := a.Machine.States()
	if err != nil {
		return nil, nil, err
	}
	for _, st := range states {
		if _, ok := machine.CheckoutAt(st.Path); ok {
			continue
		}
		if !a.Trees.Exists(st.Path) {
			if err := a.Machine.DropState(st.Path); err != nil {
				return nil, nil, err
			}
			continue
		}
		_, project, _ := a.here(*machine, st.Path) // "" when no listed checkout holds it
		trees = append(trees, workingTree{dir: st.Path, project: project})
	}
	return trees, gone, nil
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
		return false, err
	}
	st, err := s.Machine.State(dir)
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
	before := s.r.Count()
	if err := s.Trees.CheckIgnored(dir); err != nil {
		return false, err
	}
	// A damaged block refuses the tree before any link is made.
	if err := s.exclude(tree, st.Links); err != nil {
		return false, err
	}
	links := []domain.Link{{Kind: "skill", Name: "nerve", Target: s.self.Dir}}
	for _, n := range p.Skills {
		sk, err := s.skill(s.m, n)
		if err != nil {
			return false, err
		}
		links = append(links, domain.Link{Kind: "skill", Name: n, Target: sk.Dir})
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
	if err := s.link(dir, links, &st); err != nil {
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
	if held, err = s.reconcile(settingsPath(dir), want, &st.Settings); err != nil {
		return held, err
	}
	st.Commit = s.Brain.Head()
	if err := save(); err != nil {
		return held, err
	}
	kind := EvSynced
	if s.r.Count() == before {
		kind = EvUpToDate
	}
	s.r.Add(Event{Kind: kind, Project: name, Path: dir, Target: st.Commit})
	return held, nil
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
	rels := map[string]bool{}
	for rel := range links {
		rels[rel] = true
	}
	states, err := a.Machine.States()
	if err != nil {
		return err
	}
	for _, st := range states {
		if filepath.Clean(st.Path) == filepath.Clean(tree.Top) || st.Repo != tree.Repo || !a.Trees.Exists(st.Path) {
			continue
		}
		for rel := range st.Links {
			rels[rel] = true
		}
	}
	return a.Trees.SyncExclude(tree.Top, domain.SortedKeys(rels))
}

// reconcile writes a settings file as the Brain wants it, except for drift,
// and reports each key it changes or holds back.
func (s *syncer) reconcile(path string, want map[string]any, last *map[string]any) (bool, error) {
	current, err := s.Trees.ReadSettings(path)
	if err != nil {
		return false, err
	}
	rc := domain.Reconcile(current, *last, want, s.discard)
	for _, c := range rc.Drift {
		s.r.Add(driftEvent(path, c))
	}
	if rc.Write {
		if err := s.Trees.WriteSettings(path, rc.Result); err != nil {
			return false, err
		}
	}
	for _, c := range rc.Changes {
		s.r.Add(Event{Kind: EvSetting, Path: path, Key: c.Key, Local: c.Before, Brain: c.After})
	}
	*last = rc.Next
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
// already has a skill or agent of that name, or someone pointed nerve's link
// elsewhere, it leaves it, skips that item, and says so. A link that already
// points where nerve would point it, as one left by a checkout that moved,
// becomes nerve's.
func (s *syncer) link(dir string, links []domain.Link, st *domain.TreeState) error {
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
			s.r.Add(Event{Kind: EvProblem, Path: path, Detail: fmt.Sprintf("the team has it, so %s %s is not linked here: rename yours in the Brain to use it", l.Kind, l.Name)})
			continue
		}
		if !same(rel, l) {
			if err := s.Trees.Link(path, l.Target); err != nil {
				return err
			}
			s.r.Add(Event{Kind: EvLinked, Path: path, Target: l.Target})
		} else if _, known := st.Links[rel]; !known {
			s.r.Add(Event{Kind: EvLinked, Path: path, Target: l.Target}) // adopted
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
			return err
		}
		s.r.Add(Event{Kind: EvRemoved, Path: path})
	}
	st.Links = next
	return nil
}

// clean takes out of a working tree what nerve put there: its links that
// still point where it pointed them, its share of the exclude block, the
// settings it wrote that were not changed since, and the folders it made
// that are left empty.
func (a App) clean(dir, why string, r Reporter) error {
	st, err := a.Machine.State(dir)
	if err != nil {
		return err
	}
	if a.Trees.Exists(dir) {
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
		rest, changed := domain.Strip(current, st.Settings)
		switch {
		case current == nil || !changed:
		case len(rest) == 0:
			if err := a.Trees.Remove(path); err != nil {
				return err
			}
			r.Add(Event{Kind: EvRemoved, Path: path})
		default:
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

// claudeAI builds a zip per skill of each claude.ai project, and reports
// which to upload or delete on the account: those that changed since the
// last build. A skill it cannot build or write is reported, and keeps the
// zip last built.
func (s *syncer) claudeAI() error {
	built, err := s.Machine.Built()
	if err != nil {
		return err
	}
	next := map[string]map[string]string{}
	for _, name := range domain.SortedKeys(s.m.Projects) {
		p := s.m.Projects[name]
		if p.Surface != domain.ClaudeAI {
			continue
		}
		next[name] = map[string]string{}
		for _, n := range p.Skills {
			hash, why := s.build(name, n, built[name][n])
			if why != "" {
				// The account keeps what was uploaded last: no delete for it.
				if hash, ok := built[name][n]; ok {
					next[name][n] = hash
				}
				s.r.Add(Event{Kind: EvProblem, Path: domain.ManifestFile, Key: "project." + name, Detail: why})
				continue
			}
			next[name][n] = hash
		}
	}
	for _, name := range domain.SortedKeys(built) {
		for _, n := range domain.SortedKeys(built[name]) {
			if _, ok := next[name][n]; ok {
				continue
			}
			s.Zips.Remove(name, n)
			s.r.Add(Event{Kind: EvUnload, Project: name, Item: "skill " + n})
		}
	}
	return s.Machine.SaveBuilt(next)
}

// build zips skill n of claude.ai project name, writes it when it differs
// from the zip last built (whose hash is last), and returns its hash, or
// why it could not.
func (s *syncer) build(name, n, last string) (hash, why string) {
	sk, err := s.skill(s.m, n)
	if err != nil {
		return "", err.Error()
	}
	if why := sk.ClaudeAIRejection(); why != "" {
		return "", n + ": " + why
	}
	zip, err := s.Zips.Zip(sk)
	if err != nil {
		return "", n + ": " + err.Error()
	}
	sum := sha256.Sum256(zip)
	hash = hex.EncodeToString(sum[:])
	if hash == last && s.Zips.Has(name, n) {
		return hash, ""
	}
	path, err := s.Zips.Write(name, n, zip)
	if err != nil {
		return "", n + ": " + err.Error()
	}
	s.r.Add(Event{Kind: EvUpload, Project: name, Item: "skill " + n, Path: path})
	return hash, ""
}
