package infra

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv is a temp HOME with git kept from the user's config, and
// https://github.com/ routed to local repos under ~/github.
type testEnv struct {
	t    *testing.T
	home string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	home := t.TempDir()
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	unsetRepoEnv(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("LC_ALL", "C") // git's messages, as tests match them
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file://"+filepath.Join(home, "github")+"/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/")
	return &testEnv{t: t, home: home}
}

// unsetRepoEnv keeps a test run from a git hook off the repo the hook set,
// for the test's own git commands too.
func unsetRepoEnv(t *testing.T) {
	for _, name := range repoEnv {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
}

func (e *testEnv) path(rel string) string { return filepath.Join(e.home, rel) }

// config is rel in nerve's config folder, which is not under XDG_CONFIG_HOME
// on every system.
func (e *testEnv) config(rel string) string {
	e.t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil {
		e.t.Fatal(err)
	}
	return filepath.Join(dir, "nerve", rel)
}

// cache is rel in nerve's cache folder, as config is in its config folder.
func (e *testEnv) cache(rel string) string {
	e.t.Helper()
	dir, err := os.UserCacheDir()
	if err != nil {
		e.t.Fatal(err)
	}
	return filepath.Join(dir, "nerve", rel)
}

func (e *testEnv) git(dir string, args ...string) string {
	e.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (e *testEnv) write(path, content string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) read(path string) string {
	e.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *testEnv) link(path, target string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		e.t.Fatal(err)
	}
}

// repo makes dir a git repo on main, and returns it.
func (e *testEnv) repo(dir string) string {
	e.t.Helper()
	e.git(e.home, "init", "-q", "-b", "main", dir)
	return dir
}

// commit commits everything in dir and returns the commit.
func (e *testEnv) commit(dir string) string {
	e.t.Helper()
	e.git(dir, "add", "-A")
	e.git(dir, "commit", "-q", "-m", "c")
	return e.git(dir, "rev-parse", "HEAD")
}

func skillMD(name, extra string) string {
	return "---\nname: " + name + "\ndescription: Does " + name + ".\n" + extra + "---\n\n# " + name + "\n"
}
