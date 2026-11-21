package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "ghost"); err == nil || err.Error() != "acme/up@"+sha[:7]+`: no skill "ghost" at the root, in skills/ghost/ or in ghost/: name the folder that holds it with path in its [[source]]` {
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
	if err != nil || !strings.Contains(d, "+Test harder.") {
		t.Fatalf("diff is %q (%v)", d, err)
	}

	var git []string
	for line := range strings.Lines(d) {
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
	if err != nil || !strings.Contains(d, "+Grill harder.") {
		t.Errorf("diff is %q (%v)", d, err)
	}
	if head, err := src.Head("acme/up"); err != nil || head != next {
		t.Errorf("head is %s (%v), want %s", head, err, next)
	}
	e.git(e.path("github/acme/up.git"), "rm", "-q", "-r", "skills/grilling")
	gone := e.upstream(map[string]string{"skills/other/SKILL.md": skillMD("other", "")})
	if _, err := src.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", gone); err == nil || !strings.Contains(err.Error(), "drop it from brain.toml first") {
		t.Errorf("got %v", err)
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

	if err != nil || strings.Contains(d, "DIFF TOOL") || strings.ContainsAny(d, "\x1b\x07") || !strings.Contains(d, "Grill.") {
		t.Errorf("diff is %q (%v)", d, err)
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

	if err != nil || strings.ContainsRune(d, '‮') || !strings.Contains(d, "a<U+202E>b") {
		t.Errorf("got %v, and the bidi control is not shown as such: %q", err, d[:min(len(d), 300)])
	}
	if len(d) > 300_000 || !strings.Contains(d, "the diff is cut") {
		t.Errorf("the diff is %d bytes, uncut", len(d))
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

func TestDiffShowsALineSeparatorAsSuch(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"skills/grilling/SKILL.md": skillMD("grilling", "") + "a b c\n"})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old}, "grilling", next)

	if err != nil || !strings.Contains(d, "a<U+2028>b<U+2029>c") {
		t.Errorf("got %v: %q", err, d)
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

	if err == nil || !strings.HasSuffix(err.Error(), `: no skill "grilling" at the root, in skills/grilling/ or in grilling/: name the folder that holds it with path in its [[source]]`) {
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

	if err == nil || !strings.HasSuffix(err.Error(), `skill "grilling" is in both skills/grilling/ and grilling/: name the folder that holds it with path in its [[source]]`) {
		t.Errorf("got %v", err)
	}
}

func TestDiffLooksInTheFolderThePathNames(t *testing.T) {
	e := newTestEnv(t)
	old := e.upstream(map[string]string{"plugins/x/skills/grilling/SKILL.md": skillMD("grilling", "")})
	next := e.upstream(map[string]string{"plugins/x/skills/grilling/SKILL.md": skillMD("grilling", "") + "Grill harder.\n"})

	d, err := SourceCache{}.Diff(domain.Source{Repo: "acme/up", SHA: old, Path: "plugins/x/skills"}, "grilling", next)

	if err != nil || !strings.Contains(d, "+Grill harder.") {
		t.Errorf("diff is %q (%v)", d, err)
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
