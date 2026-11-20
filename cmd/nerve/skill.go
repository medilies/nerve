package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Skill is one skill folder.
type Skill struct {
	Name   string            // its name in the Brain
	Dir    string            // absolute path of the folder
	Origin string            // "own", or the source repo
	Meta   map[string]string // frontmatter, flattened to strings
}

// claudeAIKeys are the only frontmatter keys claude.ai accepts on upload.
var claudeAIKeys = []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}

// brain resolves what a Brain offers by name, fetching a third-party source
// only when one of its skills is needed.
type brain struct {
	root string
	m    Manifest
}

func (b brain) ownSkillDir(name string) string { return filepath.Join(b.root, "skills", name) }
func (b brain) agentFile(name string) string   { return filepath.Join(b.root, "agents", name+".md") }

// source returns the source that lists a skill, if any.
func (b brain) source(name string) (Source, bool) {
	for _, s := range b.m.Sources {
		if slices.Contains(s.Skills, name) {
			return s, true
		}
	}
	return Source{}, false
}

// hasSkill reports whether the Brain offers a skill, without fetching.
func (b brain) hasSkill(name string) bool {
	if _, err := os.Stat(filepath.Join(b.ownSkillDir(name), "SKILL.md")); err == nil {
		return true
	}
	_, ok := b.source(name)
	return ok
}

func (b brain) hasAgent(name string) bool {
	_, err := os.Stat(b.agentFile(name))
	return err == nil
}

// skill returns a skill by name: an own one, else a third-party one at its
// pin.
func (b brain) skill(name string) (Skill, error) {
	if dir := b.ownSkillDir(name); fileExists(filepath.Join(dir, "SKILL.md")) {
		return newSkill(name, dir, "own")
	}
	src, ok := b.source(name)
	if !ok {
		return Skill{}, fmt.Errorf("the Brain has no skill %q", name)
	}
	tree, err := fetch(src.Repo, src.SHA)
	if err != nil {
		return Skill{}, err
	}
	dir, err := findSkill(tree, name)
	if err != nil {
		return Skill{}, fmt.Errorf("%s@%s: %w", src.Repo, src.SHA[:7], err)
	}
	return newSkill(name, dir, src.Repo)
}

// ownSkills returns the Brain's own skills, one per folder.
func (b brain) ownSkills() ([]Skill, error) {
	dir := filepath.Join(b.root, "skills")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := newSkill(e.Name(), filepath.Join(dir, e.Name()), "own")
		if err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	return skills, nil
}

func newSkill(name, dir, origin string) (Skill, error) {
	meta, err := readFrontmatter(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, fmt.Errorf("skill %s: %w", name, err)
	}
	return Skill{Name: name, Dir: dir, Origin: origin, Meta: meta}, nil
}

// findSkill finds a skill in a repo checkout: the whole repo when its root
// SKILL.md names it, else the one folder of that name holding a SKILL.md.
func findSkill(tree, name string) (string, error) {
	if meta, err := readFrontmatter(filepath.Join(tree, "SKILL.md")); err == nil && meta["name"] == name {
		return tree, nil
	}
	var found []string
	err := filepath.WalkDir(tree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() && d.Name() == name && fileExists(filepath.Join(path, "SKILL.md")) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no skill %q", name)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("skill %q is ambiguous: %v", name, found)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readFrontmatter reads the top-level keys of a Markdown file's YAML
// frontmatter. Folded, literal and nested values, and lists, are joined into
// one line.
func readFrontmatter(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("%s has no frontmatter", filepath.Base(path))
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return nil, fmt.Errorf("%s frontmatter is not closed", filepath.Base(path))
	}
	meta := map[string]string{}
	key := ""
	for line := range strings.SplitSeq(text[4:4+end], "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		case key != "" && (line == "-" || strings.HasPrefix(line, "- ")): // a list item, which YAML lets start at column 0
			meta[key] = strings.TrimSpace(meta[key] + " " + trimmed)
		case line[0] != ' ' && line[0] != '\t':
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				return nil, fmt.Errorf("frontmatter line %q is not key: value", line)
			}
			key = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if strings.HasPrefix(v, ">") || strings.HasPrefix(v, "|") {
				v = ""
			}
			meta[key] = unquote(v)
		case key != "":
			meta[key] = strings.TrimSpace(meta[key] + " " + trimmed)
		}
	}
	return meta, nil
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// problems lists what makes a skill invalid.
func (s Skill) problems() []string {
	var p []string
	if s.Meta["name"] != s.Name {
		p = append(p, fmt.Sprintf("frontmatter name %q differs from folder name", s.Meta["name"]))
	}
	if !namePattern.MatchString(s.Name) || len(s.Name) > 64 {
		p = append(p, "name must be lowercase words joined by hyphens, at most 64 characters")
	}
	switch d := s.Meta["description"]; {
	case d == "":
		p = append(p, "no description")
	case utf8.RuneCountInString(d) > 1024:
		p = append(p, fmt.Sprintf("description is %d characters, over 1024", utf8.RuneCountInString(d)))
	}
	return p
}

// claudeAIRejection says why claude.ai would refuse the skill's zip, or "".
func (s Skill) claudeAIRejection() string {
	if link := folderLink(s.Dir); link != "" {
		return link + " links to a folder or to nothing, which a zip cannot hold"
	}
	var bad []string
	for k := range s.Meta {
		if !slices.Contains(claudeAIKeys, k) {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return ""
	}
	slices.Sort(bad)
	return "frontmatter keys claude.ai rejects: " + strings.Join(bad, ", ")
}

// folderLink returns the first link in dir, relative to it, that leads to a
// folder or to nothing, or "". Links to files are fine: zips hold what they
// point to.
func folderLink(dir string) string {
	found := ""
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipAll
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(path); err != nil || fi.IsDir() {
				found, _ = filepath.Rel(dir, path)
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}
