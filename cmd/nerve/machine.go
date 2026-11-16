package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Machine is this machine's own file, never part of a Brain: where the
// Brain is checked out, and which profile each Claude Code config follows.
type Machine struct {
	Brain   string   `toml:"brain"`
	Configs []Config `toml:"config"`
}

// Config is one Claude Code config dir and the profile it follows ("" for
// none).
type Config struct {
	Dir     string `toml:"dir"`
	Profile string `toml:"profile,omitempty"`
}

// machineDir holds the machine file and the local overlays.
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
	if keys := meta.Undecoded(); len(keys) > 0 {
		return m, fmt.Errorf("%s: unknown keys %v", path, keys)
	}
	if m.Brain == "" {
		return m, fmt.Errorf("%s: no brain", path)
	}
	if len(m.Configs) == 0 {
		return m, fmt.Errorf("%s: no configs", path)
	}
	if m.Brain, err = expandHome(m.Brain); err != nil {
		return m, err
	}
	for i := range m.Configs {
		if m.Configs[i].Dir, err = expandHome(m.Configs[i].Dir); err != nil {
			return m, err
		}
	}
	return m, nil
}

// checkProfiles fails when a config follows a profile the Brain lacks.
func (m Machine) checkProfiles(b Manifest) error {
	for _, c := range m.Configs {
		if c.Profile != "" && !slices.Contains(b.Profiles, c.Profile) {
			return fmt.Errorf("%s follows profile %q, which %s does not list", c.Dir, c.Profile, manifestFile)
		}
	}
	return nil
}

// runInit writes the machine file: args are the Brain's checkout, then one
// <config dir>[=<profile>] per Claude Code config.
func runInit(args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New(usage)
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
	b, err := loadManifest(brain)
	if err != nil {
		return err
	}
	m := Machine{Brain: brain}
	for _, arg := range args[1:] {
		dir, profile, _ := strings.Cut(arg, "=")
		m.Configs = append(m.Configs, Config{Dir: dir, Profile: profile})
	}
	if err := m.checkProfiles(b); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(m); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %s: run nerve sync\n", path)
	return f.Close()
}
