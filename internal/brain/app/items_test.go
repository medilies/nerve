package app_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

func target(dir string) app.Target { return app.Target{Dir: dir} }

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
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI}
	for _, c := range []struct {
		project, kind, name, want string
	}{
		{"", "skill", "ghost", `the Brain has no skill "ghost": add skills/ghost/, or a source that lists it`},
		{"", "skill", "nerve", "nerve's own skill reaches every project already"},
		{"", "agent", "nobody", `the Brain has no agent "nobody"`},
		{"", "hook", "fmt", `brain.toml defines no hook "fmt"`},
		{"", "plugin", "gopls", `plugin "gopls" must be <plugin>@<marketplace>`},
		{"", "plugin", "a@b@c", "must be <plugin>@<marketplace>"},
		{"claude-ai-work", "plugin", "a@b", "is a claude.ai account, which takes skills only"},
	} {
		err := e.app.AddItem(app.Target{Dir: "/w/app", Project: c.project}, c.kind, c.name, &recorder{})
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

func TestListItemsShowsLinksAndTheTeamsOwn(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Skills = []string{"go-stack", "grilling"}
	e.ownSkill("go-stack")
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.skills["acme/up@"+pinA+"/grilling"] = domain.Skill{Name: "grilling", Dir: "/cache/grilling"}
	e.ws.files["/w/app/.claude/skills/go-stack"] = true
	e.must(e.sync("/w/app", app.SyncOptions{}))
	r := &recorder{}

	e.must(r, e.app.ListItems(target("/w/app"), "skill", r))

	contains(t, r,
		"skill go-stack own, linked as .claude/skills/medilies-go-stack",
		"skill grilling from acme/up@5863806, linked as .claude/skills/grilling",
		"skill nerve nerve's own",
		"skill go-stack the team's")
}

func TestUpdatePinShowsEachSkillsDiffThenMovesThePin(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.heads["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	r := &recorder{}

	e.must(r, e.app.UpdatePin("https://github.com/acme/up.git", "", r))

	if len(e.brain.moved) != 1 || e.brain.moved[0] != [2]string{pinA, pinB} {
		t.Errorf("moved %v", e.brain.moved)
	}
	contains(t, r, "diff grilling 5863806..84b741e", "pinned   acme/up at "+pinB)

	r = &recorder{}
	e.must(r, e.app.UpdatePin("acme/up", pinB, r))
	contains(t, r, "acme/up is already pinned at "+pinB)

	for repo, want := range map[string]string{"acme/other": "acme/other is not a source in brain.toml"} {
		if err := e.app.UpdatePin(repo, "", &recorder{}); err == nil || err.Error() != want {
			t.Errorf("got %v", err)
		}
	}
	if err := e.app.UpdatePin("acme/up", "main", &recorder{}); err == nil || !strings.Contains(err.Error(), "full 40-character") {
		t.Errorf("got %v", err)
	}
}

func TestUpdatePinRefusesAPinThatDropsASkill(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}

	err := e.app.UpdatePin("acme/up", pinB, &recorder{})

	if err == nil || !strings.Contains(err.Error(), "drop it from brain.toml first") || len(e.brain.moved) > 0 {
		t.Errorf("got %v, moved %v", err, e.brain.moved)
	}
}
