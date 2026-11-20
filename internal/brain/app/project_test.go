package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

func TestAddProjectRecordsTheCheckoutsSettingsAgainstCommon(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")
	e.brain.m.Common = map[string]any{"model": "opus", "syncClaudeAiSkills": false}
	e.trees.settings["/w/app/.claude/settings.local.json"] = map[string]any{"model": "opus", "effortLevel": "low", "env": map[string]any{"TOKEN": "x"}}
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
	if len(e.trees.links) > 0 || len(e.trees.settings) != 1 {
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
	e.trees.trees = []domain.Tree{{Top: "/w/app-feature", Main: "/w/app", Repo: "/w/app/.git"}}
	r := &recorder{}

	e.must(r, e.app.AddProject("/w/app-feature", "app", r))

	if got := e.machine.machine.Checkouts[0].Path; got != "/w/app" {
		t.Errorf("listed %s, want the main checkout", got)
	}
}

func TestAddProjectFromAWorktreeImportsTheMainCheckoutsSettings(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")
	e.worktree("/w/app-feature", "/w/app")
	e.trees.settings["/w/app/.claude/settings.local.json"] = map[string]any{"model": "opus"}
	e.trees.settings["/w/app-feature/.claude/settings.local.json"] = map[string]any{"model": "sonnet"}

	e.must(&recorder{}, e.app.AddProject("/w/app-feature", "app", &recorder{}))

	if got := jsonText(e.project("app").Settings); got != `{"model":"opus"}` {
		t.Errorf("recorded %s, want the settings of the checkout it lists", got)
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
	e.machine.states["/w/app"] = domain.TreeState{Path: "/w/app", Commit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Links: map[string]domain.Link{"x": {Kind: "skill", Name: "a"}}}
	r := &recorder{}

	e.must(r, e.app.ListProjects(r))

	contains(t, r,
		"project  app: autosync on, 2 skills",
		"checkout /w/app (app): synced at bbbbbbb, behind the Brain at aaaaaaa",
		"checkout /w/gone (gone): its project left the Brain: sync cleans it")
}

func TestRemoveProjectCleansItsTreesAndDropsIt(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	p := e.listed("app", "/w/app")
	p.Skills, p.Settings = []string{"go-stack"}, map[string]any{"model": "opus"}
	e.listed("other", "/w/other")
	e.machine.overlays["app"] = map[string]any{"env": map[string]any{"TOKEN": "x"}}
	e.must(e.sync("/w", appSyncAll()))
	r := &recorder{}

	e.must(r, e.app.RemoveProject("app", r))

	if _, ok := e.brain.m.Projects["app"]; ok {
		t.Errorf("the project is still in the Brain")
	}
	if len(e.machine.machine.Checkouts) != 1 || e.settings("/w/app") != nil {
		t.Errorf("checkouts %v, settings %v", e.machine.machine.Checkouts, e.settings("/w/app"))
	}
	for link := range e.trees.links {
		if strings.HasPrefix(link, "/w/app/") {
			t.Errorf("%s is left", link)
		}
	}
	if got := e.trees.excludes["/w/app"]; got != nil {
		t.Errorf("the exclude block is left: %v", got)
	}
	if _, ok := e.machine.overlays["app"]; ok {
		t.Errorf("the local overlay is left")
	}
	if _, ok := e.trees.links["/w/other/.claude/skills/nerve"]; !ok {
		t.Errorf("another project was cleaned")
	}
	contains(t, r, "cleaned  /w/app: project app removed", "removed  /cfg/nerve/overlays/app.json", "project  app: dropped from brain.toml")
	if err := e.app.RemoveProject("app", &recorder{}); err == nil {
		t.Errorf("removing it again passed")
	}
}

func TestRemoveProjectRemovesTheFoldersNerveMadeAndNoOthers(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.trees.mkdirs("/w/app/.claude/agents") // the team's, before nerve
	e.must(e.sync("/w/app", app.SyncOptions{}))

	e.must(&recorder{}, e.app.RemoveProject("app", &recorder{}))

	for dir, want := range map[string]bool{"/w/app/.claude/skills": false, "/w/app/.claude/agents": true, "/w/app/.claude": true} {
		if e.trees.dirs[dir] != want {
			t.Errorf("%s is there: %v, want %v", dir, e.trees.dirs[dir], want)
		}
	}
	e.listed("other", "/w/other")
	e.must(e.sync("/w/other", app.SyncOptions{}))
	e.must(&recorder{}, e.app.RemoveProject("other", &recorder{}))
	if e.trees.dirs["/w/other/.claude"] {
		t.Errorf(".claude is left, though nerve made it")
	}
}

func TestAddProjectThatFailsPartWayImportsAgainWhenRunAgain(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")
	e.trees.settings["/w/app/.claude/settings.local.json"] = map[string]any{"env": map[string]any{"TOKEN": "x"}, "model": "opus"}
	e.machine.saveErr = errors.New("config dir is read-only")

	if err := e.app.AddProject("/w/app", "app", &recorder{}); err == nil {
		t.Fatal("add passed")
	}
	if e.project("app") != nil {
		t.Errorf("the project reached the Brain without its overlay")
	}
	e.machine.saveErr = nil
	e.must(&recorder{}, e.app.AddProject("/w/app", "app", &recorder{}))

	if got := jsonText(e.machine.overlays["app"]); got != `{"env":{"TOKEN":"x"}}` {
		t.Errorf("overlay is %s", got)
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

func TestSetAutosyncRefusesAClaudeAIAccount(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Projects["acct"] = &domain.Project{Surface: domain.ClaudeAI, Skills: []string{"x"}}

	err := e.app.SetAutosync(app.ProjectRef{Project: "acct"}, false, false, &recorder{})

	if err == nil || !strings.Contains(err.Error(), "is a claude.ai account") || e.brain.saves != 0 {
		t.Errorf("got %v", err)
	}
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
