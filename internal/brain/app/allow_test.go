package app_test

import (
	"slices"
	"strings"
	"testing"
)

func TestAllowAddsKeysTheBrainMayHoldAndPushesThem(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"statusLine"}
	r := &recorder{}

	e.must(r, e.app.AllowSettings([]string{"mcpServers", "statusLine", "sandbox.enabled"}, r))

	if got := e.brain.m.AllowedSettings; !slices.Equal(got, []string{"statusLine", "mcpServers", "sandbox.enabled"}) {
		t.Errorf("allowedSettings is %q", got)
	}
	if e.brain.saves != 1 {
		t.Errorf("saves = %d, want 1", e.brain.saves)
	}
	contains(t, r, "allowed  mcpServers", "allowed  sandbox.enabled", "allowed  statusLine already")
}

func TestAllowOfAKeyAllowedAlreadyPushesNothing(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"statusLine"}
	r := &recorder{}

	e.must(r, e.app.AllowSettings([]string{"statusLine.command"}, r))

	if e.brain.saves != 0 {
		t.Errorf("an allow that adds nothing changed the Brain")
	}
	contains(t, r, "allowed  statusLine.command already")
}

func TestAllowRefusesAKeyNerveAllowsItself(t *testing.T) {
	e := newEnv(t)

	err := e.app.AllowSettings([]string{"statusLine", "effortLevel"}, &recorder{})

	if want := "nerve allows effortLevel itself: it needs no entry in allowedSettings"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
	if e.brain.saves != 0 {
		t.Errorf("a refused allow changed the Brain")
	}
}

func TestAllowRefusesAKeyUnderOneNerveAllowsItself(t *testing.T) {
	e := newEnv(t)

	err := e.app.AllowSettings([]string{"attribution.commit"}, &recorder{})

	if err == nil || !strings.Contains(err.Error(), "nerve allows attribution.commit itself") {
		t.Errorf("got %v", err)
	}
}

func TestAllowRefusesAKeyNerveWrites(t *testing.T) {
	e := newEnv(t)

	err := e.app.AllowSettings([]string{"hooks.Stop"}, &recorder{})

	if err == nil || !strings.Contains(err.Error(), `allowedSettings: "hooks.Stop" is written by nerve from hooks and plugins`) {
		t.Errorf("got %v", err)
	}
}

func TestAllowRefusesWhatIsNotASettingsKey(t *testing.T) {
	e := newEnv(t)

	err := e.app.AllowSettings([]string{"a..b"}, &recorder{})

	if err == nil || !strings.Contains(err.Error(), `allowedSettings: "a..b" is not a settings key`) {
		t.Errorf("got %v", err)
	}
}

func TestAllowOfAWiderKeyDropsTheKeysItCovers(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"statusLine.command", "mcpServers"}
	r := &recorder{}

	e.must(r, e.app.AllowSettings([]string{"sandbox.enabled", "sandbox", "statusLine"}, r))

	if got := e.brain.m.AllowedSettings; !slices.Equal(got, []string{"mcpServers", "sandbox", "statusLine"}) {
		t.Errorf("allowedSettings is %q", got)
	}
	contains(t, r, "dropped  sandbox.enabled from allowedSettings", "dropped  statusLine.command from allowedSettings", "allowed  statusLine")
}

func TestAllowRmDropsKeysAndPushes(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"effortLevel", "statusLine", "modelSettings"}
	e.brain.m.Common = map[string]any{"effortLevel": "high"} // nerve allows it itself
	r := &recorder{}

	e.must(r, e.app.RemoveAllowedSettings([]string{"effortLevel", "modelSettings"}, r))

	if got := e.brain.m.AllowedSettings; !slices.Equal(got, []string{"statusLine"}) {
		t.Errorf("allowedSettings is %q", got)
	}
	if e.brain.saves != 1 {
		t.Errorf("saves = %d, want 1", e.brain.saves)
	}
	contains(t, r, "dropped  effortLevel from allowedSettings", "dropped  modelSettings from allowedSettings")
}

func TestAllowRmRefusesAKeyNotListed(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"mcpServers"}

	err := e.app.RemoveAllowedSettings([]string{"mcpServers", "ghost"}, &recorder{})

	if want := "allowedSettings does not list ghost"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
	if e.brain.saves != 0 {
		t.Errorf("a refused allow rm changed the Brain")
	}
}

func TestAllowRmRefusesAKeyAProjectsSettingsHold(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"statusLine"}
	e.listed("app", "/w/app").Settings = map[string]any{"statusLine": map[string]any{"command": "x"}}

	err := e.app.RemoveAllowedSettings([]string{"statusLine"}, &recorder{})

	if want := "projects.app.settings.statusLine.command holds it: nerve setting rm --project app statusLine.command first"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

func TestAllowRmRefusesAKeyCommonSettingsHold(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"statusLine"}
	e.brain.m.Common = map[string]any{"statusLine": map[string]any{"command": "x"}}

	err := e.app.RemoveAllowedSettings([]string{"statusLine"}, &recorder{})

	if want := "common.settings.statusLine.command holds it: nerve setting rm --common statusLine.command first"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

func TestAllowRmDropsAKeyListedTwice(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"statusLine", "mcpServers", "statusLine"}
	r := &recorder{}

	e.must(r, e.app.RemoveAllowedSettings([]string{"statusLine"}, r))

	if got := e.brain.m.AllowedSettings; !slices.Equal(got, []string{"mcpServers"}) {
		t.Errorf("allowedSettings is %q", got)
	}
}

func TestAllowRmTakesAKeyAsItIsWrittenOrQuoted(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"statusLine", `extraKnownMarketplaces."acme.tools"`}
	r := &recorder{}

	e.must(r, e.app.RemoveAllowedSettings([]string{`"statusLine"`, `extraKnownMarketplaces."acme.tools"`}, r))

	if got := e.brain.m.AllowedSettings; len(got) > 0 {
		t.Errorf("allowedSettings is %q", got)
	}
}

func TestCheckNotesKeysNerveAllowsItselfWithoutFailing(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{"effortLevel", "statusLine", "attribution.commit"}
	r := &recorder{}

	e.must(r, e.app.Check(r))

	contains(t, r,
		"note     brain.json: allowedSettings lists effortLevel, which nerve allows itself: nerve allow rm effortLevel",
		"note     brain.json: allowedSettings lists attribution.commit, which nerve allows itself: nerve allow rm attribution.commit",
	)
	if strings.Contains(r.String(), "lists statusLine") {
		t.Errorf("a key nerve does not allow is noted:\n%s", r)
	}
}

func TestAllowRmQuotesForTheShellAKeyThatNeedsIt(t *testing.T) {
	e := newEnv(t)
	e.brain.m.AllowedSettings = []string{`extraKnownMarketplaces."acme.tools"`}
	e.brain.m.Common = map[string]any{"extraKnownMarketplaces": map[string]any{"acme.tools": map[string]any{"source": "x"}}}

	err := e.app.RemoveAllowedSettings([]string{`extraKnownMarketplaces."acme.tools"`}, &recorder{})

	if want := `common.settings.extraKnownMarketplaces."acme.tools".source holds it: nerve setting rm --common 'extraKnownMarketplaces."acme.tools".source' first`; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}
