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
// claude.ai zips changed.
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
	targets := l.machine.Checkouts
	if o.Here {
		t, name, err := a.here(l.machine, o.Dir)
		if err != nil {
			return err
		}
		targets = []domain.Checkout{{Path: t.Top, Project: name}}
	}
	failed, drifted := 0, 0
	kept := slices.Clone(l.machine.Checkouts)
	for _, c := range targets {
		p := l.m.Projects[c.Project]
		if p == nil || p.Surface == domain.ClaudeAI {
			if err := a.clean(c.Path, fmt.Sprintf("project %s left the Brain", c.Project), r); err != nil {
				return err
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
	if len(kept) != len(l.machine.Checkouts) {
		l.machine.Checkouts = kept
		if err := a.Machine.Save(l.machine); err != nil {
			return err
		}
	}
	if s.dirty {
		if err := a.Brain.Save(l.m); err != nil {
			return err
		}
	}
	if !o.Here {
		if err := s.claudeAI(); err != nil {
			return err
		}
	}
	switch {
	case failed > 0:
		return fmt.Errorf("%d of %d checkouts did not sync", failed, len(targets))
	case drifted > 0 && !o.Hook:
		return fmt.Errorf("settings drifted in %d of %d checkouts: move each change into the Brain, or sync --discard", drifted, len(targets))
	}
	return nil
}

func problemEvent(p domain.Problem) Event {
	return Event{Kind: EvProblem, Path: p.Path, Key: p.Key, Detail: p.Detail}
}

// syncTree carries one project to one working tree and records what it did,
// even when a step fails part way. It returns whether settings drifted.
func (s *syncer) syncTree(name string, p *domain.Project, dir string) (held bool, err error) {
	if !s.Workspace.Exists(dir) {
		return false, errors.New("checkout is gone")
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
	st.Project = name
	before := s.r.Count()
	if err := s.Workspace.CheckIgnored(dir); err != nil {
		return false, err
	}
	s.writeBack(name, p, dir, &st)
	links := []domain.Link{{Kind: "skill", Name: "nerve", Target: s.self.Dir}}
	for _, n := range p.Skills {
		if n == "nerve" {
			return false, errors.New("nerve is nerve's own skill: drop it from the project")
		}
		sk, err := s.skill(s.m, n)
		if err != nil {
			return false, err
		}
		links = append(links, domain.Link{Kind: "skill", Name: n, Target: sk.Dir})
	}
	for _, n := range p.Agents {
		file, ok := s.Brain.AgentFile(n)
		if !ok {
			return false, fmt.Errorf("the Brain has no agent %q", n)
		}
		links = append(links, domain.Link{Kind: "agent", Name: n, Target: file})
	}
	if err := s.link(dir, links, &st); err != nil {
		return false, err
	}
	if err := s.Workspace.SyncExclude(dir, domain.SortedKeys(st.Links)); err != nil {
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

// reconcile writes a settings file as the Brain wants it, except for drift,
// and reports each key it changes or holds back.
func (s *syncer) reconcile(path string, want map[string]any, last *map[string]any) (bool, error) {
	current, err := s.Workspace.ReadSettings(path)
	if err != nil {
		return false, err
	}
	rc := domain.Reconcile(current, *last, want, s.discard)
	for _, c := range rc.Drift {
		s.r.Add(Event{Kind: EvDrift, Path: path, Key: c.Key, Local: c.After, Brain: c.Before})
	}
	for _, c := range rc.Changes {
		s.r.Add(Event{Kind: EvSetting, Path: path, Key: c.Key, Local: c.Before, Brain: c.After})
	}
	if rc.Write {
		if err := s.Workspace.WriteSettings(path, rc.Result); err != nil {
			return false, err
		}
	}
	*last = rc.Next
	return len(rc.Drift) > 0, nil
}

// writeBack drops from the project each skill or agent whose link nerve made
// and the user deleted.
func (s *syncer) writeBack(name string, p *domain.Project, dir string, st *domain.TreeState) {
	for _, rel := range domain.SortedKeys(st.Links) {
		if s.Workspace.Present(filepath.Join(dir, rel)) {
			continue
		}
		kind, item, _ := strings.Cut(st.Links[rel], ":")
		delete(st.Links, rel)
		list := p.List(kind)
		if list == nil || !slices.Contains(*list, item) {
			continue
		}
		*list = slices.DeleteFunc(*list, func(x string) bool { return x == item })
		s.dirty = true
		s.r.Add(Event{Kind: EvWroteBack, Project: name, Item: kind + " " + item})
	}
}

// link makes dir hold each link, and removes the links nerve made that are
// no longer wanted. Where the team already has a skill or agent of that
// name, it links nerve's as <owner>-<name>. It never touches what nerve did
// not make.
func (s *syncer) link(dir string, links []domain.Link, st *domain.TreeState) error {
	ours := func(rel string) bool {
		return st.Links[rel] != "" && s.Workspace.IsLink(filepath.Join(dir, rel))
	}
	taken := func(rel string) bool {
		return s.Workspace.Present(filepath.Join(dir, rel)) && !ours(rel)
	}
	next := map[string]string{}
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
		if target, ok := s.Workspace.LinkTarget(path); !ok || target != l.Target {
			if err := s.Workspace.Link(path, l.Target); err != nil {
				return err
			}
			s.r.Add(Event{Kind: EvLinked, Path: path, Target: l.Target})
		}
		next[rel] = l.Carries()
		st.Links[rel] = next[rel] // ours from here on, should a later link collide
	}
	for _, rel := range domain.SortedKeys(st.Links) {
		if _, ok := next[rel]; ok || !ours(rel) {
			continue
		}
		path := filepath.Join(dir, rel)
		if err := s.Workspace.Remove(path); err != nil {
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

// clean takes out of a working tree what nerve put there: its links, its
// exclude block, and the settings it wrote that were not changed since.
func (a App) clean(dir, why string, r Reporter) error {
	st, err := a.Machine.State(dir)
	if err != nil {
		return err
	}
	if a.Workspace.Exists(dir) {
		for _, rel := range domain.SortedKeys(st.Links) {
			path := filepath.Join(dir, rel)
			if a.Workspace.IsLink(path) {
				if err := a.Workspace.Remove(path); err != nil {
					return err
				}
				r.Add(Event{Kind: EvRemoved, Path: path})
			}
		}
		if err := a.Workspace.SyncExclude(dir, nil); err != nil {
			return err
		}
		path := settingsPath(dir)
		current, err := a.Workspace.ReadSettings(path)
		if err != nil {
			return err
		}
		rest, changed := domain.Strip(current, st.Settings)
		switch {
		case current == nil || !changed:
		case len(rest) == 0:
			if err := a.Workspace.Remove(path); err != nil {
				return err
			}
			r.Add(Event{Kind: EvRemoved, Path: path})
		default:
			if err := a.Workspace.WriteSettings(path, rest); err != nil {
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
			if err != nil {
				return err
			}
			why := sk.ClaudeAIRejection()
			if link := s.Zips.FolderLink(sk.Dir); link != "" {
				why = link + " links to a folder or to nothing, which a zip cannot hold"
			}
			if why != "" {
				s.r.Add(Event{Kind: EvProblem, Path: domain.ManifestFile, Key: "project." + name, Detail: n + ": " + why})
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
