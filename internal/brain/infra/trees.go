package infra

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// LocalTrees are the working trees of checkouts on this machine's disk, and git
// in them.
type LocalTrees struct{}

var _ app.Trees = LocalTrees{}

func (LocalTrees) Locate(dir string) (domain.Tree, error) {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return domain.Tree{}, fmt.Errorf("not in a git repo: %w", err)
	}
	common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return domain.Tree{}, err
	}
	c := filepath.Clean(trim(common))
	t := domain.Tree{Top: filepath.Clean(trim(top)), Main: filepath.Clean(trim(top)), Repo: c}
	if filepath.Base(c) == ".git" {
		t.Main = filepath.Dir(c)
	}
	return t, nil
}

func (LocalTrees) Exists(path string) bool { return fileExists(path) }

func (LocalTrees) Present(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func isLink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

func (LocalTrees) LinkTarget(path string) (string, bool) {
	target, err := os.Readlink(path)
	return target, err == nil
}

func (LocalTrees) Link(path, target string) error {
	if isLink(path) {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

func (LocalTrees) Remove(path string) error { return os.Remove(path) }

func (LocalTrees) RemoveEmptyFolder(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.IsDir() {
		return false, nil
	}
	if entries, err := os.ReadDir(path); err != nil || len(entries) > 0 {
		return false, err
	}
	return true, os.Remove(path)
}

func (LocalTrees) Entries(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

func (LocalTrees) CheckIgnored(dir string) error {
	rel := domain.LocalSettings
	_, err := git(dir, "ls-files", "--error-unmatch", "--", rel)
	switch {
	case err == nil:
		return fmt.Errorf("git tracks %s, so the team shares it: untrack it before nerve writes it", rel)
	case exitCode(err) != 1: // 1: git does not track it
		return err
	}
	_, err = git(dir, "check-ignore", "-q", "--", rel)
	switch {
	case err == nil:
		return nil
	case exitCode(err) != 1: // 1: git does not ignore it
		return err
	}
	return fmt.Errorf("git does not ignore %s: add **/%s to %s, your global git ignore file, or to the repo's .gitignore", rel, rel, globalIgnore(dir))
}

// globalIgnore is the global git ignore file git reads in dir.
func globalIgnore(dir string) string {
	if out, err := git(dir, "config", "--path", "core.excludesFile"); err == nil && trim(out) != "" {
		return shortenHome(trim(out))
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, _ := os.UserHomeDir()
		config = filepath.Join(home, ".config")
	}
	return shortenHome(filepath.Join(config, "git", "ignore"))
}

const (
	excludeBegin = "# nerve: begin"
	excludeEnd   = "# nerve: end"
)

// SyncExclude keeps rels out of git through a marked block in the repo's
// info/exclude, which only this clone reads, and all its worktrees share.
func (LocalTrees) SyncExclude(dir string, rels []string) error {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	path := trim(out)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var kept []string // the lines outside nerve's block
	begin, end := -1, -1
	lines := strings.SplitAfter(string(old), "\n")
	for i, line := range lines {
		switch strings.TrimRight(line, "\r\n") {
		case excludeBegin:
			if begin >= 0 {
				return damagedExclude(path)
			}
			begin = i
		case excludeEnd:
			if begin < 0 || end >= 0 {
				return damagedExclude(path)
			}
			end = i
		default:
			if begin < 0 || end >= 0 {
				kept = append(kept, line)
			}
		}
	}
	if begin >= 0 && end < 0 {
		return damagedExclude(path)
	}
	text := strings.Join(kept, "")
	if len(rels) > 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += excludeBegin + "\n"
		for _, rel := range rels {
			text += "/" + filepath.ToSlash(rel) + "\n"
		}
		text += excludeEnd + "\n"
	}
	if text == string(old) {
		return nil
	}
	return writeFile(path, []byte(text))
}

func damagedExclude(path string) error {
	return fmt.Errorf("%s: nerve's block must be one %q line, then its links, then one %q line: fix it by hand", path, excludeBegin, excludeEnd)
}

func (LocalTrees) ReadSettings(path string) (map[string]any, error) { return readObject(path) }

func (LocalTrees) WriteSettings(path string, settings map[string]any) error {
	return writeObject(path, settings)
}
