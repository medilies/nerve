package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	sha := e.upstream(map[string]string{"skills/eng/grilling/SKILL.md": skillMD("grilling", ""), "skills/x/tdd/SKILL.md": skillMD("tdd", "")})

	s, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "grilling")

	if err != nil || s.Origin != "acme/up" || s.Dir != e.path(".cache/nerve/sources/acme/up/"+sha+"/skills/eng/grilling") {
		t.Errorf("got %+v (%v)", s, err)
	}
	if _, err := (SourceCache{}).Skill(domain.Source{Repo: "acme/up", SHA: sha}, "ghost"); err == nil || err.Error() != "acme/up@"+sha[:7]+`: no skill "ghost"` {
		t.Errorf("got %v", err)
	}
}

func TestSkillTakesAWholeRepoWhoseRootIsTheSkill(t *testing.T) {
	e := newTestEnv(t)
	sha := e.upstream(map[string]string{"SKILL.md": skillMD("tui-test", ""), "crates/x/skill.rs": ""})

	s, err := SourceCache{}.Skill(domain.Source{Repo: "acme/up", SHA: sha}, "tui-test")

	if err != nil || s.Dir != e.path(".cache/nerve/sources/acme/up/"+sha) {
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

	d, err := SourceCache{}.Diff("acme/up", "tui-test", old, next)
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

	d, err := src.Diff("acme/up", "grilling", old, next)
	if err != nil || !strings.Contains(d, "+Grill harder.") {
		t.Errorf("diff is %q (%v)", d, err)
	}
	if head, err := src.Head("acme/up"); err != nil || head != next {
		t.Errorf("head is %s (%v), want %s", head, err, next)
	}
	e.git(e.path("github/acme/up.git"), "rm", "-q", "-r", "skills/grilling")
	gone := e.upstream(map[string]string{"skills/other/SKILL.md": skillMD("other", "")})
	if _, err := src.Diff("acme/up", "grilling", old, gone); err == nil || !strings.Contains(err.Error(), "drop it from brain.toml first") {
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

	d, err := SourceCache{}.Diff("acme/up", "grilling", old, next)

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
	if leftovers, _ := filepath.Glob(e.path(".cache/nerve/sources/acme/up/*.tmp-*")); len(leftovers) > 0 {
		t.Errorf("left %v", leftovers)
	}
}
