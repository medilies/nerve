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

func TestSyncRefusesATreeWhoseSettingsGitSeesOrShares(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.listed("team", "/w/team")
	e.trees.unignored["/w/app"] = true
	e.trees.tracked["/w/team"] = true

	r, err := e.sync("/w", app.SyncOptions{})

	if err == nil || err.Error() != "2 of 2 checkouts did not sync" {
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

func TestDeletingALinkDropsItFromTheProject(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	e.ownSkill("tdd")
	p.Skills = []string{"go-stack", "tdd"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	delete(e.trees.links, "/w/app/.claude/skills/go-stack")
	delete(e.trees.links, "/w/app/.claude/skills/nerve")

	r := e.must(e.sync("/w/app", app.SyncOptions{}))

	if got := e.project("app").Skills; !reflect.DeepEqual(got, []string{"tdd"}) || e.brain.saves != 1 {
		t.Errorf("skills are %v after %d saves, want go-stack written back out", got, e.brain.saves)
	}
	if _, ok := e.trees.links["/w/app/.claude/skills/go-stack"]; ok {
		t.Errorf("the deleted link came back")
	}
	if _, ok := e.trees.links["/w/app/.claude/skills/nerve"]; !ok {
		t.Errorf("nerve's own skill was not linked again")
	}
	contains(t, r, "dropped  skill go-stack from project app in brain.toml, since its link was deleted: commit it")
}

func TestSyncLinksUnderTheOwnersPrefixWhereTheTeamHasTheName(t *testing.T) {
	e := newEnv(t)
	p := e.listed("app", "/w/app")
	e.ownSkill("go-stack")
	p.Skills = []string{"go-stack"}
	e.trees.files["/w/app/.claude/skills/go-stack"] = true

	e.must(e.sync("/w/app", app.SyncOptions{}))

	if got := e.trees.links["/w/app/.claude/skills/medilies-go-stack"]; got != "/brain/skills/go-stack" {
		t.Errorf("prefixed link points to %q", got)
	}
	if !e.trees.files["/w/app/.claude/skills/go-stack"] {
		t.Errorf("the team's skill was touched")
	}

	e.brain.owner = ""
	e.trees.files["/w/other/.claude/skills/go-stack"] = true
	e.listed("other", "/w/other").Skills = []string{"go-stack"}
	r, _ := e.sync("/w/other", app.SyncOptions{Here: true})
	contains(t, r, "problem  /w/other: the team has .claude/skills/go-stack: the Brain has no origin remote")
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
	contains(t, r, "cleaned  /w/app: project app left the Brain")
}

func TestSyncRefusesSensitiveValuesInTheBrain(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app").Settings = map[string]any{"env": map[string]any{"TOKEN": "x"}}
	e.brain.m.Common = map[string]any{"statusLine": map[string]any{"command": "/home/bob/bin/line"}}
	e.brain.m.Hooks = map[string]domain.Hook{"guard": {Event: "PreToolUse", Matcher: "Bash(/Users/bob/*)", Command: "x"}}

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || !strings.Contains(err.Error(), "3 values in brain.toml are sensitive") || len(e.trees.links) > 0 {
		t.Errorf("got %v, links %v", err, e.trees.links)
	}
	contains(t, r,
		"problem  brain.toml: project.app.settings.env.TOKEN: env may hold secrets",
		"problem  brain.toml: common.settings.statusLine.command: holds a home path",
		"problem  brain.toml: hook.guard.matcher: holds a home path")
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

func TestSyncReportsAFailedCleanAndStillSavesWhatChanged(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.listed("gone", "/w/gone")
	e.must(e.sync("/w", app.SyncOptions{}))
	delete(e.brain.m.Projects, "gone")
	e.trees.damaged["/w/gone"] = true                       // its clean fails
	delete(e.trees.links, "/w/app/.claude/skills/go-stack") // a write-back in the other checkout

	r, err := e.sync("/w", app.SyncOptions{})

	if err == nil || !strings.Contains(err.Error(), "1 of 2 checkouts did not sync") {
		t.Errorf("got %v", err)
	}
	contains(t, r, "problem  /w/gone: info/exclude: nerve's block is damaged", "dropped  skill go-stack from project app")
	if strings.Contains(r.String(), "gone at") {
		t.Errorf("the failed checkout is reported as synced:\n%s", r)
	}
	if len(e.project("app").Skills) != 0 {
		t.Errorf("the write-back was not saved: skills are %v", e.project("app").Skills)
	}
	if len(e.machine.machine.Checkouts) != 2 {
		t.Errorf("the checkout that failed to clean was dropped: %v", e.machine.machine.Checkouts)
	}
}

func TestSyncReportsAFailedSaveOfTheBrain(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	delete(e.trees.links, "/w/app/.claude/skills/go-stack")
	e.brain.saveErr = errors.New("brain.toml: read-only")

	_, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || !strings.Contains(err.Error(), "brain.toml: read-only") {
		t.Errorf("got %v", err)
	}
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
	e.trees.files["/w/app/.claude/skills/go-stack"] = true
	e.brain.owner = "" // the team's go-stack needs a prefix, and the Brain has no remote to name one

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
	e.trees.files["/w/app-feature/.claude/skills/go-stack"] = true // the team's, on this branch

	e.must(e.sync("/w/app-feature", app.SyncOptions{Here: true}))

	want := []string{".claude/skills/go-stack", ".claude/skills/medilies-go-stack", ".claude/skills/nerve"}
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

	e.must(e.sync("/w/app", app.SyncOptions{}))

	if got := e.trees.links["/w/app/.claude/skills/go-stack"]; got != "/home/me/my-go-stack" {
		t.Errorf("the user's link points to %s", got)
	}
	if got := e.trees.links["/w/app/.claude/skills/medilies-go-stack"]; got != "/brain/skills/go-stack" {
		t.Errorf("nerve's go-stack is linked to %q, want it under the owner's prefix", got)
	}
	p.Skills = nil
	e.must(e.sync("/w/app", app.SyncOptions{}))
	if _, ok := e.trees.links["/w/app/.claude/skills/go-stack"]; !ok {
		t.Errorf("the user's link was removed")
	}
}

func TestADanglingLinkIsNotADeletedOne(t *testing.T) {
	e := newEnv(t)
	e.ownSkill("go-stack")
	e.listed("app", "/w/app").Skills = []string{"go-stack"}
	e.must(e.sync("/w/app", app.SyncOptions{}))
	e.trees.dangling["/brain/skills/go-stack"] = true // say, the Brain is checked out on another branch

	e.must(e.sync("/w/app", app.SyncOptions{}))

	if len(e.project("app").Skills) != 1 {
		t.Errorf("a dangling link was written back")
	}
}

func TestSyncTakesAnApprovalAsDriftOfOneItem(t *testing.T) {
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
	contains(t, r, `drift    permissions.allow has "Bash(make)" here, not in the Brain: left as is`)
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

func TestADamagedExcludeBlockFailsTheTreeWithoutCallingItSynced(t *testing.T) {
	e := newEnv(t)
	e.listed("app", "/w/app")
	e.trees.damaged["/w/app"] = true

	r, err := e.sync("/w/app", app.SyncOptions{})

	if err == nil || strings.Contains(r.String(), "synced") {
		t.Errorf("got %v:\n%s", err, r)
	}
	contains(t, r, "problem  /w/app: info/exclude: nerve's block is damaged")
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
	e.trees.files["/w/feature/.claude/skills/go-stack"] = true

	e.must(e.sync("/w/feature", app.SyncOptions{Here: true}))

	want := []string{".claude/skills/go-stack", ".claude/skills/medilies-go-stack", ".claude/skills/nerve"}
	if got := e.trees.excludes["/w/feature"]; !reflect.DeepEqual(got, want) {
		t.Errorf("excluded %v, want the links of both trees", got)
	}
}
