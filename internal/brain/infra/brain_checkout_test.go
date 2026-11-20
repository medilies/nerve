package infra

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
)

type events []app.Event

func (e *events) Add(ev app.Event) { *e = append(*e, ev) }
func (e *events) Count() int       { return len(*e) }

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
	if m.Common["cleanupPeriodDays"] != float64(30) || m.Projects["app"].Settings["model"] != "opus" || m.Hooks["fmt"].Event != "PostToolUse" {
		t.Errorf("loaded %+v", m)
	}
	if err := brain.Save(m); err != nil {
		t.Fatal(err)
	}
	got := e.read(e.path("brain/brain.toml"))
	if !strings.Contains(got, "cleanupPeriodDays = 30\n") || strings.Contains(got, "[project]\n") || strings.Contains(got, "[common]\n") || strings.Contains(got, "[hook]\n") {
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

func TestOwnerIsTheOriginRemotesOwner(t *testing.T) {
	e := newTestEnv(t)
	brain := e.repo(e.path("brain"))
	b := NewBrainCheckout(brain)
	if _, err := b.Owner(); err == nil || !strings.Contains(err.Error(), "no origin remote") {
		t.Errorf("got %v", err)
	}
	e.git(brain, "remote", "add", "origin", "x")
	for url, want := range map[string]string{
		"git@github.com:Medilies/brain.git":     "medilies",
		"https://github.com/medilies/brain.git": "medilies",
		"ssh://git@host:22/team/brain":          "team",
	} {
		e.git(brain, "remote", "set-url", "origin", url)
		if got, err := b.Owner(); got != want {
			t.Errorf("%s: owner %q (%v), want %q", url, got, err, want)
		}
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
