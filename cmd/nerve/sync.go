package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// localSettings is the one settings file nerve writes in a project.
var localSettings = filepath.Join(".claude", "settings.local.json")

// syncer carries a Brain to working trees.
type syncer struct {
	b       brain
	self    Skill  // nerve's own skill
	cmd     string // what autosync runs
	discard bool
	r       *report
	dirty   bool   // brain.toml changed: a deleted link was written back
	owner   string // the Brain's owner, once looked up
}

func runSync(machine Machine, f flags, r *report) error {
	root := machine.Brain
	if os.Getenv(upgradedEnv) == "" { // a re-run after an upgrade has pulled already
		if err := pullBrain(root, r); err != nil {
			return err
		}
	}
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	if err := upgradeToPin(m, r); err != nil {
		return err
	}
	if p := manifestProblems(m); len(p) > 0 {
		for _, e := range p {
			r.add(e)
		}
		return fmt.Errorf("%d values in %s are sensitive: move them to a local overlay", len(p), manifestFile)
	}
	self, err := installSelf()
	if err != nil {
		return err
	}
	cmd, err := autosyncCommand()
	if err != nil {
		return err
	}
	s := &syncer{b: brain{root, m}, self: self, cmd: cmd, discard: f.discard, r: r}
	targets := machine.Checkouts
	if f.here {
		t, name, err := machine.here()
		if err != nil {
			return err
		}
		targets = []Checkout{{Path: t.Top, Project: name}}
	}
	failed, drifted := 0, 0
	kept := slices.Clone(machine.Checkouts)
	for _, c := range targets {
		p := m.Projects[c.Project]
		if p == nil || p.Surface == claudeAI {
			if err := cleanTree(c.Path, fmt.Sprintf("project %s left the Brain", c.Project), r); err != nil {
				return err
			}
			kept = slices.DeleteFunc(kept, func(k Checkout) bool { return k.Project == c.Project && samePath(k.Path, c.Path) })
			continue
		}
		held, err := s.syncTree(c.Project, p, c.Path)
		if err != nil {
			r.add(Event{Kind: evProblem, Path: c.Path, Detail: err.Error()})
			failed++
			continue
		}
		if held {
			drifted++
		}
	}
	if len(kept) != len(machine.Checkouts) {
		machine.Checkouts = kept
		if err := saveMachine(machine); err != nil {
			return err
		}
	}
	if s.dirty {
		if err := saveManifest(root, m); err != nil {
			return err
		}
	}
	if !f.here {
		if err := s.syncClaudeAI(); err != nil {
			return err
		}
	}
	switch {
	case failed > 0:
		return fmt.Errorf("%d of %d checkouts did not sync", failed, len(targets))
	case drifted > 0 && r.mode != asHook:
		return fmt.Errorf("settings drifted in %d of %d checkouts: move each change into the Brain, or sync --discard", drifted, len(targets))
	}
	return nil
}

// syncTree carries one project to one working tree and records what it did,
// even when a step fails part way. It returns whether settings drifted.
func (s *syncer) syncTree(name string, p *Project, dir string) (held bool, err error) {
	if _, err := os.Stat(dir); err != nil {
		return false, fmt.Errorf("checkout is gone: %w", err)
	}
	st, err := loadState(dir)
	if err != nil {
		return false, err
	}
	defer func() {
		if serr := saveState(st); err == nil {
			err = serr
		}
	}()
	st.Project = name
	before := s.r.count
	if err := checkIgnored(dir); err != nil {
		return false, err
	}
	s.writeBack(name, p, dir, &st)
	wants := []want{{kind: "skill", name: "nerve", target: s.self.Dir}}
	for _, n := range p.Skills {
		if n == "nerve" {
			return false, errors.New("nerve is nerve's own skill: drop it from the project")
		}
		sk, err := s.b.skill(n)
		if err != nil {
			return false, err
		}
		wants = append(wants, want{kind: "skill", name: n, target: sk.Dir})
	}
	for _, n := range p.Agents {
		if !s.b.hasAgent(n) {
			return false, fmt.Errorf("the Brain has no agent %q", n)
		}
		wants = append(wants, want{kind: "agent", name: n, target: s.b.agentFile(n)})
	}
	if err := s.link(dir, wants, &st); err != nil {
		return false, err
	}
	if err := syncExclude(dir, sortedKeys(st.Links)); err != nil {
		return false, err
	}
	settings, err := wantSettings(s.b.root, s.b.m, name, p, s.cmd)
	if err != nil {
		return false, err
	}
	if held, err = syncFile(filepath.Join(dir, localSettings), settings, &st.Settings, s.discard, false, s.r); err != nil {
		return held, err
	}
	st.Commit = brainHead(s.b.root)
	kind := evSynced
	if s.r.count == before {
		kind = evUpToDate
	}
	s.r.add(Event{Kind: kind, Project: name, Path: dir, Target: st.Commit})
	return held, nil
}

// writeBack drops from the project each skill or agent whose link nerve made
// and the user deleted.
func (s *syncer) writeBack(name string, p *Project, dir string, st *state) {
	for _, rel := range sortedKeys(st.Links) {
		if _, err := os.Lstat(filepath.Join(dir, rel)); err == nil {
			continue
		}
		kind, item, _ := strings.Cut(st.Links[rel], ":")
		delete(st.Links, rel)
		list := map[string]*[]string{"skill": &p.Skills, "agent": &p.Agents}[kind]
		if list == nil || !slices.Contains(*list, item) {
			continue
		}
		*list = slices.DeleteFunc(*list, func(x string) bool { return x == item })
		s.dirty = true
		s.r.add(Event{Kind: evWroteBack, Project: name, Item: kind + " " + item})
	}
}

// want is one link a working tree should hold.
type want struct{ kind, name, target string }

func (w want) rel(prefix string) string {
	if w.kind == "agent" {
		return filepath.Join(".claude", "agents", prefix+w.name+".md")
	}
	return filepath.Join(".claude", "skills", prefix+w.name)
}

// link makes dir hold one link per want, and removes the links nerve made
// that are no longer wanted. Where the team already has a skill or agent of
// that name, it links nerve's as <owner>-<name>. It never touches what nerve
// did not make.
func (s *syncer) link(dir string, wants []want, st *state) error {
	ours := func(rel string) bool {
		fi, err := os.Lstat(filepath.Join(dir, rel))
		return err == nil && fi.Mode()&fs.ModeSymlink != 0 && st.Links[rel] != ""
	}
	taken := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(dir, rel))
		return err == nil && !ours(rel)
	}
	next := map[string]string{}
	for _, w := range wants {
		rel := w.rel("")
		if taken(rel) {
			owner, err := s.ownerName()
			if err != nil {
				return fmt.Errorf("the team has %s: %w", rel, err)
			}
			if rel = w.rel(owner + "-"); taken(rel) {
				return fmt.Errorf("the team has both %s and %s: nerve has nowhere to link %s %s", w.rel(""), rel, w.kind, w.name)
			}
		}
		path := filepath.Join(dir, rel)
		if target, err := os.Readlink(path); err != nil || target != w.target {
			if err == nil {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(w.target, path); err != nil {
				return err
			}
			s.r.add(Event{Kind: evLinked, Path: path, Target: w.target})
		}
		next[rel] = w.kind + ":" + w.name
		st.Links[rel] = next[rel] // ours from here on, should a later want collide
	}
	for _, rel := range sortedKeys(st.Links) {
		if _, ok := next[rel]; ok || !ours(rel) {
			continue
		}
		path := filepath.Join(dir, rel)
		if err := os.Remove(path); err != nil {
			return err
		}
		s.r.add(Event{Kind: evRemoved, Path: path})
	}
	st.Links = next
	return nil
}

var ownerPattern = regexp.MustCompile(`[/:]([A-Za-z0-9_.-]+)/[^/]+?(\.git)?/?$`)

// ownerName is the owner of the Brain's origin remote, such as the GitHub
// user, looked up when first needed and never stored.
func (s *syncer) ownerName() (string, error) {
	if s.owner != "" {
		return s.owner, nil
	}
	url, err := git(s.b.root, "remote", "get-url", "origin")
	if err != nil {
		return "", errors.New("the Brain has no origin remote to name the prefix nerve links yours under")
	}
	match := ownerPattern.FindStringSubmatch(trim(url))
	if match == nil {
		return "", fmt.Errorf("cannot read an owner from the Brain's remote %s", trim(url))
	}
	s.owner = strings.ToLower(match[1])
	return s.owner, nil
}

// checkIgnored refuses a working tree whose settings.local.json git tracks
// or does not ignore: nerve writes only a file the team never sees.
func checkIgnored(dir string) error {
	rel := filepath.ToSlash(localSettings)
	if _, err := git(dir, "ls-files", "--error-unmatch", "--", rel); err == nil {
		return fmt.Errorf("git tracks %s, so the team shares it: untrack it before nerve writes it", rel)
	}
	if _, err := git(dir, "check-ignore", "-q", "--", rel); err != nil {
		return fmt.Errorf("git does not ignore %s: add **/%s to your global git ignore file (~/.config/git/ignore) or to the repo's .gitignore", rel, rel)
	}
	return nil
}

const (
	excludeBegin = "# nerve: begin"
	excludeEnd   = "# nerve: end"
)

// syncExclude keeps nerve's links out of git through a marked block in the
// repo's info/exclude, which only this clone reads.
func syncExclude(dir string, rels []string) error {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	path := trim(out)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := string(old)
	if i := strings.Index(text, excludeBegin+"\n"); i >= 0 {
		if j := strings.Index(text[i:], excludeEnd+"\n"); j >= 0 {
			text = text[:i] + text[i+j+len(excludeEnd)+1:]
		}
	}
	if len(rels) > 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += excludeBegin + "\n"
		for _, rel := range rels {
			text += "/" + filepath.ToSlash(rel) + "\n"
		}
		text += excludeEnd + "\n"
	}
	if text == string(old) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFile(path, []byte(text))
}

// cleanTree takes out of a working tree what nerve put there: its links,
// its exclude block, and the settings it wrote that were not changed since.
func cleanTree(dir, why string, r *report) error {
	st, err := loadState(dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		for _, rel := range sortedKeys(st.Links) {
			path := filepath.Join(dir, rel)
			if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
				if err := os.Remove(path); err != nil {
					return err
				}
				r.add(Event{Kind: evRemoved, Path: path})
			}
		}
		if err := syncExclude(dir, nil); err != nil {
			return err
		}
		path := filepath.Join(dir, localSettings)
		current, err := readObject(path)
		if err != nil {
			return err
		}
		have, last := leaves(current), leaves(st.Settings)
		n := len(have)
		for k := range last {
			if same(have, last, k) {
				delete(have, k)
			}
		}
		switch {
		case current == nil || len(have) == n:
		case len(have) == 0:
			if err := os.Remove(path); err != nil {
				return err
			}
			r.add(Event{Kind: evRemoved, Path: path})
		default:
			if err := writeObject(path, unflatten(have)); err != nil {
				return err
			}
			r.add(Event{Kind: evWrote, Path: path})
		}
	}
	if err := dropState(dir); err != nil {
		return err
	}
	r.add(Event{Kind: evCleaned, Path: dir, Detail: why})
	return nil
}

// brainHead is the Brain's checked-out commit, "" outside git.
func brainHead(root string) string {
	out, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return trim(out)
}

// autosyncCommand is the hook command that syncs the working tree a session
// starts in.
func autosyncCommand() (string, error) {
	path, err := selfPath()
	if err != nil {
		return "", err
	}
	return shellQuote(path) + " sync --here --hook", nil
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./~+-]+$`)

func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// syncClaudeAI builds a zip per skill of each claude.ai project, and reports
// which to upload or delete on the account: those that changed since the
// last build.
func (s *syncer) syncClaudeAI() error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	path, err := statePath("claude-ai")
	if err != nil {
		return err
	}
	built := map[string]map[string]string{} // project -> skill -> zip hash
	if err := readJSON(path, &built); err != nil {
		return err
	}
	next := map[string]map[string]string{}
	for _, name := range sortedKeys(s.b.m.Projects) {
		p := s.b.m.Projects[name]
		if p.Surface != claudeAI {
			continue
		}
		dir := filepath.Join(cache, "nerve", "claude.ai", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		next[name] = map[string]string{}
		for _, n := range p.Skills {
			sk, err := s.b.skill(n)
			if err != nil {
				return err
			}
			if why := sk.claudeAIRejection(); why != "" {
				s.r.add(Event{Kind: evProblem, Path: manifestFile, Key: "project." + name, Detail: n + ": " + why})
				continue
			}
			b, err := zipDir(sk.Dir, n)
			if err != nil {
				return fmt.Errorf("zip %s: %w", n, err)
			}
			sum := sha256.Sum256(b)
			hash := hex.EncodeToString(sum[:])
			next[name][n] = hash
			zipPath := filepath.Join(dir, n+".zip")
			if built[name][n] == hash && fileExists(zipPath) {
				continue
			}
			if err := writeFile(zipPath, b); err != nil {
				return err
			}
			s.r.add(Event{Kind: evUpload, Project: name, Item: "skill " + n, Path: zipPath})
		}
	}
	for _, name := range sortedKeys(built) {
		for _, n := range sortedKeys(built[name]) {
			if _, ok := next[name][n]; ok {
				continue
			}
			os.Remove(filepath.Join(cache, "nerve", "claude.ai", name, n+".zip"))
			s.r.add(Event{Kind: evUnload, Project: name, Item: "skill " + n})
		}
	}
	return writeJSON(path, next)
}

// zipDir zips a skill folder under prefix, the same bytes for the same
// files.
func zipDir(src, prefix string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		w, err := zw.Create(prefix + "/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	})
	if err == nil {
		err = zw.Close()
	}
	return buf.Bytes(), err
}
