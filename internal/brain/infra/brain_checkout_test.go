package infra

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

type events []app.Event

func (e *events) Add(ev app.Event) { *e = append(*e, ev) }

func (e events) String() string {
	var lines []string
	for _, ev := range e {
		lines = append(lines, ev.String())
	}
	return strings.Join(lines, "\n")
}

func TestManifestRoundTripsAsAPersonWouldWriteIt(t *testing.T) {
	e := newTestEnv(t)
	brain := NewBrainCheckout(e.path("brain"))
	e.write(e.path("brain/brain.toml"), `allowed-settings = ["statusLine"]

[[source]]
repo = "acme/up"
sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"
path = "plugins/x/skills"
skills = ["grilling"]

[hook.fmt]
event = "PostToolUse"
command = "{{brain}}/hooks/fmt.sh"

[common.settings]
cleanupPeriodDays = 30
[common.settings.permissions]
deny = ["NotebookEdit"]

[project.app]
skills = ["grilling"]
[project.app.settings]
model = "opus"
`)

	m, err := brain.Load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Common["cleanupPeriodDays"] != int64(30) || m.Projects["app"].Settings["model"] != "opus" || m.Hooks["fmt"].Event != "PostToolUse" || !slices.Equal(m.AllowedSettings, []string{"statusLine"}) {
		t.Errorf("loaded %+v", m)
	}
	if err := brain.Save(m); err != nil {
		t.Fatal(err)
	}
	got := e.read(e.path("brain/brain.toml"))
	if !strings.Contains(got, "sha = \"5863806dd4e15c63174bee4e7515a4256792ecfa\"\npath = \"plugins/x/skills\"\n") || !strings.Contains(got, "cleanupPeriodDays = 30\n") || !strings.HasPrefix(got, "allowed-settings = [\"statusLine\"]\n") || strings.Contains(got, "[project]\n") || strings.Contains(got, "[common]\n") || strings.Contains(got, "[hook]\n") {
		t.Errorf("saved as:\n%s", got)
	}
	again, err := brain.Load()
	if err != nil || !reflect.DeepEqual(again, m) {
		t.Errorf("a saved manifest reads back as %+v (%v), want %+v", again, err, m)
	}
}

func TestLoadRefusesUnknownKeysAndInvalidValues(t *testing.T) {
	e := newTestEnv(t)
	brain := NewBrainCheckout(e.path("brain"))
	for content, want := range map[string]string{
		"profiles = []\n":                          "brain.toml: unknown keys [profiles]",
		"[project.app]\nskill = [\"x\"]\n":         "brain.toml: unknown keys [project.app.skill]",
		"[project.app]\nsurface = \"claude.ai\"\n": "brain.toml: unknown keys [project.app.surface]",
		"nerve = \"v0.3.0\"\n":                     "brain.toml: unknown keys [nerve]",
		"autosync = false\n":                       "brain.toml: unknown keys [autosync]",
	} {
		e.write(e.path("brain/brain.toml"), content)
		if _, err := brain.Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", content, err, want)
		}
	}
}

// published makes the Brain a clone of a remote that has one more commit,
// and returns that commit.
func (e *testEnv) published() string {
	e.t.Helper()
	brain := e.repo(e.path("brain"))
	e.write(filepath.Join(brain, "brain.toml"), "")
	e.commit(brain)
	remote := e.path("brain.git")
	e.git(e.home, "clone", "-q", "--bare", brain, remote)
	e.git(brain, "remote", "add", "origin", remote)
	e.git(brain, "fetch", "-q", "origin")
	e.git(brain, "branch", "-q", "-u", "origin/main")
	other := e.path("other")
	e.git(e.home, "clone", "-q", remote, other)
	e.write(filepath.Join(other, "brain.toml"), "allowed-settings = [\"statusLine\"]\n")
	head := e.commit(other)
	e.git(other, "push", "-q")
	return head
}

func TestBrainSyncMakesTheBrainItsRemotes(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil {
		t.Fatal(err)
	}

	if got := e.git(e.path("brain"), "rev-parse", "HEAD"); got != head {
		t.Errorf("Brain is at %s, want the pushed %s", got, head)
	}
	if r.String() != "synced   the Brain to "+head[:7] {
		t.Errorf("events: %s", r)
	}
}

func TestBrainSyncSkipsABrainWhoseRemoteCannotBeReached(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	if err := os.Rename(e.path("brain.git"), e.path("moved.git")); err != nil {
		t.Fatal(err)
	}
	var r events

	err := NewBrainCheckout(e.path("brain")).Sync(true, &r)

	if err != nil || !strings.HasPrefix(r.String(), "no sync  the Brain at "+e.path("brain")+": could not fetch: fatal: '"+e.path("brain.git")+"' does not appear to be a git repository") {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestBrainSyncFetchesAnUpstreamItNeverFetched(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	e.git(e.path("brain"), "update-ref", "-d", "refs/remotes/origin/main") // as a clone of one branch, or a prune, leaves it
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil || e.git(e.path("brain"), "rev-parse", "HEAD") != head {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestBrainSyncGivesUpOnARemoteThatDoesNotAnswer(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_1", "protocol.ext.allow")
	t.Setenv("GIT_CONFIG_VALUE_1", "always")
	e.git(e.path("brain"), "remote", "set-url", "origin", "ext::sh -c sleep% 10")
	defer func(d time.Duration) { syncTimeout = d }(syncTimeout)
	syncTimeout = time.Second
	var r events

	err := NewBrainCheckout(e.path("brain")).Sync(true, &r)

	if err != nil || r.String() != "no sync  the Brain at "+e.path("brain")+": could not fetch: git fetch: no answer after 1s" {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestBrainSyncFollowsABranchATagIsNamedLike(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	e.git(e.path("brain"), "tag", "main")
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil || e.git(e.path("brain"), "rev-parse", "HEAD") != head {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestBrainSyncSaysWhenItsUpstreamBranchIsGoneFromTheRemote(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.git(e.path("brain.git"), "update-ref", "-d", "refs/heads/main")
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil || !strings.HasSuffix(r.String(), ": its upstream branch is not on the remote") {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestBrainSyncSaysWhenHeadIsDetached(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.git(e.path("brain"), "checkout", "-q", "--detach")
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil || !strings.HasSuffix(r.String(), ": detached HEAD") {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestBrainSyncSaysWhenGitCannotRun(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	t.Setenv("PATH", t.TempDir())
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil || !strings.Contains(r.String(), ": cannot read its git state: ") {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestStateCountsANewFileNotYetCommitted(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.write(e.path("brain/skills/newone/SKILL.md"), skillMD("newone", ""))

	if got := NewBrainCheckout(e.path("brain")).State(); got != "uncommitted changes" {
		t.Errorf("state is %q", got)
	}
}

func TestStateCountsTheCommitsNotPushed(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.write(e.path("brain/skills/x/SKILL.md"), skillMD("x", ""))
	e.commit(e.path("brain"))

	if got := NewBrainCheckout(e.path("brain")).State(); got != "1 commit not pushed" {
		t.Errorf("state is %q", got)
	}
}

func TestStateCountsTheCommitsOfABranchWithNoUpstream(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.git(e.path("brain"), "checkout", "-q", "-b", "feat")
	for _, name := range []string{"x", "y"} {
		e.write(e.path("brain/skills/"+name+"/SKILL.md"), skillMD(name, ""))
		e.commit(e.path("brain"))
	}

	if got := NewBrainCheckout(e.path("brain")).State(); got != "2 commits not pushed" {
		t.Errorf("state is %q", got)
	}
}

func TestStateSaysWhenGitFails(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.write(e.path("brain/.git/index"), "garbage")

	if got := NewBrainCheckout(e.path("brain")).State(); !strings.HasPrefix(got, "cannot read its git state: ") {
		t.Errorf("state is %q", got)
	}
}

func TestOwnSkillsReadEveryFolderAndLinkedFolder(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("brain/skills/ok/SKILL.md"), skillMD("ok", ""))
	e.write(e.path("brain/skills/notes/todo.txt"), "x")
	e.write(e.path("real/linked/SKILL.md"), "---\nname: linked\n---\n")
	e.link(e.path("brain/skills/linked"), e.path("real/linked"))
	e.write(e.path("brain/skills/README.md"), "x")
	e.link(e.path("brain/skills/gone"), e.path("real/gone"))

	all, err := NewBrainCheckout(e.path("brain")).OwnSkills()

	got := map[string]string{}
	for _, s := range all {
		got[s.Name] = fmt.Sprintf("%v %s", s.Err != nil, s.Meta["name"])
	}
	if want := map[string]string{"ok": "false ok", "notes": "true ", "linked": "false linked", "gone": "true "}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("own skills are %v (%v), want %v", got, err, want)
	}
}

func TestAgentsAreReadFromABrainWhosePathLooksLikeAPattern(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("b[1]/brain/agents/reviewer.md"), skillMD("reviewer", ""))
	e.write(e.path("b1/brain/agents/other.md"), skillMD("other", "")) // what the pattern b[1] matches

	agents, err := NewBrainCheckout(e.path("b[1]/brain")).Agents()

	if err != nil || len(agents) != 1 || agents[0].Name != "reviewer" {
		t.Errorf("agents are %+v (%v)", agents, err)
	}
}

func TestSaveRefusesWhatLoadWould(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("brain/brain.toml"), "[project.app]\n")
	m := domain.Manifest{Projects: map[string]*domain.Project{"app": {Agents: []string{"Reviewer"}}}}

	err := NewBrainCheckout(e.path("brain")).Save(m)

	if err == nil || !strings.Contains(err.Error(), `agent "Reviewer" must be lowercase words joined by hyphens`) || e.read(e.path("brain/brain.toml")) != "[project.app]\n" {
		t.Errorf("got %v, brain.toml:\n%s", err, e.read(e.path("brain/brain.toml")))
	}
}

func TestOwnSkillsAndAgentsAreReadFromTheBrain(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("brain/skills/go-stack/SKILL.md"), skillMD("go-stack", ""))
	e.write(e.path("brain/agents/reviewer.md"), skillMD("reviewer", ""))
	e.write(e.path("brain/agents/broken.md"), "no frontmatter")
	b := NewBrainCheckout(e.path("brain"))

	s, ok, err := b.OwnSkill("go-stack")
	if !ok || err != nil || s.Dir != e.path("brain/skills/go-stack") || s.Meta["description"] != "Does go-stack." {
		t.Errorf("got %+v, %v, %v", s, ok, err)
	}
	if _, ok, _ := b.OwnSkill("ghost"); ok {
		t.Errorf("found a skill that is not there")
	}
	if all, err := b.OwnSkills(); err != nil || len(all) != 1 {
		t.Errorf("own skills are %v (%v)", all, err)
	}
	if path, ok := b.AgentFile("reviewer"); !ok || path != e.path("brain/agents/reviewer.md") {
		t.Errorf("agent file %s, %v", path, ok)
	}
	agents, err := b.Agents()
	if err != nil || len(agents) != 2 || agents[0].Err == nil || agents[1].Meta["name"] != "reviewer" {
		t.Errorf("agents are %+v (%v)", agents, err)
	}
}

func TestHeadAndStateOfAPlainFolderAreEmpty(t *testing.T) {
	e := newTestEnv(t)
	b := NewBrainCheckout(e.path("brain"))
	e.write(e.path("brain/brain.toml"), "")
	if b.Head() != "" || b.State() != "" {
		t.Errorf("head %q, state %q", b.Head(), b.State())
	}
	var r events
	if err := b.Sync(true, &r); err != nil || r.String() != "no sync  the Brain at "+e.path("brain")+": it is not a git repo" {
		t.Errorf("synced a plain folder: %v %s", err, r)
	}
}

func TestHeadOfAPlainFolderInsideARepoIsEmpty(t *testing.T) {
	e := newTestEnv(t)
	dot := e.repo(e.path("dot"))
	e.write(filepath.Join(dot, "brain/brain.toml"), "")
	e.commit(dot)

	if head := NewBrainCheckout(filepath.Join(dot, "brain")).Head(); head != "" {
		t.Errorf("head is %q, the commit of the repo around the Brain", head)
	}
}

func TestLoadRefusesSettingsJSONCannotHold(t *testing.T) {
	e := newTestEnv(t)
	for toml, key := range map[string]string{
		"[common.settings]\nx = inf\n":                  "common.settings.x",
		"[project.app.settings]\nx = [nan]\n":           "project.app.settings.x",
		"[project.app.settings.a]\nwhen = 2024-01-01\n": "project.app.settings.a.when",
	} {
		e.write(e.path("brain/brain.toml"), toml)
		if _, err := NewBrainCheckout(e.path("brain")).Load(); err == nil || !strings.Contains(err.Error(), key+": settings cannot hold inf, nan or dates") {
			t.Errorf("%s: got %v", toml, err)
		}
	}
}

func TestSettingsRoundTripBrainTomlWithEveryDigit(t *testing.T) {
	e := newTestEnv(t)
	b := NewBrainCheckout(e.path("brain"))
	m := domain.Manifest{Common: map[string]any{"big": domain.ParseValue("9007199254740993"), "f": domain.ParseValue("1.5"), "n": domain.ParseValue("2.0")}}

	if err := b.Save(m); err != nil {
		t.Fatal(err)
	}
	got, err := b.Load()

	if err != nil || !reflect.DeepEqual(got.Common, m.Common) {
		t.Errorf("loaded %v (%v), want %v", got.Common, err, m.Common)
	}
}

func TestSettingsRoundTripBrainTomlWithFloatsBeyondInt64(t *testing.T) {
	e := newTestEnv(t)
	b := NewBrainCheckout(e.path("brain"))
	m := domain.Manifest{Common: map[string]any{"a": domain.ParseValue("1e19"), "b": domain.ParseValue("1e20")}}

	if err := b.Save(m); err != nil {
		t.Fatal(err)
	}
	got, err := b.Load()
	if err != nil || !reflect.DeepEqual(got.Common, m.Common) {
		t.Fatalf("loaded %#v (%v), want %#v", got.Common, err, m.Common)
	}

	if err := b.Save(got); err != nil {
		t.Errorf("saving what was loaded: %v", err)
	}
}

func TestLoadRefusesAWholeNumberBeyondInt64(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("brain/brain.toml"), "[common.settings]\nuid = 18446744073709551616\n")

	if _, err := NewBrainCheckout(e.path("brain")).Load(); err == nil {
		t.Errorf("loaded a whole number beyond 64 bits")
	}
}

func TestSaveRefusesAWholeNumberBrainTomlCannotHold(t *testing.T) {
	e := newTestEnv(t)
	m := domain.Manifest{Common: map[string]any{"a": map[string]any{"uid": domain.ParseValue("18446744073709551615")}}}

	if err := NewBrainCheckout(e.path("brain")).Save(m); err == nil || !strings.Contains(err.Error(), "common.settings.a.uid") {
		t.Errorf("got %v, want the key named", err)
	}
}

func TestSaveNamesASettingThatHoldsNull(t *testing.T) {
	e := newTestEnv(t)
	m := domain.Manifest{Projects: map[string]*domain.Project{"app": {Settings: map[string]any{"a": []any{nil, 1}}}}}

	err := NewBrainCheckout(e.path("brain")).Save(m)

	if err == nil || !strings.Contains(err.Error(), "project.app.settings.a: settings cannot hold null") {
		t.Errorf("got %v", err)
	}
}

func TestBrainSyncDropsLocalCommitsAndChangesAndSaysSo(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	e.write(e.path("brain/skills/x/SKILL.md"), "x")
	e.commit(e.path("brain"))
	e.write(e.path("brain/brain.toml"), "[project.app]\n") // a tracked file, edited
	e.write(e.path("brain/skills/new/SKILL.md"), "new")    // untracked, left
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil {
		t.Fatal(err)
	}

	if got := e.git(e.path("brain"), "rev-parse", "HEAD"); got != head {
		t.Errorf("Brain is at %s, want the remote's %s", got, head)
	}
	if b, _ := os.ReadFile(e.path("brain/brain.toml")); string(b) != "allowed-settings = [\"statusLine\"]\n" {
		t.Errorf("brain.toml is %q, want the remote's", b)
	}
	if _, err := os.Stat(e.path("brain/skills/new/SKILL.md")); err != nil {
		t.Errorf("an untracked file is gone: %v", err)
	}
	if !strings.HasPrefix(r.String(), "synced   the Brain to "+head[:7]+", dropping 1 local commit (") || !strings.HasSuffix(r.String(), " c) and changes to 1 file (brain.toml)") {
		t.Errorf("events: %s", r)
	}
}

func TestBrainSyncTakesTheRemotesFileOrFolderOverAnUntrackedOneInTheWay(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.write(e.path("other/x/file"), "theirs")
	e.write(e.path("other/y"), "theirs")
	head := e.commit(e.path("other"))
	e.git(e.path("other"), "push", "-q")
	e.write(e.path("brain/x"), "mine")   // a file where the remote has a folder
	e.write(e.path("brain/y/a"), "mine") // a folder where the remote has a file
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil {
		t.Fatalf("got %v, events: %s", err, r)
	}

	if got := e.git(e.path("brain"), "rev-parse", "HEAD"); got != head {
		t.Errorf("Brain is at %s, want %s", got, head)
	}
	if got := e.read(e.path("brain/y")); got != "theirs" {
		t.Errorf("y is %q, want the remote's", got)
	}
	if !strings.HasSuffix(r.String(), ", dropping 2 untracked files the remote has (x, y/a)") {
		t.Errorf("events: %s", r)
	}
}

func TestBrainSyncNamesAFewOfTheFilesItDrops(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	for _, f := range []string{"a", "b", "c", "d"} {
		e.write(e.path("brain/"+f), f)
	}
	e.git(e.path("brain"), "add", "-A")
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil {
		t.Fatal(err)
	}

	if r.String() != "synced   the Brain to "+head[:7]+", dropping changes to 4 files (a, b, c and 1 more)" {
		t.Errorf("events: %s", r)
	}
}

func TestBrainSyncSaysWhenThereIsNoUpstream(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.git(e.path("brain"), "branch", "-q", "--unset-upstream")
	var r events

	if err := NewBrainCheckout(e.path("brain")).Sync(true, &r); err != nil || !strings.HasSuffix(r.String(), ": no upstream branch") {
		t.Errorf("got %v, events: %s", err, r)
	}
}

// identity gives git the user's name and email, as their git config does.
func (e *testEnv) identity() {
	e.t.Setenv("GIT_CONFIG_COUNT", "3")
	e.t.Setenv("GIT_CONFIG_KEY_1", "user.name")
	e.t.Setenv("GIT_CONFIG_VALUE_1", "t")
	e.t.Setenv("GIT_CONFIG_KEY_2", "user.email")
	e.t.Setenv("GIT_CONFIG_VALUE_2", "t@t")
}

func TestPushRefusesWhenTheRemoteHasCommitsTheBrainLacks(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	theirs := e.published() // the remote holds a commit this Brain lacks
	e.write(e.path("brain/skills/y/SKILL.md"), "y")
	e.commit(e.path("brain")) // and this Brain one the remote lacks
	e.write(e.path("brain/skills/x/SKILL.md"), "x")

	err := NewBrainCheckout(e.path("brain")).Push("add x", false, &events{})

	if err == nil || !strings.Contains(err.Error(), "the remote has 1 commit ("+theirs[:7]) || !strings.Contains(err.Error(), "nerve push -f overwrites them") {
		t.Errorf("got %v", err)
	}
	if got := e.git(e.path("brain.git"), "rev-parse", "main"); got != theirs {
		t.Errorf("remote moved to %s", got)
	}
	if e.git(e.path("brain"), "status", "--porcelain") != "?? skills/x/" {
		t.Errorf("the Brain's changes were committed")
	}
}

func TestPushForceCommitsEverythingAndOverwritesTheRemote(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	e.published() // the remote holds a commit this Brain lacks
	e.write(e.path("brain/skills/x/SKILL.md"), "x")
	var r events

	if err := NewBrainCheckout(e.path("brain")).Push("add x", true, &r); err != nil {
		t.Fatal(err)
	}

	head := e.git(e.path("brain"), "rev-parse", "HEAD")
	if got := e.git(e.path("brain.git"), "rev-parse", "main"); got != head {
		t.Errorf("remote is at %s, want the Brain's %s", got, head)
	}
	if got := e.git(e.path("brain"), "log", "-1", "--format=%s"); got != "add x" {
		t.Errorf("commit says %q", got)
	}
	if r.String() != "pushed   the Brain at "+head[:7]+": add x" {
		t.Errorf("events: %s", r)
	}
}

func TestPushOfABrainItsRemoteHoldsSaysSo(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	b := NewBrainCheckout(e.path("brain"))
	e.must(b.Sync(true, &events{}))
	var r events

	if err := b.Push("", false, &r); err != nil || r.String() != "the Brain at "+b.Head()[:7]+" is its remote's already" {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestRefreshBringsTheRemotesCommits(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	e.write(e.path("brain/skills/x/SKILL.md"), "x") // untracked, kept
	var r events

	if err := NewBrainCheckout(e.path("brain")).Refresh(&r); err != nil {
		t.Fatal(err)
	}

	if got := e.git(e.path("brain"), "rev-parse", "HEAD"); got != head {
		t.Errorf("Brain is at %s, want %s", got, head)
	}
	if e.read(e.path("brain/skills/x/SKILL.md")) != "x" || r.String() != "synced   the Brain to "+head[:7] {
		t.Errorf("events: %s", r)
	}
}

func TestRefreshWarnsThatPushWouldOverwriteTheRemote(t *testing.T) {
	e := newTestEnv(t)
	e.published() // the remote has a commit this Brain lacks
	e.write(e.path("brain/brain.toml"), "[project.app]\n")

	err := NewBrainCheckout(e.path("brain")).Refresh(&events{})

	if err == nil || !strings.Contains(err.Error(), "the remote has 1 commit (") {
		t.Errorf("got %v", err)
	}
}

func TestRefreshRefusesWhatItWouldDrop(t *testing.T) {
	for want, setup := range map[string]func(e *testEnv){
		"brain.toml has changes not pushed: nerve push sends them, nerve sync drops them": func(e *testEnv) {
			e.git(e.path("brain"), "pull", "-q", "--ff-only")
			e.write(e.path("brain/brain.toml"), "[project.app]\n")
		},
		"its remote lacks: nerve push sends them, nerve sync drops them": func(e *testEnv) {
			e.git(e.path("brain"), "pull", "-q", "--ff-only")
			e.write(e.path("brain/skills/x/SKILL.md"), "x")
			e.commit(e.path("brain"))
		},
		"has no upstream branch: give its branch one, with git push -u": func(e *testEnv) {
			e.git(e.path("brain"), "branch", "-q", "--unset-upstream")
		},
		"could not fetch": func(e *testEnv) {
			e.git(e.path("brain"), "remote", "set-url", "origin", e.path("nowhere.git"))
		},
		"would be overwritten": func(e *testEnv) { // an untracked file where the remote now has one
			e.write(e.path("brain/x"), "mine")
			e.write(e.path("other/x"), "theirs")
			e.commit(e.path("other"))
			e.git(e.path("other"), "push", "-q")
		},
		"is not a git repo": func(e *testEnv) {
			if err := os.Rename(e.path("brain/.git"), e.path("brain/.git-away")); err != nil {
				e.t.Fatal(err)
			}
		},
	} {
		e := newTestEnv(t)
		e.published()
		setup(e)
		before, _ := os.ReadFile(e.path("brain/brain.toml"))

		err := NewBrainCheckout(e.path("brain")).Refresh(&events{})

		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want %q", err, want)
		}
		if after, _ := os.ReadFile(e.path("brain/brain.toml")); string(after) != string(before) {
			t.Errorf("%s: brain.toml changed", want)
		}
		if b, err := os.ReadFile(e.path("brain/x")); err == nil && string(b) != "mine" {
			t.Errorf("%s: an untracked file was overwritten", want)
		}
	}
}

func TestPushManifestCommitsBrainTomlAloneAndPushesIt(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	e.published()
	b := NewBrainCheckout(e.path("brain"))
	e.must(b.Refresh(&events{}))
	e.write(e.path("brain/brain.toml"), "[project.app]\n")
	e.write(e.path("brain/skills/x/SKILL.md"), "x") // untracked: not part of it
	e.write(e.path("brain/README"), "edited")       // a new file, staged: not part of it
	e.git(e.path("brain"), "add", "README")
	var r events

	if err := b.PushManifest("nerve: add project app", &r); err != nil {
		t.Fatal(err)
	}

	head := e.git(e.path("brain"), "rev-parse", "HEAD")
	if got := e.git(e.path("brain.git"), "rev-parse", "main"); got != head {
		t.Errorf("remote is at %s, want %s", got, head)
	}
	if got := e.git(e.path("brain"), "show", "--name-only", "--format=%s", "HEAD"); got != "nerve: add project app\n\nbrain.toml" {
		t.Errorf("commit is %q", got)
	}
	if r.String() != "pushed   the Brain at "+head[:7]+": nerve: add project app" {
		t.Errorf("events: %s", r)
	}
}

func TestPushManifestThatCannotPushLeavesBrainTomlAsTheRemoteHoldsIt(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	e.published()
	b := NewBrainCheckout(e.path("brain"))
	e.must(b.Refresh(&events{}))
	before := e.git(e.path("brain"), "rev-parse", "HEAD")
	theirs := e.read(e.path("brain/brain.toml"))
	e.write(e.path("other/z"), "z") // the remote moves on meanwhile
	e.commit(e.path("other"))
	e.git(e.path("other"), "push", "-q")
	e.write(e.path("brain/brain.toml"), "[project.app]\n")
	e.write(e.path("brain/skills/x/SKILL.md"), "mine") // the user's work, kept
	e.git(e.path("brain"), "add", "skills/x/SKILL.md")

	if err := b.PushManifest("nerve: add project app", &events{}); !errors.Is(err, app.ErrRemoteMoved) {
		t.Errorf("got %v", err)
	}

	if got := e.git(e.path("brain"), "rev-parse", "HEAD"); got != before {
		t.Errorf("Brain is at %s, want %s as before", got, before)
	}
	if e.read(e.path("brain/brain.toml")) != theirs || e.read(e.path("brain/skills/x/SKILL.md")) != "mine" {
		t.Errorf("brain.toml or the user's work changed")
	}
	if err := b.Refresh(&events{}); err != nil {
		t.Errorf("the next command cannot run: %v", err)
	}
}

func TestPushRefusesChangesWithNoMessageAndABrainWithNoUpstream(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.write(e.path("brain/skills/x/SKILL.md"), "x")

	if err := NewBrainCheckout(e.path("brain")).Push("", false, &events{}); err == nil || !strings.Contains(err.Error(), "say what they are") {
		t.Errorf("got %v", err)
	}
	e.git(e.path("brain"), "branch", "-q", "--unset-upstream")
	if err := NewBrainCheckout(e.path("brain")).Push("add x", false, &events{}); err == nil || !strings.HasSuffix(err.Error(), ": no upstream branch") {
		t.Errorf("got %v", err)
	}
}

func TestStateOfABrainWithNoCommitsYetSaysSo(t *testing.T) {
	e := newTestEnv(t)
	brain := e.repo(e.path("brain"))
	e.write(filepath.Join(brain, "brain.toml"), "")
	e.git(brain, "remote", "add", "origin", e.path("brain.git"))

	if got := NewBrainCheckout(brain).State(); got != "uncommitted changes, no commits yet" {
		t.Errorf("state is %q", got)
	}
}

func TestOnRemoteSaysWhetherTheRemoteHoldsAPathAsTheBrainDoes(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	e.published()
	b := NewBrainCheckout(e.path("brain"))
	e.must(b.Refresh(&events{}))
	e.write(e.path("brain/skills/new/SKILL.md"), "new")

	if !b.OnRemote("brain.toml") || b.OnRemote("skills/new") {
		t.Errorf("brain.toml on the remote: %v; an untracked skill: %v", b.OnRemote("brain.toml"), b.OnRemote("skills/new"))
	}
	e.write(e.path("brain/brain.toml"), "[project.app]\n")
	if b.OnRemote("brain.toml") {
		t.Errorf("a changed file is on the remote as the Brain holds it")
	}
}

func TestBrainSyncRefusesToDropWhatWasNotPushedUnlessForced(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	e.write(e.path("brain/brain.toml"), "[project.app]\n")
	before := e.git(e.path("brain"), "rev-parse", "HEAD")
	var r events

	err := NewBrainCheckout(e.path("brain")).Sync(false, &r)

	if err == nil || !strings.Contains(err.Error(), "would drop changes to 1 file (brain.toml)") || !strings.Contains(err.Error(), "nerve push sends them, nerve sync -f drops them") {
		t.Errorf("got %v", err)
	}
	if e.git(e.path("brain"), "rev-parse", "HEAD") != before || e.read(e.path("brain/brain.toml")) != "[project.app]\n" {
		t.Errorf("the Brain changed")
	}
	e.must(NewBrainCheckout(e.path("brain")).Sync(true, &events{}))
	if e.git(e.path("brain"), "rev-parse", "HEAD") != head {
		t.Errorf("-f did not make the Brain its remote's")
	}
}

func TestPushOfABrainWithNoCommitsRefusesARemoteThatHasSome(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	theirs := e.published()
	fresh := e.repo(e.path("fresh"))
	e.git(fresh, "remote", "add", "origin", e.path("brain.git"))
	e.git(fresh, "fetch", "-q", "origin")
	e.git(fresh, "config", "branch.main.remote", "origin")
	e.git(fresh, "config", "branch.main.merge", "refs/heads/main")
	e.write(filepath.Join(fresh, "brain.toml"), "")

	err := NewBrainCheckout(fresh).Push("first", false, &events{})

	if err == nil || !strings.Contains(err.Error(), "this Brain lacks") || e.git(e.path("brain.git"), "rev-parse", "main") != theirs {
		t.Errorf("got %v; the remote's history must stay", err)
	}
}

func TestPushManifestThatCannotPushKeepsWhatTheUserStaged(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	e.published()
	b := NewBrainCheckout(e.path("brain"))
	e.must(b.Refresh(&events{}))
	e.write(e.path("other/z"), "z")
	e.commit(e.path("other"))
	e.git(e.path("other"), "push", "-q")
	e.write(e.path("brain/new"), "staged")
	e.git(e.path("brain"), "add", "new")
	e.write(e.path("brain/brain.toml"), "[project.app]\n")

	if err := b.PushManifest("nerve: add project app", &events{}); err == nil {
		t.Fatal("no error")
	}

	if got := e.git(e.path("brain"), "status", "--porcelain"); got != "A  new" {
		t.Errorf("status is %q, want the user's staging kept", got)
	}
}

func TestPushTakesTheRemotesCommitsWhenTheBrainHasNoneOfItsOwn(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	theirs := e.published() // the remote moved; this Brain only has changes
	e.write(e.path("brain/skills/x/SKILL.md"), "x")
	var r events

	if err := NewBrainCheckout(e.path("brain")).Push("add x", false, &r); err != nil {
		t.Fatal(err)
	}

	if got := e.git(e.path("brain.git"), "log", "--format=%s", "-2", "main"); got != "add x\nc" || e.git(e.path("brain"), "rev-parse", "HEAD~1") != theirs {
		t.Errorf("remote log %q: want the change on top of the remote's commit", got)
	}
	if !strings.HasPrefix(r.String(), "synced   the Brain to "+theirs[:7]) {
		t.Errorf("events: %s", r)
	}
}

func TestPushManifestSaysWhetherTheRemoteMoved(t *testing.T) {
	e := newTestEnv(t)
	e.identity()
	e.published()
	b := NewBrainCheckout(e.path("brain"))
	e.must(b.Refresh(&events{}))
	e.write(e.path("other/z"), "z") // the remote moves on meanwhile
	e.commit(e.path("other"))
	e.git(e.path("other"), "push", "-q")
	e.write(e.path("brain/brain.toml"), "[project.app]\n")

	if err := b.PushManifest("nerve: add project app", &events{}); !errors.Is(err, app.ErrRemoteMoved) {
		t.Errorf("got %v, want the remote moved", err)
	}

	e.git(e.path("brain"), "remote", "set-url", "origin", e.path("nowhere.git"))
	e.write(e.path("brain/brain.toml"), "[project.app]\n")
	if err := b.PushManifest("nerve: add project app", &events{}); err == nil || errors.Is(err, app.ErrRemoteMoved) {
		t.Errorf("got %v, want a failure that is not the remote moving", err)
	}
}
