package infra

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

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
	e.write(e.path("brain/brain.toml"), `nerve = "v0.3.0"
allowed-settings = ["statusLine"]

[[source]]
repo = "acme/up"
sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"
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
	if !strings.Contains(got, "cleanupPeriodDays = 30\n") || !strings.Contains(got, "\nallowed-settings = [\"statusLine\"]\n") || strings.Contains(got, "[project]\n") || strings.Contains(got, "[common]\n") || strings.Contains(got, "[hook]\n") {
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
		"profiles = []\n":                  "brain.toml: unknown keys [profiles]",
		"[project.app]\nskill = [\"x\"]\n": "brain.toml: unknown keys [project.app.skill]",
		"nerve = \"latest\"\n":             "must be a release tag",
	} {
		e.write(e.path("brain/brain.toml"), content)
		if _, err := brain.Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", content, err, want)
		}
	}
}

func TestMovePinTouchesOnlyThePin(t *testing.T) {
	e := newTestEnv(t)
	old, next := strings.Repeat("a", 40), strings.Repeat("b", 40)
	e.write(e.path("brain/brain.toml"), "# pins\n[[source]]\nrepo = \"acme/up\"\nsha = \""+old+"\"\nskills = [\"x\"]\n")

	if err := NewBrainCheckout(e.path("brain")).MovePin(old, next); err != nil {
		t.Fatal(err)
	}
	if got := e.read(e.path("brain/brain.toml")); got != "# pins\n[[source]]\nrepo = \"acme/up\"\nsha = \""+next+"\"\nskills = [\"x\"]\n" {
		t.Errorf("brain.toml is:\n%s", got)
	}
}

func TestMovePinLeavesTheShaWhereItIsNotThePin(t *testing.T) {
	e := newTestEnv(t)
	old, next := strings.Repeat("a", 40), strings.Repeat("b", 40)
	e.write(e.path("brain/brain.toml"), "[[source]]\n# was "+old+"\nrepo = \"acme/up\"\nsha = \""+old+"\"\nskills = [\"x\"]\n")

	if err := NewBrainCheckout(e.path("brain")).MovePin(old, next); err != nil {
		t.Fatal(err)
	}

	if got := e.read(e.path("brain/brain.toml")); got != "[[source]]\n# was "+old+"\nrepo = \"acme/up\"\nsha = \""+next+"\"\nskills = [\"x\"]\n" {
		t.Errorf("brain.toml is:\n%s", got)
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
	e.write(filepath.Join(other, "brain.toml"), "autosync = false\n")
	head := e.commit(other)
	e.git(other, "push", "-q")
	return head
}

func TestPullFastForwardsTheBrain(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	var r events

	if err := NewBrainCheckout(e.path("brain")).Pull(&r); err != nil {
		t.Fatal(err)
	}

	if got := e.git(e.path("brain"), "rev-parse", "HEAD"); got != head {
		t.Errorf("Brain is at %s, want the pushed %s", got, head)
	}
	if r.String() != "pulled   the Brain to "+head[:7] {
		t.Errorf("events: %s", r)
	}
}

func TestPullSaysWhyItSkipped(t *testing.T) {
	e := newTestEnv(t)
	head := e.published()
	e.write(e.path("brain/brain.toml"), "autosync = true\n") // a tracked file, edited
	var r events

	if err := NewBrainCheckout(e.path("brain")).Pull(&r); err != nil {
		t.Fatal(err)
	}

	if e.git(e.path("brain"), "rev-parse", "HEAD") == head || r.String() != "no pull  "+e.path("brain")+": uncommitted changes" {
		t.Errorf("events: %s", r)
	}
	e.git(e.path("brain"), "branch", "-q", "--unset-upstream")
	r = nil
	_ = NewBrainCheckout(e.path("brain")).Pull(&r)
	if !strings.HasSuffix(r.String(), ": no upstream branch") {
		t.Errorf("events: %s", r)
	}
}

func TestPullSkipsABrainWhoseRemoteCannotBeReached(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	if err := os.Rename(e.path("brain.git"), e.path("moved.git")); err != nil {
		t.Fatal(err)
	}
	var r events

	err := NewBrainCheckout(e.path("brain")).Pull(&r)

	if err != nil || r.String() != "no pull  "+e.path("brain")+": could not reach its remote" {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestPullFailsOnADivergedBrain(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.write(e.path("brain/skills/x/SKILL.md"), "x")
	e.commit(e.path("brain"))

	err := NewBrainCheckout(e.path("brain")).Pull(&events{})

	if err == nil || !strings.Contains(err.Error(), "pull "+e.path("brain")) {
		t.Errorf("got %v", err)
	}
}

func TestPullSaysWhenHeadIsDetached(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	e.git(e.path("brain"), "checkout", "-q", "--detach")
	var r events

	if err := NewBrainCheckout(e.path("brain")).Pull(&r); err != nil || !strings.HasSuffix(r.String(), ": detached HEAD") {
		t.Errorf("got %v, events: %s", err, r)
	}
}

func TestPullSaysWhenGitCannotRun(t *testing.T) {
	e := newTestEnv(t)
	e.published()
	t.Setenv("PATH", t.TempDir())
	var r events

	if err := NewBrainCheckout(e.path("brain")).Pull(&r); err != nil || !strings.Contains(r.String(), ": cannot read its git state: ") {
		t.Errorf("got %v, events: %s", err, r)
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
	if err := b.Pull(&r); err != nil || len(r) > 0 {
		t.Errorf("pulled a plain folder: %v %s", err, r)
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
