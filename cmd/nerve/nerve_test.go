package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/infra"
)

func TestNervesOwnSkillMeetsTheSkillRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	s, err := infra.Nerve{SkillMD: selfSkill}.Skill()
	if err != nil {
		t.Fatal(err)
	}
	if p := s.Problems(); len(p) > 0 {
		t.Errorf("the nerve skill breaks the rules: %v", p)
	}
}

func TestAnErrorReachesTheTerminalWithNoControlCharacter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	var stdout, stderr strings.Builder

	if code := cli([]string{"init", filepath.Join(home, "brain\x1b]0;owned\x07")}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "has no brain.toml") || strings.ContainsAny(stderr.String(), "\x1b\x07") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}

// failingWriter is a stdout that is closed.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestAReportThatCannotBePrintedExitsOne(t *testing.T) {
	var stderr strings.Builder

	if code := cli([]string{"help"}, failingWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "broken pipe") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}
