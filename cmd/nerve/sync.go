package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

func runSync(machine Machine, discard bool, r *report) error {
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
	if secrets := secretProblems(root); len(secrets) > 0 {
		for _, e := range secrets {
			r.add(e)
		}
		return fmt.Errorf("%d settings in the Brain may hold secrets: move them to a local overlay", len(secrets))
	}
	if err := machine.checkProfiles(m); err != nil {
		return err
	}
	self, err := installSelf()
	if err != nil {
		return err
	}
	drifted := 0
	for _, c := range machine.Configs {
		held, err := syncConfig(root, c, self, discard, r)
		if err != nil {
			return err
		}
		if held {
			drifted++
		}
	}
	if drifted > 0 {
		return fmt.Errorf("settings drifted in %d of %d configs: move each change into the Brain, or sync --discard", drifted, len(machine.Configs))
	}
	return nil
}

// syncConfig carries the Brain to one config and records what it did, even
// when a step fails part way. It returns whether drift held settings back.
func syncConfig(root string, c Config, self Skill, discard bool, r *report) (held bool, err error) {
	st, err := loadState(c.Dir)
	if err != nil {
		return false, err
	}
	defer func() {
		if serr := saveState(c.Dir, st); err == nil {
			err = serr
		}
	}()
	if err := link(filepath.Join(c.Dir, "skills"), []Skill{self}, &st.Links, r); err != nil {
		return false, err
	}
	if err := syncKeybindings(root, c.Dir, &st, r); err != nil {
		return false, err
	}
	if err := syncInstructions(root, c, r); err != nil {
		return false, err
	}
	return syncSettings(root, c, &st, discard, r)
}

// link makes dir hold exactly one link per skill, plus whatever sync did not
// make; made lists the links sync made before, and is updated, even when a
// link fails part way. It never touches a real folder.
func link(dir string, skills []Skill, made *[]string, r *report) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	record := func(name string) {
		if !slices.Contains(*made, name) {
			*made = append(*made, name)
		}
	}
	var names []string
	for _, s := range skills {
		names = append(names, s.Name)
		path := filepath.Join(dir, s.Name)
		if fi, err := os.Lstat(path); err == nil {
			if fi.Mode()&fs.ModeSymlink == 0 {
				return fmt.Errorf("%s is a real folder, not a link: move it away, then sync again", path)
			}
			if target, _ := os.Readlink(path); target == s.Dir {
				record(s.Name)
				continue
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		if err := os.Symlink(s.Dir, path); err != nil {
			return err
		}
		record(s.Name)
		r.add(Event{Kind: evLinked, Path: path, Target: s.Dir})
	}
	for _, name := range *made {
		if slices.Contains(names, name) {
			continue
		}
		path := filepath.Join(dir, name)
		if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
			r.add(Event{Kind: evRemoved, Path: path})
		}
	}
	slices.Sort(names)
	*made = names
	return nil
}
