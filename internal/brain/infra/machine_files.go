package infra

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// MachineFiles is what this machine keeps under its config folder: the
// machine file, local overlays and state.
type MachineFiles struct {
	dir string
}

// NewMachineFiles keeps this machine's files in the user's config folder.
func NewMachineFiles() (MachineFiles, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return MachineFiles{}, err
	}
	return MachineFiles{dir: filepath.Join(dir, "nerve")}, nil
}

var _ app.Machine = MachineFiles{}

func (f MachineFiles) path() string { return filepath.Join(f.dir, "config.json") }

type machineFile struct {
	Brain     string         `json:"brain"`
	Checkouts []checkoutLine `json:"checkouts,omitempty"`
}

type checkoutLine struct {
	Path    string `json:"path"`
	Project string `json:"project"`
}

// Load reads the machine file, with every path made absolute.
func (f MachineFiles) Load() (domain.Machine, error) {
	var mf machineFile
	path := f.path()
	err := readStrict(path, &mf)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Machine{}, fmt.Errorf("no machine file at %s: run nerve init", path)
	}
	if err != nil {
		return domain.Machine{}, fmt.Errorf("%s: %w", path, err)
	}
	if mf.Brain == "" {
		return domain.Machine{}, fmt.Errorf("%s: no brain", path)
	}
	m := domain.Machine{}
	if m.Brain, err = absolute(mf.Brain); err != nil {
		return m, fmt.Errorf("%s: brain %w", path, err)
	}
	for _, c := range mf.Checkouts {
		dir, err := absolute(c.Path)
		if err != nil {
			return m, fmt.Errorf("%s: checkout %w", path, err)
		}
		switch {
		case c.Project == "":
			return m, fmt.Errorf("%s: checkout %s has no project: add \"project\": \"<name>\" to it", path, c.Path)
		case !domain.NamePattern.MatchString(c.Project):
			return m, fmt.Errorf("%s: checkout %s: project %q must be lowercase words joined by hyphens", path, c.Path, c.Project)
		}
		if _, ok := m.CheckoutAt(dir); ok {
			return m, fmt.Errorf("%s: checkout %s is listed twice", path, dir)
		}
		m.Checkouts = append(m.Checkouts, domain.Checkout{Path: dir, Project: c.Project})
	}
	return m, nil
}

// absolute expands ~, resolves links, and refuses a relative path.
func absolute(path string) (string, error) {
	p, err := expandHome(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%q must be absolute or start with ~/", path)
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Clean(p), nil
}

// Save writes the machine file. A path it held already keeps how it was
// written, such as through a link; a new one is written under ~ when it can.
func (f MachineFiles) Save(m domain.Machine) error {
	written := map[string]string{} // absolute path -> as written
	var old machineFile
	if err := readStrict(f.path(), &old); err == nil {
		for _, p := range append([]string{old.Brain}, checkoutPaths(old.Checkouts)...) {
			if abs, err := absolute(p); err == nil {
				written[abs] = p
			}
		}
	}
	spell := func(p string) string {
		if w, ok := written[p]; ok {
			return w
		}
		return shortenHome(p)
	}
	mf := machineFile{Brain: spell(m.Brain)}
	for _, c := range m.Checkouts {
		mf.Checkouts = append(mf.Checkouts, checkoutLine{Path: spell(c.Path), Project: c.Project})
	}
	return writeJSON(f.path(), mf, private)
}

func checkoutPaths(lines []checkoutLine) []string {
	paths := make([]string, len(lines))
	for i, c := range lines {
		paths[i] = c.Path
	}
	return paths
}

func (f MachineFiles) Create(m domain.Machine) (string, error) {
	path := f.path()
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s exists: edit it instead", path)
	}
	return path, f.Save(m)
}

func (f MachineFiles) overlayPath(project string) string {
	return filepath.Join(f.dir, "overlays", project+".json")
}

func (f MachineFiles) Overlay(project string) (map[string]any, string, error) {
	path := f.overlayPath(project)
	obj, err := readObject(path)
	return obj, path, err
}

func (f MachineFiles) SaveOverlay(project string, settings map[string]any) (string, error) {
	path := f.overlayPath(project)
	return path, writeObject(path, settings, private)
}

func (f MachineFiles) DropOverlay(project string) (string, error) {
	path := f.overlayPath(project)
	err := os.Remove(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", err
	}
	return path, nil
}

// treeState is a working tree's state as kept on disk.
type treeState struct {
	Path     string               `json:"path"`
	Repo     string               `json:"repo,omitempty"`
	Project  string               `json:"project,omitempty"`
	Links    map[string]linkState `json:"links,omitempty"`
	Settings map[string]any       `json:"settings,omitempty"`
	Before   map[string]any       `json:"before,omitempty"`
	Personal []string             `json:"personal,omitempty"`
	Commit   string               `json:"commit,omitempty"`
}

type linkState struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Target string `json:"target"`
}

func (st treeState) domain() domain.TreeState {
	domain.Numbers(st.Settings)
	domain.Numbers(st.Before)
	d := domain.TreeState{Path: st.Path, Repo: st.Repo, Project: st.Project, Settings: st.Settings, Before: st.Before, Personal: st.Personal, Commit: st.Commit, Links: map[string]domain.Link{}}
	for rel, l := range st.Links {
		d.Links[rel] = domain.Link(l)
	}
	return d
}

func fromDomain(d domain.TreeState) treeState {
	st := treeState{Path: d.Path, Repo: d.Repo, Project: d.Project, Settings: d.Settings, Before: d.Before, Personal: d.Personal, Commit: d.Commit}
	if len(d.Links) > 0 {
		st.Links = map[string]linkState{}
		for rel, l := range d.Links {
			st.Links[rel] = linkState(l)
		}
	}
	return st
}

func (f MachineFiles) statePath(dir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	return filepath.Join(f.dir, "state", hex.EncodeToString(sum[:8])+".json")
}

func (f MachineFiles) State(dir string) (domain.TreeState, error) {
	st := treeState{Path: dir}
	if err := readJSON(f.statePath(dir), &st); err != nil {
		return domain.TreeState{}, fmt.Errorf("%w at %s: %v", app.ErrUnreadable, f.statePath(dir), err)
	}
	return st.domain(), nil
}

func (f MachineFiles) SaveState(st domain.TreeState) error {
	return writeJSON(f.statePath(st.Path), fromDomain(st), private)
}

func (f MachineFiles) DropState(dir string) error {
	if err := os.Remove(f.statePath(dir)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// statePattern matches the names statePath gives, and no other file in the
// state folder.
var statePattern = regexp.MustCompile(`^[0-9a-f]{16}\.json$`)

// States reads every tree's state it can, and says whether some file could
// not be read; State reports those to their own trees. Other files in the state folder are left alone.
func (f MachineFiles) States() ([]domain.TreeState, bool, error) {
	entries, err := os.ReadDir(filepath.Join(f.dir, "state"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	unreadable := false
	var states []domain.TreeState
	for _, e := range entries {
		if !statePattern.MatchString(e.Name()) {
			continue
		}
		file := filepath.Join(f.dir, "state", e.Name())
		var st treeState
		b, err := os.ReadFile(file)
		if err == nil {
			err = decodeJSON(b, &st)
		}
		if err == nil && st.Path == "" {
			err = errors.New("no path")
		}
		if err != nil {
			unreadable = true
			continue
		}
		states = append(states, st.domain())
	}
	return states, unreadable, nil
}
