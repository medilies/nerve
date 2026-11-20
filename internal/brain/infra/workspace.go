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

// LocalWorkspace is project working trees on this machine's disk, and git
// in them.
type LocalWorkspace struct{}

var _ app.Workspace = LocalWorkspace{}

func (LocalWorkspace) Locate(dir string) (domain.Tree, error) {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return domain.Tree{}, fmt.Errorf("not in a git repo: %w", err)
	}
	common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return domain.Tree{}, err
	}
	t := domain.Tree{Top: filepath.Clean(trim(top)), Main: filepath.Clean(trim(top))}
	if c := filepath.Clean(trim(common)); filepath.Base(c) == ".git" {
		t.Main = filepath.Dir(c)
	}
	return t, nil
}

func (LocalWorkspace) Exists(path string) bool { return fileExists(path) }

func (LocalWorkspace) Present(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (LocalWorkspace) IsLink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

func (LocalWorkspace) LinkTarget(path string) (string, bool) {
	target, err := os.Readlink(path)
	return target, err == nil
}

func (w LocalWorkspace) Link(path, target string) error {
	if w.IsLink(path) {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

func (LocalWorkspace) Remove(path string) error { return os.Remove(path) }

func (LocalWorkspace) Entries(dir string) []string {
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

func (LocalWorkspace) CheckIgnored(dir string) error {
	rel := domain.LocalSettings
	if _, err := git(dir, "ls-files", "--error-unmatch", "--", rel); err == nil {
		return fmt.Errorf("git tracks %s, so the team shares it: untrack it before nerve writes it", rel)
	}
	if _, err := git(dir, "check-ignore", "-q", "--", rel); err != nil {
		return fmt.Errorf("git does not ignore %s: add **/%s to your global git ignore file (~/.config/git/ignore) or to the repo's .gitignore", rel, rel)
	}
	return nil
}

const (
	excludeBegin = "# nerve: begin"
	excludeEnd   = "# nerve: end"
)

// SyncExclude keeps rels out of git through a marked block in the repo's
// info/exclude, which only this clone reads.
func (LocalWorkspace) SyncExclude(dir string, rels []string) error {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	path := trim(out)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := string(old)
	if i := strings.Index(text, excludeBegin+"\n"); i >= 0 {
		if j := strings.Index(text[i:], excludeEnd+"\n"); j >= 0 {
			text = text[:i] + text[i+j+len(excludeEnd)+1:]
		}
	}
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

func (LocalWorkspace) ReadSettings(path string) (map[string]any, error) { return readObject(path) }

func (LocalWorkspace) WriteSettings(path string, settings map[string]any) error {
	return writeObject(path, settings)
}
