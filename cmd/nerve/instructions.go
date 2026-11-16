package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The block nerve owns in each config's CLAUDE.md; the rest of the file is
// the machine's own.
const (
	blockStart = "<!-- nerve: the Brain's instructions, rewritten by nerve sync -->"
	blockEnd   = "<!-- /nerve -->"
)

// syncInstructions keeps the config's CLAUDE.md importing the Brain's base
// instructions and those of its profile.
func syncInstructions(brain string, c Config, r *report) error {
	var imports []string
	for _, name := range []string{"base", c.Profile} {
		p := filepath.Join(brain, "instructions", name+".md")
		if _, err := os.Stat(p); name != "" && err == nil {
			imports = append(imports, "@"+p)
		}
	}
	path := filepath.Join(c.Dir, "CLAUDE.md")
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(b)
	own := text
	if i := strings.Index(text, blockStart); i >= 0 {
		j := strings.Index(text[i:], blockEnd)
		if j < 0 {
			return fmt.Errorf("%s: %q has no %q after it", path, blockStart, blockEnd)
		}
		own = text[:i] + strings.TrimLeft(text[i+j+len(blockEnd):], "\n")
	}
	want := own
	if len(imports) > 0 {
		block := blockStart + "\n" + strings.Join(imports, "\n") + "\n" + blockEnd + "\n"
		if own != "" {
			block += "\n"
		}
		want = block + own
	}
	if want == text || (len(imports) == 0 && b == nil) {
		return nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	if err := writeFile(path, []byte(want)); err != nil {
		return err
	}
	r.add(Event{Kind: evWrote, Path: path})
	return nil
}

// instructionsProblems lists instruction files named after no profile.
func instructionsProblems(root string, m Manifest) []Event {
	files, _ := filepath.Glob(filepath.Join(root, "instructions", "*.md"))
	var p []Event
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".md")
		if name != "base" && !slices.Contains(m.Profiles, name) {
			p = append(p, Event{Kind: evProblem, Path: filepath.Join("instructions", filepath.Base(f)), Detail: fmt.Sprintf("%q is neither base nor a profile in %s", name, manifestFile)})
		}
	}
	return p
}
