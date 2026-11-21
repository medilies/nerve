package app_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// brainCommands are the commands that change brain.json, each with the
// message it pushes the change with.
func brainCommands(e *env) []struct {
	name, message string
	run           func(r *recorder) error
} {
	return []struct {
		name, message string
		run           func(r *recorder) error
	}{
		{"project add", "nerve: add project fresh", func(r *recorder) error { return e.app.AddProject("/w/fresh", "fresh", r) }},
		{"skill add", "nerve: add skill go-stack to project app", func(r *recorder) error {
			return e.app.AddItem(target("/w/app"), "skill", "go-stack", r)
		}},
		{"skill rm", "nerve: remove skill tdd from project app", func(r *recorder) error {
			return e.app.RemoveItem(target("/w/app"), "skill", "tdd", false, r)
		}},
		{"skill rm --all", "nerve: remove skill tdd from every project", func(r *recorder) error {
			e.project("app").Skills = []string{"tdd"}
			return e.app.RemoveItem(target("/w/app"), "skill", "tdd", true, r)
		}},
		{"setting set", "nerve: set model in project app", func(r *recorder) error {
			return e.app.SetSetting(target("/w/app"), "model", "opus", false, r)
		}},
		{"setting set --common", "nerve: set model in common", func(r *recorder) error {
			return e.app.SetSetting(target("/w/app"), "model", "opus", true, r)
		}},
		{"setting add", "nerve: add an item to permissions.allow in project app", func(r *recorder) error {
			return e.app.AddSetting(target("/w/app"), "permissions.allow", "Read", false, r)
		}},
		{"setting rm", "nerve: remove outputStyle from project app", func(r *recorder) error {
			e.project("app").Settings = map[string]any{"outputStyle": "x"}
			return e.app.RemoveSetting(target("/w/app"), "outputStyle", false, false, r)
		}},
		{"update", "nerve: pin acme/up at " + pinB[:7], func(r *recorder) error { return e.app.Update("/w/app", "", r) }},
		{"project rm", "nerve: remove project app", func(r *recorder) error { return e.app.RemoveProject("app", r) }},
	}
}

func brainEnv(t *testing.T) *env {
	e := newEnv(t)
	e.listed("app", "/w/app").Skills = []string{"tdd"}
	e.checkout("/w/fresh")
	e.ownSkill("go-stack")
	e.ownSkill("tdd")
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.heads["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	return e
}

func TestACommandThatChangesBrainTomlBringsTheRemoteFirstThenPushesTheChange(t *testing.T) {
	for i := range brainCommands(brainEnv(t)) {
		e := brainEnv(t)
		c := brainCommands(e)[i]
		r := &recorder{}
		e.must(r, c.run(r))
		calls := slices.DeleteFunc(slices.Clone(e.brain.calls), func(s string) bool { return s == "sync" })
		if len(calls) < 3 || calls[0] != "refresh" || calls[1] != "load" {
			t.Errorf("%s: calls %v, want refresh, then load, first", c.name, calls)
		}
		if i := slices.Index(calls, "save"); i < 0 || i+1 >= len(calls) || calls[i+1] != "push-manifest" {
			t.Errorf("%s: calls %v, want each save manifestPushes at once", c.name, calls)
		}
		if !slices.Equal(e.brain.manifestPushes, []string{c.message}) {
			t.Errorf("%s: manifestPushes %q, want %q", c.name, e.brain.manifestPushes, c.message)
		}
		contains(t, r, "pushed   the Brain at aaaaaaa: "+c.message)
	}
}

func TestACommandThatCannotBringTheRemoteChangesNothing(t *testing.T) {
	for i := range brainCommands(brainEnv(t)) {
		e := brainEnv(t)
		c := brainCommands(e)[i]
		e.brain.refreshErr = errors.New("brain.json has changes not pushed")
		err := c.run(&recorder{})
		if err == nil || !strings.Contains(err.Error(), "brain.json has changes not pushed") {
			t.Errorf("%s: got %v", c.name, err)
		}
		if e.brain.saves != 0 || len(e.machine.machine.Checkouts) != 1 || e.trees.writes != 0 {
			t.Errorf("%s: changed something: %d saves, %d checkouts, %d writes", c.name, e.brain.saves, len(e.machine.machine.Checkouts), e.trees.writes)
		}
	}
}

func TestACommandWhosePushFailsChangesNothing(t *testing.T) {
	e := brainEnv(t)
	e.brain.manifestPushErr = fmt.Errorf("%w: it has 1 commit", app.ErrRemoteMoved)
	err := e.app.AddItem(target("/w/app"), "skill", "go-stack", &recorder{})
	if err == nil || !strings.Contains(err.Error(), "nothing changed: run the command again") || strings.Contains(err.Error(), "nerve push") {
		t.Errorf("got %v", err)
	}
	e.brain.manifestPushErr = errors.New("cannot push: fatal: unable to access the remote")
	err = e.app.AddItem(target("/w/app"), "skill", "go-stack", &recorder{})
	if err == nil || !strings.HasSuffix(err.Error(), ": nothing changed") {
		t.Errorf("a push that fails again offers to run it again: %v", err)
	}
	if slices.Contains(e.project("app").Skills, "go-stack") {
		t.Errorf("brain.json kept a change its remote refused")
	}
}

func TestASettingThatGoesToTheOverlayPushesNothing(t *testing.T) {
	e := brainEnv(t)
	r := e.must(&recorder{}, nil)
	e.must(r, e.app.SetSetting(target("/w/app"), "theme", "dark", false, r))
	if len(e.brain.manifestPushes) != 0 || e.brain.saves != 0 {
		t.Errorf("manifestPushes %q", e.brain.manifestPushes)
	}
}

func TestAProjectAddJoiningAProjectPushesNothing(t *testing.T) {
	e := brainEnv(t)
	e.checkout("/w/app2")
	r := &recorder{}
	e.must(r, e.app.AddProject("/w/app2", "app", r))
	if len(e.brain.manifestPushes) != 0 {
		t.Errorf("manifestPushes %q", e.brain.manifestPushes)
	}
	contains(t, r, "checkout /w/app2 (app): listed")
}

func TestAProjectAddSyncsTheCheckoutButNotTheBrainItJustPushed(t *testing.T) {
	e := brainEnv(t)
	r := &recorder{}
	e.must(r, e.app.AddProject("/w/fresh", "fresh", r))
	// The Brain is its remote's once the project is pushed: a sync of the
	// Brain would only drop the user's work in it.
	if e.brain.syncs != 0 {
		t.Errorf("project add synced the Brain %d times", e.brain.syncs)
	}
	contains(t, r, "synced   fresh at aaaaaaa in /w/fresh")
}

func TestUpdateSyncsTheListedCheckoutAroundTheWorkingDirectory(t *testing.T) {
	e := brainEnv(t)
	r := &recorder{}
	e.must(r, e.app.Update("/w/app", "", r))
	// The Brain is its remote's once the pin is pushed: a sync of the Brain
	// would only drop the user's work in it.
	if e.brain.syncs != 0 {
		t.Errorf("calls %v: update made the Brain its remote's again", e.brain.calls)
	}
	contains(t, r, "pinned   acme/up at 84b741e", "synced   app at aaaaaaa in /w/app")
}

func TestUpdateOutsideAListedCheckoutOnlyMovesPins(t *testing.T) {
	e := brainEnv(t)
	e.checkout("/w/elsewhere")
	r := &recorder{}
	e.must(r, e.app.Update("/w/elsewhere", "", r))
	if e.brain.syncs != 0 || len(e.brain.manifestPushes) != 1 {
		t.Errorf("syncs %d, manifestPushes %q", e.brain.syncs, e.brain.manifestPushes)
	}
}

func TestUpdateReportsAPinOnlyOnceItIsPushed(t *testing.T) {
	e := brainEnv(t)
	e.brain.manifestPushErr = errors.New("git push: rejected")
	r := &recorder{}
	if err := e.app.Update("/w/app", "", r); err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(r.String(), "pinned") {
		t.Errorf("reported a pin that was not pushed:\n%s", r)
	}
}

func TestUpdateWithNoSourcesSaysSo(t *testing.T) {
	e := newEnv(t)
	r := &recorder{}
	e.must(r, e.app.Update("/w", "", r))
	contains(t, r, "brain.json has no sources to update")
}

func TestSyncHereOutsideAListedCheckoutLeavesTheBrainAsItIs(t *testing.T) {
	e := newEnv(t)
	e.checkout("/w/unlisted")
	_, err := e.sync("/w/unlisted", app.SyncOptions{Here: true})
	if err == nil || e.brain.syncs != 0 {
		t.Errorf("err %v, Brain synced %d times", err, e.brain.syncs)
	}
	_, err = e.sync("/nowhere", app.SyncOptions{Here: true})
	if err == nil || e.brain.syncs != 0 {
		t.Errorf("err %v, Brain synced %d times", err, e.brain.syncs)
	}
}

func TestProjectAddRefusesTheBrainsOwnCheckout(t *testing.T) {
	e := newEnv(t)
	e.checkout("/brain")
	err := e.app.AddProject("/brain", "inbrain", &recorder{})
	if err == nil || !strings.Contains(err.Error(), "is the Brain's checkout") {
		t.Errorf("got %v", err)
	}
}

func TestUpdateOutsideAListedCheckoutSaysNothingHereIsSynced(t *testing.T) {
	e := brainEnv(t)
	e.checkout("/w/elsewhere")
	r := &recorder{}

	e.must(r, e.app.Update("/w/elsewhere", "", r))

	contains(t, r, "no listed checkout here: nerve sync carries the new pins to each one")
}
