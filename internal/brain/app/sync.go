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
	Dir      string // the working directory
	Here     bool   // only the tree around Dir
	Discard  bool   // overwrite drift
	Hook     bool   // run by autosync, whose report must not fail
	SkipPull bool   // a run that replaced an older nerve, which pulled already
}

// syncer carries one Brain to working trees.
type syncer struct {
	App
	m       domain.Manifest
	r       Reporter
	self    domain.Skill
	cmd     string
	discard bool
	dirty   bool   // the manifest changed: a deleted link was written back
	owner   string // the Brain's owner, once looked up
}

// Sync pulls the Brain and carries it to every checkout this machine lists,
// or only to the tree around the working directory, then reports which
// claude.ai zips changed. A checkout that fails is reported, and the others
// still sync; what the run changed in the Brain and on this machine is
// saved either way.
func (a App) Sync(o SyncOptions, r Reporter) error {
	if !o.SkipPull {
		if err := a.Brain.Pull(r); err != nil {
			return err
		}
	}
	l, err := a.load()
	if err != nil {
		return err
	}
	if err := a.Self.Upgrade(l.m, r); err != nil {
		return err
	}
	if p := l.m.SensitiveValues(); len(p) > 0 {
		for _, e := range p {
			r.Add(problemEvent(e))
		}
		return fmt.Errorf("%d values in %s are sensitive: move them to a local overlay", len(p), domain.ManifestFile)
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
	checkouts := l.machine.Checkouts
	if o.Here {
		t, name, err := a.here(l.machine, o.Dir)
		if err != nil {
			return err
		}
		checkouts = []domain.Checkout{{Path: t.Top, Project: name}}
	}
	failed, drifted := 0, 0
	kept := slices.Clone(l.machine.Checkouts)
	for _, c := range checkouts {
		p := l.m.Projects[c.Project]
		if p == nil || p.Surface == domain.ClaudeAI {
			if err := a.clean(c.Path, fmt.Sprintf("project %s left the Brain", c.Project), r); err != nil {
				r.Add(Event{Kind: EvProblem, Path: c.Path, Detail: err.Error()})
				failed++
				continue
			}
			kept = slices.DeleteFunc(kept, func(k domain.Checkout) bool { return k == c })
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
	if len(kept) != len(l.machine.Checkouts) {
		l.machine.Checkouts = kept
		errs = append(errs, a.Machine.Save(l.machine))
	}
	if s.dirty {
		errs = append(errs, a.Brain.Save(l.m))
	}
	if !o.Here {
		errs = append(errs, s.claudeAI())
	}
	if failed > 0 {
		errs = append(errs, fmt.Errorf("%d of %d checkouts did not sync", failed, len(checkouts)))
	}
	if drifted > 0 && !o.Hook {
		discard := "nerve sync --discard"
		if o.Here {
			discard = "nerve sync --here --discard"
		}
		errs = append(errs, fmt.Errorf("settings drifted in %d of %d checkouts: move each change into the Brain, or drop it with %s", drifted, len(checkouts), discard))
	}
	return errors.Join(errs...)
}

func problemEvent(p domain.Problem) Event {
	return Event{Kind: EvProblem, Path: p.Path, Key: p.Key, Detail: p.Detail}
}

// syncTree carries one project to one working tree and records what it did,
// even when a step fails part way. It returns whether settings drifted.
func (s *syncer) syncTree(name string, p *domain.Project, dir string) (held bool, err error) {
	if !s.Trees.Exists(dir) {
		return false, errors.New("checkout is gone")
	}
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
	defer func() {
		if serr := s.Machine.SaveState(st); err == nil {
			err = serr
		}
	}()
	st.Project, st.Repo = name, tree.Repo
	before := s.r.Count()
	if err := s.Trees.CheckIgnored(dir); err != nil {
		return false, err
	}
	s.writeBack(name, p, dir, &st)
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
	for _, c := range rc.Changes {
		s.r.Add(Event{Kind: EvSetting, Path: path, Key: c.Key, Local: c.Before, Brain: c.After})
	}
	if rc.Write {
		if err := s.Trees.WriteSettings(path, rc.Result); err != nil {
			return false, err
		}
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

// writeBack drops from the project each skill or agent whose link nerve made
// and the user deleted.
func (s *syncer) writeBack(name string, p *domain.Project, dir string, st *domain.TreeState) {
	for _, rel := range domain.SortedKeys(st.Links) {
		if s.Trees.Present(filepath.Join(dir, rel)) {
			continue
		}
		l := st.Links[rel]
		delete(st.Links, rel)
		list := p.List(l.Kind)
		if list == nil || !slices.Contains(*list, l.Name) {
			continue
		}
		*list = slices.DeleteFunc(*list, func(x string) bool { return x == l.Name })
		s.dirty = true
		s.r.Add(Event{Kind: EvWroteBack, Project: name, Item: l.Item()})
	}
}

// ours says whether the link at rel in dir is still the one nerve made: a
// link that points where nerve pointed it.
func (a App) ours(dir, rel string, l domain.Link) bool {
	target, ok := a.Trees.LinkTarget(filepath.Join(dir, rel))
	return ok && target == l.Target
}

// link makes dir hold each link, and removes the links nerve made that are
// no longer wanted. Where the team already has a skill or agent of that
// name, it links nerve's as <owner>-<name>. It never touches what nerve did
// not make, nor a link someone pointed elsewhere.
func (s *syncer) link(dir string, links []domain.Link, st *domain.TreeState) error {
	ours := func(rel string) bool {
		l, ok := st.Links[rel]
		return ok && s.ours(dir, rel, l)
	}
	taken := func(rel string) bool {
		return s.Trees.Present(filepath.Join(dir, rel)) && !ours(rel)
	}
	next := map[string]domain.Link{}
	for _, l := range links {
		rel := l.Rel("")
		if taken(rel) {
			owner, err := s.ownerName()
			if err != nil {
				return fmt.Errorf("the team has %s: %w", rel, err)
			}
			if rel = l.Rel(owner + "-"); taken(rel) {
				return fmt.Errorf("the team has both %s and %s: nerve has nowhere to link %s %s", l.Rel(""), rel, l.Kind, l.Name)
			}
		}
		path := filepath.Join(dir, rel)
		if target, ok := s.Trees.LinkTarget(path); !ok || target != l.Target {
			if err := s.Trees.Link(path, l.Target); err != nil {
				return err
			}
			s.r.Add(Event{Kind: EvLinked, Path: path, Target: l.Target})
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

func (s *syncer) ownerName() (string, error) {
	if s.owner == "" {
		owner, err := s.Brain.Owner()
		if err != nil {
			return "", err
		}
		s.owner = owner
	}
	return s.owner, nil
}

// clean takes out of a working tree what nerve put there: its links that
// still point where it pointed them, its share of the exclude block, and
// the settings it wrote that were not changed since.
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
		if tree, err := a.Trees.Locate(dir); err == nil { // no git, no exclude block
			if err := a.exclude(tree, nil); err != nil {
				return err
			}
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
	}
	if err := a.Machine.DropState(dir); err != nil {
		return err
	}
	r.Add(Event{Kind: EvCleaned, Path: dir, Detail: why})
	return nil
}

// claudeAI builds a zip per skill of each claude.ai project, and reports
// which to upload or delete on the account: those that changed since the
// last build.
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
			sk, err := s.skill(s.m, n)
			why := ""
			if err != nil {
				why = err.Error()
			} else if why = sk.ClaudeAIRejection(); why != "" {
				why = n + ": " + why
			} else if link := s.Zips.FolderLink(sk.Dir); link != "" {
				why = n + ": " + link + " links to a folder or to nothing, which a zip cannot hold"
			}
			if why != "" {
				// The account keeps what was uploaded last: no delete for it.
				if hash, ok := built[name][n]; ok {
					next[name][n] = hash
				}
				s.r.Add(Event{Kind: EvProblem, Path: domain.ManifestFile, Key: "project." + name, Detail: why})
				continue
			}
			zip, err := s.Zips.Zip(sk)
			if err != nil {
				return fmt.Errorf("zip %s: %w", n, err)
			}
			sum := sha256.Sum256(zip)
			hash := hex.EncodeToString(sum[:])
			next[name][n] = hash
			if built[name][n] == hash && s.Zips.Has(name, n) {
				continue
			}
			path, err := s.Zips.Write(name, n, zip)
			if err != nil {
				return err
			}
			s.r.Add(Event{Kind: EvUpload, Project: name, Item: "skill " + n, Path: path})
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
