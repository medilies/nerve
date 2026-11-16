package main

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// world is a temp HOME holding a skills repo, a fake GitHub, and one config.
type world struct {
	t        *testing.T
	home     string
	root     string
	upstream string // fake github.com/acme/up
	config   string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	// Route https://github.com/ to local repos, and keep the user's git config out.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file://"+filepath.Join(home, "github")+"/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/")
	w := &world{
		t:        t,
		home:     home,
		root:     filepath.Join(home, "repo"),
		upstream: filepath.Join(home, "github", "acme", "up.git"),
		config:   filepath.Join(home, ".cfg"),
	}
	w.gitIn(home, "init", "-q", w.upstream)
	w.gitIn(w.upstream, "config", "uploadpack.allowAnySHA1InWant", "true")
	return w
}

func (w *world) gitIn(dir string, args ...string) string {
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

func skillMD(name, extra string) string {
	return "---\nname: " + name + "\ndescription: Does " + name + ".\n" + extra + "---\n\n# " + name + "\n\n" + overridesHeading + "\n\nRepo wins.\n"
}

// commitUpstream writes skills/<name>/SKILL.md upstream and returns the commit.
func (w *world) commitUpstream(files map[string]string) string {
	w.t.Helper()
	for name, content := range files {
		w.write(filepath.Join(w.upstream, "skills", name, "SKILL.md"), content)
	}
	w.gitIn(w.upstream, "add", "-A")
	w.gitIn(w.upstream, "commit", "-q", "-m", "c")
	return w.gitIn(w.upstream, "rev-parse", "HEAD")
}

func (w *world) manifest(sha string, skills ...string) {
	quoted := make([]string, len(skills))
	for i, s := range skills {
		quoted[i] = `"` + s + `"`
	}
	w.write(filepath.Join(w.root, manifestFile), `configs = ["~/.cfg"]
[[source]]
repo = "acme/up"
sha = "`+sha+`"
skills = [`+strings.Join(quoted, ", ")+`]
`)
}

func (w *world) run(cmd func(string, *strings.Builder) error) string {
	w.t.Helper()
	var out strings.Builder
	if err := cmd(w.root, &out); err != nil {
		w.t.Fatalf("%v\n%s", err, out.String())
	}
	return out.String()
}

func syncCmd(root string, out *strings.Builder) error { return runSync(root, out) }

func linkTarget(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("%s is not a link: %v", path, err)
	}
	return target
}

func TestSyncLinksOwnAndPinnedSkillsAndRemovesOnlyItsOwnLinks(t *testing.T) {
	w := newWorld(t)
	sha := w.commitUpstream(map[string]string{"grill": skillMD("grill", ""), "plan": skillMD("plan", "")})
	w.write(filepath.Join(w.root, "skills", "start-project", "SKILL.md"), skillMD("start-project", ""))
	w.manifest(sha, "grill", "plan")
	handMade := filepath.Join(w.config, "skills", "hand-made")
	w.write(filepath.Join(handMade, "SKILL.md"), skillMD("hand-made", ""))

	w.run(syncCmd)

	skills := filepath.Join(w.config, "skills")
	if got, want := linkTarget(t, filepath.Join(skills, "start-project")), filepath.Join(w.root, "skills", "start-project"); got != want {
		t.Errorf("own skill links to %s, want the checkout %s", got, want)
	}
	if got := linkTarget(t, filepath.Join(skills, "grill")); !strings.Contains(got, sha) {
		t.Errorf("pinned skill links to %s, want a checkout at %s", got, sha)
	}

	w.manifest(sha, "grill")
	w.run(syncCmd)

	if _, err := os.Lstat(filepath.Join(skills, "plan")); !os.IsNotExist(err) {
		t.Errorf("plan was dropped from the manifest but is still installed")
	}
	if _, err := os.Stat(filepath.Join(handMade, "SKILL.md")); err != nil {
		t.Errorf("sync touched a skill it did not install: %v", err)
	}
}

func TestSyncRefusesToReplaceARealFolder(t *testing.T) {
	w := newWorld(t)
	sha := w.commitUpstream(map[string]string{"grill": skillMD("grill", "")})
	w.manifest(sha, "grill")
	w.write(filepath.Join(w.config, "skills", "grill", "SKILL.md"), "mine")

	var out strings.Builder
	err := runSync(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), "real folder") {
		t.Fatalf("got %v, want a refusal to replace the real folder", err)
	}
}

func TestSyncZipsOnlySkillsClaudeAIAccepts(t *testing.T) {
	w := newWorld(t)
	sha := w.commitUpstream(map[string]string{
		"grill":  skillMD("grill", ""),
		"wizard": skillMD("wizard", "disable-model-invocation: true\n"),
	})
	w.manifest(sha, "grill", "wizard")

	out := w.run(syncCmd)

	zr, err := zip.OpenReader(filepath.Join(w.root, "dist", "grill.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "grill/SKILL.md" {
		t.Errorf("grill.zip holds %v, want grill/SKILL.md", zr.File)
	}
	if _, err := os.Stat(filepath.Join(w.root, "dist", "wizard.zip")); !os.IsNotExist(err) {
		t.Errorf("wizard.zip was built, but claude.ai rejects disable-model-invocation")
	}
	if !strings.Contains(out, "wizard: Claude Code only (frontmatter keys claude.ai rejects: disable-model-invocation)") {
		t.Errorf("output does not say why wizard is skipped:\n%s", out)
	}
}

func TestSyncReportsStaleAndMissingClaudeAICopies(t *testing.T) {
	w := newWorld(t)
	sha := w.commitUpstream(map[string]string{"grill": skillMD("grill", ""), "plan": skillMD("plan", "")})
	w.manifest(sha, "grill", "plan")
	w.write(filepath.Join(w.root, "skills", "start-project", "SKILL.md"), skillMD("start-project", ""))
	synced := filepath.Join(w.config, "skills", "synced", "bucket")
	w.write(filepath.Join(synced, "grill", "SKILL.md"), skillMD("grill", ""))
	w.write(filepath.Join(synced, "grill", "added-by-claude-ai.json"), "{}")
	w.write(filepath.Join(synced, "start-project", "SKILL.md"), "old")
	// An older bucket holding an outdated grill must not make it stale.
	w.write(filepath.Join(w.config, "skills", "synced", "old-bucket", "grill", "SKILL.md"), "old")

	out := w.run(syncCmd)

	for _, want := range []string{
		"stale    start-project: upload dist/start-project.zip",
		"missing  plan: upload dist/plan.zip",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " grill:") {
		t.Errorf("grill matches its claude.ai copy but is reported:\n%s", out)
	}
}

func TestUpdateShowsUpstreamDiffAndMovesThePin(t *testing.T) {
	w := newWorld(t)
	oldSHA := w.commitUpstream(map[string]string{"grill": skillMD("grill", "")})
	w.manifest(oldSHA, "grill")
	newSHA := w.commitUpstream(map[string]string{"grill": skillMD("grill", "") + "Ask harder.\n"})

	out := w.run(func(root string, out *strings.Builder) error { return runUpdate(root, "acme/up", "", out) })

	if !strings.Contains(out, "+Ask harder.") {
		t.Errorf("output lacks the upstream change:\n%s", out)
	}
	m, err := loadManifest(w.root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Sources[0].SHA != newSHA {
		t.Errorf("pin is %s, want %s", m.Sources[0].SHA, newSHA)
	}
}

func TestCheckFlagsOwnSkillsThatBreakTheRules(t *testing.T) {
	w := newWorld(t)
	sha := w.commitUpstream(map[string]string{"wizard": skillMD("wizard", "disable-model-invocation: true\n")})
	w.manifest(sha, "wizard")
	w.write(filepath.Join(w.root, "skills", "start-project", "SKILL.md"),
		"---\nname: start-project\ndescription: Starts.\nargument-hint: x\n---\n\nNo overrides.\n")

	var out strings.Builder
	err := runCheck(w.root, &out)

	if err == nil {
		t.Fatalf("check passed, want problems:\n%s", out.String())
	}
	for _, want := range []string{
		"start-project (own): frontmatter keys claude.ai rejects: argument-hint",
		"start-project (own): no " + overridesHeading + " section",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "wizard") {
		t.Errorf("a third-party skill was held to own-skill rules:\n%s", out.String())
	}
}

func TestCheckRejectsTwoSkillsWithOneName(t *testing.T) {
	w := newWorld(t)
	sha := w.commitUpstream(map[string]string{"grill": skillMD("grill", "")})
	w.manifest(sha, "grill")
	w.write(filepath.Join(w.root, "skills", "grill", "SKILL.md"), skillMD("grill", ""))

	var out strings.Builder
	err := runCheck(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), `skill "grill" comes from both own and acme/up`) {
		t.Fatalf("got %v, want a duplicate-name error", err)
	}
}

func TestSyncHandlesARepoThatIsOneSkillWithoutShippingGitHistory(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.upstream, "SKILL.md"), skillMD("whole", ""))
	w.gitIn(w.upstream, "add", "-A")
	w.gitIn(w.upstream, "commit", "-q", "-m", "c")
	w.manifest(w.gitIn(w.upstream, "rev-parse", "HEAD"), "whole")

	w.run(syncCmd)

	zr, err := zip.OpenReader(filepath.Join(w.root, "dist", "whole.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.Contains(f.Name, ".git/") {
			t.Fatalf("whole.zip ships git history: %s", f.Name)
		}
	}
}
