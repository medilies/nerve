package app_test

import (
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// A line that needs action ends with the commands that resolve it.

func TestStatusEndsAWaitingCheckoutWithTheSyncThatResolvesIt(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	r := &recorder{}

	e.must(r, e.app.Status(target("/w/app"), r))

	contains(t, r, "checkout /w/app (app): never synced; 2 changes wait: nerve sync --here")
}

func TestStatusOfAnotherProjectsCheckoutEndsWithAFullSync(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.listed("other", "/w/other")
	r := &recorder{}

	e.must(r, e.app.Status(app.ProjectRef{Dir: "/w/app", Project: "other"}, r))

	contains(t, r, "checkout /w/other (other): never synced; 2 changes wait: nerve sync")
}

func TestStatusEndsABrainWithWorkNotPushedWithPush(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.state = "1 commit not pushed"
	r := &recorder{}

	e.must(r, e.app.Status(target("/w/app"), r))

	contains(t, r, "brain    /brain (1 commit not pushed): nerve push")
}

func TestStatusDoesNotListWhatIsLinkedAsItShouldBe(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	r := &recorder{}

	e.must(r, e.app.Status(target("/w/app"), r))

	if strings.Contains(r.String(), "linked as") {
		t.Errorf("status lists links in order:\n%s", r)
	}
}

func TestDriftEndsWithTheCommandsThatKeepOrDropIt(t *testing.T) {
	for _, c := range []struct {
		name  string
		drift func(settings map[string]any)
		want  string
	}{
		{"a value changed", func(s map[string]any) { s["model"] = "sonnet" },
			`model is "sonnet" here, "opus" in the Brain: left as is: keep it with nerve setting set model sonnet, or drop all drift here with nerve sync --here -f`},
		{"a value that reads as JSON", func(s map[string]any) { s["model"] = "true" },
			`keep it with nerve setting set model '"true"', or`},
		{"a key removed", func(s map[string]any) { delete(s, "model") },
			`keep it with nerve setting rm model, or`},
		{"an item removed", func(s map[string]any) { s["permissions"] = map[string]any{"allow": []any{}} },
			`keep it with nerve setting rm permissions.allow=Read, or`},
	} {
		e := newEnv(t)
		e.listed("app", "/w/app").Settings = map[string]any{"model": "opus", "permissions": map[string]any{"allow": []any{"Read"}}}
		e.must(e.sync("/w/app", app.SyncOptions{}))
		c.drift(e.trees.settings["/w/app/.claude/settings.local.json"])

		r, _ := e.sync("/w/app", app.SyncOptions{Here: true})

		if !strings.Contains(r.String(), c.want) {
			t.Errorf("%s: events lack %q:\n%s", c.name, c.want, r)
		}
	}
}

func TestChangedSkillNamesItsFolderAtTheTip(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.tips["acme/up"] = pinB
	e.sources.skills["acme/up@"+pinB+"/grilling"] = domain.Skill{}
	e.sources.files["acme/up@"+pinB+"/grilling"] = []app.FileChange{{Path: "SKILL.md", Added: 1}}
	r := &recorder{}

	e.must(r, e.app.Update("/w", "", false, true, r))

	contains(t, r, "changed  skill grilling: SKILL.md +1 -0 (at /cache/acme/up/"+pinB+"/grilling)")
}
