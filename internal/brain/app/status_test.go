package app_test

import (
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

func TestStatusShowsTheProjectItsLinksAndDriftWithoutChangingIt(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"model": "opus"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.ws.settings["/w/app/.claude/settings.local.json"]["model"] = "sonnet"
	e.ws.files["/w/app/.claude/agents/team.md"] = true
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
	if err == nil || err.Error() != "/w/app is not a listed checkout: run nerve project add <name> here, or pass --project" {
		t.Errorf("got %v", err)
	}
}

func TestCheckRefusesWhatAProjectCannotGet(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	bad := e.ownSkill("nerve")
	bad.Meta["description"] = ""
	e.brain.m.Common = map[string]any{"model": "opus", "apiKeyHelper": "x"}
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"go-stack", "missing"}}}
	e.brain.m.Projects["app"] = &domain.Project{
		Skills: []string{"go-stack", "ghost"}, Agents: []string{"nobody"}, Hooks: []string{"fmt"}, Plugins: []string{"bad"},
		Settings: map[string]any{"model": "opus"},
	}
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI, Plugins: []string{"x@y"}}
	r := &recorder{}

	err := e.app.Check(r)

	if err == nil {
		t.Fatal("check passed")
	}
	contains(t, r,
		"problem  skills/nerve: no description",
		"nerve is nerve's own skill: rename this one",
		"skill go-stack comes from both skills/ and acme/up",
		`acme/up@5863806: no skill "missing"`,
		`project.app: the Brain has no skill "ghost"`,
		`project.app: the Brain has no agent "nobody"`,
		`project.app: brain.toml defines no hook "fmt"`,
		`project.app: plugin "bad" must be <plugin>@<marketplace>`,
		"project.app.settings.model: equals common: drop it from the project",
		"project.claude-ai-work: a claude.ai account takes skills only",
		"common.settings.apiKeyHelper: apiKeyHelper may hold secrets")
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
