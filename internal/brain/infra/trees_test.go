package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestLocateTakesAWorktreeOfABareRepoAsItsOwnCheckout(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	e.write(filepath.Join(app, "README"), "x")
	e.commit(app)
	bare := e.path("app.git")
	e.git(e.home, "clone", "-q", "--bare", app, bare)
	tree := e.path("wt")
	e.git(bare, "worktree", "add", "-q", tree)

	got, err := LocalTrees{}.Locate(tree)

	if err != nil || got.Top != tree || got.Main != tree || got.Repo != bare {
		t.Errorf("located %+v (%v): a bare repo has no main checkout, and its exclude file is its own", got, err)
	}
}

func TestLocateFindsARepoWhoseGitFolderIsElsewhere(t *testing.T) {
	e := newTestEnv(t)
	app := e.path("app")
	e.git(e.home, "init", "-q", "--separate-git-dir", e.path("app-git"), app)

	got, err := LocalTrees{}.Locate(app)

	if err != nil || got.Top != app || got.Main != app || got.Repo != e.path("app-git") {
		t.Errorf("located %+v (%v)", got, err)
	}
}

func TestLocateTakesASubmoduleAsARepoOfItsOwn(t *testing.T) {
	e := newTestEnv(t)
	lib := e.repo(e.path("lib"))
	e.write(filepath.Join(lib, "README"), "x")
	e.commit(lib)
	app := e.repo(e.path("app"))
	e.git(app, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "lib")
	sub := filepath.Join(app, "lib")

	got, err := LocalTrees{}.Locate(sub)

	if err != nil || got.Top != sub || got.Main != sub || got.Repo != filepath.Join(app, ".git", "modules", "lib") {
		t.Errorf("located %+v (%v)", got, err)
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

	if err == nil || !strings.HasSuffix(err.Error(), "add **/.claude/settings.local.json to ~/my-ignore, your global git ignore file") {
		t.Errorf("got %v", err)
	}
}

func TestSyncExcludeKeepsOnlyItsOwnBlock(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	exclude := filepath.Join(app, ".git/info/exclude")
	e.write(exclude, "*.log")
	w := LocalTrees{}

	if err := w.SyncExclude(app, lines([]string{".claude/skills/a", ".claude/agents/b.md"})); err != nil {
		t.Fatal(err)
	}
	if got := e.read(exclude); got != "*.log\n# nerve: begin\n/.claude/skills/a\n/.claude/agents/b.md\n# nerve: end\n" {
		t.Errorf("exclude is:\n%s", got)
	}
	e.write(filepath.Join(app, ".claude/skills/a/SKILL.md"), "x")
	if out := e.git(app, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("git sees an excluded link: %s", out)
	}
	if err := w.SyncExclude(app, lines([]string{".claude/skills/c"})); err != nil {
		t.Fatal(err)
	}
	if err := w.SyncExclude(app, lines(nil)); err != nil {
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
		if err := w.SyncExclude(app, lines([]string{".claude/skills/b"})); err == nil || !strings.Contains(err.Error(), "fix it by hand") {
			t.Errorf("%q: got %v", text, err)
		}
		if got := e.read(exclude); got != text {
			t.Errorf("the damaged file became %q", got)
		}
	}
}

func TestSyncExcludeRefusesAnExcludeFileOrFolderThatIsALink(t *testing.T) {
	for _, linked := range []string{"info/exclude", "info"} {
		t.Run(linked, func(t *testing.T) {
			e := newTestEnv(t)
			app := e.repo(e.path("app"))
			outside := e.path("dotfiles/" + linked)
			e.write(e.path("dotfiles/info/exclude"), "*.log\n")
			if err := os.RemoveAll(filepath.Join(app, ".git", linked)); err != nil {
				t.Fatal(err)
			}
			e.link(filepath.Join(app, ".git", linked), outside)

			err := LocalTrees{}.SyncExclude(app, lines([]string{".claude/skills/a"}))

			if err == nil || !strings.HasSuffix(err.Error(), "is a link, so nerve would write outside the repo: make it a plain "+map[string]string{"info": "folder", "info/exclude": "file"}[linked]) {
				t.Errorf("got %v", err)
			}
			if got := e.read(e.path("dotfiles/info/exclude")); got != "*.log\n" {
				t.Errorf("the file outside became %q", got)
			}
		})
	}
}

func TestSyncExcludeKeepsLinesAfterItsBlock(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	exclude := filepath.Join(app, ".git/info/exclude")
	e.write(exclude, "*.log\n# nerve: begin\n/.claude/skills/a\n# nerve: end\nmine-1\n")

	if err := (LocalTrees{}).SyncExclude(app, lines([]string{".claude/skills/b"})); err != nil {
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

	if err := (LocalTrees{}).SyncExclude(tree, lines([]string{".claude/skills/a"})); err != nil {
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

func TestSettingsReadAnEmptyFileOrOneAfterAByteOrderMarkAsNoSettings(t *testing.T) {
	e := newTestEnv(t)
	path := e.path("app/.claude/settings.local.json")
	for name, content := range map[string]string{"empty": "", "blank": " \n\t\n", "BOM": "\ufeff{}\n"} {
		t.Run(name, func(t *testing.T) {
			e.write(path, content)

			if s, err := (LocalTrees{}).ReadSettings(path); err != nil || s == nil || len(s) != 0 {
				t.Errorf("reads %v (%v), want {}", s, err)
			}
		})
	}
}

func TestSettingsKeepEveryDigitOfAWholeNumberBeyondInt64(t *testing.T) {
	e := newTestEnv(t)
	path := e.path("proj/.claude/settings.local.json")
	e.write(path, `{"uid": 18446744073709551615}`)
	w := LocalTrees{}

	s, err := w.ReadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSettings(path, s); err != nil {
		t.Fatal(err)
	}

	if got := e.read(path); !strings.Contains(got, `"uid": 18446744073709551615`) {
		t.Errorf("written as %s", got)
	}
}

// lines are the paths SyncExclude keeps out of git, as given.
func lines(rels []string) func() ([]string, error) {
	return func() ([]string, error) { return rels, nil }
}

func TestSyncExcludeWritesOneRunsBlockAtATime(t *testing.T) {
	e := newTestEnv(t)
	app := e.repo(e.path("app"))
	inFirst, release, done := make(chan struct{}), make(chan struct{}), make(chan error)
	go func() {
		done <- LocalTrees{}.SyncExclude(app, func() ([]string, error) {
			close(inFirst)
			<-release
			return []string{".claude/skills/a"}, nil
		})
	}()
	<-inFirst
	inSecond := make(chan struct{})
	go func() {
		done <- LocalTrees{}.SyncExclude(app, func() ([]string, error) {
			close(inSecond)
			return []string{".claude/skills/a", ".claude/skills/b"}, nil
		})
	}()

	select {
	case <-inSecond:
		t.Fatal("a second run read what to exclude while the first was writing its block")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if got := e.read(filepath.Join(app, ".git/info/exclude")); !strings.Contains(got, "/.claude/skills/b\n") {
		t.Errorf("the second run's block is lost:\n%s", got)
	}
}

func TestFrontmatterReadsAMarkdownFile(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("app/.claude/agents/team.md"), "---\nname: team\ndescription: Reviews.\n---\n")
	e.write(e.path("app/.claude/agents/README.md"), "# Agents\n")

	if meta, err := (LocalTrees{}).Frontmatter(e.path("app/.claude/agents/team.md")); err != nil || meta["name"] != "team" {
		t.Errorf("got %v, %v", meta, err)
	}
	if _, err := (LocalTrees{}).Frontmatter(e.path("app/.claude/agents/README.md")); err == nil {
		t.Errorf("a file with no frontmatter reads as one")
	}
}

func TestWriteSettingsMakesANewFileForTheUserOnlyAndKeepsAnOldOnesMode(t *testing.T) {
	e := newTestEnv(t)
	fresh, old := e.path("a/.claude/settings.local.json"), e.path("b/.claude/settings.local.json")
	e.write(old, "{}")
	if err := os.Chmod(old, 0o640); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{fresh, old} {
		if err := (LocalTrees{}).WriteSettings(path, map[string]any{"env": map[string]any{"TOKEN": "x"}}); err != nil {
			t.Fatal(err)
		}
	}

	for path, want := range map[string]os.FileMode{fresh: 0o600, old: 0o640} {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: got %v (%v), want %v", path, fi.Mode().Perm(), err, want)
		}
	}
}
