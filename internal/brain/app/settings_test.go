package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
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
}

func TestSetSettingSendsASettingNoListAllowsToTheOverlay(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), "statusLine.command", "~/bin/line", false, r))

	if e.project("app").Settings != nil || e.brain.saves != 0 {
		t.Errorf("a setting no list allows reached the Brain")
	}
	contains(t, r, "overlay  statusLine.command set in /cfg/nerve/overlays/app.json: not an allowed setting: list it in allowed-settings to share it")
	err := e.app.SetSetting(target("/w/app"), "statusLine.command", "~/bin/line", true, &recorder{})
	if err == nil || !strings.Contains(err.Error(), "statusLine.command is not an allowed setting: list it in allowed-settings") {
		t.Errorf("got %v, want common refused", err)
	}
}

func TestSetSettingPutsAnAllowedSettingInTheBrain(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.AllowedSettings = []string{"statusLine"}

	e.must(&recorder{}, e.app.SetSetting(target("/w/app"), "statusLine.command", "~/bin/line", false, &recorder{}))

	if got := jsonText(e.project("app").Settings); got != `{"statusLine":{"command":"~/bin/line"}}` {
		t.Errorf("project settings are %s", got)
	}
}

func TestSetSettingSendsAnObjectHoldingASecretKeyToTheOverlay(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.AllowedSettings = []string{"mcpServers"}
	v := map[string]any{"db": map[string]any{"headers": map[string]any{"X-Api-Key": "k"}}}

	e.must(&recorder{}, e.app.SetSetting(target("/w/app"), "mcpServers", v, false, &recorder{}))

	if e.project("app").Settings != nil {
		t.Errorf("a secret reached the Brain: %s", jsonText(e.project("app").Settings))
	}
}

func TestSetSettingSendsAnIdentifyingKeyToTheOverlay(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), `enabledPlugins."a@bob.io"`, true, false, r))

	if e.project("app").Settings != nil || e.brain.saves != 0 {
		t.Errorf("an identifying key reached the Brain")
	}
	if got := jsonText(e.machine.overlays["app"]); got != `{"enabledPlugins":{"a@bob.io":true}}` {
		t.Errorf("overlay is %s", got)
	}
	contains(t, r, "overlay  enabledPlugins.(hidden) set in /cfg/nerve/overlays/app.json: the key holds an email address")
}

func TestSetSettingReportsTheObjectItSets(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), "permissions", map[string]any{"defaultMode": "plan"}, false, r))

	contains(t, r, `set      permissions = {"defaultMode":"plan"}`)
}

func TestSetSettingRefusesWhatBrainTomlCannotHold(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	for _, c := range []struct {
		key  string
		v    any
		want string
	}{
		{"model.", "x", "must be keys joined by dots"},
		{"a..b", "x", "must be keys joined by dots"},
		{"foo", nil, "foo cannot hold null: to remove a setting, use setting rm"},
		{"foo", []any{"a", nil}, "cannot hold null"},
	} {
		err := e.app.SetSetting(target("/w/app"), c.key, c.v, false, &recorder{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s = %v: got %v, want %q", c.key, c.v, err, c.want)
		}
	}
	if e.brain.saves != 0 {
		t.Errorf("a refused setting changed the Brain")
	}
}

func TestSetSettingTakesAQuotedKeyWithDots(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.AllowedSettings = []string{"extraKnownMarketplaces"}
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), `extraKnownMarketplaces."acme.tools"`, true, false, r))

	if got := jsonText(e.project("app").Settings); got != `{"extraKnownMarketplaces":{"acme.tools":true}}` {
		t.Errorf("project settings are %s", got)
	}
	e.must(r, e.app.RemoveSetting(target("/w/app"), `extraKnownMarketplaces."acme.tools"`, false, false, r))
	if e.project("app").Settings != nil {
		t.Errorf("the quoted key was not removed: %s", jsonText(e.project("app").Settings))
	}
}

func TestSettingCommandsRefuseTwoPlacesAtOnce(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	acct := app.ProjectRef{Dir: "/w/app", Project: "app"}
	for name, err := range map[string]error{
		"set --common --project":   e.app.SetSetting(acct, "model", "opus", true, &recorder{}),
		"add --common --project":   e.app.AddSetting(acct, "permissions.deny", "Cron", true, &recorder{}),
		"rm --all --project":       e.app.RemoveSetting(acct, "model", false, true, &recorder{}),
		"rm --all --common":        e.app.RemoveSetting(target("/w/app"), "model", true, true, &recorder{}),
		"skill rm --all --project": e.app.RemoveItem(acct, "skill", "x", true, &recorder{}),
		"autosync --all --project": e.app.SetAutosync(acct, false, true, &recorder{}),
	} {
		if err == nil || !strings.Contains(err.Error(), "each say where to act: pass one") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if e.brain.saves != 0 {
		t.Errorf("a refused command changed the Brain")
	}
}

func TestAddSettingAddsOneItemToAList(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"permissions": map[string]any{"allow": []any{"Read"}}, "model": "opus"}
	r := &recorder{}

	e.must(r, e.app.AddSetting(target("/w/app"), "permissions.allow", "Bash(make)", false, r))
	e.must(r, e.app.AddSetting(target("/w/app"), "permissions.allow", "Bash(make)", false, r))
	e.must(r, e.app.AddSetting(target("/w/app"), "permissions.deny", "Cron", true, r))

	if got := jsonText(e.project("app").Settings["permissions"]); got != `{"allow":["Read","Bash(make)"]}` {
		t.Errorf("project permissions are %s", got)
	}
	if got := jsonText(e.brain.m.Common); got != `{"permissions":{"deny":["Cron"]}}` {
		t.Errorf("common is %s", got)
	}
	contains(t, r,
		`set      permissions.allow = ["Read","Bash(make)"] (was ["Read"])`,
		`permissions.allow already holds "Bash(make)" in brain.toml (project app)`,
		`set      permissions.deny = ["Cron"]`)
	if err := e.app.AddSetting(target("/w/app"), "model", "x", false, &recorder{}); err == nil || err.Error() != "model is not a list" {
		t.Errorf("got %v, want a value that is not a list refused", err)
	}
}

func TestAddSettingSendsASensitiveItemToTheOverlay(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.AddSetting(target("/w/app"), "permissions.allow", "Read(/home/bob/notes)", false, r))

	if e.project("app").Settings != nil || e.brain.saves != 0 {
		t.Errorf("a home path reached the Brain")
	}
	if got := jsonText(e.machine.overlays["app"]); got != `{"permissions":{"allow":["Read(/home/bob/notes)"]}}` {
		t.Errorf("overlay is %s", got)
	}
	err := e.app.AddSetting(target("/w/app"), "permissions.allow", "Read(/home/bob/notes)", true, &recorder{})
	if err == nil || !strings.Contains(err.Error(), "cannot go in common") {
		t.Errorf("got %v, want common refused", err)
	}
}

func TestRemoveSettingTakesItFromTheOverlayToo(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.listed("other", "/w/other")
	e.machine.overlays["app"] = map[string]any{"env": map[string]any{"FOO": "bar"}}
	e.machine.overlays["other"] = map[string]any{"env": map[string]any{"FOO": "baz", "KEEP": "x"}}
	r := &recorder{}

	e.must(r, e.app.RemoveSetting(target("/w/app"), "env.FOO", false, false, r))

	if _, ok := e.machine.overlays["app"]; ok {
		t.Errorf("the emptied overlay is kept: %v", e.machine.overlays["app"])
	}
	contains(t, r, "dropped  setting env.FOO from the local overlay of project app")
	e.must(r, e.app.RemoveSetting(target("/w"), "env.FOO", false, true, r))
	if got := jsonText(e.machine.overlays["other"]); got != `{"env":{"KEEP":"x"}}` {
		t.Errorf("other's overlay is %s", got)
	}
	if e.brain.saves != 0 {
		t.Errorf("removing from overlays changed the Brain")
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

func TestRemoveSettingNeverShowsASecretItem(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.machine.overlays["app"] = map[string]any{"permissions": map[string]any{"allow": []any{"Read(/home/bob/x)"}}}
	r := &recorder{}

	e.must(r, e.app.RemoveSetting(target("/w/app"), "permissions.allow=Read(/home/bob/x)", false, false, r))
	err := e.app.RemoveSetting(target("/w/app"), "env.TOKEN=sk-abcdefghijklmnopqrstuvwxyz", false, false, r)

	if out := r.String() + errText(err); strings.Contains(out, "/home/bob") || strings.Contains(out, "sk-") {
		t.Errorf("a secret is shown: %s", out)
	}
	contains(t, r, "dropped  setting permissions.allow=(hidden) from the local overlay of project app")
	if err == nil || err.Error() != "no project app holds env.TOKEN=(hidden)" {
		t.Errorf("got %v", err)
	}
}

func TestRemoveSettingReportsWhatItSavedBeforeAFailure(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"model": "opus"}
	e.machine.overlays["app"] = map[string]any{"model": "haiku"}
	e.machine.fail["DropOverlay"] = errors.New("overlays: read-only")
	r := &recorder{}

	err := e.app.RemoveSetting(target("/w/app"), "model", false, false, r)

	if err == nil || e.project("app").Settings != nil {
		t.Errorf("got %v, project settings %v", err, e.project("app").Settings)
	}
	contains(t, r, "dropped  setting model from project app")
	if strings.Contains(r.String(), "local overlay") {
		t.Errorf("reports a removal it did not save:\n%s", r)
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
		`env.T = (hidden)  (local overlay)`)
}

func TestSetSettingKeepsNoRepeatInAListInTheBrain(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")

	_ = e.app.SetSetting(target("/w/app"), "permissions.allow", []any{"a", "a"}, false, &recorder{})

	if got := jsonText(e.project("app").Settings); strings.Contains(got, `"a","a"`) {
		t.Errorf("project settings are %s", got)
	}
}

func TestSetSettingInTheBrainDropsTheOverlaysValueThatWouldWin(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.AllowedSettings = []string{"statusLine"}
	e.machine.overlays["app"] = map[string]any{"statusLine": map[string]any{"command": "x", "padding": int64(1)}}
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), "statusLine.command", "y", false, r))

	if got := jsonText(e.machine.overlays["app"]); got != `{"statusLine":{"padding":1}}` {
		t.Errorf("overlay is %s, want only the key set in the Brain dropped", got)
	}
	contains(t, r, `set      statusLine.command = "y"`, "dropped  setting statusLine.command from the local overlay of project app")
}

func TestSetSettingInTheBrainDropsAnOverlayValueAboveTheKey(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.AllowedSettings = []string{"statusLine"}
	e.machine.overlays["app"] = map[string]any{"statusLine": "x"}

	e.must(&recorder{}, e.app.SetSetting(target("/w/app"), "statusLine.command", "y", false, &recorder{}))

	if _, ok := e.machine.overlays["app"]; ok {
		t.Errorf("overlay is %s, want it gone with the value that would win", jsonText(e.machine.overlays["app"]))
	}
}

func TestSetSettingNamesHowToShareAPluginOrAHook(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.SetSetting(target("/w/app"), `enabledPlugins."x@y"`, true, false, r))

	contains(t, r, `overlay  enabledPlugins.x@y set in /cfg/nerve/overlays/app.json: not an allowed setting: share a plugin with nerve plugin add, a hook with [hook.<name>]`)
}

func TestSetSettingInCommonNamesTheKeyAndWhy(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")

	err := e.app.SetSetting(target("/w/app"), "apiKeyHelper", "x", true, &recorder{})

	if want := "apiKeyHelper: apiKeyHelper may hold secrets, so it cannot go in common: set it per project, where it goes to the local overlay"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}
