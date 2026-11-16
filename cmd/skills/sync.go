package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// stateFile records, in each config's skills dir, the links sync made, so it
// removes only its own links.
const stateFile = ".skills-sync.json"

func runSync(root string, out io.Writer) error {
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	skills, err := loadSkills(root, m)
	if err != nil {
		return err
	}
	configs, err := m.configDirs()
	if err != nil {
		return err
	}
	for _, cfg := range configs {
		if err := link(filepath.Join(cfg, "skills"), skills, out); err != nil {
			return err
		}
	}
	if err := buildZips(filepath.Join(root, "dist"), skills, out); err != nil {
		return err
	}
	return reportDrift(configs, skills, out)
}

// link makes dir hold exactly one link per skill, plus whatever sync did not
// make. It never touches a real folder.
func link(dir string, skills []Skill, out io.Writer) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var made []string
	if b, err := os.ReadFile(filepath.Join(dir, stateFile)); err == nil {
		if err := json.Unmarshal(b, &made); err != nil {
			return fmt.Errorf("%s: %w", filepath.Join(dir, stateFile), err)
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
				continue
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		if err := os.Symlink(s.Dir, path); err != nil {
			return err
		}
		fmt.Fprintf(out, "linked   %s -> %s\n", path, s.Dir)
	}
	for _, name := range made {
		if slices.Contains(names, name) {
			continue
		}
		path := filepath.Join(dir, name)
		if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
			fmt.Fprintf(out, "removed  %s\n", path)
		}
	}
	slices.Sort(names)
	b, _ := json.MarshalIndent(names, "", "  ")
	return os.WriteFile(filepath.Join(dir, stateFile), append(b, '\n'), 0o644)
}

// buildZips writes dist/<name>.zip, ready to upload to claude.ai, for every
// skill claude.ai accepts.
func buildZips(dist string, skills []Skill, out io.Writer) error {
	if err := os.RemoveAll(dist); err != nil {
		return err
	}
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return err
	}
	for _, s := range skills {
		if r := s.claudeAIRejection(); r != "" {
			fmt.Fprintf(out, "skipped  %s: Claude Code only (%s)\n", s.Name, r)
			continue
		}
		if err := zipDir(filepath.Join(dist, s.Name+".zip"), s.Dir, s.Name); err != nil {
			return fmt.Errorf("zip %s: %w", s.Name, err)
		}
	}
	return nil
}

func zipDir(dest, src, prefix string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return skipGit(d, err)
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
	if err != nil {
		return err
	}
	return zw.Close()
}

// reportDrift compares each skill with the copy claude.ai syncs down into
// each config, and says which zips to upload.
func reportDrift(configs []string, skills []Skill, out io.Writer) error {
	for _, cfg := range configs {
		synced, err := syncedSkills(filepath.Join(cfg, "skills", "synced"))
		if err != nil {
			return err
		}
		if synced == nil {
			fmt.Fprintf(out, "claude.ai (account of %s): no synced skills, skipped\n", cfg)
			continue
		}
		var lines []string
		for _, s := range skills {
			if s.claudeAIRejection() != "" {
				continue
			}
			copies := synced[s.Name]
			switch {
			case len(copies) == 0:
				lines = append(lines, "  missing  "+s.Name+": upload dist/"+s.Name+".zip")
			case !slices.ContainsFunc(copies, func(c string) bool { return holds(c, s.Dir) }):
				lines = append(lines, "  stale    "+s.Name+": upload dist/"+s.Name+".zip")
			}
		}
		if len(lines) == 0 {
			fmt.Fprintf(out, "claude.ai (account of %s): up to date\n", cfg)
			continue
		}
		fmt.Fprintf(out, "claude.ai (account of %s):\n%s\n", cfg, strings.Join(lines, "\n"))
	}
	return nil
}

// syncedSkills maps names to their folders in synced/<bucket>/<name>/ (one
// per bucket that has the name), or returns nil when the config has no
// synced skills.
func syncedSkills(dir string) (map[string][]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*", "*", "SKILL.md"))
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	found := map[string][]string{}
	for _, m := range matches {
		skillDir := filepath.Dir(m)
		if name := filepath.Base(skillDir); !strings.HasPrefix(name, ".") {
			found[name] = append(found[name], skillDir)
		}
	}
	return found, nil
}

// holds reports whether the claude.ai copy has every file of the skill,
// unchanged. Files claude.ai adds to its copy are ignored.
func holds(copyDir, skillDir string) bool {
	want, errW := treeHashes(skillDir)
	got, errG := treeHashes(copyDir)
	if errW != nil || errG != nil {
		return false
	}
	for k, v := range want {
		if h, ok := got[k]; !ok || h != v {
			return false
		}
	}
	return true
}

func treeHashes(dir string) (map[string][32]byte, error) {
	hashes := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return skipGit(d, err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		hashes[rel] = sha256.Sum256(b)
		return nil
	})
	return hashes, err
}

// skipGit keeps a walk out of .git folders: a skill that is a whole repo
// checkout ships without its history.
func skipGit(d fs.DirEntry, err error) error {
	if err == nil && d.Name() == ".git" {
		return filepath.SkipDir
	}
	return err
}
