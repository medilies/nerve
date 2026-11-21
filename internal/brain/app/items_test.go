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
		e.must(r, e.app.AddItem(target("/w/app"), item[0], []string{item[1]}, r))
	}
	e.must(r, e.app.AddItem(target("/w/app"), "skill", []string{"go-stack"}, r))

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
		{"", "hook", "fmt", `brain.json defines no hook "fmt"`},
		{"", "plugin", "gopls", `plugin "gopls" must be <plugin>@<marketplace>`},
		{"", "plugin", "a@b@c", "must be <plugin>@<marketplace>"},
		{"", "agent", "Reviewer", `agent "Reviewer" must be lowercase words joined by hyphens`},
		{"", "skill", "foo/", `skill "foo/" must be lowercase words joined by hyphens`},
		{"", "skill", "grill", "skill grill comes from both skills/ and acme/up: drop one of them"},
		{"", "skill", "bad", "skill bad: SKILL.md has no frontmatter"},
	} {
		err := e.app.AddItem(app.ProjectRef{Dir: "/w/app", Project: c.project}, c.kind, []string{c.name}, &recorder{})
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
	e.ownSkill("go-stack")
	e.ownSkill("tdd")
	r := &recorder{}

	e.must(r, e.app.RemoveItem(target("/w/app"), "skill", []string{"tdd"}, false, r))
	e.must(r, e.app.RemoveItem(target("/w"), "skill", []string{"go-stack"}, true, r))

	for _, name := range []string{"app", "other"} {
		if len(e.project(name).Skills) > 0 {
			t.Errorf("%s keeps %v", name, e.project(name).Skills)
		}
	}
	contains(t, r, "dropped  skill go-stack from project app", "dropped  skill go-stack from project other")
	if err := e.app.RemoveItem(target("/w/app"), "skill", []string{"tdd"}, false, &recorder{}); err == nil || err.Error() != "project app has no skill tdd" {
		t.Errorf("got %v", err)
	}
	if err := e.app.RemoveItem(target("/w"), "skill", []string{"tdd"}, true, &recorder{}); err == nil || err.Error() != "no project has skill tdd" {
		t.Errorf("got %v", err)
	}
}

func TestRemoveItemWhoseBrainSaveFailsReportsNothingDropped(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.brain.failSaves = 1
	r := &recorder{}

	if err := e.app.RemoveItem(target("/w/app"), "skill", []string{"go-stack"}, false, r); err == nil {
		t.Fatal("remove passed")
	}

	if strings.Contains(r.String(), "dropped") {
		t.Errorf("reports a drop the Brain did not save:\n%s", r)
	}
}

func TestUpdateShowsEachSkillsChangesThenMovesThePin(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling", "tdd"}}}
	e.sources.tips["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	e.sources.skills["acme/up@"+pinB+"/tdd"] = domain.Skill{}
	e.sources.files["acme/up@"+pinB+"/grilling"] = []app.FileChange{{Path: "SKILL.md", Added: 3, Removed: 1}, {Path: "logo.png", Binary: true}}
	r := &recorder{}

	e.must(r, e.app.Update("/w", "https://github.com/acme/up.git", false, false, r))

	if got := e.brain.m.Sources[0].SHA; got != pinB {
		t.Errorf("pinned at %s", got)
	}
	contains(t, r,
		"tip      acme/up: pinned at 5863806 (2026-10-03), tip 84b741e (2026-10-04)",
		"changed  skill grilling: SKILL.md +3 -1, logo.png binary",
		"same     skill tdd",
		"pinned   acme/up at 84b741e",
	)
	for _, ev := range r.events {
		if ev.Kind == app.EvChanged && ev.Path != "/cache/acme/up/"+pinB+"/grilling" {
			t.Errorf("changed skill is at %q, want its folder at the tip", ev.Path)
		}
	}
	if strings.Contains(r.String(), "diff grilling") {
		t.Errorf("shows the whole diff unasked:\n%s", r)
	}

	r = &recorder{}
	e.must(r, e.app.Update("/w", "", false, false, r))
	contains(t, r, "acme/up is already pinned at "+pinB)

	if err := e.app.Update("/w", "acme/other", false, false, &recorder{}); err == nil || err.Error() != "acme/other is not a source in brain.json" {
		t.Errorf("got %v", err)
	}
}

func TestUpdateDryRunShowsTheChangesAndMovesNothing(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.tips["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	e.sources.files["acme/up@"+pinB+"/grilling"] = []app.FileChange{{Path: "SKILL.md", Added: 1}}
	r := &recorder{}

	e.must(r, e.app.Update("/w/app", "", false, true, r))

	contains(t, r, "changed  skill grilling: SKILL.md +1 -0", "dry run: no pin moved")
	if e.brain.m.Sources[0].SHA != pinA || len(e.brain.manifestPushes) > 0 || strings.Contains(r.String(), "synced") {
		t.Errorf("moved something: sources %v, pushed %q:\n%s", e.brain.m.Sources, e.brain.manifestPushes, r)
	}
}

func TestUpdateDryRunReadsTheBrainAsItIsHere(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.tips["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	e.brain.refreshErr = errors.New("brain.json has changes not pushed")
	r := &recorder{}

	e.must(r, e.app.Update("/w", "", false, true, r))
}

func TestUpdateDryRunWithEveryPinAtItsTipOffersNothing(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinB, Skills: []string{"grilling"}}}
	e.sources.tips["acme/up"] = pinB
	r := &recorder{}

	e.must(r, e.app.Update("/w", "", false, true, r))

	if strings.Contains(r.String(), "dry run") {
		t.Errorf("offers an update with nothing to move:\n%s", r)
	}
}

func TestUpdateDryRunReportsASourceThatCannotMove(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.tips["acme/up"] = pinB
	r := &recorder{}

	err := e.app.Update("/w", "", false, true, r)

	if err == nil || e.brain.m.Sources[0].SHA != pinA || len(e.brain.manifestPushes) > 0 {
		t.Errorf("got %v, sources %v, pushed %q", err, e.brain.m.Sources, e.brain.manifestPushes)
	}
	contains(t, r, `problem  brain.json: acme/up: at 84b741e: no skill "grilling"`)
}

func TestUpdateDryRunWithDiffShowsTheWholeDiff(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.tips["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	e.sources.files["acme/up@"+pinB+"/grilling"] = []app.FileChange{{Path: "SKILL.md", Added: 1}}
	r := &recorder{}

	e.must(r, e.app.Update("/w", "", true, true, r))

	contains(t, r, "diff grilling 5863806..84b741e")
}

func TestUpdateWithDiffShowsTheWholeDiffOfEachChangedSkill(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.tips["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	e.sources.files["acme/up@"+pinB+"/grilling"] = []app.FileChange{{Path: "SKILL.md", Added: 1}}
	r := &recorder{}

	e.must(r, e.app.Update("/w", "", true, false, r))

	contains(t, r, "changed  skill grilling: SKILL.md +1 -0", "diff grilling 5863806..84b741e")
}

func TestUpdateLeavesASourceThatDropsASkillAndMovesTheOthers(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{
		{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}},
		{Repo: "acme/other", SHA: pinA, Skills: []string{"tdd"}},
	}
	e.sources.tips["acme/up"], e.sources.tips["acme/other"] = pinB, pinB
	e.sources.skills["acme/other@"+pinB+"/tdd"] = domain.Skill{}
	r := &recorder{}

	err := e.app.Update("/w", "", false, false, r)

	if err == nil || e.brain.m.Sources[0].SHA != pinA || e.brain.m.Sources[1].SHA != pinB {
		t.Errorf("got %v, sources %v", err, e.brain.m.Sources)
	}
	contains(t, r, "problem  brain.json: acme/up: at 84b741e: no skill \"grilling\"; drop it from brain.json first")
}

func TestUpdateMovesEveryEntryOfARepoTogether(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{
		{Repo: "acme/up", SHA: pinA, Path: "skills/eng", Skills: []string{"grilling"}},
		{Repo: "acme/up", SHA: pinA, Path: "skills/pro", Skills: []string{"grill-me"}},
	}
	e.sources.tips["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	e.sources.skills["acme/up@"+pinB+"/grill-me"] = domain.Skill{}
	r := &recorder{}

	e.must(r, e.app.Update("/w", "acme/up", false, false, r))

	if e.brain.m.Sources[0].SHA != pinB || e.brain.m.Sources[1].SHA != pinB {
		t.Errorf("sources %v", e.brain.m.Sources)
	}
	contains(t, r, "same     skill grilling", "same     skill grill-me")
	if got := e.brain.manifestPushes; len(got) != 1 || got[0] != "nerve: pin acme/up at 84b741e" {
		t.Errorf("pushed %q", got)
	}
	for _, line := range []string{"tip      acme/up", "pinned   acme/up"} {
		if n := strings.Count(r.String(), line); n != 1 {
			t.Errorf("%q %d times:\n%s", line, n, r)
		}
	}
}

func TestUpdateFindsARepoHoweverItIsSpelled(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{
		{Repo: "acme/up", SHA: pinA, Path: "skills/eng", Skills: []string{"grilling"}},
		{Repo: "Acme/Up", SHA: pinA, Path: "skills/pro", Skills: []string{"grill-me"}},
	}
	// The fake, unlike GitHub, minds case: it serves the repo either way.
	e.sources.tips["acme/up"], e.sources.tips["Acme/Up"] = pinB, pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	e.sources.skills["Acme/Up@"+pinB+"/grill-me"] = domain.Skill{}
	r := &recorder{}

	e.must(r, e.app.Update("/w", "ACME/UP", false, false, r))

	if e.brain.m.Sources[0].SHA != pinB || e.brain.m.Sources[1].SHA != pinB {
		t.Errorf("sources %v:\n%s", e.brain.m.Sources, r)
	}
}

func TestUpdateLeavesEveryEntryOfARepoWhenOneCannotMove(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{
		{Repo: "acme/up", SHA: pinA, Path: "skills/eng", Skills: []string{"grilling"}},
		{Repo: "acme/up", SHA: pinA, Path: "skills/pro", Skills: []string{"grill-me"}},
	}
	e.sources.tips["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grill-me"] = domain.Skill{}

	err := e.app.Update("/w", "", false, false, &recorder{})

	if err == nil || e.brain.m.Sources[0].SHA != pinA || e.brain.m.Sources[1].SHA != pinA || len(e.brain.manifestPushes) > 0 {
		t.Errorf("got %v, sources %v, pushed %q", err, e.brain.m.Sources, e.brain.manifestPushes)
	}
}

func TestAddItemToACheckoutWhoseProjectLeftTheBrainSaysWhatToDo(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	delete(e.brain.m.Projects, "app")

	err := e.app.AddItem(target("/w/app"), "skill", []string{"go-stack"}, &recorder{})

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

	e.must(r, e.app.AddItem(target("/w/app"), "skill", []string{"mine"}, r))

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
