package app_test

import (
	"encoding/json"
	"errors"
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
		if got := e.trees.links[link]; got != target {
			t.Errorf("%s links to %q, want %s", link, got, target)
		}
	}
	if got := e.trees.excludes["/w/app"]; !reflect.DeepEqual(got, []string{".claude/agents/reviewer.md", ".claude/skills/go-stack", ".claude/skills/grilling", ".claude/skills/nerve"}) {
		t.Errorf("excluded %v", got)
	}
	if st := e.machine.states["/w/app"]; st.Project != "app" || st.Commit != e.brain.head || len(st.Links) != 4 {
		t.Errorf("state is %+v", st)
	}
	if e.brain.pulls != 1 {
		t.Errorf("pulled %d times, want once", e.brain.pulls)
	}
	if seen := e.trees.gitSees("/w/app"); seen != nil {
		t.Errorf("git sees %v", seen)
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

func TestSyncRefusesATreeWhoseSettingsGitSeesOrShares(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.listed("team", "/w/team")
	e.trees.unignored["/w/app"] = true
	e.trees.tracked["/w/team"] = true

	r, err := e.sync("/w", app.SyncOptions{})

	if err == nil || err.Error() != "2 of 2 working trees did not sync" {
		t.Fatalf("got %v", err)
	}
	contains(t, r,
		"problem  /w/app: git does not ignore .claude/settings.local.json",
		"problem  /w/team: git tracks .claude/settings.local.json")
	if len(e.trees.links)+len(e.trees.settings) > 0 {
		t.Errorf("wrote in a refused tree: %v %v", e.trees.links, e.trees.settings)
	}
}

func TestSyncKeepsDriftAndFailsUnlessRunByAutosync(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	p.Settings = map[string]any{"model": "opus"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.trees.settings["/w/app/.claude/settings.local.json"]["model"] = "sonnet"

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || !strings.Contains(err.Error(), "settings drifted in 1 of 1 working trees") {
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

	if _, ok := e.trees.links["/w/app/.claude/skills/go-stack"]; ok {
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

func TestADeletedLinkIsLinkedAgainAndTheBrainKeepsItsItem(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.ownSkill("tdd")
	e.listed("app", "/w/app").Skills = []string{"go-stack", "tdd"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	delete(e.trees.links, "/w/app/.claude/skills/go-stack")

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if got := e.project("app").Skills; len(got) != 2 || e.brain.saves != 0 {
		t.Errorf("skills are %v after %d saves, want both kept", got, e.brain.saves)
	}
	contains(t, r, "linked   /w/app/.claude/skills/go-stack -> /brain/skills/go-stack")
}

func TestANameTheTeamHasIsNotLinkedAndSyncSaysSo(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.ownSkill("tdd")
	e.listed("app", "/w/app").Skills = []string{"go-stack", "tdd"}
	e.trees.mkdirs("/w/app/.claude/skills/go-stack")

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if _, ok := e.trees.links["/w/app/.claude/skills/tdd"]; !ok || len(e.trees.links) != 2 || !e.trees.dirs["/w/app/.claude/skills/go-stack"] {
		t.Errorf("links are %v, want nerve and tdd, and the team's go-stack untouched", e.trees.links)
	}
	contains(t, r, "problem  /w/app/.claude/skills/go-stack: the team has it, so skill go-stack is not linked here: rename yours in the Brain to use it")
}

func TestSyncHereSyncsOnlyTheTreeAroundTheDirectory(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.listed("other", "/w/other")
	e.trees.trees = append([]domain.Tree{{Top: "/w/app-feature", Main: "/w/app", Repo: "/w/app/.git"}}, e.trees.trees...)
	e.trees.dirs["/w/app-feature"] = true

	e.must(e.sync("/w/app-feature/src", app.SyncOptions{Here: true}))

	if _, ok := e.trees.links["/w/app-feature/.claude/skills/nerve"]; !ok {
		t.Errorf("the worktree was not synced")
	}
	if len(e.trees.links) != 1 {
		t.Errorf("other trees were synced: %v", e.trees.links)
	}
}

func TestSyncCleansACheckoutWhoseProjectLeftTheBrain(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	p.Settings = map[string]any{"model": "opus"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.trees.settings["/w/app/.claude/settings.local.json"]["outputStyle"] = "terse" // the user's own
	delete(e.brain.m.Projects, "app")                                               // another machine removed it

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if len(e.trees.links) > 0 || e.trees.excludes["/w/app"] != nil {
		t.Errorf("links %v or exclude block %v are left", e.trees.links, e.trees.excludes["/w/app"])
	}
	if got := jsonText(e.settings("/w/app")); got != `{"outputStyle":"terse"}` {
		t.Errorf("settings are %s, want only the user's own", got)
	}
	if len(e.machine.machine.Checkouts) != 0 || len(e.machine.states) != 0 {
		t.Errorf("the checkout %v or its state is still recorded", e.machine.machine.Checkouts)
	}
	contains(t, r, "cleaned  /w/app: project app left the Brain", "unlisted /w/app, a checkout of app: project app left the Brain")
}

func TestSyncCleansACheckoutWhoseProjectBecameAClaudeAIAccount(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.brain.m.Projects["app"].Surface = domain.ClaudeAI

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	contains(t, r,
		"cleaned  /w/app: project app is now a claude.ai account",
		"unlisted /w/app, a checkout of app: project app is now a claude.ai account")
}

func TestSyncHereUnlistsACheckoutWhoseProjectLeftTheBrain(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	delete(e.brain.m.Projects, "app")

	r := e.must(e.sync("/w/app", app.SyncOptions{Here: true}))

	if len(e.machine.machine.Checkouts) != 0 {
		t.Errorf("the cleaned checkout is still listed: %v", e.machine.machine.Checkouts)
	}
	contains(t, r, "cleaned  /w/app: project app left the Brain")
}

func TestSyncRefusesSensitiveValuesInTheBrain(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"env": map[string]any{"TOKEN": "x"}}
	e.brain.m.Common = map[string]any{"statusLine": map[string]any{"command": "/home/bob/bin/line"}}
	e.brain.m.Hooks = map[string]domain.Hook{"guard": {Event: "PreToolUse", Matcher: "Bash(/Users/bob/*)", Command: "x"}}

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || !strings.Contains(err.Error(), "3 values in brain.toml are refused") || len(e.trees.links) > 0 {
		t.Errorf("got %v, links %v", err, e.trees.links)
	}
	contains(t, r,
		"problem  brain.toml: project.app.settings.env.TOKEN: env may hold secrets",
		"problem  brain.toml: common.settings.statusLine.command: holds a home path",
		"problem  brain.toml: hook.guard.matcher: holds a home path")
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
	e.zips.refused["/brain/skills/notes"] = "refs links out of the skill, which a zip cannot hold"
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI, Skills: []string{"research", "notes"}}

	r := e.must(e.sync("/w", app.SyncOptions{}))

	contains(t, r,
		"problem  brain.toml: project.claude-ai-work: research: frontmatter keys claude.ai rejects: model",
		"problem  brain.toml: project.claude-ai-work: notes: refs links out of the skill, which a zip cannot hold")
	if len(e.zips.written) > 0 {
		t.Errorf("zipped %v", e.zips.written)
	}
}

func TestSyncReportsAFailedCleanAndStillSyncsTheOthers(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.listed("gone", "/w/gone")
	e.must(e.sync("/w", app.SyncOptions{}))
	delete(e.brain.m.Projects, "gone")
	e.trees.damaged["/w/gone"] = true                       // its clean fails
	delete(e.trees.links, "/w/app/.claude/skills/go-stack") // a link to make again in the other checkout

	r, err := e.sync("/w", app.SyncOptions{})

	if err == nil || !strings.Contains(err.Error(), "1 of 2 working trees did not sync") {
		t.Errorf("got %v", err)
	}
	contains(t, r, "problem  /w/gone: info/exclude: nerve's block is damaged", "linked   /w/app/.claude/skills/go-stack -> /brain/skills/go-stack")
	if strings.Contains(r.String(), "gone at") {
		t.Errorf("the failed checkout is reported as synced:\n%s", r)
	}
	if _, ok := e.machine.states["/w/app"].Links[".claude/skills/go-stack"]; !ok {
		t.Errorf("the other checkout's state was not saved: %+v", e.machine.states["/w/app"])
	}
	if len(e.machine.machine.Checkouts) != 2 {
		t.Errorf("the checkout that failed to clean was dropped: %v", e.machine.machine.Checkouts)
	}
}

func TestLinksAndSettingsGitCleanRemovedAreMadeAgain(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.ownSkill("py")
	e.listed("app", "/w/app").Skills = []string{"go-stack", "py"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	for p := range e.trees.links {
		delete(e.trees.links, p) // as git clean -fdx does
	}
	delete(e.trees.settings, "/w/app/.claude/settings.local.json")
	for _, f := range domain.TreeFolders {
		delete(e.trees.dirs, "/w/app/"+f)
	}

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if got := e.project("app").Skills; len(got) != 2 || e.brain.saves != 0 {
		t.Errorf("skills are %v after %d saves, want both kept", got, e.brain.saves)
	}
	if _, ok := e.trees.links["/w/app/.claude/skills/py"]; !ok {
		t.Errorf("the lost links were not made again")
	}
	if strings.Contains(r.String(), "drift") {
		t.Errorf("a settings file that is gone drifts:\n%s", r)
	}
}

func TestAFullSyncUnlistsACheckoutThatIsGone(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.listed("gone", "/w/gone")
	e.must(e.sync("/w", app.SyncOptions{}))
	delete(e.trees.dirs, "/w/gone")

	r := e.must(e.sync("/w", app.SyncOptions{}))

	if got := e.machine.machine.Checkouts; len(got) != 1 || got[0].Path != "/w/app" {
		t.Errorf("checkouts are %v", got)
	}
	if _, ok := e.machine.states["/w/gone"]; ok {
		t.Errorf("the state of the gone checkout is kept")
	}
	contains(t, r, "unlisted /w/gone, a checkout of gone that is gone")
	e.must(e.sync("/w", app.SyncOptions{}))
}

func TestAFullSyncReachesTheWorktreesSyncedBefore(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.worktree("/w/app-feature", "/w/app")
	e.must(e.sync("/w/app-feature", app.SyncOptions{Here: true}))
	e.ownSkill("tdd")
	e.project("app").Skills = []string{"tdd"}

	e.must(e.sync("/w", app.SyncOptions{}))

	if _, ok := e.trees.links["/w/app-feature/.claude/skills/tdd"]; !ok {
		t.Errorf("the worktree did not get the new skill: %v", e.trees.links)
	}
	if _, ok := e.trees.links["/w/app-feature/.claude/skills/go-stack"]; ok {
		t.Errorf("the worktree kept the skill the project dropped")
	}
	delete(e.brain.m.Projects, "app")

	r := e.must(e.sync("/w", app.SyncOptions{}))

	if len(e.trees.links) > 0 {
		t.Errorf("links are left: %v", e.trees.links)
	}
	contains(t, r, "cleaned  /w/app-feature: project app left the Brain")
	delete(e.trees.dirs, "/w/app-feature")
	e.must(e.sync("/w", app.SyncOptions{}))
	if len(e.machine.states) > 0 {
		t.Errorf("the state of a worktree that is gone is kept: %v", e.machine.states)
	}
}

func TestAStateThatCannotBeSavedFailsTheTree(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.machine.stateErr = errors.New("state: read-only")

	r, err := e.sync("/w/app", app.SyncOptions{Here: true})

	if err == nil || strings.Contains(r.String(), "synced") {
		t.Errorf("got %v:\n%s", err, r)
	}
	contains(t, r, "problem  /w/app: state: read-only")
}

func TestACleanThatCannotDropTheStateKeepsTheCheckoutListed(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	delete(e.brain.m.Projects, "app")
	e.machine.stateErr = errors.New("state: read-only")

	r, err := e.sync("/w/app", app.SyncOptions{Here: true})

	if err == nil || len(e.machine.machine.Checkouts) != 1 {
		t.Errorf("got %v, checkouts %v", err, e.machine.machine.Checkouts)
	}
	contains(t, r, "problem  /w/app: state: read-only")
}

func TestSyncFailsWhenThePullFails(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.pullErr = errors.New("pull /brain: diverged")

	_, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || err.Error() != "pull /brain: diverged" || len(e.trees.links) > 0 {
		t.Errorf("got %v, links %v", err, e.trees.links)
	}
}

func TestSyncKeepsLinksOutOfGitEvenWhenLinkingFailsPartWay(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("a-skill")
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"a-skill", "go-stack"}
	e.trees.linkErr = map[string]error{"/w/app/.claude/skills/go-stack": errors.New("read-only")}

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil {
		t.Fatalf("sync passed:\n%s", r)
	}
	if got := e.trees.excludes["/w/app"]; !reflect.DeepEqual(got, []string{".claude/skills/a-skill", ".claude/skills/nerve"}) {
		t.Errorf("excluded %v, want the links made before the failure", got)
	}
}

func TestWorktreesShareOneExcludeBlock(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.worktree("/w/app-feature", "/w/app")
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	e.trees.mkdirs("/w/app-feature/.claude/skills/go-stack") // the team's, on this branch

	e.must(e.sync("/w/app-feature", app.SyncOptions{Here: true}))

	want := []string{".claude/skills/go-stack", ".claude/skills/nerve"}
	if got := e.trees.excludes["/w/app"]; !reflect.DeepEqual(got, want) {
		t.Errorf("excluded %v, want the links of both trees", got)
	}
	e.must(&recorder{}, e.app.RemoveProject("app", &recorder{}))
	if got := e.trees.excludes["/w/app"]; got != nil {
		t.Errorf("excluded %v after both trees were cleaned", got)
	}
}

func TestCleaningOneWorktreeKeepsTheOthersLinksExcluded(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.worktree("/w/app-feature", "/w/app")
	e.ownSkill("go-stack")
	e.project("app").Skills = []string{"go-stack"}
	e.must(e.sync("/w/app-feature", app.SyncOptions{Here: true}))
	e.project("app").Skills = nil
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	delete(e.brain.m.Projects, "app")

	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))

	if got := e.trees.excludes["/w/app"]; !reflect.DeepEqual(got, []string{".claude/skills/go-stack", ".claude/skills/nerve"}) {
		t.Errorf("excluded %v, want the worktree's links kept", got)
	}
}

func TestSyncRefusesItemsTheBrainCannotGive(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	p.Hooks, p.Plugins = []string{"ghost"}, []string{"bad"}

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil {
		t.Fatal("sync passed")
	}
	contains(t, r, `problem  /w/app: brain.toml defines no hook "ghost": add a [hook.ghost] table; plugin "bad" must be <plugin>@<marketplace>`)
	if e.settings("/w/app") != nil || len(e.trees.links) > 0 {
		t.Errorf("wrote in a refused tree")
	}
}

func TestSyncLeavesALinkSomeonePointedElsewhere(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	p := e.listed("app", "/w/app")
	p.Skills = []string{"go-stack"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.trees.links["/w/app/.claude/skills/go-stack"] = "/home/me/my-go-stack"

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if strings.Contains(r.String(), "problem") {
		t.Errorf("reports the user's own link as a problem:\n%s", r)
	}

	if got := e.trees.links["/w/app/.claude/skills/go-stack"]; got != "/home/me/my-go-stack" || len(e.trees.links) != 2 {
		t.Errorf("links are %v, want the user's go-stack kept and no second one", e.trees.links)
	}
	p.Skills = nil
	e.must(e.sync("/w/app", app.SyncOptions{}))
	if _, ok := e.trees.links["/w/app/.claude/skills/go-stack"]; !ok {
		t.Errorf("the user's link was removed")
	}
}

func TestSyncTakesAnApprovalAsOnePersonalItem(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	p.Settings = map[string]any{"permissions": map[string]any{"allow": []any{"Read"}}}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	allow := e.settings("/w/app")["permissions"].(map[string]any)
	allow["allow"] = append(allow["allow"].([]any), "Bash(make)") // as Claude Code does on "don't ask again"
	p.Settings = map[string]any{"permissions": map[string]any{"allow": []any{"Read", "Edit"}}}

	r, err := e.sync("/w/app", app.SyncOptions{Hook: true})

	e.must(r, err)
	if got := jsonText(e.settings("/w/app")["permissions"]); got != `{"allow":["Read","Bash(make)","Edit"]}` {
		t.Errorf("permissions are %s, want the approval kept and Edit added", got)
	}
	contains(t, r, `personal permissions.allow has "Bash(make)": kept, the Brain does not set it`)
}

func TestSyncNeverReportsASecret(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.machine.overlays["app"] = map[string]any{"env": map[string]any{"TOKEN": "sk-abcdefghijklmnopqrstuvwxyz"}}

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if strings.Contains(r.String(), "sk-") {
		t.Errorf("the report shows the secret:\n%s", r)
	}
	contains(t, r, "set      env.TOKEN = (hidden)")
}

func TestClaudeAIProjectKeepsAZipWhoseSkillCannotBeBuiltNow(t *testing.T) {
	e := newEnv(t)
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: pinA, Skills: []string{"grilling"}}}
	e.sources.skills["acme/up@"+pinA+"/grilling"] = domain.Skill{Name: "grilling", Dir: "/cache/grilling", Meta: map[string]string{"name": "grilling"}}
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI, Skills: []string{"grilling"}}
	e.must(e.sync("/w", app.SyncOptions{}))
	delete(e.sources.skills, "acme/up@"+pinA+"/grilling") // say, the network is down

	r := e.must(e.sync("/w", app.SyncOptions{}))

	if strings.Contains(r.String(), "delete") {
		t.Errorf("a skill that failed to build is to be deleted:\n%s", r)
	}
	contains(t, r, `problem  brain.toml: project.claude-ai-work: acme/up@5863806: no skill "grilling"`)
}

func TestADamagedExcludeBlockRefusesTheTreeBeforeAnyLink(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.trees.damaged["/w/app"] = true

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || strings.Contains(r.String(), "synced") {
		t.Errorf("got %v:\n%s", err, r)
	}
	if len(e.trees.links) > 0 || e.trees.gitSees("/w/app") != nil {
		t.Errorf("a refused tree got links git can see: %v", e.trees.links)
	}
	contains(t, r, "problem  /w/app: info/exclude: nerve's block is damaged")
}

func TestClaudeAIBuildKeepsWhatItBuiltWhenAnotherSkillFails(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("research")
	e.ownSkill("notes")
	e.zips.refused["/brain/skills/notes"] = "x links to nothing, which a zip cannot hold"
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI, Skills: []string{"research", "notes"}}
	e.must(e.sync("/w", app.SyncOptions{}))

	r := e.must(e.sync("/w", app.SyncOptions{}))

	if strings.Contains(r.String(), "upload") {
		t.Errorf("a zip built before is reported again:\n%s", r)
	}
}

func TestWorktreesOfABareRepoShareItsExcludeBlock(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	for _, dir := range []string{"/w/main", "/w/feature"} { // each its own checkout, one git folder
		e.trees.trees = append(e.trees.trees, domain.Tree{Top: dir, Main: dir, Repo: "/w/repo.git"})
		e.trees.dirs[dir] = true
		e.machine.machine.Checkouts = append(e.machine.machine.Checkouts, domain.Checkout{Path: dir, Project: "app"})
	}
	e.brain.m.Projects["app"] = &domain.Project{Skills: []string{"go-stack"}}
	e.must(e.sync("/w/main", app.SyncOptions{Here: true}))
	e.trees.mkdirs("/w/feature/.claude/skills/go-stack")

	e.must(e.sync("/w/feature", app.SyncOptions{Here: true}))

	want := []string{".claude/skills/go-stack", ".claude/skills/nerve"}
	if got := e.trees.excludes["/w/feature"]; !reflect.DeepEqual(got, want) {
		t.Errorf("excluded %v, want the links of both trees", got)
	}
}

func lostWorktree(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.ownSkill("go-stack")
	p := e.listed("app", "/w/app")
	p.Skills, p.Settings = []string{"go-stack"}, map[string]any{"model": "opus"}
	e.worktree("/w/app-feature", "/w/app")
	e.trees.dirs["/w/app/.git"] = true
	e.must(e.sync("/w/app-feature", app.SyncOptions{Here: true}))
	e.trees.lost["/w/app-feature"] = true // say, git worktree prune
	delete(e.brain.m.Projects, "app")
	return e
}

func TestAWorktreeGitCannotLocateIsCleanedWhenItsProjectLeaves(t *testing.T) {
	e := lostWorktree(t)

	r, err := e.sync("/w", app.SyncOptions{})

	if _, kept := e.machine.states["/w/app-feature"]; err != nil || kept || e.settings("/w/app-feature") != nil {
		t.Errorf("got %v, state kept %v, settings %v:\n%s", err, kept, e.settings("/w/app-feature"), r)
	}
}

func TestAWorktreeGitCannotLocateIsKeptWhileItsProjectIsInTheBrain(t *testing.T) {
	e := lostWorktree(t)
	e.brain.m.Projects["app"] = &domain.Project{Skills: []string{"go-stack"}}

	r, err := e.sync("/w", app.SyncOptions{})

	if _, kept := e.machine.states["/w/app-feature"]; err == nil || !kept || e.settings("/w/app-feature") == nil {
		t.Errorf("got %v, state kept %v, settings %v:\n%s", err, kept, e.settings("/w/app-feature"), r)
	}
	contains(t, r, "problem  /w/app-feature: git cannot locate it, so nerve leaves it as it is: not in a git repo: fatal: not a git repository: /nowhere")
}

func TestCleaningAWorktreeGitCannotLocateReportsWhatItLeftInInfoExclude(t *testing.T) {
	e := lostWorktree(t)

	r, _ := e.sync("/w", app.SyncOptions{})

	if !strings.Contains(r.String(), "info/exclude") {
		t.Errorf("no event says what is left in info/exclude:\n%s", r)
	}
}

func TestSyncWhoseSettingsCannotBeWrittenReportsNothingSet(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"model": "opus"}
	e.trees.writeErr = errors.New("settings.local.json: read-only")

	r, err := e.sync("/w/app", app.SyncOptions{Here: true})

	if err == nil || strings.Contains(r.String(), "set      model") {
		t.Errorf("got %v, and a setting reported set that was not written:\n%s", err, r)
	}
}

func TestAFullSyncWhoseMachineFileCannotBeSavedReportsNothingUnlisted(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.listed("gone", "/w/gone")
	e.must(e.sync("/w", app.SyncOptions{}))
	delete(e.trees.dirs, "/w/gone")
	e.machine.saveErr = errors.New("config.toml: read-only")

	r, err := e.sync("/w", app.SyncOptions{})

	if err == nil || strings.Contains(r.String(), "unlisted") {
		t.Errorf("got %v, and a checkout reported unlisted that the machine file still lists:\n%s", err, r)
	}
}

func TestSyncReportsAReleaseOtherThanThePin(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.Nerve = "v0.4.0"

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	contains(t, r, "problem  brain.toml: nerve: nerve v0.3.0 runs, but the Brain pins v0.4.0: install v0.4.0")
}

func TestSyncSaysNothingOfThePinWhenADevelopmentBuildRuns(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.brain.m.Nerve = "v0.4.0"
	e.self.version = "(devel)"

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if strings.Contains(r.String(), "pins") {
		t.Errorf("a development build is told to install the pin:\n%s", r)
	}
}

func TestSyncRefusesATreeWhereAFolderOrFileNerveWritesIsALink(t *testing.T) {
	for _, rel := range []string{".claude/skills", ".claude/agents", ".claude/settings.local.json"} {
		t.Run(rel, func(t *testing.T) {
			e := newEnv(t)
			e.listed("app", "/w/app")
			e.trees.links["/w/app/"+rel] = "/home/me/elsewhere"

			_, err := e.sync("/w/app", app.SyncOptions{})

			if err == nil || len(e.trees.links) != 1 || e.settings("/w/app") != nil {
				t.Errorf("got %v, links %v, settings %v: want the tree left as it is", err, e.trees.links, e.settings("/w/app"))
			}
		})
	}
}

func TestCleaningATreeWhoseSettingsFileIsALinkLeavesItAndSaysSo(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.must(e.sync("/w/app", app.SyncOptions{}))
	written := e.settings("/w/app")
	delete(e.trees.settings, "/w/app/.claude/settings.local.json")
	e.trees.links["/w/app/.claude/settings.local.json"] = "/home/me/dot/settings.json"
	e.trees.settings["/home/me/dot/settings.json"] = written

	err := e.app.RemoveProject("app", &recorder{})

	if err == nil || !strings.Contains(err.Error(), ".claude/settings.local.json is a link") {
		t.Errorf("got %v, want the link named", err)
	}
	if _, kept := e.trees.links["/w/app/.claude/settings.local.json"]; !kept || e.project("app") == nil {
		t.Errorf("project rm went on through a link")
	}
}

func TestASourceSkillThatCannotBeFetchedCostsOnlyThatSkill(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.brain.m.Sources = []domain.Source{{Repo: "acme/up", SHA: strings.Repeat("1", 40), Skills: []string{"grilling"}}}
	p := e.listed("app", "/w/app")
	p.Skills, p.Settings = []string{"grilling", "go-stack"}, map[string]any{"model": "opus"}

	r, _ := e.sync("/w/app", app.SyncOptions{Here: true})

	if _, ok := e.trees.links["/w/app/.claude/skills/go-stack"]; !ok || e.settings("/w/app")["model"] != "opus" {
		t.Errorf("links %v, settings %v: want the rest of the project synced", e.trees.links, e.settings("/w/app"))
	}
	contains(t, r, `problem  /w/app/.claude/skills/grilling: not linked: acme/up@1111111: no skill "grilling"`)
}

func TestASourceSkillThatCannotBeFetchedKeepsItsLink(t *testing.T) {
	e := newEnv(t)
	src := domain.Source{Repo: "acme/up", SHA: strings.Repeat("1", 40), Skills: []string{"grilling"}}
	e.brain.m.Sources = []domain.Source{src}
	e.sources.skills["acme/up@"+src.SHA+"/grilling"] = domain.Skill{Name: "grilling", Dir: "/cache/acme/up/grilling"}
	e.listed("app", "/w/app").Skills = []string{"grilling"}
	e.must(e.sync("/w/app", app.SyncOptions{Here: true}))
	delete(e.sources.skills, "acme/up@"+src.SHA+"/grilling") // say, offline with a cold cache

	r, _ := e.sync("/w/app", app.SyncOptions{Here: true})

	if got := e.trees.links["/w/app/.claude/skills/grilling"]; got != "/cache/acme/up/grilling" {
		t.Errorf("link is %q, want it kept", got)
	}
	contains(t, r, `problem  /w/app/.claude/skills/grilling: left as last linked: acme/up@1111111: no skill "grilling"`)
}

func TestASyncThatChangesNothingIsUpToDateDespiteAStandingProblem(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.trees.mkdirs("/w/app/.claude/skills/go-stack")
	e.must(e.sync("/w/app", app.SyncOptions{}))

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	contains(t, r, "app is up to date at aaaaaaa")
}

func TestALinkThatAlreadyPointsWhereNerveWouldIsAdoptedQuietly(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.trees.links["/w/app/.claude/skills/nerve"] = "/cache/nerve/skill/nerve" // left by a checkout that moved

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if strings.Contains(r.String(), "linked") {
		t.Errorf("reports a link nerve did not make:\n%s", r)
	}
	if _, ok := e.machine.states["/w/app"].Links[".claude/skills/nerve"]; !ok {
		t.Errorf("the link was not adopted")
	}
}

func TestASettingTheBrainDoesNotSetIsPersonalAndReportedOnce(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"permissions": map[string]any{"allow": []any{"Read"}}}
	e.trees.settings["/w/app/.claude/settings.local.json"] = map[string]any{"outputStyle": "terse", "env": map[string]any{"TOKEN": "x"}}

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err != nil || e.settings("/w/app")["outputStyle"] != "terse" {
		t.Fatalf("got %v, settings %v: want the sync to pass and keep them", err, e.settings("/w/app"))
	}
	contains(t, r,
		`personal env.TOKEN = (hidden): kept, the Brain does not set it`,
		`personal outputStyle = "terse": kept, the Brain does not set it`)
	e.trees.settings["/w/app/.claude/settings.local.json"]["permissions"].(map[string]any)["allow"] = []any{"Read", "Bash(make)"} // an approval

	r = e.must(e.sync("/w/app", app.SyncOptions{}))

	if got := r.String(); strings.Contains(got, "outputStyle") || !strings.Contains(got, `personal permissions.allow has "Bash(make)": kept, the Brain does not set it`) {
		t.Errorf("want only the new approval reported:\n%s", got)
	}
}

func TestClaudeAIZipThatCannotBeRemovedIsReported(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("notes")
	e.brain.m.Projects["claude-ai-work"] = &domain.Project{Surface: domain.ClaudeAI, Skills: []string{"notes"}}
	e.must(e.sync("/w", app.SyncOptions{}))
	e.brain.m.Projects["claude-ai-work"].Skills = nil
	e.zips.removeErr = errors.New("notes.zip: permission denied")

	r, _ := e.sync("/w", app.SyncOptions{})

	contains(t, r, "problem  /cache/nerve/claude.ai/claude-ai-work/notes.zip: cannot remove it: notes.zip: permission denied")
}
