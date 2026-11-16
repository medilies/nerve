package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Skill is one skill folder, own or third-party.
type Skill struct {
	Name   string            // folder name, which is also the invoked name
	Dir    string            // absolute path of the folder
	Origin string            // "own", or the source repo
	Meta   map[string]string // frontmatter, flattened to strings
}

func (s Skill) own() bool { return s.Origin == "own" }

const overridesHeading = "## Project overrides"

// claudeAIKeys are the only frontmatter keys claude.ai accepts on upload.
var claudeAIKeys = []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// loadSkills returns own skills, then each source's pinned skills, fetching
// sources that are not cached yet. Two skills with one name is an error.
func loadSkills(root string, m Manifest) ([]Skill, error) {
	skills, err := ownSkills(root)
	if err != nil {
		return nil, err
	}
	for _, src := range m.Sources {
		tree, err := fetch(src.Repo, src.SHA)
		if err != nil {
			return nil, err
		}
		for _, name := range src.Skills {
			dir, err := findSkill(tree, name)
			if err != nil {
				return nil, fmt.Errorf("%s@%s: %w", src.Repo, src.SHA[:7], err)
			}
			s, err := newSkill(name, dir, src.Repo)
			if err != nil {
				return nil, err
			}
			skills = append(skills, s)
		}
	}
	seen := map[string]string{}
	for _, s := range skills {
		if other, ok := seen[s.Name]; ok {
			return nil, fmt.Errorf("skill %q comes from both %s and %s", s.Name, other, s.Origin)
		}
		seen[s.Name] = s.Origin
	}
	return skills, nil
}

func ownSkills(root string) ([]Skill, error) {
	entries, err := os.ReadDir(filepath.Join(root, "skills"))
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
		s, err := newSkill(e.Name(), filepath.Join(root, "skills", e.Name()), "own")
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

// findSkill returns the one folder named name that holds a SKILL.md, or the
// tree itself when the whole repo is that skill.
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
		if d.IsDir() && d.Name() == name {
			if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
				found = append(found, path)
			}
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

// readFrontmatter reads the top-level keys of a SKILL.md's YAML frontmatter.
// Folded, literal and nested values are joined into one line.
func readFrontmatter(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, errors.New("SKILL.md has no frontmatter")
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return nil, errors.New("SKILL.md frontmatter is not closed")
	}
	meta := map[string]string{}
	key := ""
	for line := range strings.SplitSeq(text[4:4+end], "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
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

// problems lists what makes a skill invalid on any surface.
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
	case len(d) > 1024:
		p = append(p, fmt.Sprintf("description is %d characters, over 1024", len(d)))
	}
	if s.own() {
		if r := s.claudeAIRejection(); r != "" {
			p = append(p, r)
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, "SKILL.md"))
		if err != nil || !strings.Contains(string(b), "\n"+overridesHeading+"\n") {
			p = append(p, "no "+overridesHeading+" section")
		}
	}
	return p
}

// claudeAIRejection says why claude.ai would refuse the skill, or "".
func (s Skill) claudeAIRejection() string {
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
