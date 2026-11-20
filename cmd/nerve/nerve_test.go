package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
	"github.com/medilies/nerve/internal/brain/infra"
)

// These tests drive the CLI end to end, on real git and files; each layer's
// own tests sit next to it under internal/brain.

// world is a temp HOME holding a Brain and a project checkout listed nowhere
// yet.
type world struct {
	t     *testing.T
	home  string
	brain string
	proj  string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	w := &world{t: t, home: home, brain: filepath.Join(home, "brain"), proj: filepath.Join(home, "proj")}
	// As Claude Code does the first time it writes a settings.local.json.
	w.write(filepath.Join(home, ".config", "git", "ignore"), "**/.claude/settings.local.json\n")
	w.write(filepath.Join(w.brain, domain.ManifestFile), "")
	w.git(home, "init", "-q", "-b", "main", w.brain)
	w.git(w.brain, "remote", "add", "origin", "git@github.com:Medilies/brain.git")
	w.git(home, "init", "-q", w.proj)
	w.write(filepath.Join(w.proj, "README"), "x")
	w.git(w.proj, "add", "-A")
	w.git(w.proj, "commit", "-q", "-m", "a")
	return w
}

func (w *world) git(dir string, args ...string) string {
	w.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (w *world) write(path, content string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) read(path string) string {
	w.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

// in runs nerve in dir, printing as mode, and returns what it printed.
func (w *world) in(dir string, mode int, args ...string) (string, error) {
	w.t.Helper()
	w.t.Chdir(dir)
	var out bytes.Buffer
	r := infra.NewReport(&out, mode)
	err := run(args, r)
	r.Finish(err)
	return out.String(), err
}

// nerve runs nerve in dir and fails the test if it fails.
func (w *world) nerve(dir string, args ...string) string {
	w.t.Helper()
	out, err := w.in(dir, infra.AsText, args...)
	if err != nil {
		w.t.Fatalf("nerve %v: %v\n%s", args, err, out)
	}
	return out
}

func (w *world) skill(name string) {
	w.write(filepath.Join(w.brain, "skills", name, "SKILL.md"), "---\nname: "+name+"\ndescription: Does "+name+".\n---\n")
}

func TestHelpPrintsUsage(t *testing.T) {
	w := newWorld(t)
	if out := w.nerve(w.home, "help"); !strings.Contains(out, "usage: nerve <command>") {
		t.Errorf("help prints:\n%s", out)
	}
	if _, err := w.in(w.home, infra.AsText, "skill", "--bogus"); err != errUsage {
		t.Errorf("an unknown flag gives %v", err)
	}
}

func TestAProjectGetsWhatItListsAndGitSeesNone(t *testing.T) {
	w := newWorld(t)
	w.skill("go-stack")
	w.write(filepath.Join(w.brain, "agents", "reviewer.md"), "---\nname: reviewer\ndescription: Reviews.\n---\n")
	w.write(filepath.Join(w.brain, domain.ManifestFile), "[common.settings]\nmodel = \"opus\"\n")
	w.nerve(w.home, "init", w.brain)
	w.write(filepath.Join(w.proj, domain.LocalSettings), `{"outputStyle": "terse"}`)

	w.nerve(w.proj, "project", "add", "app")
	w.nerve(w.proj, "skill", "add", "go-stack")
	w.nerve(w.proj, "agent", "add", "reviewer")
	w.nerve(w.proj, "setting", "set", "permissions.deny", `["Cron"]`)
	w.nerve(w.proj, "sync", "--here")

	for link, target := range map[string]string{
		".claude/skills/go-stack":    filepath.Join(w.brain, "skills", "go-stack"),
		".claude/agents/reviewer.md": filepath.Join(w.brain, "agents", "reviewer.md"),
		".claude/skills/nerve":       filepath.Join(w.home, ".cache", "nerve", "skill", "nerve"),
	} {
		if got, err := os.Readlink(filepath.Join(w.proj, link)); err != nil || got != target {
			t.Errorf("%s links to %q (%v), want %s", link, got, err, target)
		}
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(w.read(filepath.Join(w.proj, domain.LocalSettings))), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["model"] != "opus" || settings["outputStyle"] != "terse" || settings["hooks"] == nil {
		t.Errorf("settings are %v", settings)
	}
	if out := w.git(w.proj, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("git sees what nerve wrote:\n%s", out)
	}
	if got := w.read(filepath.Join(w.brain, domain.ManifestFile)); !strings.Contains(got, "[project.app]\nskills = [\"go-stack\"]\nagents = [\"reviewer\"]\n") {
		t.Errorf("brain.toml is:\n%s", got)
	}
	if out := w.nerve(w.proj, "status"); !strings.Contains(out, "project  app: autosync on, 1 skill, 1 agent, 2 settings") {
		t.Errorf("status:\n%s", out)
	}
}

func TestAutosyncPrintsAHookOutput(t *testing.T) {
	w := newWorld(t)
	w.skill("go-stack")
	w.nerve(w.home, "init", w.brain)
	w.nerve(w.proj, "project", "add", "app")
	w.nerve(w.proj, "skill", "add", "go-stack")

	out, _ := w.in(w.proj, infra.AsHook, "sync", "--here", "--hook")

	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if msg, _ := doc["systemMessage"].(string); !strings.Contains(msg, "linked   "+filepath.Join(w.proj, ".claude", "skills", "go-stack")) {
		t.Errorf("message:\n%s", msg)
	}
	if doc["hookSpecificOutput"] == nil {
		t.Errorf("no skill rescan after linking a skill")
	}
	settings := w.read(filepath.Join(w.proj, domain.LocalSettings))
	if !strings.Contains(settings, " sync --here --hook") {
		t.Errorf("the autosync hook is not in the settings:\n%s", settings)
	}
}

func TestCommandsWorkFromAWorktree(t *testing.T) {
	w := newWorld(t)
	w.skill("go-stack")
	w.nerve(w.home, "init", w.brain)
	w.nerve(w.proj, "project", "add", "app")
	tree := filepath.Join(w.home, "proj-feature")
	w.git(w.proj, "worktree", "add", "-q", "-b", "feature", tree)

	w.nerve(tree, "skill", "add", "go-stack")
	w.nerve(tree, "sync", "--here")

	if _, err := os.Readlink(filepath.Join(tree, ".claude", "skills", "go-stack")); err != nil {
		t.Errorf("the worktree is not linked: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(w.proj, ".claude", "skills", "go-stack")); err == nil {
		t.Errorf("sync --here in a worktree wrote into the main checkout")
	}
	if out := w.git(tree, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("git sees what nerve wrote in the worktree:\n%s", out)
	}
}

func TestJSONReportCarriesEventsAndTheError(t *testing.T) {
	w := newWorld(t)
	w.nerve(w.home, "init", w.brain)

	out, err := w.in(w.proj, infra.AsJSON, "skill", "add", "ghost")

	var doc struct {
		Events []app.Event
		Error  string
	}
	if err == nil || json.Unmarshal([]byte(out), &doc) != nil || !strings.Contains(doc.Error, "not a listed checkout") || doc.Events == nil {
		t.Errorf("got %v, document %s", err, out)
	}
}

func TestCheckUsesTheBrainAroundTheWorkingDirectory(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.brain, "skills", "bad", "SKILL.md"), "---\nname: other\ndescription: x\n---\n")

	out, err := w.in(filepath.Join(w.brain, "skills"), infra.AsText, "check")

	if err == nil || !strings.Contains(out, `skills/bad: frontmatter name "other" differs from folder name`) {
		t.Errorf("got %v:\n%s", err, out)
	}
	if _, err := w.in(w.home, infra.AsText, "check"); err == nil || !strings.Contains(err.Error(), "run nerve init") {
		t.Errorf("check outside a Brain, without a machine file: %v", err)
	}
}

func TestNervesOwnSkillMeetsTheSkillRules(t *testing.T) {
	newWorld(t)
	s, err := infra.Nerve{SkillMD: selfSkill}.Skill()
	if err != nil {
		t.Fatal(err)
	}
	if p := s.Problems(); len(p) > 0 {
		t.Errorf("the nerve skill breaks the rules: %v", p)
	}
}
