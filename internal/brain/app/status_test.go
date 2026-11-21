package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

func TestStatusShowsTheProjectItsLinksAndDriftWithoutChangingIt(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"model": "opus"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.trees.settings["/w/app/.claude/settings.local.json"]["model"] = "sonnet"
	e.trees.files["/w/app/.claude/agents/team.md"] = true
	e.trees.frontmatter["/w/app/.claude/agents/team.md"] = map[string]string{"name": "team", "description": "Reviews."}
	r := &recorder{}

	e.must(r, e.app.Status(target("/w/app"), r))

	contains(t, r,
		"nerve    v0.3.0",
		"brain    /brain",
		"project  app: autosync on, 1 setting",
		"checkout /w/app (app): synced at aaaaaaa",
		"skill nerve linked as .claude/skills/nerve",
		"agent team the team's",
		`drift    model is "sonnet" here, "opus" in the Brain`)
	if e.settings("/w/app")["model"] != "sonnet" {
		t.Errorf("status changed the settings")
	}
}

func TestStatusOutsideACheckoutListsTheProjects(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.Status(target("/elsewhere"), r))

	contains(t, r, "project  app: autosync on", "checkout /w/app (app): never synced")
}

func TestStatusWithoutAMachineFileSaysSo(t *testing.T) {
	e := newEnv(t)
	e.machine.missing = true
	r := &recorder{}

	err := e.app.Status(target("/w"), r)

	if err == nil || !strings.Contains(err.Error(), "run nerve init") || r.String() != "nerve    v0.3.0" {
		t.Errorf("got %v\n%s", err, r)
	}
}

func TestCommandsOutsideAListedCheckoutSayHowToAddIt(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")
	err := e.app.AddItem(target("/w/app"), "skill", "x", &recorder{})
	if err == nil || err.Error() != "/w/app is not a listed checkout: run nerve project add <name> in it, or pass --project" {
		t.Errorf("got %v", err)
	}
}

func TestSyncHereOutsideAListedCheckoutSaysOnlyHowToAddIt(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")

	_, err := e.sync("/w/app", app.SyncOptions{Here: true})

	if err == nil || err.Error() != "/w/app is not a listed checkout: run nerve project add <name> in it" {
		t.Errorf("got %v", err)
	}
}

func TestStatusRefusesAProjectTheBrainDoesNotList(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")

	err := e.app.Status(app.ProjectRef{Dir: "/w/app", Project: "typo"}, &recorder{})

	if err == nil || err.Error() != `brain.toml lists no project "typo"` {
		t.Errorf("got %v", err)
	}
}

// check runs check on e's Brain, and returns what it reported.
func check(t *testing.T, e *env) string {
	t.Helper()
	r := &recorder{}
	if err := e.app.Check(r); err == nil {
		t.Fatalf("check passed:\n%s", r)
	}
	return r.String()
}

func TestCheckRefusesAnInvalidOwnSkill(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack").Meta["description"] = ""
	if out := check(t, e); !strings.Contains(out, "problem  skills/go-stack: no description") {
		t.Errorf("check says:\n%s", out)
	}
}

func TestCheckReportsAnOwnSkillItCannotReadAndGoesOn(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack").Meta["description"] = ""
	e.brain.ownErr = map[string]error{"notes": errors.New("open skills/notes/SKILL.md: no such file or directory")}

	out := check(t, e)

	for _, want := range []string{"problem  skills/notes: open skills/notes/SKILL.md: no such file or directory", "problem  skills/go-stack: no description"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

func TestCheckRefusesAnOwnSkillNamedNerve(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("nerve")
	if out := check(t, e); !strings.Contains(out, "nerve is nerve's own skill: rename this one") {
		t.Errorf("check says:\n%s", out)
	}
}

func TestCheckRefusesASkillFromTwoPlaces(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"go-stack"}}}
	e.sources.skills["acme/up@"+pinA+"/go-stack"] = domain.Skill{}
	if out := check(t, e); !strings.Contains(out, "skill go-stack comes from both skills/ and acme/up") {
		t.Errorf("check says:\n%s", out)
	}
}

func TestCheckRefusesASourceSkillMissingAtItsPin(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"missing"}}}
	if out := check(t, e); !strings.Contains(out, `acme/up@5863806: no skill "missing"`) {
		t.Errorf("check says:\n%s", out)
	}
}

func TestCheckRefusesItemsTheBrainCannotGive(t *testing.T) {
	for kind, want := range map[string]string{
		"skill":  `project.app: the Brain has no skill "ghost"`,
		"agent":  `project.app: the Brain has no agent "ghost"`,
		"hook":   `project.app: brain.toml defines no hook "ghost"`,
		"plugin": `project.app: plugin "ghost" must be <plugin>@<marketplace>`,
	} {
		e := newEnv(t)
		*e.listed("app", "/w/app").List(kind) = []string{"ghost"}
		if out := check(t, e); !strings.Contains(out, want) {
			t.Errorf("%s: check says:\n%s", kind, out)
		}
	}
}

func TestCheckRefusesAProjectSettingThatEqualsCommon(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Common = map[string]any{"model": "opus"}
	e.listed("app", "/w/app").Settings = map[string]any{"model": "opus"}
	if out := check(t, e); !strings.Contains(out, "project.app.settings.model: equals common: drop it from the project") {
		t.Errorf("check says:\n%s", out)
	}
}

func TestCheckRefusesAClaudeAIAccountThatTakesMoreThanSkills(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Projects["acct"] = &domain.Project{Surface: domain.ClaudeAI, Plugins: []string{"x@y"}}
	if out := check(t, e); !strings.Contains(out, "project.acct: a claude.ai account takes skills only") {
		t.Errorf("check says:\n%s", out)
	}
}

func TestCheckRefusesSensitiveValues(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Common = map[string]any{"apiKeyHelper": "x"}
	if out := check(t, e); !strings.Contains(out, "common.settings.apiKeyHelper: apiKeyHelper may hold secrets") {
		t.Errorf("check says:\n%s", out)
	}
}

func TestCheckPassesAGoodBrain(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.brain.agents["reviewer"] = true
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	r := &recorder{}

	e.must(r, e.app.Check(r))

	if r.String() != "no problems" {
		t.Errorf("check says:\n%s", r)
	}
}

func TestStatusOfAProjectReportsEachOfItsCheckouts(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/p1")
	e.checkout("/w/p2")
	e.machine.machine.Checkouts = append(e.machine.machine.Checkouts, domain.Checkout{Path: "/w/p2", Project: "app"})
	r := &recorder{}

	e.must(r, e.app.Status(app.ProjectRef{Dir: "/elsewhere", Project: "app"}, r))

	contains(t, r, "checkout /w/p1 (app): never synced", "checkout /w/p2 (app): never synced")
}

func TestStatusSaysNothingOfThePinToADevelopmentBuild(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.Nerve = "v0.4.0"
	e.self.version = "(devel)"
	r := &recorder{}

	e.must(r, e.app.Status(target("/w/app"), r))

	if strings.Contains(r.String(), "install") {
		t.Errorf("tells a development build to install the pin:\n%s", r)
	}
}
