package app_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

func target(dir string) app.ProjectRef { return app.ProjectRef{Dir: dir} }

func appSyncAll() app.SyncOptions { return app.SyncOptions{} }

func TestAddItemAddsWhatTheBrainOffers(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	e.brain.agents["reviewer"] = true
	e.brain.m.Hooks = map[string]domain.Hook{"fmt": {Event: "PostToolUse", Command: "x"}}
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	r := &recorder{}

	for _, item := range [][2]string{{"skill", "go-stack"}, {"skill", "grilling"}, {"agent", "reviewer"}, {"hook", "fmt"}, {"plugin", "gopls@claude-plugins-official"}} {
		e.must(r, e.app.AddItem(target("/w/app"), item[0], item[1], r))
	}
	e.must(r, e.app.AddItem(target("/w/app"), "skill", "go-stack", r))

	p := e.project("app")
	if !reflect.DeepEqual(p.Skills, []string{"go-stack", "grilling"}) || len(p.Agents)+len(p.Hooks)+len(p.Plugins) != 3 {
		t.Errorf("project is %+v", p)
	}
	contains(t, r, "added    skill go-stack to project app", "skill go-stack already in project app")
}

func TestAddItemRefusesWhatTheBrainDoesNotOffer(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.agents["Reviewer"] = true // agents/Reviewer.md
	e.ownSkill("foo/")                // skills/foo/, as a trailing slash finds it
	e.ownSkill("grill")
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: strings.Repeat("a", 40), Skills: []string{"grill"}}}
	e.brain.ownErr = map[string]error{"bad": errors.New("skill bad: SKILL.md has no frontmatter")}
	for _, c := range []struct {
		project, kind, name, want string
	}{
		{"", "skill", "ghost", `the Brain has no skill "ghost": add skills/ghost/, or a source that lists it`},
		{"", "skill", "nerve", "nerve's own skill reaches every project already"},
		{"", "agent", "nobody", `the Brain has no agent "nobody"`},
		{"", "hook", "fmt", `brain.toml defines no hook "fmt"`},
		{"", "plugin", "gopls", `plugin "gopls" must be <plugin>@<marketplace>`},
		{"", "plugin", "a@b@c", "must be <plugin>@<marketplace>"},
		{"", "agent", "Reviewer", `agent "Reviewer" must be lowercase words joined by hyphens`},
		{"", "skill", "foo/", `skill "foo/" must be lowercase words joined by hyphens`},
		{"", "skill", "grill", "skill grill comes from both skills/ and acme/up: drop one of them"},
		{"", "skill", "bad", "skill bad: SKILL.md has no frontmatter"},
	} {
		err := e.app.AddItem(app.ProjectRef{Dir: "/w/app", Project: c.project}, c.kind, c.name, &recorder{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: got %v, want %q", c.kind, c.name, err, c.want)
		}
	}
	if e.brain.saves != 0 {
		t.Errorf("a refused item changed the Brain")
	}
}

func TestRemoveItemFromTheProjectOrEveryProject(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Skills = []string{"go-stack", "tdd"}
	e.listed("other", "/w/other").Skills = []string{"go-stack"}
	e.brain.m.Projects["quiet"] = &domain.Project{}
	r := &recorder{}

	e.must(r, e.app.RemoveItem(target("/w/app"), "skill", "tdd", false, r))
	e.must(r, e.app.RemoveItem(target("/w"), "skill", "go-stack", true, r))

	for _, name := range []string{"app", "other"} {
		if len(e.project(name).Skills) > 0 {
			t.Errorf("%s keeps %v", name, e.project(name).Skills)
		}
	}
	contains(t, r, "dropped  skill go-stack from project app", "dropped  skill go-stack from project other")
	if err := e.app.RemoveItem(target("/w/app"), "skill", "tdd", false, &recorder{}); err == nil || err.Error() != "project app has no skill tdd" {
		t.Errorf("got %v", err)
	}
	if err := e.app.RemoveItem(target("/w"), "skill", "tdd", true, &recorder{}); err == nil || err.Error() != "no project has skill tdd" {
		t.Errorf("got %v", err)
	}
}

func TestRemoveItemWhoseBrainSaveFailsReportsNothingDropped(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.brain.failSaves = 1
	r := &recorder{}

	if err := e.app.RemoveItem(target("/w/app"), "skill", "go-stack", false, r); err == nil {
		t.Fatal("remove passed")
	}

	if strings.Contains(r.String(), "dropped") {
		t.Errorf("reports a drop the Brain did not save:\n%s", r)
	}
}

func TestUpdateShowsEachSkillsDiffThenMovesThePin(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.heads["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	r := &recorder{}

	e.must(r, e.app.Update("/w", "https://github.com/acme/up.git", r))

	if got := e.brain.m.Sources[0].SHA; got != pinB {
		t.Errorf("pinned at %s", got)
	}
	contains(t, r, "diff grilling 5863806..84b741e", "pinned   acme/up at 84b741e")

	r = &recorder{}
	e.must(r, e.app.Update("/w", "", r))
	contains(t, r, "acme/up is already pinned at "+pinB)

	if err := e.app.Update("/w", "acme/other", &recorder{}); err == nil || err.Error() != "acme/other is not a source in brain.toml" {
		t.Errorf("got %v", err)
	}
}

func TestUpdateLeavesASourceThatDropsASkillAndMovesTheOthers(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{
		{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}},
		{Repo: "acme/other", SHA: pinA, Skills: []string{"tdd"}},
	}
	e.sources.heads["acme/up"], e.sources.heads["acme/other"] = pinB, pinB
	e.sources.skills["acme/other@"+pinB+"/tdd"] = domain.Skill{}
	r := &recorder{}

	err := e.app.Update("/w", "", r)

	if err == nil || e.brain.m.Sources[0].SHA != pinA || e.brain.m.Sources[1].SHA != pinB {
		t.Errorf("got %v, sources %v", err, e.brain.m.Sources)
	}
	contains(t, r, "problem  brain.toml: acme/up: at 84b741e: no skill \"grilling\"; drop it from brain.toml first")
}

func TestAddItemToACheckoutWhoseProjectLeftTheBrainSaysWhatToDo(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	delete(e.brain.m.Projects, "app")

	err := e.app.AddItem(target("/w/app"), "skill", "go-stack", &recorder{})

	if err == nil || !strings.Contains(err.Error(), "nerve sync cleans this checkout, and nerve project add lists it again") {
		t.Errorf("got %v", err)
	}
}

func TestAddItemSaysAnItemIsListedBeforeAnythingElse(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Skills = []string{"mine"}
	e.ownSkill("mine")
	e.brain.notPushed = map[string]bool{"skills/mine": true} // edited here since
	r := &recorder{}

	e.must(r, e.app.AddItem(target("/w/app"), "skill", "mine", r))

	contains(t, r, "skill mine already in project app")
}

func TestPushSaysHowManyProblemsAsPeopleDo(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Common = map[string]any{"apiKeyHelper": "x"}

	err := e.app.Push("x", false, &recorder{})

	if err == nil || !strings.HasPrefix(err.Error(), "the Brain has 1 problem, so nothing is pushed") {
		t.Errorf("got %v", err)
	}
}
