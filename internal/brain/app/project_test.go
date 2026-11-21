package app_test

import (
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

func TestAddProjectListsTheCheckoutAndRecordsNoneOfItsSettings(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")
	e.trees.settings["/w/app/.claude/settings.local.json"] = map[string]any{"model": "opus", "env": map[string]any{"TOKEN": "x"}}
	r := &recorder{}

	e.must(r, e.app.AddProject("/w/app/src", "app", r))

	if got := e.project("app"); got == nil || got.Settings != nil {
		t.Errorf("project is %+v, want an empty entry", got)
	}
	if got, ok := e.machine.overlays["app"]; ok {
		t.Errorf("overlay is %s, want none", jsonText(got))
	}
	if got := e.machine.machine.Checkouts; len(got) != 1 || got[0] != (domain.Checkout{Path: "/w/app", Project: "app"}) {
		t.Errorf("checkouts are %v", got)
	}
	if len(e.trees.links) > 0 || jsonText(e.settings("/w/app")) != `{"env":{"TOKEN":"x"},"model":"opus"}` {
		t.Errorf("project add wrote in the project")
	}
	contains(t, r,
		"project  app: added to brain.toml",
		"checkout /w/app (app): listed: review brain.toml, then nerve sync --here")
}

func TestRemovingAProjectKeepsTheSettingsItsCheckoutHadBefore(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")
	e.trees.settings["/w/app/.claude/settings.local.json"] = map[string]any{"model": "opus", "env": map[string]any{"TOKEN": "x"}}
	e.must(&recorder{}, e.app.AddProject("/w/app", "app", &recorder{}))
	if _, err := e.sync("/w/app", app.SyncOptions{Hook: true}); err != nil {
		t.Fatal(err)
	}

	e.must(&recorder{}, e.app.RemoveProject("app", &recorder{}))

	if got := jsonText(e.settings("/w/app")); got != `{"env":{"TOKEN":"x"},"model":"opus"}` {
		t.Errorf("settings are %s, want the checkout's own kept", got)
	}
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

func TestAddProjectJoinsAnExistingOneOrRefuses(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.checkout("/w/app2")
	e.checkout("/w/app3")
	r := &recorder{}

	e.must(r, e.app.AddProject("/w/app2", "app", r))

	contains(t, r, "project  app: already in the Brain: this checkout joins it")
	if e.brain.saves != 0 {
		t.Errorf("joining a project changed the Brain")
	}
	for _, c := range []struct{ dir, name, want string }{
		{"/w/app", "app", "already listed"},
		{"/w/app", "other", "/w/app is listed as project app"},
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
	if _, ok := e.machine.overlays["app"]; !ok {
		t.Errorf("the local overlay is gone, and what it held with it")
	}
	if _, ok := e.trees.links["/w/other/.claude/skills/nerve"]; !ok {
		t.Errorf("another project was cleaned")
	}
	contains(t, r, "cleaned  /w/app: project app removed", "project  app: dropped from brain.toml",
		"kept     /cfg/nerve/overlays/app.json: the local overlay of project app may hold what is nowhere else: delete it once you no longer need it")
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

func TestRemovingAProjectWhoseBrainSaveFailsKeepsItsCheckoutListed(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.brain.failSaves = 1

	if err := e.app.RemoveProject("app", &recorder{}); err == nil {
		t.Fatal("remove passed")
	}

	if got := e.machine.machine.Checkouts; len(got) != 1 {
		t.Errorf("checkouts are %v, want /w/app kept while app is in the Brain", got)
	}
}

func TestRemoveProjectRefusesANameThatIsAPath(t *testing.T) {
	e := newEnv(t)
	e.machine.overlays["../../victim"] = map[string]any{"x": "y"} // a file of the user's that the name reaches

	err := e.app.RemoveProject("../../victim", &recorder{})

	if _, kept := e.machine.overlays["../../victim"]; err == nil || !kept {
		t.Errorf("got %v, and the file the name reaches was removed: %v", err, !kept)
	}
}

func TestRemovingAProjectFromAMovedCheckoutNeverSyncedTakesOutWhatNerveMade(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/old").Skills = []string{"go-stack"}
	e.must(e.sync("/w/old", app.SyncOptions{}))
	links, settings := e.trees.links, e.settings("/w/old") // as moved, with .git, to /w/new
	e.trees.links = map[string]string{}
	for p, target := range links {
		e.trees.links[strings.Replace(p, "/w/old", "/w/new", 1)] = target
	}
	settings["outputStyle"] = "terse" // the user's own
	e.trees.settings["/w/new/.claude/settings.local.json"] = settings
	e.checkout("/w/new")
	e.machine.machine.Checkouts = []domain.Checkout{{Path: "/w/new", Project: "app"}}

	e.must(&recorder{}, e.app.RemoveProject("app", &recorder{}))

	if len(e.trees.links) > 0 || jsonText(e.settings("/w/new")) != `{"outputStyle":"terse"}` {
		t.Errorf("links %v, settings %s: want only the user's own left", e.trees.links, jsonText(e.settings("/w/new")))
	}
}

func TestRemovingAProjectKeepsWhatItsCheckoutHadAsTheBrainSetsIt(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"model": "opus", "permissions": map[string]any{"allow": []any{"Read", "Edit"}}}
	e.trees.settings["/w/app/.claude/settings.local.json"] = map[string]any{"model": "opus", "permissions": map[string]any{"allow": []any{"Read"}}}
	if _, err := e.sync("/w/app", app.SyncOptions{Hook: true}); err != nil {
		t.Fatal(err)
	}

	e.must(&recorder{}, e.app.RemoveProject("app", &recorder{}))

	if got := jsonText(e.settings("/w/app")); got != `{"model":"opus","permissions":{"allow":["Read"]}}` {
		t.Errorf("settings are %s, want what the checkout had before nerve", got)
	}
}

func TestRemovingAProjectNeverSyncedLeavesItsCheckoutsSettings(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"model": "opus"}
	e.trees.settings["/w/app/.claude/settings.local.json"] = map[string]any{"model": "opus"}

	e.must(&recorder{}, e.app.RemoveProject("app", &recorder{}))

	if got := jsonText(e.settings("/w/app")); got != `{"model":"opus"}` {
		t.Errorf("settings are %s, want the file nerve never wrote left as it is", got)
	}
}

func TestAddProjectWhoseBrainSaveFailsListsNothing(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/app")
	e.brain.failSaves = 1
	r := &recorder{}

	if err := e.app.AddProject("/w/app", "app", r); err == nil {
		t.Fatal("add passed")
	}

	if len(e.machine.machine.Checkouts) > 0 || len(r.events) > 0 {
		t.Errorf("checkouts %v, events:\n%s", e.machine.machine.Checkouts, r)
	}
}
