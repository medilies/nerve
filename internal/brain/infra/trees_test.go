package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocateFindsTheMainCheckoutOfAWorktree(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	e.write(filepath.Join(app, "README"), "x")
	e.commit(app)
	tree := e.path("app-feature")
	e.git(app, "worktree", "add", "-q", "-b", "feature", tree)
	if err := os.MkdirAll(filepath.Join(tree, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := LocalTrees{}

	got, err := w.Locate(filepath.Join(tree, "src"))
	if err != nil || got.Top != tree || got.Main != app || got.Repo != filepath.Join(app, ".git") {
		t.Errorf("located %+v (%v), want top %s, main %s", got, err, tree, app)
	}
	if got, _ := w.Locate(app); got.Top != app || got.Main != app {
		t.Errorf("located %+v in the main checkout", got)
	}
	if _, err := w.Locate(e.home); err == nil || !strings.Contains(err.Error(), "not in a git repo") {
		t.Errorf("got %v outside a repo", err)
	}
}

func TestCheckIgnoredRefusesSettingsGitWouldSeeOrShares(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	w := LocalTrees{}

	if err := w.CheckIgnored(app); err == nil || !strings.Contains(err.Error(), "git does not ignore .claude/settings.local.json") {
		t.Errorf("got %v", err)
	}
	e.write(e.path(".config/git/ignore"), "**/.claude/settings.local.json\n") // as Claude Code does
	if err := w.CheckIgnored(app); err != nil {
		t.Errorf("an ignored file is refused: %v", err)
	}
	e.write(filepath.Join(app, ".claude/settings.local.json"), "{}")
	e.git(app, "add", "-f", ".claude/settings.local.json")
	if err := w.CheckIgnored(app); err == nil || !strings.Contains(err.Error(), "git tracks .claude/settings.local.json") {
		t.Errorf("got %v", err)
	}
}

func TestCheckIgnoredTellsAGitFailureFromAFileGitSees(t *testing.T) {
	e := newTestEnv(t)
	err := LocalTrees{}.CheckIgnored(e.path("."))
	if err == nil || strings.Contains(err.Error(), "does not ignore") || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("got %v outside a repo", err)
	}
}

func TestCheckIgnoredNamesTheGlobalIgnoreFileGitReads(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_1", "core.excludesFile")
	t.Setenv("GIT_CONFIG_VALUE_1", "~/my-ignore")

	err := LocalTrees{}.CheckIgnored(app)

	if err == nil || !strings.Contains(err.Error(), "add **/.claude/settings.local.json to ~/my-ignore") {
		t.Errorf("got %v", err)
	}
}

func TestSyncExcludeKeepsOnlyItsOwnBlock(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	exclude := filepath.Join(app, ".git/info/exclude")
	e.write(exclude, "*.log")
	w := LocalTrees{}

	if err := w.SyncExclude(app, []string{".claude/skills/a", ".claude/agents/b.md"}); err != nil {
		t.Fatal(err)
	}
	if got := e.read(exclude); got != "*.log\n# nerve: begin\n/.claude/skills/a\n/.claude/agents/b.md\n# nerve: end\n" {
		t.Errorf("exclude is:\n%s", got)
	}
	e.write(filepath.Join(app, ".claude/skills/a/SKILL.md"), "x")
	if out := e.git(app, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("git sees an excluded link: %s", out)
	}
	if err := w.SyncExclude(app, []string{".claude/skills/c"}); err != nil {
		t.Fatal(err)
	}
	if err := w.SyncExclude(app, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.read(exclude); got != "*.log\n" {
		t.Errorf("exclude is %q after removing the block", got)
	}
}

func TestSyncExcludeRefusesADamagedBlock(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	exclude := filepath.Join(app, ".git/info/exclude")
	w := LocalTrees{}
	for _, text := range []string{
		"*.log\n# nerve: begin\n/.claude/skills/a\nmine-1\n",                // no end
		"# nerve: end\n*.log\n",                                             // no begin
		"# nerve: begin\n# nerve: begin\n/.claude/skills/a\n# nerve: end\n", // two begins
	} {
		e.write(exclude, text)
		if err := w.SyncExclude(app, []string{".claude/skills/b"}); err == nil || !strings.Contains(err.Error(), "fix it by hand") {
			t.Errorf("%q: got %v", text, err)
		}
		if got := e.read(exclude); got != text {
			t.Errorf("the damaged file became %q", got)
		}
	}
}

func TestSyncExcludeKeepsLinesAfterItsBlock(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	exclude := filepath.Join(app, ".git/info/exclude")
	e.write(exclude, "*.log\n# nerve: begin\n/.claude/skills/a\n# nerve: end\nmine-1\n")

	if err := (LocalTrees{}).SyncExclude(app, []string{".claude/skills/b"}); err != nil {
		t.Fatal(err)
	}
	if got := e.read(exclude); got != "*.log\nmine-1\n# nerve: begin\n/.claude/skills/b\n# nerve: end\n" {
		t.Errorf("exclude is %q", got)
	}
}

func TestSyncExcludeFromAWorktreeWritesTheRepos(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	e.write(filepath.Join(app, "README"), "x")
	e.commit(app)
	tree := e.path("app-feature")
	e.git(app, "worktree", "add", "-q", "-b", "feature", tree)

	if err := (LocalTrees{}).SyncExclude(tree, []string{".claude/skills/a"}); err != nil {
		t.Fatal(err)
	}
	e.write(filepath.Join(app, ".claude/skills/a/SKILL.md"), "x")
	if out := e.git(app, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("the main checkout sees a link excluded from its worktree: %s", out)
	}
}

func TestLinkReplacesALinkButNeverAFile(t *testing.T) {
	e := newTestEnv(t)
	w := LocalTrees{}
	link := e.path("app/.claude/skills/x")

	if err := w.Link(link, "/one"); err != nil {
		t.Fatal(err)
	}
	if err := w.Link(link, "/two"); err != nil {
		t.Fatal(err)
	}
	if target, ok := w.LinkTarget(link); !ok || target != "/two" || !isLink(link) || !w.Present(link) || w.Exists(link) {
		t.Errorf("link to %s (%v); a broken link is present, not existing", target, ok)
	}
	file := e.path("app/.claude/skills/team")
	e.write(file, "x")
	if err := w.Link(file, "/one"); err == nil || isLink(file) {
		t.Errorf("a real file was replaced")
	}
	if got := w.Entries(e.path("app/.claude/skills")); strings.Join(got, ",") != "team,x" {
		t.Errorf("entries are %v", got)
	}
}

func TestSettingsAreReadAndWrittenAsJSON(t *testing.T) {
	e := newTestEnv(t)
	w := LocalTrees{}
	path := e.path("app/.claude/settings.local.json")

	if s, err := w.ReadSettings(path); s != nil || err != nil {
		t.Errorf("a missing file reads %v (%v)", s, err)
	}
	if err := w.WriteSettings(path, map[string]any{"model": "opus"}); err != nil {
		t.Fatal(err)
	}
	if got := e.read(path); got != "{\n  \"model\": \"opus\"\n}\n" {
		t.Errorf("written as %q", got)
	}
	e.write(path, "not json")
	if _, err := w.ReadSettings(path); err == nil {
		t.Errorf("a broken file reads")
	}
}
