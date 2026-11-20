package app_test

import (
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

func TestAddProjectRecordsTheCheckoutsSettingsAgainstCommon(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")
	e.brain.m.Common = map[string]any{"model": "opus", "syncClaudeAiSkills": false}
	e.ws.settings["/w/app/.claude/settings.local.json"] = map[string]any{"model": "opus", "effortLevel": "low", "env": map[string]any{"TOKEN": "x"}}
	r := &recorder{}

	e.must(r, e.app.AddProject("/w/app/src", "app", r))

	if got := jsonText(e.project("app").Settings); got != `{"effortLevel":"low"}` {
		t.Errorf("project settings are %s", got)
	}
	if got := jsonText(e.machine.overlays["app"]); got != `{"env":{"TOKEN":"x"}}` {
		t.Errorf("overlay is %s", got)
	}
	if got := e.machine.machine.Checkouts; len(got) != 1 || got[0] != (domain.Checkout{Path: "/w/app", Project: "app"}) {
		t.Errorf("checkouts are %v", got)
	}
	if len(e.ws.links) > 0 || len(e.ws.settings) != 1 {
		t.Errorf("project add wrote in the project")
	}
	contains(t, r,
		"imported model: already given by common",
		"imported syncClaudeAiSkills: added by common at the next sync",
		"imported env.TOKEN: kept in the local overlay: env may hold secrets",
		"project  app: added to brain.toml",
		"checkout /w/app (app): listed: review brain.toml, then nerve sync --here")
}

func TestAddProjectFromAWorktreeListsTheMainCheckout(t *testing.T) {
	e := newEnv(t)
	e.ws.trees = []domain.Tree{{Top: "/w/app-feature", Main: "/w/app"}}
	r := &recorder{}

	e.must(r, e.app.AddProject("/w/app-feature", "app", r))

	if got := e.machine.machine.Checkouts[0].Path; got != "/w/app" {
		t.Errorf("listed %s, want the main checkout", got)
	}
}

func TestAddProjectJoinsAnExistingOneOrRefuses(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.checkout("/w/app2")
	e.checkout("/w/app3")
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI}
	r := &recorder{}

	e.must(r, e.app.AddProject("/w/app2", "app", r))

	contains(t, r, "project  app: already in the Brain: this checkout joins it")
	if e.brain.saves != 0 {
		t.Errorf("joining a project changed the Brain")
	}
	for _, c := range []struct{ dir, name, want string }{
		{"/w/app", "app", "already listed"},
		{"/w/app", "other", "/w/app is listed as project app"},
		{"/w/app3", "claude-ai-work", "is a claude.ai account"},
		{"/w/app3", "App", "must be lowercase words"},
	} {
		name, want := c.name, c.want
		r := &recorder{}
		err := e.app.AddProject(c.dir, name, r)
		if !strings.Contains(r.String()+errText(err), want) {
			t.Errorf("adding %s: got %v\n%s", name, err, r)
		}
	}
	if err := e.app.AddProject("/elsewhere", "x", &recorder{}); err == nil || !strings.Contains(err.Error(), "not in a git repo") {
		t.Errorf("got %v outside a repo", err)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestListProjectsShowsEachCheckoutsSyncState(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Skills = []string{"a", "b"}
	e.machine.machine.Checkouts = append(e.machine.machine.Checkouts, domain.Checkout{Path: "/w/gone", Project: "gone"})
	e.machine.states["/w/app"] = domain.TreeState{Path: "/w/app", Commit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Links: map[string]string{"x": "skill:a"}}
	r := &recorder{}

	e.must(r, e.app.ListProjects(r))

	contains(t, r,
		"project  app: autosync on, 2 skills",
		"checkout /w/app (app): synced at bbbbbbb, behind the Brain at aaaaaaa",
		"checkout /w/gone (gone): its project left the Brain: sync cleans it")
}

func TestRemoveProjectCleansItsTreesAndDropsIt(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"model": "opus"}
	e.listed("other", "/w/other")
	r, err := e.sync("/w/app", appSyncAll())
	e.must(r, err)
	r = &recorder{}

	e.must(r, e.app.RemoveProject("app", r))

	if _, ok := e.brain.m.Projects["app"]; ok {
		t.Errorf("the project is still in the Brain")
	}
	if len(e.machine.machine.Checkouts) != 1 || e.settings("/w/app") != nil {
		t.Errorf("checkouts %v, settings %v", e.machine.machine.Checkouts, e.settings("/w/app"))
	}
	if _, ok := e.ws.links["/w/other/.claude/skills/nerve"]; !ok {
		t.Errorf("another project was cleaned")
	}
	contains(t, r, "cleaned  /w/app: project app removed", "project  app: dropped from brain.toml")
	if err := e.app.RemoveProject("app", &recorder{}); err == nil {
		t.Errorf("removing it again passed")
	}
}

func TestSetAutosyncForAProjectOrTheBrain(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.SetAutosync(target("/w/app"), false, false, r))
	e.must(r, e.app.SetAutosync(target("/w"), false, true, r))

	if p := e.project("app"); p.Autosync == nil || *p.Autosync || e.brain.m.Autosync == nil || *e.brain.m.Autosync {
		t.Errorf("project %v, brain %v", p.Autosync, e.brain.m.Autosync)
	}
	contains(t, r, "autosync off for app", "autosync off by default")
}

func TestInitNamesTheBrainOnce(t *testing.T) {
	e := newEnv(t)
	e.machine.missing = true
	r := &recorder{}

	e.must(r, e.app.Init(r))

	if e.machine.machine.Brain != "/brain" {
		t.Errorf("machine names %q", e.machine.machine.Brain)
	}
	if err := e.app.Init(&recorder{}); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("got %v", err)
	}
}
