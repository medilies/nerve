package infra

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileReplacesWhatALinkPointsTo(t *testing.T) {
	e := newTestEnv(t)
	real := e.path("dotfiles/settings.json")
	e.write(real, "old")
	link := e.path("app/.claude/settings.local.json")
	e.link(link, "../../dotfiles/settings.json")

	if err := writeFile(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if !isLink(link) || e.read(real) != "new" {
		t.Errorf("the link was replaced, or its file holds %q", e.read(real))
	}
}

func TestWriteFileCreatesTheFileADanglingLinkPointsTo(t *testing.T) {
	e := newTestEnv(t)
	real := e.path("dotfiles/app/settings.json")
	link := e.path("app/.claude/settings.local.json")
	e.link(link, real)

	if err := writeFile(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if !isLink(link) || e.read(real) != "new" {
		t.Errorf("the link was replaced by a file")
	}
}

func TestWriteFileKeepsTheFilesMode(t *testing.T) {
	e := newTestEnv(t)
	path := e.path("app/settings.json")
	e.write(path, "old")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode is %v (%v)", fi.Mode(), err)
	}
}

func TestWriteFileFailsWithoutTouchingTheFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes in a read-only folder")
	}
	e := newTestEnv(t)
	dir := e.path("app")
	e.write(filepath.Join(dir, "settings.json"), "old")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := writeFile(filepath.Join(dir, "settings.json"), []byte("new")); err == nil {
		t.Errorf("wrote in a read-only folder")
	}
	if got := e.read(filepath.Join(dir, "settings.json")); got != "old" {
		t.Errorf("the file holds %q", got)
	}
}

func TestWriteObjectRefusesWhatJSONCannotHoldAndKeepsTheFile(t *testing.T) {
	e := newTestEnv(t)
	path := e.path("app/settings.json")
	e.write(path, `{"model": "opus"}`)

	if err := writeObject(path, map[string]any{"x": math.Inf(1)}); err == nil {
		t.Errorf("wrote inf")
	}
	if got := e.read(path); got != `{"model": "opus"}` {
		t.Errorf("the file holds %q", got)
	}
}

func TestGitIgnoresTheRepoAGitHookPointsAt(t *testing.T) {
	e := newTestEnv(t)
	outer := e.repo(e.path("outer"))
	inner := e.repo(e.path("inner"))
	t.Setenv("GIT_DIR", filepath.Join(outer, ".git"))

	top, err := git(inner, "rev-parse", "--show-toplevel")
	if err != nil || trim(top) != inner {
		t.Errorf("git ran in %q (%v), want %s", trim(top), err, inner)
	}
}
