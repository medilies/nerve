package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Skill is one skill folder.
type Skill struct {
	Name   string            // its name in the Brain
	Dir    string            // where its folder is
	Origin string            // "own", "nerve", or the source repo
	Meta   map[string]string // frontmatter, flattened to strings
}

// claudeAIKeys are the only frontmatter keys claude.ai accepts on upload.
var claudeAIKeys = []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}

// Problems lists what makes a skill invalid.
func (s Skill) Problems() []string {
	var p []string
	if s.Meta["name"] != s.Name {
		p = append(p, fmt.Sprintf("frontmatter name %q differs from folder name", s.Meta["name"]))
	}
	if !NamePattern.MatchString(s.Name) || len(s.Name) > 64 {
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

// ClaudeAIRejection says which of the skill's frontmatter keys claude.ai
// would refuse, or "".
func (s Skill) ClaudeAIRejection() string {
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

// ParseFrontmatter reads the top-level keys of a Markdown file's YAML
// frontmatter; file names it in errors. Folded, literal and nested values,
// and lists, are joined into one line. Comments, and a byte order mark
// before the file, are left out.
func ParseFrontmatter(file, text string) (map[string]string, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if lines[0] != "---" || len(lines) == 1 {
		return nil, fmt.Errorf("%s has no frontmatter", file)
	}
	end := slices.IndexFunc(lines[1:], func(l string) bool { return strings.TrimRight(l, " \t") == "---" })
	if end < 0 {
		return nil, fmt.Errorf("%s frontmatter is not closed", file)
	}
	meta := map[string]string{}
	key := ""
	for _, line := range lines[1 : 1+end] {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		case key != "" && (line == "-" || strings.HasPrefix(line, "- ")): // a list item, which YAML lets start at column 0
			meta[key] = strings.TrimSpace(meta[key] + " " + trimmed)
		case line[0] != ' ' && line[0] != '\t':
			k, v, ok := cutKey(line)
			if !ok {
				return nil, fmt.Errorf("frontmatter line %q is not key: value", line)
			}
			key = k
			v = scalar(strings.TrimSpace(v))
			if strings.HasPrefix(v, ">") || strings.HasPrefix(v, "|") {
				v = ""
			}
			meta[key] = v
		case key != "":
			meta[key] = strings.TrimSpace(meta[key] + " " + trimmed)
		}
	}
	return meta, nil
}

// cutKey splits a "key: value" line, where the key may be quoted.
func cutKey(line string) (key, value string, ok bool) {
	if q := line[0]; q == '"' || q == '\'' {
		end := quoteEnd(line)
		if end < 0 {
			return "", "", false
		}
		rest := strings.TrimLeft(line[end+1:], " \t")
		if !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		return line[1:end], rest[1:], true
	}
	k, v, ok := strings.Cut(line, ":")
	return strings.TrimSpace(k), v, ok
}

// quoteEnd is the index of the quote that closes the one v starts with, or
// -1.
func quoteEnd(v string) int {
	for i := 1; i < len(v); i++ {
		switch {
		case v[0] == '"' && v[i] == '\\', v[0] == '\'' && v[i] == '\'' && i+1 < len(v) && v[i+1] == '\'':
			i++ // an escaped character
		case v[i] == v[0]:
			return i
		}
	}
	return -1
}

// scalar reads a YAML value on one line: a quoted one without its quotes,
// and a plain one without the comment after it.
func scalar(v string) string {
	if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
		if end := quoteEnd(v); end > 0 {
			return v[1:end]
		}
		return v
	}
	if strings.HasPrefix(v, "#") {
		return ""
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// AgentProblem says what is wrong with an agent file's frontmatter, or nil.
func AgentProblem(name string, meta map[string]string) error {
	switch {
	case meta["name"] != name:
		return fmt.Errorf("frontmatter name %q differs from file name", meta["name"])
	case meta["description"] == "":
		return errors.New("no description")
	}
	return nil
}
