package infra

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// RanAs is the running binary as it was run, such as bin/nerve linking
// v1/nerve, so a hook calling it follows the link to the next version. It
// is the binary itself when the name it was run by leads elsewhere.
func RanAs() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	name := os.Args[0]
	if !strings.ContainsRune(name, filepath.Separator) {
		if name, err = exec.LookPath(name); err != nil {
			return exe, nil
		}
	}
	name, err = filepath.Abs(name)
	if err != nil {
		return exe, nil
	}
	ran, err := os.Stat(name)
	if err != nil {
		return exe, nil
	}
	if self, err := os.Stat(exe); err != nil || !os.SameFile(ran, self) {
		return exe, nil
	}
	return name, nil
}

// Nerve is the running nerve: its embedded skill, and the path it was run
// by.
type Nerve struct {
	SkillMD []byte
	Path    func() (string, error)
}

var _ app.Self = Nerve{}

// Skill writes the binary's skill into the user cache, where sync links it
// from, so the two always match.
func (n Nerve) Skill() (domain.Skill, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return domain.Skill{}, err
	}
	dir := filepath.Join(cache, "nerve", "skill", "nerve")
	path := filepath.Join(dir, "SKILL.md")
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, n.SkillMD) {
		if err := writeFile(path, n.SkillMD); err != nil { // at once: another session may read it
			return domain.Skill{}, err
		}
	}
	return readSkill("nerve", dir, "nerve")
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./~+-]+$`)

func (n Nerve) Command() (string, error) {
	path, err := n.Path()
	if err != nil {
		return "", err
	}
	if !shellSafe.MatchString(path) {
		path = "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	}
	return path + " status --hook", nil
}
