package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// selfSkill is the nerve skill, carried by the binary so the two always
// match.
//
//go:embed skill/SKILL.md
var selfSkill []byte

const selfOrigin = "nerve"

// installSelf writes the binary's skill into the user cache, where sync links
// it from like any other skill.
func installSelf() (Skill, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return Skill{}, err
	}
	dir := filepath.Join(cache, "nerve", "skill", "nerve")
	path := filepath.Join(dir, "SKILL.md")
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, selfSkill) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Skill{}, err
		}
		if err := os.WriteFile(path, selfSkill, 0o644); err != nil {
			return Skill{}, err
		}
	}
	return newSkill("nerve", dir, selfOrigin)
}

// withSelf adds the nerve skill to the Brain's skills, which must not have
// one of that name.
func withSelf(skills []Skill) ([]Skill, error) {
	self, err := installSelf()
	if err != nil {
		return nil, err
	}
	for _, s := range skills {
		if s.Name == self.Name {
			return nil, fmt.Errorf("skill %q from %s takes the name of nerve's own skill: rename it", s.Name, s.Origin)
		}
	}
	return append(skills, self), nil
}
