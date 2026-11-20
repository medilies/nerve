package app_test

import (
	"strings"
	"testing"
)

func TestSetSettingInTheProjectOrCommon(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), "permissions.deny", []any{"Cron"}, false, r))
	e.must(r, e.app.SetSetting(target("/w/app"), "model", "opus", true, r))

	if got := jsonText(e.project("app").Settings); got != `{"permissions":{"deny":["Cron"]}}` {
		t.Errorf("project settings are %s", got)
	}
	if got := jsonText(e.brain.m.Common); got != `{"model":"opus"}` {
		t.Errorf("common is %s", got)
	}
	contains(t, r, `set      permissions.deny = ["Cron"]`, `set      model = "opus"`)
}

func TestSetSettingSendsASensitiveValueToTheOverlay(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), "env", map[string]any{"TOKEN": "abc"}, false, r))

	if e.project("app").Settings != nil || e.brain.saves != 0 {
		t.Errorf("a secret reached the Brain")
	}
	if got := jsonText(e.machine.overlays["app"]); got != `{"env":{"TOKEN":"abc"}}` {
		t.Errorf("overlay is %s", got)
	}
	contains(t, r, "overlay  env set in /cfg/nerve/overlays/app.json: env may hold secrets")
	err := e.app.SetSetting(target("/w/app"), "env", map[string]any{"TOKEN": "abc"}, true, &recorder{})
	if err == nil || !strings.Contains(err.Error(), "cannot go in common") {
		t.Errorf("got %v, want common refused", err)
	}
	if err := e.app.SetSetting(target("/w/app"), "model.", "x", false, &recorder{}); err == nil {
		t.Errorf("a key ending in a dot was set")
	}
}

func TestRemoveSettingEverywhereRevokesIt(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Common = map[string]any{"permissions": map[string]any{"deny": []any{"NotebookEdit", "Cron"}}}
	e.listed("app", "/w/app").Settings = map[string]any{"permissions": map[string]any{"deny": []any{"NotebookEdit"}}, "model": "opus"}
	e.listed("other", "/w/other")
	r := &recorder{}

	e.must(r, e.app.RemoveSetting(target("/w"), "permissions.deny=NotebookEdit", false, true, r))
	e.must(r, e.app.RemoveSetting(target("/w/app"), "model", false, false, r))

	if got := jsonText(e.brain.m.Common); got != `{"permissions":{"deny":["Cron"]}}` {
		t.Errorf("common is %s", got)
	}
	if got := e.project("app").Settings; got != nil {
		t.Errorf("project settings are %s, want none", jsonText(got))
	}
	contains(t, r, "dropped  setting permissions.deny=NotebookEdit from common", "dropped  setting permissions.deny=NotebookEdit from project app")
	for _, c := range []struct {
		common, all bool
		want        string
	}{{false, false, "no project app holds"}, {true, false, "no common holds"}, {false, true, "no project nor common holds"}} {
		if err := e.app.RemoveSetting(target("/w/app"), "model", c.common, c.all, &recorder{}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("got %v, want %q", err, c.want)
		}
	}
}

func TestListSettingsNamesTheLayersGivingEach(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Common = map[string]any{"permissions": map[string]any{"deny": []any{"A"}}}
	p := e.listed("app", "/w/app")
	p.Settings = map[string]any{"permissions": map[string]any{"deny": []any{"B"}}}
	p.Plugins = []string{"x@y"}
	e.machine.overlays["app"] = map[string]any{"env": map[string]any{"T": "1"}}
	r := &recorder{}

	e.must(r, e.app.ListSettings(target("/w/app"), r))

	contains(t, r,
		`permissions.deny = ["A","B"]  (common + project)`,
		`enabledPlugins.x@y = true  (nerve: plugins, hooks, autosync)`,
		`env.T = "1"  (local overlay)`)
}
