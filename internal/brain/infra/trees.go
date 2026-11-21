package infra

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

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
		if _, serr := os.Stat(dir); errors.Is(serr, fs.ErrNotExist) {
			return domain.Tree{}, fmt.Errorf("%s: no such folder", dir)
		}
		return domain.Tree{}, fmt.Errorf("not in a git repo: %w", err)
	}
	common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return domain.Tree{}, err
	}
	return domain.Tree{Top: filepath.Clean(trim(top)), Repo: filepath.Clean(trim(common))}, nil
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
	return fmt.Errorf("git does not ignore %s: add **/%s to %s, your global git ignore file", rel, rel, globalIgnore(dir))
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

// SyncExclude keeps the paths wanted returns out of git, through a marked
// block in the repo's info/exclude, which only this clone reads, and all
// its worktrees share. Syncs of two worktrees at once take turns, so
// neither writes a block that lacks the other's links.
func (LocalTrees) SyncExclude(dir string, wanted func(current []string) ([]string, error)) error {
	// The common git folder, where info/ is for every worktree: git would
	// give info/exclude with its links followed.
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	path := filepath.Join(trim(out), "info", "exclude")
	for p, what := range map[string]string{filepath.Dir(path): "folder", path: "file"} {
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a link, so nerve would write outside the repo: make it a plain %s", p, what)
		}
	}
	unlock, err := lockFolder(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer unlock()
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var kept []string    // the lines outside nerve's block
	var current []string // the paths in it
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
			} else if rel, ours := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "/"); ours && rel != "" { // as nerve writes its lines
				current = append(current, rel)
			}
		}
	}
	if begin >= 0 && end < 0 {
		return damagedExclude(path)
	}
	rels, err := wanted(current)
	if err != nil || slices.Equal(rels, current) { // as it is: written as the user left it
		return err
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

// lockFolder holds a lock on dir until unlock, waiting for another run
// that holds it. It locks the folder, not a file in it: writeFile replaces
// files, and a lock on a replaced one would hold nothing.
func lockFolder(dir string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", dir, err)
	}
	return func() { _ = f.Close() }, nil // closing it unlocks it
}

func damagedExclude(path string) error {
	return fmt.Errorf("%s: nerve's block must be one %q line, then its links, then one %q line: fix it by hand", path, excludeBegin, excludeEnd)
}

func (LocalTrees) ReadSettings(path string) (map[string]any, error) { return readObject(path) }

func (LocalTrees) Frontmatter(path string) (map[string]string, error) { return readFrontmatter(path) }

func (LocalTrees) WriteSettings(path string, settings map[string]any) error {
	return writeObject(path, settings, mine)
}
