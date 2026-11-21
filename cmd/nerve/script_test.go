package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// The CLI's end-to-end tests are the scripts in testdata/script. Each runs
// the real nerve, git and files on a temp HOME, set up by setupWorld.

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"nerve": main})
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/script",
		Setup:               setupWorld,
		Condition:           openBugs,
		RequireExplicitExec: true,
		RequireUniqueNames:  true,
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"link":    cmdLink,
			"perm":    cmdPerm,
			"replace": cmdReplace,
		},
	})
}

// setupWorld makes $WORK a HOME holding a Brain at brain, whose remote is
// git@github.com:me/brain.git, and a project checkout at proj, listed
// nowhere yet. A script's own files are written first, so it can give the
// Brain a brain.toml, skills and agents. Git reads no config of the user's,
// and https://github.com/ is routed to local repos under $WORK/github.
func setupWorld(env *testscript.Env) error {
	home, err := filepath.EvalSymlinks(env.WorkDir)
	if err != nil {
		return err
	}
	for k, v := range map[string]string{
		"HOME":                home,
		"XDG_CACHE_HOME":      filepath.Join(home, ".cache"),
		"XDG_CONFIG_HOME":     filepath.Join(home, ".config"),
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_COUNT":    "3",
		"GIT_CONFIG_KEY_0":    "user.name",
		"GIT_CONFIG_VALUE_0":  "t",
		"GIT_CONFIG_KEY_1":    "user.email",
		"GIT_CONFIG_VALUE_1":  "t@t",
		"GIT_CONFIG_KEY_2":    "url.file://" + filepath.Join(home, "github") + "/.insteadOf",
		"GIT_CONFIG_VALUE_2":  "https://github.com/",
	} {
		env.Setenv(k, v)
	}
	env.Cd = home
	// As Claude Code does the first time it writes a settings.local.json.
	if err := writeNew(filepath.Join(home, ".config", "git", "ignore"), "**/.claude/settings.local.json\n"); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(home, "brain", "brain.toml"), ""); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(home, "proj", "README"), "x"); err != nil {
		return err
	}
	for _, c := range []struct {
		dir  string
		args []string
	}{
		{"brain", []string{"init", "-q", "-b", "main"}},
		{"proj", []string{"init", "-q", "-b", "main"}},
		{"proj", []string{"add", "-A"}},
		{"proj", []string{"commit", "-q", "-m", "a"}},
	} {
		cmd := exec.Command("git", c.args...)
		cmd.Dir = filepath.Join(home, c.dir)
		cmd.Env = env.Vars
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", c.args, err, out)
		}
	}
	return nil
}

// writeNew writes path unless the script already gave it.
func writeNew(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// openBugs is the condition [bugs]: a script of a bug not fixed yet starts
// with "[!bugs] skip", so it runs only when NERVE_OPEN_BUGS is set. Fixing
// the bug deletes that line.
func openBugs(cond string) (bool, error) {
	if cond != "bugs" {
		return false, fmt.Errorf("unknown condition %q", cond)
	}
	return os.Getenv("NERVE_OPEN_BUGS") != "", nil
}

// cmdLink checks that path is a link to target: link path target. Negated,
// it checks that path is no link: ! link path.
func cmdLink(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		if len(args) != 1 {
			ts.Fatalf("usage: ! link path")
		}
		if got, err := os.Readlink(ts.MkAbs(args[0])); err == nil {
			ts.Fatalf("%s links to %s", args[0], got)
		}
		return
	}
	if len(args) != 2 {
		ts.Fatalf("usage: link path target")
	}
	got, err := os.Readlink(ts.MkAbs(args[0]))
	ts.Check(err)
	if got != args[1] {
		ts.Fatalf("%s links to %s, want %s", args[0], got, args[1])
	}
}

// cmdPerm checks a file's permission bits, in octal: perm path 600.
func cmdPerm(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: perm path mode")
	}
	want, err := strconv.ParseUint(args[1], 8, 32)
	ts.Check(err)
	info, err := os.Stat(ts.MkAbs(args[0]))
	ts.Check(err)
	if got := info.Mode().Perm(); uint64(got) != want {
		ts.Fatalf("%s has mode %o, want %s", args[0], got, args[1])
	}
}

// cmdReplace edits a file as a person would: replace path old new. Every
// old is replaced, and there must be one.
func cmdReplace(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 3 {
		ts.Fatalf("usage: replace path old new")
	}
	path := ts.MkAbs(args[0])
	b, err := os.ReadFile(path)
	ts.Check(err)
	if !strings.Contains(string(b), args[1]) {
		ts.Fatalf("%s holds no %q", args[0], args[1])
	}
	ts.Check(os.WriteFile(path, []byte(strings.ReplaceAll(string(b), args[1], args[2])), 0o644))
}
