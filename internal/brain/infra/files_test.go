package infra

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/medilies/nerve/internal/brain/domain"
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

func TestWriteFileFollowsARelativeLinkFromWhereItReallyIs(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("shared/x.json"), "old")
	e.link(e.path("shared/claude/settings.local.json"), "../x.json")
	e.link(e.path("proj/.claude"), e.path("shared/claude")) // .claude is itself a link

	if err := writeFile(e.path("proj/.claude/settings.local.json"), []byte("new")); err != nil {
		t.Fatal(err)
	}

	if got := e.read(e.path("shared/x.json")); got != "new" {
		t.Errorf("the link's target holds %q", got)
	}
	if _, err := os.Stat(e.path("proj/x.json")); err == nil {
		t.Errorf("wrote proj/x.json, as if the link were relative to proj/.claude as written")
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

func TestATempFileHasItsFilesModeBeforeItHoldsAnything(t *testing.T) {
	e := newTestEnv(t)
	path := e.path("app/overlay.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	f, err := newTemp(path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if fi, err := f.Stat(); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode is %v (%v)", fi.Mode(), err)
	}
}

func TestWriteFileLeavesTheFilesBesideIt(t *testing.T) {
	e := newTestEnv(t)
	path := e.path("app/.claude/settings.local.json")
	old := time.Now().Add(-2 * time.Hour)
	for _, name := range []string{".settings.local.json.bak", ".settings.local.json.abc123"} {
		e.write(e.path("app/.claude/"+name), "{}")
		if err := os.Chtimes(e.path("app/.claude/"+name), old, old); err != nil {
			t.Fatal(err)
		}
	}

	if err := writeFile(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{".settings.local.json.bak", ".settings.local.json.abc123"} {
		if !fileExists(e.path("app/.claude/" + name)) {
			t.Errorf("%s, which nerve did not make here, is gone", name)
		}
	}
}

func TestWritePrivateIsForTheUserOnlyWhateverTheUmask(t *testing.T) {
	e := newTestEnv(t)
	defer syscall.Umask(syscall.Umask(0o022))
	path := e.path("cfg/nerve/overlays/app.json")
	e.write(path, "{}") // as an earlier nerve left it: 0644

	if err := writePrivate(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(e.path("cfg/nerve/state/x.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}

	for p, want := range map[string]os.FileMode{path: 0o600, e.path("cfg/nerve/state/x.json"): 0o600, e.path("cfg/nerve/state"): 0o700} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v (%v), want %v", p, fi.Mode().Perm(), err, want)
		}
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

	if err := writeObject(path, map[string]any{"x": math.Inf(1)}, anyone); err == nil {
		t.Errorf("wrote inf")
	}
	if got := e.read(path); got != `{"model": "opus"}` {
		t.Errorf("the file holds %q", got)
	}
}

func TestSettingsKeepTheirTextAndEveryDigit(t *testing.T) {
	e := newTestEnv(t)
	path := e.path("app/settings.json")
	e.write(path, `{"command": "a && b > c", "big": 9007199254740993, "f": 1.5}`)

	obj, err := readObject(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeObject(path, obj, anyone); err != nil {
		t.Fatal(err)
	}

	got := e.read(path)
	for _, want := range []string{`"a && b > c"`, `9007199254740993`, `1.5`} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %s:\n%s", want, got)
		}
	}
	if !reflect.DeepEqual(obj["big"], domain.ParseValue("9007199254740993")) {
		t.Errorf("a number read from a file differs from the same number typed: %T", obj["big"])
	}
}

func TestGitNeverRunsAnAskpassHelper(t *testing.T) {
	e := newTestEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	asked := e.path("asked")
	helper := e.path("askpass.sh")
	e.write(helper, "#!/bin/sh\necho asked >> "+asked+"\necho secret\n")
	if err := os.Chmod(helper, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ASKPASS", helper) // as VS Code sets it
	t.Setenv("SSH_ASKPASS", helper)
	config := e.path("gitconfig")
	e.write(config, "[core]\n\taskPass = "+helper+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", config) // as a user may set it

	_, err := git(e.home, "ls-remote", srv.URL+"/private/repo.git")

	if err == nil {
		t.Errorf("ls-remote passed")
	}
	if _, serr := os.Stat(asked); serr == nil {
		t.Errorf("git ran the askpass helper: %s", e.read(asked))
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
