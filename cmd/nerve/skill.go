package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Skill is one skill folder.
type Skill struct {
	Name string            // folder name, which is also the invoked name
	Dir  string            // absolute path of the folder
	Meta map[string]string // frontmatter, flattened to strings
}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ownSkills returns the skills in dir, one per folder.
func ownSkills(dir string) ([]Skill, error) {
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
		s, err := newSkill(e.Name(), filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	return skills, nil
}

func newSkill(name, dir string) (Skill, error) {
	meta, err := readFrontmatter(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, fmt.Errorf("skill %s: %w", name, err)
	}
	return Skill{Name: name, Dir: dir, Meta: meta}, nil
}

// readFrontmatter reads the top-level keys of a SKILL.md's YAML frontmatter.
// Folded, literal and nested values, and lists, are joined into one line.
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
