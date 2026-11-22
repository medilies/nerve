package app_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// A command that changes what a project gets carries it to the listed
// checkout around the working directory, when the change reaches it.

func TestAddItemLinksTheItemInTheCheckoutHere(t *testing.T) {
	e := brainEnv(t)
	r := &recorder{}

	e.must(r, e.app.AddItem(target("/w/app"), "skill", []string{"go-stack"}, r))

	if _, ok := e.trees.links["/w/app/.claude/skills/go-stack"]; !ok {
		t.Errorf("links are %v, want go-stack", e.trees.links)
	}
	if e.brain.syncs != 0 {
		t.Errorf("calls %v: the Brain just pushed was made its remote's again", e.brain.calls)
	}
}

func TestAddItemTakesSeveralNamesInOnePush(t *testing.T) {
	e := brainEnv(t)
	e.brain.agents["reviewer"] = true
	e.brain.agents["explorer"] = true
	r := &recorder{}

	e.must(r, e.app.AddItem(target("/w/app"), "agent", []string{"reviewer", "explorer"}, r))

	if got := e.project("app").Agents; !reflect.DeepEqual(got, []string{"reviewer", "explorer"}) {
		t.Errorf("agents are %v", got)
	}
	if want := []string{"nerve: add agent reviewer, explorer to project app"}; !reflect.DeepEqual(e.brain.manifestPushes, want) {
		t.Errorf("pushes are %q, want %q", e.brain.manifestPushes, want)
	}
	contains(t, r, "added    agent reviewer to project app", "added    agent explorer to project app")
}

func TestAddItemRefusesEveryNameWhenOneIsRefused(t *testing.T) {
	e := brainEnv(t)

	err := e.app.AddItem(target("/w/app"), "skill", []string{"go-stack", "ghost"}, &recorder{})

	if err == nil || e.brain.saves != 0 {
		t.Errorf("err %v, saves %d: want a refusal that changes nothing", err, e.brain.saves)
	}
}

func TestAddItemRefusesACheckoutHereSyncWouldRefuseAndChangesNothing(t *testing.T) {
	e := brainEnv(t)
	e.trees.unignored["/w/app"] = true

	err := e.app.AddItem(target("/w/app"), "skill", []string{"go-stack"}, &recorder{})

	if err == nil || e.brain.saves != 0 {
		t.Errorf("err %v, saves %d: want a refusal that changes nothing", err, e.brain.saves)
	}
}

func TestAddItemForAnotherProjectLeavesTheCheckoutHereAsItIs(t *testing.T) {
	e := brainEnv(t)
	e.brain.m.Projects["other"] = &domain.Project{}
	r := &recorder{}

	e.must(r, e.app.AddItem(app.ProjectRef{Dir: "/w/app", Project: "other"}, "skill", []string{"go-stack"}, r))

	if len(e.trees.links) > 0 {
		t.Errorf("links %v: want no sync", e.trees.links)
	}
}

func TestAddItemOutsideAListedCheckoutOnlyPushes(t *testing.T) {
	e := brainEnv(t)
	r := &recorder{}

	e.must(r, e.app.AddItem(app.ProjectRef{Dir: "/w/fresh", Project: "app"}, "skill", []string{"go-stack"}, r))

	if len(e.brain.manifestPushes) != 1 || len(e.trees.links) > 0 {
		t.Errorf("pushes %q, links %v: want one push and no sync", e.brain.manifestPushes, e.trees.links)
	}
}

func TestAddItemAlreadyListedPushesNothingButSyncsHere(t *testing.T) {
	e := brainEnv(t)
	r := &recorder{}

	e.must(r, e.app.AddItem(target("/w/app"), "skill", []string{"tdd"}, r))

	if _, ok := e.trees.links["/w/app/.claude/skills/tdd"]; !ok || len(e.brain.manifestPushes) > 0 {
		t.Errorf("pushes %q, links %v: want tdd linked, and no push", e.brain.manifestPushes, e.trees.links)
	}
}

func TestRemoveItemTakesSeveralNamesAndUnlinksThemHere(t *testing.T) {
	e := brainEnv(t)
	e.project("app").Skills = []string{"tdd", "go-stack"}
	e.must(&recorder{}, e.app.Sync(app.SyncOptions{Dir: "/w/app", Here: true}, &recorder{}))
	r := &recorder{}

	e.must(r, e.app.RemoveItem(target("/w/app"), "skill", []string{"tdd", "go-stack"}, false, r))

	if len(e.project("app").Skills) > 0 || len(e.brain.manifestPushes) != 1 {
		t.Errorf("skills %v, pushes %q", e.project("app").Skills, e.brain.manifestPushes)
	}
	for _, name := range []string{"tdd", "go-stack"} {
		if _, ok := e.trees.links["/w/app/.claude/skills/"+name]; ok {
			t.Errorf("%s still linked here", name)
		}
	}
}

func TestRemoveItemRefusesEveryNameWhenOneIsNotListed(t *testing.T) {
	e := brainEnv(t)

	err := e.app.RemoveItem(target("/w/app"), "skill", []string{"tdd", "ghost"}, false, &recorder{})

	if err == nil || e.brain.saves != 0 {
		t.Errorf("err %v, saves %d: want a refusal that changes nothing", err, e.brain.saves)
	}
}

func TestSetSettingWritesItInTheCheckoutHere(t *testing.T) {
	for _, common := range []bool{false, true} {
		e := brainEnv(t)
		r := &recorder{}

		e.must(r, e.app.SetSetting(target("/w/app"), "model", "opus", common, r))

		if got := e.settings("/w/app")["model"]; got != "opus" {
			t.Errorf("common %v: model here is %v", common, got)
		}
	}
}

func TestSetSettingInTheOverlayWritesItInTheCheckoutHere(t *testing.T) {
	e := brainEnv(t)
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), "statusLine.command", "line", false, r))

	if got := jsonText(e.settings("/w/app")["statusLine"]); got != `{"command":"line"}` {
		t.Errorf("statusLine here is %s", got)
	}
}

func TestRemoveSettingTakesItOutOfTheCheckoutHere(t *testing.T) {
	e := brainEnv(t)
	e.must(&recorder{}, e.app.SetSetting(target("/w/app"), "model", "opus", false, &recorder{}))
	r := &recorder{}

	e.must(r, e.app.RemoveSetting(target("/w/app"), "model", false, false, r))

	if got, ok := e.settings("/w/app")["model"]; ok {
		t.Errorf("model here is %v", got)
	}
}

func TestSetSettingRefusesACheckoutHereSyncWouldRefuseAndChangesNothing(t *testing.T) {
	e := brainEnv(t)
	e.trees.unignored["/w/app"] = true

	err := e.app.SetSetting(target("/w/app"), "model", "opus", true, &recorder{})

	if err == nil || e.brain.saves != 0 {
		t.Errorf("err %v, saves %d: want a refusal that changes nothing", err, e.brain.saves)
	}
}

func TestAddItemWithTheProjectHereNamedSyncsHere(t *testing.T) {
	e := brainEnv(t)
	r := &recorder{}

	e.must(r, e.app.AddItem(app.ProjectRef{Dir: "/w/app", Project: "app"}, "skill", []string{"go-stack"}, r))

	if _, ok := e.trees.links["/w/app/.claude/skills/go-stack"]; !ok {
		t.Errorf("links are %v, want go-stack", e.trees.links)
	}
}

func TestRemoveItemSkipsARepeatedName(t *testing.T) {
	e := brainEnv(t)
	r := &recorder{}

	e.must(r, e.app.RemoveItem(target("/w/app"), "skill", []string{"tdd", "tdd"}, false, r))

	if want := []string{"nerve: remove skill tdd from project app"}; !reflect.DeepEqual(e.brain.manifestPushes, want) {
		t.Errorf("pushes are %q, want %q", e.brain.manifestPushes, want)
	}
}

func TestRemoveItemFromEveryProjectUnlinksItHere(t *testing.T) {
	e := brainEnv(t)
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	r := &recorder{}

	e.must(r, e.app.RemoveItem(target("/w/app"), "skill", []string{"tdd"}, true, r))

	if _, ok := e.trees.links["/w/app/.claude/skills/tdd"]; ok {
		t.Errorf("tdd still linked here")
	}
}

func TestRemoveSettingFromCommonOrEveryProjectTakesItOutHere(t *testing.T) {
	for _, all := range []bool{false, true} {
		e := brainEnv(t)
		e.must(&recorder{}, e.app.SetSetting(target("/w/app"), "model", "opus", true, &recorder{}))
		r := &recorder{}

		e.must(r, e.app.RemoveSetting(target("/w/app"), "model", !all, all, r))

		if got, ok := e.settings("/w/app")["model"]; ok {
			t.Errorf("all %v: model here is %v", all, got)
		}
	}
}

func TestRemoveSettingOnlyCommonHoldsNamesTheCommandForEveryProject(t *testing.T) {
	e := brainEnv(t)
	e.brain.m.Common = map[string]any{"model": "opus"}

	err := e.app.RemoveSetting(target("/w/app"), "model", false, false, &recorder{})

	if err == nil || !strings.Contains(err.Error(), "nerve setting rm --common model") {
		t.Errorf("got %v, want the command that takes it out of every project", err)
	}
}

// What a command names wins over drift here: the user said so.

func TestSetSettingOverwritesItsDriftHereAndLeavesOtherDrift(t *testing.T) {
	e := brainEnv(t)
	e.project("app").Settings = map[string]any{"model": "opus", "effortLevel": "high"}
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	e.settings("/w/app")["model"] = "sonnet"
	e.settings("/w/app")["effortLevel"] = "low"
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), "model", "haiku", false, r))

	if got := e.settings("/w/app"); got["model"] != "haiku" || got["effortLevel"] != "low" {
		t.Errorf("settings here are %v, want model haiku and effortLevel left low", got)
	}
}

func TestSetSettingToTheBrainsValueDropsItsDriftHere(t *testing.T) {
	e := brainEnv(t)
	e.project("app").Settings = map[string]any{"model": "opus"}
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	e.settings("/w/app")["model"] = "sonnet"
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), "model", "opus", false, r))

	if got := e.settings("/w/app")["model"]; got != "opus" {
		t.Errorf("model here is %v", got)
	}
}

func TestAddSettingOverwritesTheDriftOfItsItemHere(t *testing.T) {
	e := brainEnv(t)
	e.project("app").Settings = map[string]any{"permissions": map[string]any{"allow": []any{"Read"}}}
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	e.settings("/w/app")["permissions"] = map[string]any{"allow": []any{}}
	r := &recorder{}

	e.must(r, e.app.AddSetting(target("/w/app"), "permissions.allow", "Read", false, r))

	if got := jsonText(e.settings("/w/app")["permissions"]); got != `{"allow":["Read"]}` {
		t.Errorf("permissions here are %s", got)
	}
}

func TestAddPluginOverwritesItsDriftHere(t *testing.T) {
	e := brainEnv(t)
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	e.settings("/w/app")["enabledPlugins"] = map[string]any{"x@y": false}
	r := &recorder{}

	e.must(r, e.app.AddItem(target("/w/app"), "plugin", []string{"x@y"}, r))

	if got := jsonText(e.settings("/w/app")["enabledPlugins"]); got != `{"x@y":true}` {
		t.Errorf("enabledPlugins here are %s", got)
	}
}
