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
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// world is a temp HOME holding a Brain, a project checkout listed nowhere
// yet, and a fake GitHub.
type world struct {
	t        *testing.T
	home     string
	brain    string
	proj     string
	upstream string // fake github.com/acme/up
	rerunEnv []string
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
		brain:    filepath.Join(home, "brain"),
		proj:     filepath.Join(home, "proj"),
		upstream: filepath.Join(home, "github", "acme", "up.git"),
	}
	// As Claude Code does the first time it writes a settings.local.json.
	w.write(filepath.Join(home, ".config", "git", "ignore"), "**/.claude/settings.local.json\n")
	w.write(filepath.Join(w.brain, manifestFile), "")
	w.gitIn(home, "init", "-q", "-b", "main", w.brain)
	w.gitIn(w.brain, "remote", "add", "origin", "git@github.com:Medilies/brain.git")
	w.gitIn(home, "init", "-q", w.upstream)
	w.gitIn(w.upstream, "config", "uploadpack.allowAnySHA1InWant", "true")
	w.gitIn(home, "init", "-q", w.proj)
	w.write(filepath.Join(home, ".config", "nerve", "config.toml"), "brain = \"~/brain\"\n")
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

func (w *world) read(path string) string {
	w.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

// in runs nerve in dir, as the given kind of report, and returns what it
// printed.
func (w *world) in(dir string, mode int, args ...string) (string, error) {
	w.t.Helper()
	w.t.Chdir(dir)
	var out strings.Builder
	r := newReport(&out, mode)
	err := run(args, r)
	r.finish(err)
	return out.String(), err
}

// nerve runs nerve in the project, and fails the test if it fails.
func (w *world) nerve(args ...string) string {
	w.t.Helper()
	out, err := w.in(w.proj, asText, args...)
	if err != nil {
		w.t.Fatalf("nerve %v: %v\n%s", args, err, out)
	}
	return out
}

// nerveFails runs nerve in the project, which must fail, and returns its
// error and output.
func (w *world) nerveFails(args ...string) string {
	w.t.Helper()
	out, err := w.in(w.proj, asText, args...)
	if err == nil {
		w.t.Fatalf("nerve %v passed:\n%s", args, out)
	}
	return err.Error() + "\n" + out
}

func (w *world) manifest(content string) {
	w.write(filepath.Join(w.brain, manifestFile), content)
}

func skillMD(name, extra string) string {
	return "---\nname: " + name + "\ndescription: Does " + name + ".\n" + extra + "---\n\n# " + name + "\n"
}

func (w *world) ownSkill(name string) {
	w.write(filepath.Join(w.brain, "skills", name, "SKILL.md"), skillMD(name, ""))
}

func (w *world) agent(name string) {
	w.write(filepath.Join(w.brain, "agents", name+".md"), skillMD(name, ""))
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

// local reads the project's settings.local.json.
func (w *world) local() map[string]any {
	w.t.Helper()
	obj, err := readObject(filepath.Join(w.proj, localSettings))
	if err != nil {
		w.t.Fatal(err)
	}
	return obj
}

func (w *world) setLocal(obj map[string]any) {
	w.t.Helper()
	if err := writeObject(filepath.Join(w.proj, localSettings), obj); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) loadManifest() Manifest {
	w.t.Helper()
	m, err := loadManifest(w.brain)
	if err != nil {
		w.t.Fatal(err)
	}
	return m
}

func linkTarget(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("%s is not a link: %v", path, err)
	}
	return target
}

func asJSONText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestProjectAddImportsExistingSettingsAgainstCommon(t *testing.T) {
	w := newWorld(t)
	w.manifest(`
[common.settings]
model = "opus"
effortLevel = "high"
syncClaudeAiSkills = false
[common.settings.permissions]
deny = ["NotebookEdit"]
`)
	w.setLocal(map[string]any{
		"model":               "opus",                                                                // the same as common
		"effortLevel":         "low",                                                                 // differs from common
		"permissions":         map[string]any{"deny": []any{"NotebookEdit", "Cron"}, "ask": []any{}}, // shares items with common
		"outputStyle":         "terse",                                                               // not in common
		"env":                 map[string]any{"TOKEN": "abc"},                                        // sensitive
		"statusLine":          map[string]any{"command": "/home/someone/bin/line"},                   // identifying
		"includeCoAuthoredBy": false,
	})

	out := w.nerve("project", "add", "app")

	m := w.loadManifest()
	got := m.Projects["app"].Settings
	want := map[string]any{
		"effortLevel":         "low",
		"permissions":         map[string]any{"deny": []any{"Cron"}, "ask": []any{}},
		"outputStyle":         "terse",
		"includeCoAuthoredBy": false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("project settings are %s, want %s", asJSONText(got), asJSONText(want))
	}
	overlay, _ := readObject(filepath.Join(w.home, ".config", "nerve", "overlays", "app.json"))
	if wantOverlay := `{"env":{"TOKEN":"abc"},"statusLine":{"command":"/home/someone/bin/line"}}`; asJSONText(overlay) != wantOverlay {
		t.Errorf("overlay is %s, want %s", asJSONText(overlay), wantOverlay)
	}
	for _, line := range []string{
		"imported model: already given by common",
		"imported effortLevel: recorded as an override of common",
		"imported permissions.deny: recorded as the items common lacks",
		"imported outputStyle: recorded in the project",
		"imported env.TOKEN: kept in the local overlay: env may hold secrets",
		"imported syncClaudeAiSkills: added by common at the next sync",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("output lacks %q:\n%s", line, out)
		}
	}
	if strings.Contains(w.read(filepath.Join(w.brain, manifestFile)), "TOKEN") {
		t.Errorf("a secret reached brain.toml")
	}
	if _, err := os.Stat(filepath.Join(w.proj, ".claude", "skills")); err == nil {
		t.Errorf("project add wrote in the project before a sync")
	}

	// The first sync keeps everything the project had and adds what common gives.
	before := w.local()
	w.nerve("project", "autosync", "off")
	w.nerve("sync", "--here")

	after := w.local()
	for k, v := range before {
		if !reflect.DeepEqual(after[k], v) {
			t.Errorf("%s went from %s to %s", k, asJSONText(v), asJSONText(after[k]))
		}
	}
	if after["syncClaudeAiSkills"] != false {
		t.Errorf("common's syncClaudeAiSkills was not added: %s", asJSONText(after))
	}
}

func TestSyncLinksSkillsAgentsAndNerveAndKeepsThemOutOfGit(t *testing.T) {
	w := newWorld(t)
	sha := w.commitUpstream(map[string]string{"grilling": skillMD("grilling", "")})
	w.ownSkill("go-stack")
	w.agent("reviewer")
	w.manifest(fmt.Sprintf(`
[[source]]
repo = "acme/up"
sha = %q
skills = ["grilling"]
`, sha))
	w.nerve("project", "add", "app")
	w.nerve("skill", "add", "go-stack")
	w.nerve("skill", "add", "grilling")
	w.nerve("agent", "add", "reviewer")

	w.nerve("sync")

	skills := filepath.Join(w.proj, ".claude", "skills")
	if got := linkTarget(t, filepath.Join(skills, "go-stack")); got != filepath.Join(w.brain, "skills", "go-stack") {
		t.Errorf("own skill links to %s, want the Brain checkout", got)
	}
	if got := linkTarget(t, filepath.Join(skills, "grilling")); !strings.Contains(got, filepath.Join("sources", "acme", "up", sha)) {
		t.Errorf("third-party skill links to %s, want the cache at its pin", got)
	}
	if b, err := os.ReadFile(filepath.Join(skills, "nerve", "SKILL.md")); err != nil || !bytes.Equal(b, selfSkill) {
		t.Errorf("the nerve skill is not linked: %v", err)
	}
	if got := linkTarget(t, filepath.Join(w.proj, ".claude", "agents", "reviewer.md")); got != filepath.Join(w.brain, "agents", "reviewer.md") {
		t.Errorf("agent links to %s", got)
	}
	if out := w.gitIn(w.proj, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("git sees what nerve wrote:\n%s", out)
	}
	if exclude := w.read(filepath.Join(w.proj, ".git", "info", "exclude")); !strings.Contains(exclude, "# nerve: begin\n/.claude/agents/reviewer.md\n") {
		t.Errorf("info/exclude lacks nerve's block:\n%s", exclude)
	}

	// Taking a skill out removes its link, and only its link.
	w.nerve("skill", "rm", "grilling")
	out := w.nerve("sync", "--here")

	if _, err := os.Lstat(filepath.Join(skills, "grilling")); err == nil {
		t.Errorf("the dropped skill is still linked")
	}
	if !strings.Contains(out, "removed  "+filepath.Join(skills, "grilling")) {
		t.Errorf("output does not report the removal:\n%s", out)
	}
	if strings.Contains(w.read(filepath.Join(w.proj, ".git", "info", "exclude")), "grilling") {
		t.Errorf("info/exclude still lists the dropped skill")
	}
}

func TestSyncRefusesASettingsFileGitDoesNotIgnoreOrTracks(t *testing.T) {
	w := newWorld(t)
	w.nerve("project", "add", "app")
	os.Remove(filepath.Join(w.home, ".config", "git", "ignore"))

	if msg := w.nerveFails("sync", "--here"); !strings.Contains(msg, "git does not ignore .claude/settings.local.json") {
		t.Errorf("got %s, want a refusal to write an unignored file", msg)
	}

	w.write(filepath.Join(w.proj, ".gitignore"), ".claude/settings.local.json\n")
	w.setLocal(map[string]any{})
	w.gitIn(w.proj, "add", "-f", ".claude/settings.local.json")

	if msg := w.nerveFails("sync", "--here"); !strings.Contains(msg, "git tracks .claude/settings.local.json") {
		t.Errorf("got %s, want a refusal to write a tracked file", msg)
	}
	if _, err := os.Lstat(filepath.Join(w.proj, ".claude", "skills", "nerve")); err == nil {
		t.Errorf("nerve linked its skill into a project it refused")
	}
}

func TestSyncWritesHooksPluginsAndAutosync(t *testing.T) {
	w := newWorld(t)
	w.manifest(`
[hook.fmt]
event = "PostToolUse"
matcher = "Edit|Write"
command = "{{brain}}/hooks/fmt.sh"
`)
	w.nerve("project", "add", "app")
	w.nerve("hook", "add", "fmt")
	w.nerve("plugin", "add", "gopls@claude-plugins-official")

	w.nerve("sync", "--here")

	hooks, _ := w.local()["hooks"].(map[string]any)
	post := asJSONText(hooks["PostToolUse"])
	if want := `[{"hooks":[{"command":"` + w.brain + `/hooks/fmt.sh","type":"command"}],"matcher":"Edit|Write"}]`; post != want {
		t.Errorf("PostToolUse is %s, want %s", post, want)
	}
	if start := asJSONText(hooks["SessionStart"]); !strings.Contains(start, " sync --here --hook") {
		t.Errorf("SessionStart is %s, want the autosync hook", start)
	}
	if got := asJSONText(w.local()["enabledPlugins"]); got != `{"gopls@claude-plugins-official":true}` {
		t.Errorf("enabledPlugins is %s", got)
	}

	w.nerve("project", "autosync", "off")
	w.nerve("sync", "--here")

	hooks, _ = w.local()["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"]; ok {
		t.Errorf("autosync off left its hook: %s", asJSONText(hooks))
	}
}

func TestSyncKeepsDriftedKeysAndWritesTheRest(t *testing.T) {
	w := newWorld(t)
	w.manifest("autosync = false\n[common.settings]\nmodel = \"opus\"\n")
	w.nerve("project", "add", "app")
	w.nerve("sync", "--here")

	// Claude Code changes one key and approves a command; the Brain changes another.
	obj := w.local()
	obj["model"] = "sonnet"
	obj["permissions"] = map[string]any{"allow": []any{"Bash(make)"}}
	w.setLocal(obj)
	w.nerve("setting", "set", "effortLevel", "high")

	msg := w.nerveFails("sync", "--here")

	for _, line := range []string{
		`drift    model is "sonnet" here, "opus" in the Brain: left as is`,
		`drift    permissions.allow is ["Bash(make)"] here, unset in the Brain: left as is`,
		`set      effortLevel = "high"`,
		"settings drifted in 1 of 1 checkouts",
	} {
		if !strings.Contains(msg, line) {
			t.Errorf("output lacks %q:\n%s", line, msg)
		}
	}
	if got := asJSONText(w.local()); got != `{"effortLevel":"high","model":"sonnet","permissions":{"allow":["Bash(make)"]}}` {
		t.Errorf("settings are %s, want the drift kept and the Brain's change written", got)
	}

	// Drift stays drift until it is moved or discarded.
	if msg := w.nerveFails("sync", "--here"); !strings.Contains(msg, "drift    model") {
		t.Errorf("drift was forgotten:\n%s", msg)
	}
	w.nerve("sync", "--here", "--discard")
	if got := asJSONText(w.local()); got != `{"effortLevel":"high","model":"opus"}` {
		t.Errorf("settings are %s after --discard, want the Brain's", got)
	}
}

func TestRemovingASettingEverywhereRevokesItAtTheNextSync(t *testing.T) {
	w := newWorld(t)
	w.manifest(`
autosync = false
[common.settings.permissions]
deny = ["NotebookEdit", "Cron"]
`)
	w.nerve("project", "add", "app")
	w.nerve("setting", "set", "permissions.deny", `["NotebookEdit"]`)
	w.nerve("setting", "set", "model", "opus")
	w.nerve("sync", "--here")

	out := w.nerve("setting", "rm", "permissions.deny=NotebookEdit", "--all")

	for _, line := range []string{"dropped  setting permissions.deny=NotebookEdit from common", "dropped  setting permissions.deny=NotebookEdit from project app"} {
		if !strings.Contains(out, line) {
			t.Errorf("output lacks %q:\n%s", line, out)
		}
	}
	w.nerve("setting", "rm", "model")
	w.nerve("sync", "--here")

	if got := asJSONText(w.local()); got != `{"permissions":{"deny":["Cron"]}}` {
		t.Errorf("settings are %s, want NotebookEdit and model revoked", got)
	}
}

func TestDeletingALinkDropsItFromTheProject(t *testing.T) {
	w := newWorld(t)
	w.ownSkill("go-stack")
	w.ownSkill("tdd")
	w.nerve("project", "add", "app")
	w.nerve("skill", "add", "go-stack")
	w.nerve("skill", "add", "tdd")
	w.nerve("sync", "--here")

	os.Remove(filepath.Join(w.proj, ".claude", "skills", "go-stack"))
	os.Remove(filepath.Join(w.proj, ".claude", "skills", "nerve"))
	out := w.nerve("sync", "--here")

	if got := w.loadManifest().Projects["app"].Skills; !slices.Equal(got, []string{"tdd"}) {
		t.Errorf("project skills are %v, want go-stack written back out", got)
	}
	if !strings.Contains(out, "dropped  skill go-stack from project app in brain.toml, since its link was deleted: commit it") {
		t.Errorf("output does not report the write-back:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(w.proj, ".claude", "skills", "go-stack")); err == nil {
		t.Errorf("the deleted link came back")
	}
	if _, err := os.Lstat(filepath.Join(w.proj, ".claude", "skills", "nerve")); err != nil {
		t.Errorf("nerve's own skill was not linked again: %v", err)
	}
}

func TestSyncLinksUnderTheOwnersPrefixWhereTheTeamHasTheName(t *testing.T) {
	w := newWorld(t)
	w.ownSkill("go-stack")
	w.nerve("project", "add", "app")
	w.nerve("skill", "add", "go-stack")
	team := filepath.Join(w.proj, ".claude", "skills", "go-stack", "SKILL.md")
	w.write(team, skillMD("go-stack", ""))

	w.nerve("sync", "--here")

	if got := linkTarget(t, filepath.Join(w.proj, ".claude", "skills", "medilies-go-stack")); got != filepath.Join(w.brain, "skills", "go-stack") {
		t.Errorf("prefixed link points to %s", got)
	}
	if fi, err := os.Lstat(filepath.Dir(team)); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("the team's skill was touched")
	}
	if out := w.nerve("skill", "ls"); !strings.Contains(out, "skill go-stack own, linked as .claude/skills/medilies-go-stack") || !strings.Contains(out, "skill go-stack the team's") {
		t.Errorf("skill ls does not show both:\n%s", out)
	}
}

// hookRun runs autosync as Claude Code does, and returns its hook output.
func (w *world) hookRun() map[string]any {
	w.t.Helper()
	out, _ := w.in(w.proj, asHook, "sync", "--here", "--hook")
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		w.t.Fatalf("hook output is not JSON: %v\n%s", err, out)
	}
	return doc
}

func TestAutosyncHandsAReportAndRescansSkillsWhenItLinkedOne(t *testing.T) {
	w := newWorld(t)
	w.ownSkill("go-stack")
	w.nerve("project", "add", "app")
	w.nerve("skill", "add", "go-stack")

	doc := w.hookRun()

	msg, _ := doc["systemMessage"].(string)
	if !strings.HasPrefix(msg, "nerve: ") || !strings.Contains(msg, "linked   "+filepath.Join(w.proj, ".claude", "skills", "go-stack")) {
		t.Errorf("message does not report the link:\n%s", msg)
	}
	if got := asJSONText(doc["hookSpecificOutput"]); got != `{"hookEventName":"SessionStart","reloadSkills":true}` {
		t.Errorf("hookSpecificOutput is %s, want a skill rescan", got)
	}

	doc = w.hookRun()

	if msg, _ := doc["systemMessage"].(string); !strings.Contains(msg, "app is up to date at an uncommitted Brain") {
		t.Errorf("a run that changed nothing says:\n%s", msg)
	}
	if _, ok := doc["hookSpecificOutput"]; ok {
		t.Errorf("a run that linked nothing asked for a rescan")
	}

	// A failure is reported to the user, not swallowed.
	os.Remove(filepath.Join(w.home, ".config", "git", "ignore"))
	if msg, _ := w.hookRun()["systemMessage"].(string); !strings.Contains(msg, "git does not ignore") {
		t.Errorf("the failure is not in the message:\n%s", msg)
	}
}

func TestProjectRmCleansTheCheckoutAndKeepsWhatTheUserChanged(t *testing.T) {
	w := newWorld(t)
	w.ownSkill("go-stack")
	w.manifest("autosync = false\n")
	w.nerve("project", "add", "app")
	w.nerve("skill", "add", "go-stack")
	w.nerve("setting", "set", "model", "opus")
	w.nerve("sync", "--here")
	obj := w.local()
	obj["outputStyle"] = "terse" // the user's own
	w.setLocal(obj)

	w.nerve("project", "rm", "app")

	if _, ok := w.loadManifest().Projects["app"]; ok {
		t.Errorf("the project is still in the Brain")
	}
	if got := asJSONText(w.local()); got != `{"outputStyle":"terse"}` {
		t.Errorf("settings are %s, want only the user's own", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(w.proj, ".claude", "skills")); len(entries) > 0 {
		t.Errorf("links are left: %v", entries)
	}
	if strings.Contains(w.read(filepath.Join(w.proj, ".git", "info", "exclude")), "nerve") {
		t.Errorf("the exclude block is left")
	}
	if strings.Contains(w.read(filepath.Join(w.home, ".config", "nerve", "config.toml")), "checkout") {
		t.Errorf("the machine file still lists the checkout")
	}
}

func TestSyncCleansACheckoutWhoseProjectLeftTheBrain(t *testing.T) {
	w := newWorld(t)
	w.nerve("project", "add", "app")
	w.nerve("sync")
	w.manifest("") // another machine removed it

	out := w.nerve("sync")

	if !strings.Contains(out, "cleaned  "+w.proj+": project app left the Brain") {
		t.Errorf("output does not report the cleanup:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(w.proj, ".claude", "skills", "nerve")); err == nil {
		t.Errorf("nerve's skill link is left")
	}
	if _, err := os.Stat(filepath.Join(w.proj, localSettings)); err == nil {
		t.Errorf("settings.local.json, all nerve's, is left")
	}
}

func TestClaudeAIProjectReportsEachZipOnceAndSkillsToDelete(t *testing.T) {
	w := newWorld(t)
	w.ownSkill("research")
	w.ownSkill("notes")
	w.manifest(`
[project.claude-ai-work]
surface = "claude.ai"
skills = ["research", "notes"]
`)

	out := w.nerve("sync")

	zipPath := filepath.Join(w.home, ".cache", "nerve", "claude.ai", "claude-ai-work", "research.zip")
	if !strings.Contains(out, "upload   "+zipPath+" to claude.ai account claude-ai-work (skill research)") {
		t.Errorf("output does not name the zip to upload:\n%s", out)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if zr.File[0].Name != "research/SKILL.md" {
		t.Errorf("zip holds %s, want research/SKILL.md", zr.File[0].Name)
	}
	zr.Close()

	if out := w.nerve("sync"); strings.Contains(out, "upload") {
		t.Errorf("an unchanged skill was reported again:\n%s", out)
	}

	w.write(filepath.Join(w.brain, "skills", "research", "extra.md"), "more")
	w.nerve("skill", "rm", "notes", "--project", "claude-ai-work")
	out = w.nerve("sync")

	if !strings.Contains(out, "upload   "+zipPath) || !strings.Contains(out, "delete   skill notes from claude.ai account claude-ai-work") {
		t.Errorf("output does not report the change and the removal:\n%s", out)
	}
	if strings.Contains(out, "notes.zip") {
		t.Errorf("a dropped skill was zipped:\n%s", out)
	}
}

func (w *world) problems() string {
	w.t.Helper()
	out, err := w.in(w.brain, asText, "check")
	if err == nil {
		w.t.Fatalf("check passed:\n%s", out)
	}
	return out
}

func TestCheckRefusesWhatAProjectCannotGet(t *testing.T) {
	w := newWorld(t)
	w.ownSkill("go-stack")
	w.manifest(`
[common.settings]
model = "opus"

[project.app]
skills = ["go-stack", "ghost"]
agents = ["nobody"]
hooks = ["fmt"]
plugins = ["no-marketplace"]
[project.app.settings]
model = "opus"

[project.claude-ai-work]
surface = "claude.ai"
skills = ["go-stack"]
plugins = ["x@y"]
`)

	out := w.problems()

	for _, want := range []string{
		`project.app: the Brain has no skill "ghost"`,
		`project.app: the Brain has no agent "nobody"`,
		`project.app: brain.toml defines no hook "fmt"`,
		`project.app: plugin "no-marketplace" must be <plugin>@<marketplace>`,
		"project.app.settings.model: equals common: drop it from the project",
		"project.claude-ai-work: a claude.ai account takes skills only",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check output lacks %q:\n%s", want, out)
		}
	}
}

func TestCheckAndSyncRefuseSensitiveValuesInTheBrain(t *testing.T) {
	w := newWorld(t)
	w.manifest(`
[common.settings]
apiKeyHelper = "get-key"
[common.settings.statusLine]
command = "/home/someone/bin/line"

[hook.notify]
event = "Stop"
command = "notify me@example.com"

[project.app.settings.permissions]
allow = ["WebFetch(domain:ghp_abcdefghijklmnopqrstuvwxyz0123)"]
`)

	out := w.problems()

	for _, want := range []string{
		"common.settings.apiKeyHelper: apiKeyHelper may hold secrets",
		"common.settings.statusLine.command: holds a home path",
		"hook.notify.command: holds an email address",
		"project.app.settings.permissions.allow: looks like a token",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check output lacks %q:\n%s", want, out)
		}
	}
	if msg := w.nerveFails("sync"); !strings.Contains(msg, "values in brain.toml are sensitive") {
		t.Errorf("sync carried sensitive values: %s", msg)
	}
}

func TestSettingSetSendsASensitiveValueToTheOverlay(t *testing.T) {
	w := newWorld(t)
	w.nerve("project", "add", "app")

	out := w.nerve("setting", "set", "env", `{"TOKEN": "abc"}`)

	overlay := filepath.Join(w.home, ".config", "nerve", "overlays", "app.json")
	if !strings.Contains(out, "overlay  env set in "+overlay+": env may hold secrets") {
		t.Errorf("output does not say where it went:\n%s", out)
	}
	if strings.Contains(w.read(filepath.Join(w.brain, manifestFile)), "TOKEN") {
		t.Errorf("a secret reached brain.toml")
	}
	if msg := w.nerveFails("setting", "set", "env", `{"TOKEN": "abc"}`, "--common"); !strings.Contains(msg, "cannot go in common") {
		t.Errorf("got %s, want common refused", msg)
	}
	if out := w.nerve("setting", "ls"); !strings.Contains(out, `env.TOKEN = "abc"  (local overlay)`) {
		t.Errorf("setting ls does not show the overlay:\n%s", out)
	}
}

func TestCommandsFindTheProjectFromAWorktree(t *testing.T) {
	w := newWorld(t)
	w.ownSkill("go-stack")
	w.write(filepath.Join(w.proj, "README"), "x")
	w.gitIn(w.proj, "add", "-A")
	w.gitIn(w.proj, "commit", "-q", "-m", "a")
	w.nerve("project", "add", "app")
	tree := filepath.Join(w.home, "proj-feature")
	w.gitIn(w.proj, "worktree", "add", "-q", "-b", "feature", tree)

	if out, err := w.in(tree, asText, "skill", "add", "go-stack"); err != nil {
		t.Fatalf("skill add from a worktree: %v\n%s", err, out)
	}
	if out, err := w.in(tree, asText, "sync", "--here"); err != nil {
		t.Fatalf("sync from a worktree: %v\n%s", err, out)
	}

	if got := linkTarget(t, filepath.Join(tree, ".claude", "skills", "go-stack")); got != filepath.Join(w.brain, "skills", "go-stack") {
		t.Errorf("worktree link points to %s", got)
	}
	if _, err := os.Lstat(filepath.Join(w.proj, ".claude", "skills", "go-stack")); err == nil {
		t.Errorf("sync --here in a worktree wrote into the main checkout")
	}
	if out := w.gitIn(tree, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("git sees what nerve wrote in the worktree:\n%s", out)
	}
}

func TestCommandsOutsideAListedCheckoutSayHowToAddIt(t *testing.T) {
	w := newWorld(t)
	if msg := w.nerveFails("skill", "add", "go-stack"); !strings.Contains(msg, "is not a listed checkout: run nerve project add <name> here, or pass --project") {
		t.Errorf("got %s", msg)
	}
}

func TestSkillUpdateShowsUpstreamDiffAndMovesThePin(t *testing.T) {
	w := newWorld(t)
	old := w.commitUpstream(map[string]string{"grilling": skillMD("grilling", "")})
	w.manifest(fmt.Sprintf("# pins\n[[source]]\nrepo = \"acme/up\"\nsha = %q\nskills = [\"grilling\"]\n", old))
	next := w.commitUpstream(map[string]string{"grilling": skillMD("grilling", "") + "Grill harder.\n"})

	out, err := w.in(w.brain, asText, "skill", "update", "acme/up")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	if !strings.Contains(out, "+Grill harder.") {
		t.Errorf("output lacks the upstream diff:\n%s", out)
	}
	if got := w.read(filepath.Join(w.brain, manifestFile)); !strings.Contains(got, next) || !strings.HasPrefix(got, "# pins\n") {
		t.Errorf("brain.toml is not moved in place:\n%s", got)
	}
}

func TestSaveManifestKeepsValuesAsWritten(t *testing.T) {
	w := newWorld(t)
	w.manifest("[common.settings]\ncleanupPeriodDays = 30\nmodel = \"opus\"\n")
	w.nerve("project", "add", "app")

	got := w.read(filepath.Join(w.brain, manifestFile))

	if !strings.Contains(got, "cleanupPeriodDays = 30\n") || strings.Contains(got, "[project]\n") || strings.Contains(got, "[common]\n") {
		t.Errorf("brain.toml is not written as a person would:\n%s", got)
	}
	if m := w.loadManifest(); m.Common.Settings["cleanupPeriodDays"] != float64(30) {
		t.Errorf("cleanupPeriodDays reads back as %#v", m.Common.Settings["cleanupPeriodDays"])
	}
}

func TestInitWritesTheMachineFile(t *testing.T) {
	w := newWorld(t)
	os.Remove(filepath.Join(w.home, ".config", "nerve", "config.toml"))

	w.nerve("init", w.brain)

	m, err := loadMachine()
	if err != nil || m.Brain != w.brain {
		t.Errorf("machine file names %q (%v), want %s", m.Brain, err, w.brain)
	}
	if got := w.read(filepath.Join(w.home, ".config", "nerve", "config.toml")); got != "brain = \"~/brain\"\n" {
		t.Errorf("machine file is %q", got)
	}
}

func TestMachineFileRefusesTheOldConfigs(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.home, ".config", "nerve", "config.toml"), "brain = \"~/brain\"\n[[config]]\ndir = \"~/.claude\"\n")
	if _, err := loadMachine(); err == nil || !strings.Contains(err.Error(), "[[config]] is gone since nerve v0.3") {
		t.Errorf("got %v, want the old configs refused", err)
	}
}

func TestStatusShowsTheProjectItsLinksAndDrift(t *testing.T) {
	w := newWorld(t)
	w.nerve("project", "add", "app")
	w.nerve("setting", "set", "model", "opus")
	w.nerve("sync", "--here")
	obj := w.local()
	obj["model"] = "sonnet"
	w.setLocal(obj)

	out := w.nerve("status")

	for _, want := range []string{
		"brain    " + w.brain,
		"project  app: autosync on, 1 setting",
		"skill nerve linked as .claude/skills/nerve",
		`drift    model is "sonnet" here, "opus" in the Brain`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if w.local()["model"] != "sonnet" {
		t.Errorf("status changed the settings")
	}
}

func TestJSONReportCarriesEventsAndTheError(t *testing.T) {
	w := newWorld(t)
	out, err := w.in(w.proj, asJSON, "skill", "add", "ghost")
	if err == nil {
		t.Fatal("skill add outside a listed checkout passed")
	}
	var doc struct {
		Events []Event
		Error  string
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !strings.Contains(doc.Error, "not a listed checkout") || doc.Events == nil {
		t.Errorf("document is %+v", doc)
	}
}

func TestHelpPrintsUsage(t *testing.T) {
	w := newWorld(t)
	if out := w.nerve("help"); !strings.Contains(out, "usage: nerve <command>") {
		t.Errorf("help prints:\n%s", out)
	}
}

// published makes the Brain a clone of a remote that already has one more
// commit, and returns that commit.
func (w *world) published() string {
	w.t.Helper()
	remote := filepath.Join(w.home, "brain.git")
	w.gitIn(w.brain, "add", "-A")
	w.gitIn(w.brain, "commit", "-q", "-m", "a")
	w.gitIn(w.home, "clone", "-q", "--bare", w.brain, remote)
	w.gitIn(w.brain, "remote", "set-url", "origin", remote)
	w.gitIn(w.brain, "fetch", "-q", "origin")
	w.gitIn(w.brain, "branch", "-q", "-u", "origin/main")
	other := filepath.Join(w.home, "other")
	w.gitIn(w.home, "clone", "-q", remote, other)
	w.write(filepath.Join(other, manifestFile), "[common.settings]\nmodel = \"opus\"\n")
	w.gitIn(other, "add", "-A")
	w.gitIn(other, "commit", "-q", "-m", "b")
	w.gitIn(other, "push", "-q")
	return w.gitIn(other, "rev-parse", "HEAD")
}

func TestSyncPullsTheBrainBeforeCarryingIt(t *testing.T) {
	w := newWorld(t)
	head := w.published()

	w.nerve("sync")

	if got := w.gitIn(w.brain, "rev-parse", "HEAD"); got != head {
		t.Errorf("Brain is at %s, want the pushed %s", got, head)
	}
}

func TestSyncSkipsThePullWhileTheBrainHasUncommittedChanges(t *testing.T) {
	w := newWorld(t)
	head := w.published()
	w.manifest("autosync = false\n") // a tracked file, edited

	out := w.nerve("sync")

	if got := w.gitIn(w.brain, "rev-parse", "HEAD"); got == head {
		t.Errorf("a Brain with uncommitted changes was pulled")
	}
	if !strings.Contains(out, "no pull  "+w.brain+": uncommitted changes") {
		t.Errorf("output does not say the pull was skipped:\n%s", out)
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

func TestCheckAcceptsAFrontmatterListAtColumnZero(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.brain, "skills", "go-stack", "SKILL.md"), "---\nname: go-stack\ndescription: Go.\nallowed-tools:\n- Bash\n- Read\n---\n")
	if out, err := w.in(w.brain, asText, "check"); err != nil {
		t.Errorf("check refused a valid list: %v\n%s", err, out)
	}
}

func TestCheckCountsDescriptionLengthInCharacters(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.brain, "skills", "go-stack", "SKILL.md"), "---\nname: go-stack\ndescription: "+strings.Repeat("é", 1024)+"\n---\n")
	if out, err := w.in(w.brain, asText, "check"); err != nil {
		t.Errorf("check counted bytes: %v\n%s", err, out)
	}
	w.write(filepath.Join(w.brain, "skills", "tdd", "SKILL.md"), "---\nname: other\ndescription: x\n---\n")
	if out := w.problems(); !strings.Contains(out, `skills/tdd: frontmatter name "other" differs from folder name`) {
		t.Errorf("check output:\n%s", out)
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
	w.manifest("nerve = \"" + tag + "\"\n")
	return self, &reruns
}

func TestSyncUpgradesNerveToTheBrainsPinAndRunsItInstead(t *testing.T) {
	w := newWorld(t)
	self, reruns := w.fakeRelease("v0.1.0", "v0.2.0", []byte("new binary"), "")

	w.nerve("sync")

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
	out := w.nerve("sync")

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

	if msg := w.nerveFails("sync"); !strings.Contains(msg, "does not match checksums.txt") {
		t.Fatalf("got %s, want a checksum error", msg)
	}
	if b, _ := os.ReadFile(self); string(b) != "old binary" || len(*reruns) > 0 {
		t.Errorf("binary %q replaced or re-run %v despite a bad checksum", b, *reruns)
	}
}

func TestSyncLeavesADevelopmentBuildInPlace(t *testing.T) {
	w := newWorld(t)
	self, reruns := w.fakeRelease("(devel)", "v0.2.0", []byte("new binary"), "")

	out := w.nerve("sync")

	if b, _ := os.ReadFile(self); string(b) != "old binary" || len(*reruns) > 0 {
		t.Errorf("a development build was replaced")
	}
	if !strings.Contains(out, "kept     nerve (devel), a development build, though the Brain pins v0.2.0") {
		t.Errorf("output does not say the upgrade was skipped:\n%s", out)
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

	if msg := w.nerveFails("sync"); !strings.Contains(msg, "Timeout") {
		t.Fatalf("got %s, want a timeout", msg)
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

	if msg := w.nerveFails("sync"); !strings.Contains(msg, "cannot replace "+self+" with nerve v0.2.0") || !strings.Contains(msg, "install v0.2.0 by hand") {
		t.Fatalf("got %s, want an error saying how to upgrade", msg)
	}
	if len(*reruns) > 0 {
		t.Errorf("re-ran %v without replacing the binary", *reruns)
	}
}

func TestSyncUpgradesWhenTheReleaseMatchesThePinnedChecksums(t *testing.T) {
	w := newWorld(t)
	bin := []byte("new binary")
	self, _ := w.fakeRelease("v0.1.0", "v0.2.0", bin, "")
	h := sha256.Sum256(bin)
	list := sha256.Sum256([]byte(hex.EncodeToString(h[:]) + "  nerve_" + runtime.GOOS + "_" + runtime.GOARCH + "\n"))
	w.manifest("nerve = \"v0.2.0\"\nnerve-checksums = \"" + hex.EncodeToString(list[:]) + "\"\n")

	w.nerve("sync")

	if b, _ := os.ReadFile(self); string(b) != "new binary" {
		t.Errorf("binary is %q, want the pinned release", b)
	}
}

func TestSyncRefusesAReleaseWhoseChecksumsTheBrainDoesNotPin(t *testing.T) {
	w := newWorld(t)
	self, reruns := w.fakeRelease("v0.1.0", "v0.2.0", []byte("new binary"), "")
	w.manifest("nerve = \"v0.2.0\"\nnerve-checksums = \"" + strings.Repeat("0", 64) + "\"\n")

	if msg := w.nerveFails("sync"); !strings.Contains(msg, "does not match nerve-checksums") {
		t.Fatalf("got %s, want a pinned checksums error", msg)
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
	if err := runSync(m, flags{}, newReport(io.Discard, asJSON)); err != nil {
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
	r := newReport(&out, asJSON)
	r.finish(runSync(m, flags{}, r))

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
