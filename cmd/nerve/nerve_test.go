package main

import (
	"path/filepath"
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
