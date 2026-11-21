package infra

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// upstream makes a fake github.com/acme/up holding files, and returns its
// commit.
func (e *testEnv) upstream(files map[string]string) string {
	e.t.Helper()
	dir := e.path("github/acme/up.git")
	if !fileExists(dir) {
		e.repo(dir)
		e.git(dir, "config", "uploadpack.allowAnySHA1InWant", "true")
	}
	for name, content := range files {
		e.write(filepath.Join(dir, name), content)
	}
	return e.commit(dir)
}

func TestSkillFetchesASkillAtItsPin(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", ""), "tdd/SKILL.md": skillMD("tdd", "")})

	for name, dir := range map[string]string{"grilling": "skills/grilling", "tdd": "tdd"} {
		s, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, name)
		if err != nil || s.Origin != "acme/up" || s.Dir != e.cache("sources/acme/up/"+sha+"/"+dir) {
			t.Errorf("%s: got %+v (%v)", name, s, err)
		}
	}
	if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "ghost"); err == nil || err.Error() != "acme/up@"+sha[:7]+`: no skill "ghost" at the root, in skills/ghost/ or in ghost/: name the folder that holds it with path in its entry of sources` {
		t.Errorf("got %v", err)
	}
}

func TestSkillTakesAWholeRepoWhoseRootIsTheSkill(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"SKILL.md": skillMD("tui-test", ""), "crates/x/skill.rs": ""})

	s, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "tui-test")

	if err != nil || s.Dir != e.cache("sources/acme/up/"+sha) {
		t.Errorf("got %+v (%v)", s, err)
	}
}

func TestSkillThatIsTheWholeRepoHoldsNoGitFolder(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"SKILL.md": skillMD("tui-test", "")})

	s, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "tui-test")
	if err != nil {
		t.Fatal(err)
	}

	if fileExists(filepath.Join(s.Dir, ".git")) {
		t.Errorf("%s holds .git", s.Dir)
	}
}

func TestDiffOfASkillThatIsTheWholeRepoNamesNoGitFile(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"SKILL.md": skillMD("tui-test", "")})
	next := e.upstream(map[string]string{"SKILL.md": skillMD("tui-test", "") + "Test harder.\n"})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old}, "tui-test", next)
	if err != nil || !strings.Contains(d.Text, "+Test harder.") {
		t.Fatalf("diff is %q (%v)", d.Text, err)
	}

	if want := []app.FileChange{{Path: "SKILL.md", Added: 1}}; !reflect.DeepEqual(d.Files, want) {
		t.Errorf("files are %+v, want %+v", d.Files, want)
	}
	var git []string
	for line := range strings.Lines(d.Text) {
		if strings.HasPrefix(line, "diff ") && strings.Contains(line, "/.git/") {
			git = append(git, strings.TrimSpace(line[strings.LastIndex(line, "/.git/")+1:]))
		}
	}
	if len(git) > 0 {
		t.Errorf("the diff names %d files under .git, such as %q", len(git), git[0])
	}
}

func TestDiffShowsHowASkillChangedUpstream(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "") + "Grill harder.\n"})
	src := SourceCache{}

	d, err := src.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", next)
	if err != nil || !strings.Contains(d.Text, "+Grill harder.") {
		t.Errorf("diff is %q (%v)", d.Text, err)
	}
	if tip, err := src.Tip("acme/up"); err != nil || tip != next {
		t.Errorf("tip is %s (%v), want %s", tip, err, next)
	}
	e.git(e.path("github/acme/up.git"), "rm", "-q", "-r", "skills/grilling")
	gone := e.upstream(map[string]string{"skills/other/SKILL.md": skillMD("other", "")})
	if _, err := src.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", gone); err == nil || !strings.Contains(err.Error(), "drop it from brain.json first") {
		t.Errorf("got %v", err)
	}
}

func TestDiffNamesEachChangedFileOfTheSkillWithItsLines(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{
		"skills/grilling/SKILL.md": skillMD("grilling", ""),
		"skills/grilling/old.md":   "a\nb\n",
		"skills/grilling/same.md":  "same\n",
		"skills/other/SKILL.md":    skillMD("other", ""),
	})
	e.git(e.path("github/acme/up.git"), "rm", "-q", "skills/grilling/old.md")
	next := e.upstream(map[string]string{
		"skills/grilling/SKILL.md":       skillMD("grilling", "") + "One.\nTwo.\n",
		"skills/grilling/scripts/run.sh": "#!/bin/sh\n",
		"skills/grilling/logo.png":       "\x00\x01",
		"skills/other/SKILL.md":          skillMD("other", "") + "Elsewhere.\n",
	})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", next)
	if err != nil {
		t.Fatal(err)
	}

	if d.Dir != e.cache("sources/acme/up/"+next+"/skills/grilling") {
		t.Errorf("the skill's folder at the tip is %q", d.Dir)
	}
	want := []app.FileChange{
		{Path: "SKILL.md", Added: 2},
		{Path: "logo.png", Binary: true},
		{Path: "old.md", Removed: 2},
		{Path: "scripts/run.sh", Added: 1},
	}
	if !reflect.DeepEqual(d.Files, want) {
		t.Errorf("files are %+v, want %+v", d.Files, want)
	}
}

func TestDiffOfASkillThatDidNotChangeIsEmpty(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"README.md": "elsewhere\n"})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", next)

	if err != nil || len(d.Files) > 0 || d.Text != "" {
		t.Errorf("got %+v (%v)", d, err)
	}
}

func TestCommittedIsWhenTheCommitWasMade(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	want, err := time.Parse(time.RFC3339, e.git(e.path("github/acme/up.git"), "show", "-s", "--format=%cI", sha))
	if err != nil {
		t.Fatal(err)
	}

	got, err := SourceCache{}.Committed("acme/up", sha)

	if err != nil || !got.Equal(want) {
		t.Errorf("got %v (%v), want %v", got, err, want)
	}
}

// oldDownload makes the download of sha as an older nerve left it: no date
// beside it, and its .git inside.
func (e *testEnv) oldDownload(sha string) string {
	e.t.Helper()
	if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling"); err != nil {
		e.t.Fatal(err)
	}
	dir := e.cache("sources/acme/up/" + sha)
	if err := os.Remove(dir + ".date"); err != nil {
		e.t.Fatal(err)
	}
	e.write(filepath.Join(dir, ".git", "HEAD"), sha+"\n")
	return dir
}

// offline makes the fake GitHub unreachable.
func (e *testEnv) offline() {
	e.t.Helper()
	if err := os.Rename(e.path("github"), e.path("github-gone")); err != nil {
		e.t.Fatal(err)
	}
}

func TestADownloadWithNoDateIsFetchedAgainForItsDate(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	dir := e.oldDownload(sha)

	if _, err := (SourceCache{}).Committed("acme/up", sha); err != nil {
		t.Fatal(err)
	}

	if fileExists(filepath.Join(dir, ".git")) || !fileExists(dir+".date") {
		t.Errorf("the old download stays: .git %v, date %v", fileExists(filepath.Join(dir, ".git")), fileExists(dir+".date"))
	}
}

func TestADownloadWithADateItCannotReadIsFetchedAgain(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	dir := e.oldDownload(sha)
	e.write(dir+".date", "") // as a killed run may leave it

	got, err := SourceCache{}.Committed("acme/up", sha)

	if err != nil || got.IsZero() {
		t.Errorf("got %v (%v)", got, err)
	}
}

func TestASkillInADownloadWithNoDateNeedsNoNetwork(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	dir := e.oldDownload(sha)
	e.offline()

	s, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling")

	if err != nil || s.Dir != filepath.Join(dir, "skills", "grilling") {
		t.Errorf("got %+v (%v)", s, err)
	}
}

func TestAFailedFetchAgainKeepsTheOldDownload(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	dir := e.oldDownload(sha)
	e.offline()

	if _, err := (SourceCache{}).Committed("acme/up", sha); err == nil {
		t.Fatal("no error offline")
	}

	if !fileExists(filepath.Join(dir, "skills", "grilling", "SKILL.md")) {
		t.Errorf("the old download is gone")
	}
}

func TestDiffShowsAFileNameOnOneLine(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"skills/grilling/a\nb\tc.md": "x\n"})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", next)

	if err != nil || len(d.Files) != 1 || d.Files[0].Path != "a<U+000A>b<U+0009>c.md" {
		t.Errorf("got %+v (%v)", d.Files, err)
	}
}

func TestDiffShowsOnlyTheSourcesTextAsItIs(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "") + "\x1b]0;owned\x07\x1b[2JGrill.\n"})
	tool := e.path("difftool.sh")
	e.write(tool, "#!/bin/sh\necho THE USER'S DIFF TOOL\n")
	if err := os.Chmod(tool, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_1", "diff.external")
	t.Setenv("GIT_CONFIG_VALUE_1", tool)

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", next)

	if err != nil || strings.Contains(d.Text, "DIFF TOOL") || strings.ContainsAny(d.Text, "\x1b\x07") || !strings.Contains(d.Text, "Grill.") {
		t.Errorf("diff is %q (%v)", d.Text, err)
	}
}

func TestTwoFetchesOfOnePinAtOnceBothSucceed(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	errs := make(chan error, 2)

	for range 2 {
		go func() {
			_, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling")
			errs <- err
		}()
	}

	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("a fetch failed: %v", err)
		}
	}
	if leftovers, _ := filepath.Glob(e.cache("sources/acme/up/*.tmp-*")); len(leftovers) > 0 {
		t.Errorf("left %v", leftovers)
	}
}

func TestSkillFromASourceCarriesNoLink(t *testing.T) {
	e := newTestEnv(t)
	e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	e.link(e.path("github/acme/up.git/skills/grilling/leak.txt"), "/etc/passwd")
	sha := e.commit(e.path("github/acme/up.git"))

	s, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling")

	if err != nil || isLink(filepath.Join(s.Dir, "leak.txt")) {
		t.Errorf("got %v, and the source's link reached the cache", err)
	}
}

func TestDiffShowsBidiControlsAndCutsAHugeDiff(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "") + "a\u202eb\n" + strings.Repeat("x\n", 300_000)})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", next)

	if err != nil || strings.ContainsRune(d.Text, '‮') || !strings.Contains(d.Text, "a<U+202E>b") {
		t.Errorf("got %v, and the bidi control is not shown as such: %q", err, d.Text[:min(len(d.Text), 300)])
	}
	if len(d.Text) > 300_000 || !strings.Contains(d.Text, "the diff is cut") {
		t.Errorf("the diff is %d bytes, uncut", len(d.Text))
	}
}

func TestFetchSweepsAFolderAKilledFetchLeft(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	stale := e.cache("sources/acme/up/" + strings.Repeat("0", 40) + ".tmp-123")
	e.write(filepath.Join(stale, "x"), "x")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling"); err != nil {
		t.Fatal(err)
	}

	if fileExists(stale) {
		t.Errorf("the killed fetch's folder is still there")
	}
}

func TestFetchSweepsADateFileAKilledFetchLeft(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	stale := e.cache("sources/acme/up/" + strings.Repeat("0", 40) + ".tmp-123.date")
	e.write(stale, "2026-")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling"); err != nil {
		t.Fatal(err)
	}

	if fileExists(stale) {
		t.Errorf("the killed fetch's date file is still there")
	}
}

func TestDiffShowsALineSeparatorAsSuch(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "") + "a b c\n"})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", next)

	if err != nil || !strings.Contains(d.Text, "a<U+2028>b<U+2029>c") {
		t.Errorf("got %v: %q", err, d.Text)
	}
}

func TestFetchRunsNoFilterOfTheUsers(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{".gitattributes": "* filter=x\n", "skills/grilling/SKILL.md": skillMD("grilling", "")})
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_1", "filter.x.smudge")
	t.Setenv("GIT_CONFIG_VALUE_1", "touch "+e.path("smudged")+"; cat")

	if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling"); err != nil {
		t.Fatal(err)
	}

	if fileExists(e.path("smudged")) {
		t.Errorf("fetching a source ran the user's filter on it")
	}
}

func TestFetchRunsNoHookOfTheUsers(t *testing.T) {
	for _, key := range []string{"core.hooksPath", "init.templateDir"} {
		t.Run(key, func(t *testing.T) {
			e := newTestEnv(t)
			sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
			hooks := e.path("hooks")
			if key == "init.templateDir" {
				hooks = e.path("template/hooks")
			}
			for _, hook := range []string{"post-checkout", "reference-transaction"} {
				e.write(filepath.Join(hooks, hook), "#!/bin/sh\ntouch "+e.path("hooked")+"\n")
				if err := os.Chmod(filepath.Join(hooks, hook), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("GIT_CONFIG_COUNT", "2")
			t.Setenv("GIT_CONFIG_KEY_1", key)
			t.Setenv("GIT_CONFIG_VALUE_1", filepath.Dir(hooks))
			if key == "core.hooksPath" {
				t.Setenv("GIT_CONFIG_VALUE_1", hooks)
			}

			if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling"); err != nil {
				t.Fatal(err)
			}

			if fileExists(e.path("hooked")) {
				t.Errorf("fetching a source ran a hook of the user's on it")
			}
		})
	}
}

func TestFetchKeepsTheSourcesLineEnds(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_1", "core.autocrlf")
	t.Setenv("GIT_CONFIG_VALUE_1", "true")

	s, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling")

	if err != nil || strings.Contains(e.read(filepath.Join(s.Dir, "SKILL.md")), "\r") {
		t.Errorf("got %v, and the source's text changed on fetch", err)
	}
}

func TestFetchSweepsUnderACacheFolderNamedLikeAPattern(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	t.Setenv("XDG_CACHE_HOME", e.path("c[1]"))
	stale := e.path("c[1]/nerve/sources/acme/up/" + strings.Repeat("0", 40) + ".tmp-123")
	e.write(filepath.Join(stale, "x"), "x")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling"); err != nil {
		t.Fatal(err)
	}

	if fileExists(stale) {
		t.Errorf("the killed fetch's folder is still there")
	}
}

func TestSkillLooksOnlyInItsPlaces(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"examples/grilling/SKILL.md": skillMD("grilling", "")})

	_, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling")

	if err == nil || !strings.HasSuffix(err.Error(), `: no skill "grilling" at the root, in skills/grilling/ or in grilling/: name the folder that holds it with path in its entry of sources`) {
		t.Errorf("got %v, want no skill outside the root, skills/ and a folder of its name", err)
	}
}

func TestSkillIsFoundInTheFolderItsPathNames(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"plugins/x/skills/grilling/SKILL.md": skillMD("grilling", ""), "plugins/x/SKILL.md": skillMD("whole", "")})

	for name, dir := range map[string]string{"grilling": "plugins/x/skills/grilling", "whole": "plugins/x"} {
		path := map[string]string{"grilling": "plugins/x/skills", "whole": "plugins/x"}[name]
		s, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha, Path: path}, name)
		if err != nil || s.Dir != e.cache("sources/acme/up/"+sha+"/"+dir) {
			t.Errorf("%s: got %+v (%v)", name, s, err)
		}
	}
}

func TestSkillInTwoOfItsPlacesIsRefused(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", ""), "grilling/SKILL.md": skillMD("grilling", "")})

	_, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling")

	if err == nil || !strings.HasSuffix(err.Error(), `skill "grilling" is in both skills/grilling/ and grilling/: name the folder that holds it with path in its entry of sources`) {
		t.Errorf("got %v", err)
	}
}

func TestDiffLooksInTheFolderThePathNames(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"plugins/x/skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"plugins/x/skills/grilling/SKILL.md": skillMD("grilling", "") + "Grill harder.\n"})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old, Path: "plugins/x/skills"}, "grilling", next)

	if err != nil || !strings.Contains(d.Text, "+Grill harder.") {
		t.Errorf("diff is %q (%v)", d.Text, err)
	}
}

func TestSkillSaysWhenTheRootSKILLmdCannotBeRead(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"SKILL.md": "# grilling, with no frontmatter\n"})

	_, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling")

	if err == nil || !strings.Contains(err.Error(), "; its root SKILL.md cannot be read: ") {
		t.Errorf("got %v", err)
	}
}

func TestNumstatCutsOnlyTheFolderEachPathIsIn(t *testing.T) {
	out := "0\t1\t\x00old/new/x.md\x00/dev/null\x00" + "1\t0\t\x00/dev/null\x00new/old/y.md\x00"

	got := numstat(out, "old", "new")

	want := []app.FileChange{{Path: "new/x.md", Removed: 1}, {Path: "old/y.md", Added: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAFetchWaitsForAnotherFetchOfTheSameCommit(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	dir := e.oldDownload(sha)
	unlock, err := lock(dir + ".lock") // another fetch of it, under way
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := SourceCache{}.Committed("acme/up", sha)
		done <- err
	}()

	select {
	case <-done:
		t.Fatal("the fetch did not wait")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
