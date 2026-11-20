package app_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

const (
	pinA = "5863806dd4e15c63174bee4e7515a4256792ecfa"
	pinB = "84b741eca41d19d321feeace5a8ea435e19e3c1b"
)

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSyncLinksTheProjectsSkillsAgentsAndNerve(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	e.brain.agents["reviewer"] = true
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.skills["acme/up@"+pinA+"/grilling"] = domain.Skill{Name: "grilling", Dir: "/cache/sources/acme/up/" + pinA + "/skills/grilling"}
	p.Skills, p.Agents = []string{"go-stack", "grilling"}, []string{"reviewer"}

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	for link, target := range map[string]string{
		"/w/app/.claude/skills/nerve":       "/cache/nerve/skill/nerve",
		"/w/app/.claude/skills/go-stack":    "/brain/skills/go-stack",
		"/w/app/.claude/skills/grilling":    "/cache/sources/acme/up/" + pinA + "/skills/grilling",
		"/w/app/.claude/agents/reviewer.md": "/brain/agents/reviewer.md",
	} {
		if got := e.ws.links[link]; got != target {
			t.Errorf("%s links to %q, want %s", link, got, target)
		}
	}
	if got := e.ws.excludes["/w/app"]; !reflect.DeepEqual(got, []string{".claude/agents/reviewer.md", ".claude/skills/go-stack", ".claude/skills/grilling", ".claude/skills/nerve"}) {
		t.Errorf("excluded %v", got)
	}
	if st := e.machine.states["/w/app"]; st.Project != "app" || st.Commit != e.brain.head || len(st.Links) != 4 {
		t.Errorf("state is %+v", st)
	}
	if e.brain.pulls != 1 || e.self.upgrades != 1 {
		t.Errorf("pulled %d and upgraded %d times, want once each", e.brain.pulls, e.self.upgrades)
	}
	contains(t, r, "synced   app at aaaaaaa")
}

func TestSyncWritesTheProjectsSettings(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	e.brain.m.Common = map[string]any{"permissions": map[string]any{"deny": []any{"NotebookEdit"}}}
	p.Settings = map[string]any{"model": "opus"}
	e.machine.overlays["app"] = map[string]any{"env": map[string]any{"TOKEN": "x"}}

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	got := jsonText(e.settings("/w/app"))
	want := `{"env":{"TOKEN":"x"},"hooks":{"SessionStart":[{"hooks":[{"command":"nerve sync --here --hook","type":"command"}]}]},"model":"opus","permissions":{"deny":["NotebookEdit"]}}`
	if got != want {
		t.Errorf("settings are\n%s\nwant\n%s", got, want)
	}
	contains(t, r, `set      model = "opus"`)
}

func TestSyncRefusesATreeWhoseSettingsGitDoesNotIgnore(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.ws.unignored["/w/app"] = true

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || err.Error() != "1 of 1 checkouts did not sync" {
		t.Fatalf("got %v", err)
	}
	contains(t, r, "problem  /w/app: git does not ignore .claude/settings.local.json")
	if len(e.ws.links)+len(e.ws.settings) > 0 {
		t.Errorf("wrote in a refused tree: %v %v", e.ws.links, e.ws.settings)
	}
}

func TestSyncKeepsDriftAndFailsUnlessRunByAutosync(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	p.Settings = map[string]any{"model": "opus"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.ws.settings["/w/app/.claude/settings.local.json"]["model"] = "sonnet"

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || !strings.Contains(err.Error(), "settings drifted in 1 of 1 checkouts") {
		t.Errorf("got %v, want drift to fail a sync by hand", err)
	}
	contains(t, r, `drift    model is "sonnet" here, "opus" in the Brain: left as is`)
	if e.settings("/w/app")["model"] != "sonnet" {
		t.Errorf("drift was overwritten")
	}
	if _, err := e.sync("/w/app", app.SyncOptions{Hook: true}); err != nil {
		t.Errorf("autosync failed on drift: %v", err)
	}
	e.must(e.sync("/w/app", app.SyncOptions{Discard: true}))
	if e.settings("/w/app")["model"] != "opus" {
		t.Errorf("--discard kept the drift")
	}
}

func TestSyncRemovesWhatLeftTheProject(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	p.Skills = []string{"go-stack"}
	p.Settings = map[string]any{"model": "opus"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	p.Skills, p.Settings = nil, nil

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if _, ok := e.ws.links["/w/app/.claude/skills/go-stack"]; ok {
		t.Errorf("the dropped skill is still linked")
	}
	if _, ok := e.settings("/w/app")["model"]; ok {
		t.Errorf("the dropped setting is still there")
	}
	contains(t, r, "removed  /w/app/.claude/skills/go-stack", `unset    model (was "opus")`)
}

func TestSyncSaysUpToDateWhenNothingChanged(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.must(e.sync("/w/app", app.SyncOptions{}))

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if r.String() != "app is up to date at aaaaaaa" {
		t.Errorf("events are:\n%s", r)
	}
}

func TestDeletingALinkDropsItFromTheProject(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	e.ownSkill("tdd")
	p.Skills = []string{"go-stack", "tdd"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	delete(e.ws.links, "/w/app/.claude/skills/go-stack")
	delete(e.ws.links, "/w/app/.claude/skills/nerve")

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if got := e.project("app").Skills; !reflect.DeepEqual(got, []string{"tdd"}) || e.brain.saves != 1 {
		t.Errorf("skills are %v after %d saves, want go-stack written back out", got, e.brain.saves)
	}
	if _, ok := e.ws.links["/w/app/.claude/skills/go-stack"]; ok {
		t.Errorf("the deleted link came back")
	}
	if _, ok := e.ws.links["/w/app/.claude/skills/nerve"]; !ok {
		t.Errorf("nerve's own skill was not linked again")
	}
	contains(t, r, "dropped  skill go-stack from project app in brain.toml, since its link was deleted: commit it")
}

func TestSyncLinksUnderTheOwnersPrefixWhereTheTeamHasTheName(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	p.Skills = []string{"go-stack"}
	e.ws.files["/w/app/.claude/skills/go-stack"] = true

	e.must(e.sync("/w/app", app.SyncOptions{}))

	if got := e.ws.links["/w/app/.claude/skills/medilies-go-stack"]; got != "/brain/skills/go-stack" {
		t.Errorf("prefixed link points to %q", got)
	}
	if !e.ws.files["/w/app/.claude/skills/go-stack"] {
		t.Errorf("the team's skill was touched")
	}

	e.brain.owner = ""
	e.ws.files["/w/other/.claude/skills/go-stack"] = true
	e.listed("other", "/w/other").Skills = []string{"go-stack"}
	r, _ := e.sync("/w/other", app.SyncOptions{Here: true})
	contains(t, r, "problem  /w/other: the team has .claude/skills/go-stack: the Brain has no origin remote")
}

func TestSyncHereSyncsOnlyTheTreeAroundTheDirectory(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.listed("other", "/w/other")
	e.ws.trees = append([]domain.Tree{{Top: "/w/app-feature", Main: "/w/app"}}, e.ws.trees...)
	e.ws.dirs["/w/app-feature"] = true

	e.must(e.sync("/w/app-feature/src", app.SyncOptions{Here: true}))

	if _, ok := e.ws.links["/w/app-feature/.claude/skills/nerve"]; !ok {
		t.Errorf("the worktree was not synced")
	}
	if len(e.ws.links) != 1 {
		t.Errorf("other trees were synced: %v", e.ws.links)
	}
}

func TestSyncCleansACheckoutWhoseProjectLeftTheBrain(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	p.Settings = map[string]any{"model": "opus"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.ws.settings["/w/app/.claude/settings.local.json"]["outputStyle"] = "terse" // the user's own
	delete(e.brain.m.Projects, "app")                                            // another machine removed it

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if len(e.ws.links) > 0 || e.ws.excludes["/w/app"] != nil {
		t.Errorf("links %v or exclude block %v are left", e.ws.links, e.ws.excludes["/w/app"])
	}
	if got := jsonText(e.settings("/w/app")); got != `{"outputStyle":"terse"}` {
		t.Errorf("settings are %s, want only the user's own", got)
	}
	if len(e.machine.machine.Checkouts) != 0 || len(e.machine.states) != 0 {
		t.Errorf("the checkout %v or its state is still recorded", e.machine.machine.Checkouts)
	}
	contains(t, r, "cleaned  /w/app: project app left the Brain")
}

func TestSyncRefusesSensitiveValuesInTheBrain(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"env": map[string]any{"TOKEN": "x"}}

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || !strings.Contains(err.Error(), "1 values in brain.toml are sensitive") || len(e.ws.links) > 0 {
		t.Errorf("got %v, links %v", err, e.ws.links)
	}
	contains(t, r, "problem  brain.toml: project.app.settings.env.TOKEN: env may hold secrets")
}

func TestSyncSkipsThePullOfARunThatReplacedAnOlderNerve(t *testing.T) {
	e := newEnv(t)
	e.must(e.sync("/w", app.SyncOptions{SkipPull: true}))
	if e.brain.pulls != 0 {
		t.Errorf("pulled again")
	}
}

func TestClaudeAIProjectReportsEachZipOnceAndSkillsToDelete(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("research")
	e.ownSkill("notes")
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI, Skills: []string{"research", "notes"}}

	r := e.must(e.sync("/w", app.SyncOptions{}))

	contains(t, r,
		"upload   /cache/nerve/claude.ai/claude-ai-work/research.zip to claude.ai account claude-ai-work (skill research)",
		"upload   /cache/nerve/claude.ai/claude-ai-work/notes.zip")
	if r := e.must(e.sync("/w", app.SyncOptions{})); strings.Contains(r.String(), "upload") {
		t.Errorf("unchanged skills were reported again:\n%s", r)
	}

	s := e.brain.own["research"]
	s.Meta["description"] = "Researches better."
	e.brain.m.Projects["claude-ai-work"].Skills = []string{"research"}
	r = e.must(e.sync("/w", app.SyncOptions{}))

	contains(t, r, "upload   /cache/nerve/claude.ai/claude-ai-work/research.zip", "delete   skill notes from claude.ai account claude-ai-work")
	if e.zips.Has("claude-ai-work", "notes") {
		t.Errorf("the dropped skill's zip is kept")
	}
}

func TestClaudeAIProjectRefusesWhatAZipCannotHold(t *testing.T) {
	e := newEnv(t)
	s := e.ownSkill("research")
	s.Meta["model"] = "opus"
	e.ownSkill("notes")
	e.zips.folderLinks["/brain/skills/notes"] = "refs"
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI, Skills: []string{"research", "notes"}}

	r := e.must(e.sync("/w", app.SyncOptions{}))

	contains(t, r,
		"problem  brain.toml: project.claude-ai-work: research: frontmatter keys claude.ai rejects: model",
		"problem  brain.toml: project.claude-ai-work: notes: refs links to a folder or to nothing")
	if len(e.zips.written) > 0 {
		t.Errorf("zipped %v", e.zips.written)
	}
}
