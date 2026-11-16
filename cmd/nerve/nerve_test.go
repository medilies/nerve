package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// world is a temp HOME holding a Brain, a fake GitHub, and one config.
type world struct {
	t        *testing.T
	home     string
	root     string
	upstream string // fake github.com/acme/up
	config   string
	rerunEnv []string // the environment of the last re-run after an upgrade
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
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
	w.write(filepath.Join(home, ".config", "nerve", "config.toml"), "brain = \"~/repo\"\n[[config]]\ndir = \"~/.cfg\"\n")
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
	return "---\nname: " + name + "\ndescription: Does " + name + ".\n" + extra + "---\n\n# " + name + "\n"
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
	w.write(filepath.Join(w.root, manifestFile), `[[source]]
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

func syncCmd(_ string, out *strings.Builder) error {
	m, err := loadMachine()
	if err != nil {
		return err
	}
	return runSync(m, false, newReport(out, false))
}

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
	err := syncCmd(w.root, &out)

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
	if !strings.Contains(out, "skipped  wizard: Claude Code only (frontmatter keys claude.ai rejects: disable-model-invocation)") {
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
		"stale    start-project on claude.ai of " + w.config + ": upload dist/start-project.zip",
		"missing  plan on claude.ai of " + w.config + ": upload dist/plan.zip",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " grill ") {
		t.Errorf("grill matches its claude.ai copy but is reported:\n%s", out)
	}
}

func TestUpdateShowsUpstreamDiffAndMovesThePin(t *testing.T) {
	w := newWorld(t)
	oldSHA := w.commitUpstream(map[string]string{"grill": skillMD("grill", "")})
	w.manifest(oldSHA, "grill")
	newSHA := w.commitUpstream(map[string]string{"grill": skillMD("grill", "") + "Ask harder.\n"})

	out := w.run(func(root string, out *strings.Builder) error {
		return runUpdate(root, "acme/up", "", newReport(out, false))
	})

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
		"---\nname: start-project\ndescription: Starts.\nargument-hint: x\n---\n")

	var out strings.Builder
	err := runCheck(w.root, newReport(&out, false))

	if err == nil {
		t.Fatalf("check passed, want problems:\n%s", out.String())
	}
	for _, want := range []string{
		"problem  start-project: frontmatter keys claude.ai rejects: argument-hint (own)",
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
	err := runCheck(w.root, newReport(&out, false))

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

func TestInitWritesTheMachineFileThatSyncFollows(t *testing.T) {
	w := newWorld(t)
	os.Remove(filepath.Join(w.home, ".config", "nerve", "config.toml"))
	w.write(filepath.Join(w.root, manifestFile), "profiles = [\"work\"]\n")
	w.write(filepath.Join(w.root, "skills", "grill", "SKILL.md"), skillMD("grill", ""))

	w.run(func(_ string, out *strings.Builder) error {
		return runInit([]string{w.root, "~/.cfg=work"}, newReport(out, false))
	})
	w.run(syncCmd)

	if got, want := linkTarget(t, filepath.Join(w.config, "skills", "grill")), filepath.Join(w.root, "skills", "grill"); got != want {
		t.Errorf("grill links to %s, want %s", got, want)
	}
	var out strings.Builder
	if err := runInit([]string{w.root, "~/.cfg"}, newReport(&out, false)); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("second init got %v, want a refusal to overwrite the machine file", err)
	}
}

func TestSyncRefusesAProfileTheBrainLacks(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "profiles = [\"work\"]\n")
	w.write(filepath.Join(w.home, ".config", "nerve", "config.toml"), "brain = \"~/repo\"\n[[config]]\ndir = \"~/.cfg\"\nprofile = \"play\"\n")

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), `profile "play"`) {
		t.Fatalf("got %v, want an unknown-profile error", err)
	}
}

// profiled points the machine's one config at profile work, and lists it in
// the Brain.
func (w *world) profiled() {
	w.write(filepath.Join(w.root, manifestFile), "profiles = [\"work\"]\n")
	w.write(filepath.Join(w.home, ".config", "nerve", "config.toml"), "brain = \"~/repo\"\n[[config]]\ndir = \"~/.cfg\"\nprofile = \"work\"\n")
}

func (w *world) settings() map[string]any {
	w.t.Helper()
	obj, err := readObject(filepath.Join(w.config, "settings.json"))
	if err != nil || obj == nil {
		w.t.Fatalf("settings.json: %v, %v", obj, err)
	}
	return obj
}

func TestSyncLaysBaseProfileAndLocalOverlayIntoSettings(t *testing.T) {
	w := newWorld(t)
	w.profiled()
	w.write(filepath.Join(w.root, "settings", "base.json"), `{"theme": "dark", "model": "sonnet", "permissions": {"deny": ["NotebookEdit"]}}`)
	w.write(filepath.Join(w.root, "settings", "work.json"), `{"model": "opus", "permissions": {"deny": ["CronCreate", "NotebookEdit"]}, "statusLine": {"command": "{{brain}}/hooks/status.sh"}}`)
	w.write(filepath.Join(w.home, ".config", "nerve", "overlays", "work.json"), `{"env": {"TOKEN": "x"}}`)

	w.run(syncCmd)

	got, _ := json.Marshal(w.settings())
	want := `{"env":{"TOKEN":"x"},"model":"opus","permissions":{"deny":["NotebookEdit","CronCreate"]},"statusLine":{"command":"` + w.root + `/hooks/status.sh"},"theme":"dark"}`
	if string(got) != want {
		t.Errorf("settings.json is\n%s\nwant\n%s", got, want)
	}
}

func TestSyncHoldsBackSettingsChangedOutsideTheBrain(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	base := filepath.Join(w.root, "settings", "base.json")
	w.write(base, `{"model": "opus", "theme": "dark"}`)
	w.run(syncCmd)
	w.write(filepath.Join(w.config, "settings.json"), `{"model": "sonnet", "theme": "dark"}`) // as /model would
	w.write(base, `{"model": "opus", "theme": "light"}`)

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(out.String(), `model is "sonnet" here, "opus" in the Brain`) {
		t.Fatalf("got %v, want model reported as drift:\n%s", err, out.String())
	}
	if got := w.settings()["theme"]; got != "dark" {
		t.Errorf("a drifted settings.json was rewritten: theme %v", got)
	}

	w.write(base, `{"model": "sonnet", "theme": "light"}`) // the change moved into the Brain
	w.run(syncCmd)
	if got := w.settings()["theme"]; got != "light" {
		t.Errorf("theme is %v once the Brain agrees, want light", got)
	}
}

func TestSyncDiscardOverwritesDrift(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "settings", "base.json"), `{"model": "opus"}`)
	w.write(filepath.Join(w.config, "settings.json"), `{"model": "sonnet", "hand": true}`) // never written by sync

	var out strings.Builder
	if err := syncCmd(w.root, &out); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("got %v, want drift: sync must not write over settings it never wrote:\n%s", err, out.String())
	}
	m, err := loadMachine()
	if err != nil {
		t.Fatal(err)
	}
	if err := runSync(m, true, newReport(&out, false)); err != nil {
		t.Fatal(err)
	}

	if got, _ := json.Marshal(w.settings()); string(got) != `{"model":"opus"}` {
		t.Errorf("settings.json after --discard is %s", got)
	}
}

func TestCheckRefusesSecretsAndUnknownProfilesInSettings(t *testing.T) {
	w := newWorld(t)
	w.profiled()
	w.write(filepath.Join(w.root, "settings", "base.json"), `{"apiKeyHelper": "pass show anthropic"}`)
	w.write(filepath.Join(w.root, "settings", "play.json"), `{}`)

	var out strings.Builder
	err := runCheck(w.root, newReport(&out, false))

	if err == nil {
		t.Fatalf("check passed:\n%s", out.String())
	}
	for _, want := range []string{
		"problem  settings/base.json: apiKeyHelper may hold secrets",
		`problem  settings/play.json: "play" is neither base nor a profile`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestSyncTakesOverLinksRecordedByTheOldStateFile(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	old := filepath.Join(w.config, "skills", "gone")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(w.home, old); err != nil {
		t.Fatal(err)
	}
	w.write(filepath.Join(w.config, legacyStateFile), `["gone"]`)

	w.run(syncCmd)

	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Errorf("a link recorded in the old state file was left behind")
	}
	if _, err := os.Stat(filepath.Join(w.config, legacyStateFile)); !os.IsNotExist(err) {
		t.Errorf("the old state file is still there")
	}
}

func TestSyncKeepsCLAUDEmdImportingTheBrainsInstructions(t *testing.T) {
	w := newWorld(t)
	w.profiled()
	w.write(filepath.Join(w.root, "instructions", "base.md"), "Be brief.\n")
	w.write(filepath.Join(w.root, "instructions", "work.md"), "Use the VPN.\n")
	claude := filepath.Join(w.config, "CLAUDE.md")
	w.write(claude, "Written on this machine.\n")

	w.run(syncCmd)
	w.run(syncCmd)

	b, _ := os.ReadFile(claude)
	want := blockStart + "\n@" + w.root + "/instructions/base.md\n@" + w.root + "/instructions/work.md\n" + blockEnd + "\n\nWritten on this machine.\n"
	if string(b) != want {
		t.Errorf("CLAUDE.md is\n%s\nwant\n%s", b, want)
	}

	os.RemoveAll(filepath.Join(w.root, "instructions"))
	w.run(syncCmd)

	if b, _ := os.ReadFile(claude); string(b) != "Written on this machine.\n" {
		t.Errorf("CLAUDE.md is %q once the Brain has no instructions, want only the machine's own text", b)
	}
}

func TestSyncLeavesCLAUDEmdAloneWhenTheBrainHasNoInstructions(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")

	w.run(syncCmd)

	if _, err := os.Stat(filepath.Join(w.config, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("sync made a CLAUDE.md with nothing to import")
	}
}

func TestSyncLinksKeybindingsAndUnlinksThemWhenTheBrainDropsThem(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	src := filepath.Join(w.root, "keybindings.json")
	w.write(src, `[]`)
	dst := filepath.Join(w.config, "keybindings.json")

	w.run(syncCmd)

	if got := linkTarget(t, dst); got != src {
		t.Errorf("keybindings.json links to %s, want %s", got, src)
	}

	os.Remove(src)
	w.run(syncCmd)

	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Errorf("keybindings.json is still linked after the Brain dropped it")
	}
}

func TestSyncRefusesToReplaceRealKeybindings(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "keybindings.json"), `[]`)
	w.write(filepath.Join(w.config, "keybindings.json"), `["mine"]`)

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), "real file") {
		t.Fatalf("got %v, want a refusal to replace the real keybindings.json", err)
	}
}

func TestSyncRecordsItsLinksEvenWhenALaterStepFails(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "skills", "grill", "SKILL.md"), skillMD("grill", ""))
	w.write(filepath.Join(w.root, "keybindings.json"), `[]`)
	w.write(filepath.Join(w.config, "keybindings.json"), `["mine"]`) // makes sync fail after linking

	var out strings.Builder
	if err := syncCmd(w.root, &out); err == nil {
		t.Fatal("sync passed despite a real keybindings.json")
	}
	os.Remove(filepath.Join(w.config, "keybindings.json"))
	os.RemoveAll(filepath.Join(w.root, "skills", "grill"))
	w.run(syncCmd)

	if _, err := os.Lstat(filepath.Join(w.config, "skills", "grill")); !os.IsNotExist(err) {
		t.Errorf("grill was linked by a failed sync and never removed once the Brain dropped it")
	}
}

func TestInitStoresConfigDirsThatDoNotDependOnTheWorkingDirectory(t *testing.T) {
	w := newWorld(t)
	os.Remove(filepath.Join(w.home, ".config", "nerve", "config.toml"))
	w.write(filepath.Join(w.root, manifestFile), "")
	t.Chdir(w.home)

	w.run(func(_ string, out *strings.Builder) error {
		return runInit([]string{"repo", ".cfg"}, newReport(out, false))
	})
	t.Chdir(w.root)
	m, err := loadMachine()

	if err != nil || m.Configs[0].Dir != w.config {
		t.Errorf("config dir is %v (%v), want %s", m.Configs, err, w.config)
	}
}

func TestJSONReportCarriesEventsAndTheError(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "settings", "base.json"), `{"model": "opus"}`)
	w.write(filepath.Join(w.config, "settings.json"), `{"model": "sonnet"}`)
	m, err := loadMachine()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	r := newReport(&out, true)

	r.finish(runSync(m, false, r))

	var doc struct {
		Events []Event
		Error  string
	}
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if !slices.Contains(doc.Events, Event{Kind: evDrift, Path: filepath.Join(w.config, "settings.json"), Key: "model", Local: `"sonnet"`, Brain: `"opus"`}) {
		t.Errorf("no drift event for model in %+v", doc.Events)
	}
	if !strings.Contains(doc.Error, "drifted") {
		t.Errorf("error is %q, want the drift error", doc.Error)
	}
}

func TestStatusNamesTheBrainAndEachConfigsProfile(t *testing.T) {
	w := newWorld(t)
	w.profiled()

	out := w.run(func(_ string, out *strings.Builder) error { return runStatus(newReport(out, false)) })

	for _, want := range []string{"brain    " + w.root, "config   " + w.config + " (profile work)", "nerve    "} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

// published makes the Brain a clone of a remote that already has one more
// commit, and returns that commit.
func (w *world) published() string {
	w.t.Helper()
	remote := filepath.Join(w.home, "brain.git")
	w.write(filepath.Join(w.root, manifestFile), "")
	w.gitIn(w.root, "init", "-q", "-b", "main")
	w.gitIn(w.root, "add", "-A")
	w.gitIn(w.root, "commit", "-q", "-m", "a")
	w.gitIn(w.home, "clone", "-q", "--bare", w.root, remote)
	w.gitIn(w.root, "remote", "add", "origin", remote)
	w.gitIn(w.root, "fetch", "-q", "origin")
	w.gitIn(w.root, "branch", "-q", "-u", "origin/main")
	other := filepath.Join(w.home, "other")
	w.gitIn(w.home, "clone", "-q", remote, other)
	w.write(filepath.Join(other, "skills", "grill", "SKILL.md"), skillMD("grill", ""))
	w.gitIn(other, "add", "-A")
	w.gitIn(other, "commit", "-q", "-m", "b")
	w.gitIn(other, "push", "-q")
	return w.gitIn(other, "rev-parse", "HEAD")
}

func TestSyncPullsTheBrainBeforeCarryingIt(t *testing.T) {
	w := newWorld(t)
	head := w.published()

	w.run(syncCmd)

	if got := w.gitIn(w.root, "rev-parse", "HEAD"); got != head {
		t.Errorf("Brain is at %s, want the pushed %s", got, head)
	}
	if _, err := os.Lstat(filepath.Join(w.config, "skills", "grill")); err != nil {
		t.Errorf("the pulled skill was not linked: %v", err)
	}
}

func TestSyncSkipsThePullWhileTheBrainHasUncommittedChanges(t *testing.T) {
	w := newWorld(t)
	head := w.published()
	w.write(filepath.Join(w.root, manifestFile), "profiles = []\n") // a tracked file, edited

	out := w.run(syncCmd)

	if got := w.gitIn(w.root, "rev-parse", "HEAD"); got == head {
		t.Errorf("a Brain with uncommitted changes was pulled")
	}
	if !strings.Contains(out, "no pull  "+w.root+": uncommitted changes") {
		t.Errorf("output does not say the pull was skipped:\n%s", out)
	}
}

func TestSyncInstallsNervesOwnSkillButNeverForClaudeAI(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.config, "skills", "synced", "bucket", "other", "SKILL.md"), skillMD("other", ""))

	out := w.run(syncCmd)

	b, err := os.ReadFile(filepath.Join(w.config, "skills", "nerve", "SKILL.md"))
	if err != nil || !bytes.Equal(b, selfSkill) {
		t.Fatalf("the nerve skill is not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(w.root, "dist", "nerve.zip")); !os.IsNotExist(err) {
		t.Errorf("nerve.zip was built for claude.ai")
	}
	if strings.Contains(out, "nerve on claude.ai") {
		t.Errorf("the nerve skill was reported for upload:\n%s", out)
	}
}

func TestNervesOwnSkillMeetsTheSkillRules(t *testing.T) {
	newWorld(t)
	s, err := installSelf()
	if err != nil {
		t.Fatal(err)
	}
	if p := s.problems(); len(p) > 0 {
		t.Errorf("the nerve skill breaks the rules: %v", p)
	}
}

func TestSyncRefusesABrainSkillNamedNerve(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "skills", "nerve", "SKILL.md"), skillMD("nerve", ""))

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), "takes the name of nerve's own skill") {
		t.Fatalf("got %v, want a name clash error", err)
	}
}

// fakeRelease serves a release of nerve at tag holding bin, with its
// checksum, and points the upgrade at it, at a stand-in binary path, and at a
// recorded re-run. It returns that path and the recorded re-runs.
func (w *world) fakeRelease(running, tag string, bin []byte, sum string) (string, *[]string) {
	w.t.Helper()
	asset := "nerve_" + runtime.GOOS + "_" + runtime.GOARCH
	if sum == "" {
		h := sha256.Sum256(bin)
		sum = hex.EncodeToString(h[:])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/" + tag + "/checksums.txt":
			fmt.Fprintf(rw, "%s  %s\n", sum, asset)
		case "/" + tag + "/" + asset:
			rw.Write(bin)
		default:
			http.NotFound(rw, req)
		}
	}))
	w.t.Cleanup(srv.Close)
	self := filepath.Join(w.home, "bin", "nerve")
	w.write(self, "old binary")
	var reruns []string
	oldURL, oldSelf, oldExec, oldVersion, oldClient := releaseURL, selfPath, reexec, version, httpClient
	releaseURL, version = srv.URL, running
	selfPath = func() (string, error) { return self, nil }
	reexec = func(path string, env []string) error {
		reruns = append(reruns, path+" "+env[len(env)-1])
		w.rerunEnv = env
		return nil
	}
	w.t.Cleanup(func() {
		releaseURL, selfPath, reexec, version, httpClient = oldURL, oldSelf, oldExec, oldVersion, oldClient
	})
	w.write(filepath.Join(w.root, manifestFile), "nerve = \""+tag+"\"\n")
	return self, &reruns
}

func TestSyncUpgradesNerveToTheBrainsPinAndRunsItInstead(t *testing.T) {
	w := newWorld(t)
	self, reruns := w.fakeRelease("v0.1.0", "v0.2.0", []byte("new binary"), "")

	w.run(syncCmd)

	if b, _ := os.ReadFile(self); string(b) != "new binary" {
		t.Errorf("binary is %q, want the pinned release", b)
	}
	if want := []string{self + " " + upgradedEnv + "=v0.2.0"}; !slices.Equal(*reruns, want) {
		t.Errorf("re-runs are %v, want %v", *reruns, want)
	}

	// The re-run is the pinned release, and says what it upgraded from.
	version = "v0.2.0"
	t.Setenv(upgradedFromEnv, "v0.1.0")
	t.Setenv(upgradedEnv, "v0.2.0")
	out := w.run(syncCmd)

	if !strings.Contains(out, "upgraded "+self+" from v0.1.0 to v0.2.0") {
		t.Errorf("the re-run does not report the upgrade:\n%s", out)
	}
	if len(*reruns) != 1 {
		t.Errorf("the re-run upgraded again: %v", *reruns)
	}
}

func TestSyncRefusesAReleaseThatFailsItsChecksum(t *testing.T) {
	w := newWorld(t)
	self, reruns := w.fakeRelease("v0.1.0", "v0.2.0", []byte("tampered"), strings.Repeat("0", 64))

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), "does not match checksums.txt") {
		t.Fatalf("got %v, want a checksum error", err)
	}
	if b, _ := os.ReadFile(self); string(b) != "old binary" || len(*reruns) > 0 {
		t.Errorf("binary %q replaced or re-run %v despite a bad checksum", b, *reruns)
	}
}

func TestSyncLeavesADevelopmentBuildInPlace(t *testing.T) {
	w := newWorld(t)
	self, reruns := w.fakeRelease("(devel)", "v0.2.0", []byte("new binary"), "")

	out := w.run(syncCmd)

	if b, _ := os.ReadFile(self); string(b) != "old binary" || len(*reruns) > 0 {
		t.Errorf("a development build was replaced")
	}
	if !strings.Contains(out, "kept     nerve (devel), a development build, though the Brain pins v0.2.0") {
		t.Errorf("output does not say the upgrade was skipped:\n%s", out)
	}
}

func TestSyncKeepsPullingOnceItHasBuiltZipsIntoTheBrain(t *testing.T) {
	w := newWorld(t)
	w.published()
	w.run(syncCmd) // builds dist/ in a Brain that does not ignore it
	other := filepath.Join(w.home, "other")
	w.write(filepath.Join(other, "skills", "plan", "SKILL.md"), skillMD("plan", ""))
	w.gitIn(other, "add", "-A")
	w.gitIn(other, "commit", "-q", "-m", "c")
	w.gitIn(other, "push", "-q")

	w.run(syncCmd)

	if got, want := w.gitIn(w.root, "rev-parse", "HEAD"), w.gitIn(other, "rev-parse", "HEAD"); got != want {
		t.Errorf("Brain is at %s, want the pushed %s: untracked dist/ blocked the pull", got, want)
	}
}

func TestSyncGivesUpOnAStalledDownload(t *testing.T) {
	w := newWorld(t)
	self, _ := w.fakeRelease("v0.1.0", "v0.2.0", []byte("new binary"), "")
	stalled := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		<-req.Context().Done() // never answers
	}))
	t.Cleanup(stalled.Close)
	releaseURL = stalled.URL
	httpClient = &http.Client{Timeout: 100 * time.Millisecond}

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("got %v, want a timeout", err)
	}
	if b, _ := os.ReadFile(self); string(b) != "old binary" {
		t.Errorf("binary is %q after a failed download", b)
	}
}

func TestSyncSaysHowToUpgradeABinaryItCannotReplace(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	w := newWorld(t)
	self, reruns := w.fakeRelease("v0.1.0", "v0.2.0", []byte("new binary"), "")
	os.Chmod(filepath.Dir(self), 0o555)
	t.Cleanup(func() { os.Chmod(filepath.Dir(self), 0o755) })

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), "cannot replace "+self+" with nerve v0.2.0") || !strings.Contains(err.Error(), "install v0.2.0 by hand") {
		t.Fatalf("got %v, want an error saying how to upgrade", err)
	}
	if len(*reruns) > 0 {
		t.Errorf("re-ran %v without replacing the binary", *reruns)
	}
}

// pinChecksums pins sum as the release's checksums.txt in the Brain.
func (w *world) pinChecksums(tag, sum string) {
	w.write(filepath.Join(w.root, manifestFile), "nerve = \""+tag+"\"\nnerve-checksums = \""+sum+"\"\n")
}

func TestSyncUpgradesWhenTheReleaseMatchesThePinnedChecksums(t *testing.T) {
	w := newWorld(t)
	bin := []byte("new binary")
	self, _ := w.fakeRelease("v0.1.0", "v0.2.0", bin, "")
	h := sha256.Sum256(bin)
	list := sha256.Sum256([]byte(hex.EncodeToString(h[:]) + "  nerve_" + runtime.GOOS + "_" + runtime.GOARCH + "\n"))
	w.pinChecksums("v0.2.0", hex.EncodeToString(list[:]))

	w.run(syncCmd)

	if b, _ := os.ReadFile(self); string(b) != "new binary" {
		t.Errorf("binary is %q, want the pinned release", b)
	}
}

func TestSyncRefusesAReleaseWhoseChecksumsTheBrainDoesNotPin(t *testing.T) {
	w := newWorld(t)
	self, reruns := w.fakeRelease("v0.1.0", "v0.2.0", []byte("new binary"), "")
	w.pinChecksums("v0.2.0", strings.Repeat("0", 64))

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), "does not match nerve-checksums") {
		t.Fatalf("got %v, want a pinned checksums error", err)
	}
	if b, _ := os.ReadFile(self); string(b) != "old binary" || len(*reruns) > 0 {
		t.Errorf("binary %q replaced or re-run %v despite unpinned checksums", b, *reruns)
	}
}

func TestJSONSyncKeepsWhatHappenedBeforeAnUpgrade(t *testing.T) {
	w := newWorld(t)
	w.published()
	w.fakeRelease("v0.1.0", "v0.2.0", []byte("new binary"), "") // edits brain.toml, so the pull is skipped
	m, err := loadMachine()
	if err != nil {
		t.Fatal(err)
	}
	if err := runSync(m, false, newReport(io.Discard, true)); err != nil {
		t.Fatal(err)
	}

	// The re-run is the pinned release, started with the environment the upgrade gave it.
	version = "v0.2.0"
	for _, kv := range w.rerunEnv {
		if k, v, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "NERVE_") {
			t.Setenv(k, v)
		}
	}
	var out strings.Builder
	r := newReport(&out, true)
	r.finish(runSync(m, false, r))

	var doc struct{ Events []Event }
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	var kinds []string
	for _, e := range doc.Events {
		kinds = append(kinds, e.Kind)
	}
	if !slices.Contains(kinds, evUpgraded) || slices.Index(kinds, evPullSkipped) != 0 || slices.Contains(kinds[1:], evPullSkipped) {
		t.Errorf("events are %v, want the first run's one pull-skipped, then the upgrade", kinds)
	}
}

func TestSyncRecordsLinksMadeBeforeARealFolderStoppedIt(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "skills", "a-one", "SKILL.md"), skillMD("a-one", ""))
	w.write(filepath.Join(w.root, "skills", "b-two", "SKILL.md"), skillMD("b-two", ""))
	w.write(filepath.Join(w.config, "skills", "b-two", "SKILL.md"), "mine") // stops sync after a-one is linked

	var out strings.Builder
	if err := syncCmd(w.root, &out); err == nil {
		t.Fatal("sync passed despite a real folder")
	}
	os.RemoveAll(filepath.Join(w.config, "skills", "b-two"))
	os.RemoveAll(filepath.Join(w.root, "skills", "a-one"))
	w.run(syncCmd)

	if _, err := os.Lstat(filepath.Join(w.config, "skills", "a-one")); !os.IsNotExist(err) {
		t.Errorf("a-one was linked by a failed sync and never removed once the Brain dropped it")
	}
}

func TestCheckAcceptsAFrontmatterListAtColumnZero(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "skills", "grill", "SKILL.md"), skillMD("grill", "allowed-tools:\n- Read\n- Bash\n"))

	var out strings.Builder
	if err := runCheck(w.root, newReport(&out, false)); err != nil {
		t.Fatalf("check failed on valid YAML: %v\n%s", err, out.String())
	}
}

func TestCheckCountsDescriptionLengthInCharacters(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "skills", "grill", "SKILL.md"),
		"---\nname: grill\ndescription: "+strings.Repeat("é", 1000)+"\n---\n") // 2000 bytes

	var out strings.Builder
	if err := runCheck(w.root, newReport(&out, false)); err != nil {
		t.Fatalf("check refused a 1000-character description: %v\n%s", err, out.String())
	}
}

func TestSyncKeepsASkillThatLinksToAFolderOffClaudeAI(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "skills", "grill", "SKILL.md"), skillMD("grill", ""))
	w.write(filepath.Join(w.home, "shared", "notes.md"), "notes")
	if err := os.Symlink(filepath.Join(w.home, "shared"), filepath.Join(w.root, "skills", "grill", "refs")); err != nil {
		t.Fatal(err)
	}

	out := w.run(syncCmd)

	if _, err := os.Stat(filepath.Join(w.root, "dist", "grill.zip")); !os.IsNotExist(err) {
		t.Errorf("grill.zip was built without the folder its link leads to")
	}
	if !strings.Contains(out, "skipped  grill: Claude Code only (refs links to a folder or to nothing") {
		t.Errorf("output does not say why grill is skipped:\n%s", out)
	}
}

func TestSyncWritesThroughALinkedCLAUDEmd(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "instructions", "base.md"), "Be brief.\n")
	dotfile := filepath.Join(w.home, "dotfiles", "CLAUDE.md")
	w.write(dotfile, "Mine.\n")
	os.MkdirAll(w.config, 0o755)
	if err := os.Symlink(dotfile, filepath.Join(w.config, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	w.run(syncCmd)

	if got := linkTarget(t, filepath.Join(w.config, "CLAUDE.md")); got != dotfile {
		t.Errorf("CLAUDE.md links to %s, want %s", got, dotfile)
	}
	if b, _ := os.ReadFile(dotfile); !strings.Contains(string(b), blockStart) || !strings.HasSuffix(string(b), "Mine.\n") {
		t.Errorf("the linked file is %q, want the nerve block over its own text", b)
	}
}

func TestSyncRefusesSecretsInTheBrain(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.root, manifestFile), "")
	w.write(filepath.Join(w.root, "settings", "base.json"), `{"env": {"TOKEN": "x"}}`)

	var out strings.Builder
	err := syncCmd(w.root, &out)

	if err == nil || !strings.Contains(err.Error(), "may hold secrets") {
		t.Fatalf("got %v, want a refusal of secrets", err)
	}
	if _, err := os.Stat(filepath.Join(w.config, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("settings.json was written from a Brain holding secrets")
	}
}

func TestMachineFileRefusesARelativeBrain(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.home, ".config", "nerve", "config.toml"), "brain = \"repo\"\n[[config]]\ndir = \"~/.cfg\"\n")

	if _, err := loadMachine(); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("got %v, want a relative brain refused", err)
	}
}

func TestMachineFileRefusesAConfigListedTwice(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.home, ".config", "nerve", "config.toml"),
		"brain = \"~/repo\"\n[[config]]\ndir = \"~/.cfg\"\n[[config]]\ndir = \""+w.config+"/\"\n")

	if _, err := loadMachine(); err == nil || !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("got %v, want the second listing refused", err)
	}
}
