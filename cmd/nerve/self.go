package main

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
)

// selfSkill is the nerve skill, carried by the binary so the two always
// match.
//
//go:embed skill/SKILL.md
var selfSkill []byte

// installSelf writes the binary's skill into the user cache, where sync links
// it from.
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
	return newSkill("nerve", dir, "nerve")
}
