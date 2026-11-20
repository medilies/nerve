package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"
)

// Machine is this machine's own file, never part of a Brain: where the Brain
// is checked out, and which project each checkout belongs to.
type Machine struct {
	Brain     string     `toml:"brain"`
	Checkouts []Checkout `toml:"checkout,omitempty"`
}

// Checkout is one project's working tree on this machine.
type Checkout struct {
	Path    string `toml:"path"`
	Project string `toml:"project"`
}

// machineDir holds the machine file, the local overlays and the state.
func machineDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nerve"), nil
}

func machinePath() (string, error) {
	dir, err := machineDir()
	return filepath.Join(dir, "config.toml"), err
}

// loadMachine reads the machine file, with every path made absolute.
func loadMachine() (Machine, error) {
	var m Machine
	path, err := machinePath()
	if err != nil {
		return m, err
	}
	meta, err := toml.DecodeFile(path, &m)
	if errors.Is(err, fs.ErrNotExist) {
		return m, fmt.Errorf("no machine file at %s: run nerve init", path)
	}
	if err != nil {
		return m, fmt.Errorf("%s: %w", path, err)
	}
	for _, k := range meta.Undecoded() {
		if k[0] == "config" {
			return m, fmt.Errorf("%s: [[config]] is gone since nerve v0.3, which carries nothing to user level: remove it", path)
		}
		return m, fmt.Errorf("%s: unknown key %v", path, k)
	}
	if m.Brain == "" {
		return m, fmt.Errorf("%s: no brain", path)
	}
	if m.Brain, err = absolute(m.Brain); err != nil {
		return m, fmt.Errorf("%s: brain %w", path, err)
	}
	for i := range m.Checkouts {
		c := &m.Checkouts[i]
		if c.Path, err = absolute(c.Path); err != nil {
			return m, fmt.Errorf("%s: checkout %w", path, err)
		}
		if slices.ContainsFunc(m.Checkouts[:i], func(o Checkout) bool { return o.Path == c.Path }) {
			return m, fmt.Errorf("%s: checkout %s is listed twice", path, c.Path)
		}
	}
	return m, nil
}

// absolute expands ~ and refuses a relative path.
func absolute(path string) (string, error) {
	p, err := expandHome(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%q must be absolute or start with ~/", path)
	}
	return filepath.Clean(p), nil
}

func saveMachine(m Machine) error {
	path, err := machinePath()
	if err != nil {
		return err
	}
	out := Machine{Brain: shortenHome(m.Brain)}
	for _, c := range m.Checkouts {
		out.Checkouts = append(out.Checkouts, Checkout{Path: shortenHome(c.Path), Project: c.Project})
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(out); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFile(path, buf.Bytes())
}

// checkout returns the checkout at path, if listed.
func (m Machine) checkout(path string) (Checkout, bool) {
	i := slices.IndexFunc(m.Checkouts, func(c Checkout) bool { return samePath(c.Path, path) })
	if i < 0 {
		return Checkout{}, false
	}
	return m.Checkouts[i], true
}

func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// runInit writes the machine file, naming the Brain's checkout.
func runInit(args []string, r *report) error {
	if len(args) != 1 {
		return errUsage
	}
	path, err := machinePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s exists: edit it instead", path)
	}
	brain, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	if _, err := loadManifest(brain); err != nil {
		return err
	}
	if err := saveMachine(Machine{Brain: brain}); err != nil {
		return err
	}
	r.add(Event{Kind: evWrote, Path: path})
	return nil
}

// tree is the git working tree around the working directory: where nerve
// writes (Top), and the main checkout it belongs to (Main), which differs in
// a linked worktree.
type tree struct{ Top, Main string }

func locateTree(dir string) (tree, error) {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return tree{}, fmt.Errorf("not in a git repo: %w", err)
	}
	common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return tree{}, err
	}
	t := tree{Top: filepath.Clean(trim(top)), Main: filepath.Clean(trim(top))}
	if c := filepath.Clean(trim(common)); filepath.Base(c) == ".git" {
		t.Main = filepath.Dir(c)
	}
	return t, nil
}

// here returns the tree around the working directory and the project its
// checkout is listed under.
func (m Machine) here() (tree, string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return tree{}, "", err
	}
	t, err := locateTree(wd)
	if err != nil {
		return t, "", err
	}
	for _, p := range []string{t.Top, t.Main} {
		if c, ok := m.checkout(p); ok {
			return t, c.Project, nil
		}
	}
	return t, "", fmt.Errorf("%s is not a listed checkout: run nerve project add <name> here, or pass --project", t.Top)
}

// projectFor names the project a command acts on: the --project flag, else
// the one around the working directory.
func (m Machine) projectFor(f flags) (string, error) {
	if f.project != "" {
		return f.project, nil
	}
	_, name, err := m.here()
	return name, err
}
