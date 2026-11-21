package domain

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
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

// ParseFrontmatter reads the top-level keys of a Markdown file's YAML
// frontmatter; file names it in errors. Folded, literal and nested values,
// and lists, are joined into one line. Comments, and a byte order mark
// before the file, are left out.
func ParseFrontmatter(file, text string) (map[string]string, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if strings.TrimRight(lines[0], " \t") != "---" || len(lines) == 1 {
		return nil, fmt.Errorf("%s has no frontmatter", file)
	}
	end := slices.IndexFunc(lines[1:], func(l string) bool { return strings.TrimRight(l, " \t") == "---" })
	if end < 0 {
		return nil, fmt.Errorf("%s frontmatter is not closed", file)
	}
	meta := map[string]string{}
	key := ""
	quoted := ""   // a quoted value still open, its lines joined
	block := false // key's value is a folded or literal block, where # is text
	for _, line := range lines[1 : 1+end] {
		trimmed := strings.TrimSpace(line)
		switch {
		case quoted != "": // YAML folds the lines of a quoted value into one
			quoted += " " + trimmed
			if v, closed := scalarQuoted(quoted); closed {
				meta[key], quoted = v, ""
			}
		case trimmed == "" || strings.HasPrefix(trimmed, "#") && (!block || line[0] != ' ' && line[0] != '\t'):
		case key != "" && (line == "-" || strings.HasPrefix(line, "- ")): // a list item, which YAML lets start at column 0
			meta[key] = strings.TrimSpace(meta[key] + " " + trimmed)
		case line[0] != ' ' && line[0] != '\t':
			k, v, ok := cutKey(line)
			if !ok {
				return nil, fmt.Errorf("frontmatter line %q is not key: value", line)
			}
			if _, twice := meta[k]; twice {
				return nil, fmt.Errorf("%s frontmatter gives %q twice", file, k)
			}
			key, block = k, false
			v = strings.TrimSpace(v)
			if sv, closed := scalarQuoted(v); !closed {
				quoted = v
			} else if v = sv; strings.HasPrefix(v, ">") || strings.HasPrefix(v, "|") {
				v, block = "", true
			}
			meta[key] = v
		case key != "":
			meta[key] = strings.TrimSpace(meta[key] + " " + trimmed)
		}
	}
	if quoted != "" {
		return nil, fmt.Errorf("%s frontmatter: the quoted value of %q is not closed", file, key)
	}
	return meta, nil
}

// scalarQuoted reads a value as scalar does, and says whether it is whole:
// false for a quoted value whose closing quote is on a later line.
func scalarQuoted(v string) (string, bool) {
	if len(v) > 0 && (v[0] == '"' || v[0] == '\'') && quoteEnd(v) < 0 {
		return v, false
	}
	return scalar(v), true
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
		return unquote(line[:end+1]), rest[1:], true
	}
	k, v, ok := strings.Cut(line, ":")
	k = strings.TrimSpace(k)
	return k, v, ok && k != ""
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

// scalar reads a whole YAML value: a quoted one without its quotes, and a
// plain one without the comment after it.
func scalar(v string) string {
	if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
		return unquote(v[:quoteEnd(v)+1])
	}
	if strings.HasPrefix(v, "#") {
		return ""
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// unquote reads a quoted YAML scalar: escapes in double quotes, and a
// doubled quote in single ones.
func unquote(q string) string {
	if q[0] == '\'' {
		return strings.ReplaceAll(q[1:len(q)-1], "''", "'")
	}
	if s, err := strconv.Unquote(strings.ReplaceAll(q, `\/`, "/")); err == nil {
		return s
	}
	return q[1 : len(q)-1]
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
