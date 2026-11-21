package domain_test

import (
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

const (
	sha   = "5863806dd4e15c63174bee4e7515a4256792ecfa"
	other = "84b741e6aa9e2c3a8e8e2e4e44e3f0d0d4f3b1a2"
)

func TestValidateRefusesWhatAManifestCannotHold(t *testing.T) {
	for want, m := range map[string]domain.Manifest{
		"is not owner/name":                                    {Sources: []domain.Source{{Repo: "acme", SHA: sha, Skills: []string{"x"}}}},
		`"../x" is not owner/name`:                             {Sources: []domain.Source{{Repo: "../x", SHA: sha, Skills: []string{"x"}}}},
		`"acme/." is not owner/name`:                           {Sources: []domain.Source{{Repo: "acme/.", SHA: sha, Skills: []string{"x"}}}},
		"sha must be a full":                                   {Sources: []domain.Source{{Repo: "acme/up", SHA: "main", Skills: []string{"x"}}}},
		"acme/up: no skills":                                   {Sources: []domain.Source{{Repo: "acme/up", SHA: sha}}},
		`path "../x" must be a folder in`:                      {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "../x", Skills: []string{"x"}}}},
		`path "/x" must be a folder in`:                        {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "/x", Skills: []string{"x"}}}},
		`path "a/./b" must be a folder in`:                     {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "a/./b", Skills: []string{"x"}}}},
		"needs an event and a command":                         {Hooks: map[string]domain.Hook{"fmt": {Event: "Stop"}}},
		`hook "Fmt" must be lowercase`:                         {Hooks: map[string]domain.Hook{"Fmt": {Event: "Stop", Command: "x"}}},
		`project "App" must be lowercase`:                      {Projects: map[string]*domain.Project{"App": {}}},
		`skill "../x" must be lowercase`:                       {Projects: map[string]*domain.Project{"app": {Skills: []string{"../x"}}}},
		`agent "a/b" must be lowercase`:                        {Projects: map[string]*domain.Project{"app": {Agents: []string{"a/b"}}}},
		`hook "Fmt" must be lowercase w`:                       {Projects: map[string]*domain.Project{"app": {Hooks: []string{"Fmt"}}}},
		`up: skill ".." must be`:                               {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Skills: []string{".."}}}},
		`allowedSettings: "a..b" is not`:                       {AllowedSettings: []string{"a..b"}},
		`acme/up is in sources twice without a path`:           {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Skills: []string{"x"}}, {Repo: "Acme/Up", SHA: sha, Skills: []string{"y"}}}},
		`acme/up is in sources twice with path "skills/eng"`:   {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "skills/eng", Skills: []string{"x"}}, {Repo: "acme/up", SHA: sha, Path: "skills/eng", Skills: []string{"y"}}}},
		`acme/up is pinned at two commits`:                     {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "a", Skills: []string{"x"}}, {Repo: "acme/up", SHA: other, Path: "b", Skills: []string{"y"}}}},
		`acme/up: sha must be a full`:                          {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "a", Skills: []string{"x"}}, {Repo: "acme/up", SHA: "abc", Path: "b", Skills: []string{"y"}}}},
		`"hooks.Stop" is written by nerve`:                     {AllowedSettings: []string{"hooks.Stop"}},
		`"enabledPlugins" is written by`:                       {AllowedSettings: []string{"enabledPlugins"}},
		`app: skill "foo" is listed twice`:                     {Projects: map[string]*domain.Project{"app": {Skills: []string{"foo", "foo"}}}},
		`app: agent "rv" is listed twice`:                      {Projects: map[string]*domain.Project{"app": {Agents: []string{"rv", "rv"}}}},
		`app: hook "fmt" is listed twice`:                      {Projects: map[string]*domain.Project{"app": {Hooks: []string{"fmt", "fmt"}}}},
		`plugin "a@b" is listed twice`:                         {Projects: map[string]*domain.Project{"app": {Plugins: []string{"a@b", "a@b"}}}},
		`common.settings.x: settings cannot hold null`:         {Common: map[string]any{"x": nil}},
		`projects.app.settings.a.b: settings cannot hold null`: {Projects: map[string]*domain.Project{"app": {Settings: map[string]any{"a": map[string]any{"b": []any{nil}}}}}},
	} {
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want %q", err, want)
		}
	}
	if err := (domain.Manifest{Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "skills/eng", Skills: []string{"x"}}, {Repo: "acme/up", SHA: sha, Path: "skills/pro", Skills: []string{"y"}}}}).Validate(); err != nil {
		t.Errorf("a repo at two paths is refused: %v", err)
	}
	if err := (domain.Manifest{AllowedSettings: []string{"mcpServers.db.env"}, Hooks: map[string]domain.Hook{"fmt": {Event: "PostToolUse", Command: "x"}}, Projects: map[string]*domain.Project{"app": {}}}).Validate(); err != nil {
		t.Errorf("a valid manifest is refused: %v", err)
	}
}

func TestDescribeSumsUpAProject(t *testing.T) {
	m := domain.Manifest{}
	p := &domain.Project{Skills: []string{"a", "b"}, Hooks: []string{"h"}, Settings: map[string]any{"model": "opus"}}
	if got := m.Describe(p); got != "2 skills, 1 hook, 1 setting" {
		t.Errorf("described as %q", got)
	}
}

func TestValidateNamesTheFirstBadNameWhateverTheMapOrder(t *testing.T) {
	m := domain.Manifest{Hooks: map[string]domain.Hook{"C": {}, "A": {}, "B": {}, "D": {}}}
	for range 50 {
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), `hook "A"`) {
			t.Fatalf("got %v, want the error of hook A", err)
		}
	}
}

func TestDescribeCountsNoSettingInAnEmptyObject(t *testing.T) {
	p := &domain.Project{Settings: map[string]any{"x": map[string]any{}}}
	if got := (domain.Manifest{}).Describe(p); got != "lists nothing yet" {
		t.Errorf("described as %q", got)
	}
}

func TestSourceOfFindsTheSourceListingASkill(t *testing.T) {
	m := domain.Manifest{Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Skills: []string{"grilling"}}}}
	if src, ok := m.SourceOf("grilling"); !ok || src.Repo != "acme/up" {
		t.Errorf("got %v, %v", src, ok)
	}
	if _, ok := m.SourceOf("other"); ok {
		t.Errorf("found a source for a skill none lists")
	}
}

func TestRefusalsLeaveSecretsInAllowedSettingsToTheUser(t *testing.T) {
	m := domain.Manifest{
		AllowedSettings: []string{"env"},
		Common:          map[string]any{"env": map[string]any{"TOKEN": "ghp_abcdefghijklmnopqrstuvwxyz0123"}},
		Projects: map[string]*domain.Project{"app": {Settings: map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(curl -u admin:s3cret https://x)", "Read(/home/bob/x)"}},
		}}},
	}
	if got := refusals(m); got != "" {
		t.Errorf("refused:\n%s", got)
	}
}

func refusals(m domain.Manifest) string {
	var got []string
	for _, p := range m.Refusals() {
		got = append(got, p.Key+": "+p.Detail)
	}
	return strings.Join(got, "\n")
}

func TestRefusalsRefuseASettingNoListAllows(t *testing.T) {
	m := domain.Manifest{
		Common: map[string]any{"model": "opus", "attribution": map[string]any{"commit": ""}, "includeCoAuthoredBy": false, "statusLine": map[string]any{"command": "~/bin/line"}},
		Projects: map[string]*domain.Project{"app": {Settings: map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(make)"}, "additionalDirectories": []any{"../lib"}},
		}}},
	}

	want := "common.settings.includeCoAuthoredBy: not an allowed setting: run nerve allow add includeCoAuthoredBy, or run nerve setting rm --common on it, then nerve setting set per project, which keeps it in the local overlay\n" +
		"common.settings.statusLine.command: not an allowed setting: run nerve allow add statusLine.command, or run nerve setting rm --common on it, then nerve setting set per project, which keeps it in the local overlay\n" +
		"projects.app.settings.permissions.additionalDirectories: not an allowed setting: run nerve allow add permissions.additionalDirectories, or run nerve setting rm --project app on it, then nerve setting set --project app, which keeps it in the local overlay"
	if got := refusals(m); got != want {
		t.Errorf("problems are\n%s\nwant\n%s", got, want)
	}
}

func TestAllowedSettingsLetTheBrainHoldMoreKeys(t *testing.T) {
	m := domain.Manifest{
		AllowedSettings: []string{"statusLine", `extraKnownMarketplaces."acme.tools"`},
		Common:          map[string]any{"statusLine": map[string]any{"command": "~/bin/line"}, "extraKnownMarketplaces": map[string]any{"acme.tools": true}},
	}

	if got := refusals(m); got != "" {
		t.Errorf("refused:\n%s", got)
	}
}

func TestSettingRefusalLooksInsideAnObjectValue(t *testing.T) {
	m := domain.Manifest{AllowedSettings: []string{"permissions.allow"}}

	if why := m.SettingRefusal("permissions", map[string]any{"allow": []any{"Read"}, "additionalDirectories": []any{"x"}}); why != domain.NotAllowed {
		t.Errorf("got %q", why)
	}
	if why := m.SettingRefusal("permissions", map[string]any{"allow": []any{"Read"}}); why != "" {
		t.Errorf("an object of allowed settings is refused: %s", why)
	}
}

func TestWrittenByNerveNamesHooksAndPlugins(t *testing.T) {
	for key, want := range map[string]bool{"hooks": true, domain.LeafKey([]string{"enabledPlugins", "x@y"}): true, "model": false, "hooksDir": false} {
		if got := domain.WrittenByNerve(key); got != want {
			t.Errorf("%s: got %v", domain.Dotted(key), got)
		}
	}
}

func TestListGivesEachItemKindsList(t *testing.T) {
	p := &domain.Project{}
	for _, kind := range domain.ItemKinds {
		*p.List(kind) = append(*p.List(kind), kind)
	}
	if p.Skills[0] != "skill" || p.Agents[0] != "agent" || p.Hooks[0] != "hook" || p.Plugins[0] != "plugin" {
		t.Errorf("lists are %+v", p)
	}
}

func TestRefusalsRefuseASettingNerveAllowsHeldAsClaudeCodeRefusesIt(t *testing.T) {
	m := domain.Manifest{
		Common:   map[string]any{"permissions": map[string]any{"deny": "Cron"}},
		Projects: map[string]*domain.Project{"app": {Settings: map[string]any{"permissions": map[string]any{"allow": map[string]any{"x": 1.0}, "defaultMode": "x"}}}},
	}

	got := refusals(m)

	for _, want := range []string{
		`common.settings.permissions.deny: must be a list of text, such as ["Read"]`,
		`projects.app.settings.permissions.allow: must be a list of text, such as ["Read"]`,
		`projects.app.settings.permissions.defaultMode: must be one of`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}

func TestRefusalsSayHowToShareASettingNerveWrites(t *testing.T) {
	m := domain.Manifest{Common: map[string]any{"enabledPlugins": map[string]any{"x@y": true}}}

	if got := refusals(m); !strings.Contains(got, `common.settings.enabledPlugins.x@y: not an allowed setting: share a plugin with nerve plugin add, a hook in hooks`) || strings.Contains(got, "allowedSettings") {
		t.Errorf("got:\n%s", got)
	}
}

func TestValidateRefusesOneRepoInTwoSourcesWhateverTheCase(t *testing.T) {
	m := domain.Manifest{Sources: []domain.Source{
		{Repo: "Acme/skills", SHA: sha, Skills: []string{"a"}},
		{Repo: "acme/skills", SHA: sha, Skills: []string{"b"}},
	}}

	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "is in sources twice") {
		t.Errorf("got %v", err)
	}
}

func TestNerveAllowsAKeyOfItsOwnAndWhatIsUnderIt(t *testing.T) {
	for key, want := range map[string]bool{
		"effortLevel":          true,
		"attribution.commit":   true,
		"permissions.allow":    true,
		"permissions":          false, // wider than nerve's permissions keys
		"statusLine":           false,
		"effortLevelX":         false,
		`modelSettings."a.b"`:  true,
		"permissions.allowXYZ": false,
	} {
		path, err := domain.ParseKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if got := domain.NerveAllows(path); got != want {
			t.Errorf("%s: got %v, want %v", key, got, want)
		}
	}
}

func TestShellWordQuotesOnlyWhatAShellWouldChange(t *testing.T) {
	for in, want := range map[string]string{
		"statusLine.command":                  "statusLine.command",
		"mcpServers.db.headers.X-Api-Key":     "mcpServers.db.headers.X-Api-Key",
		`extraKnownMarketplaces."acme.tools"`: `'extraKnownMarketplaces."acme.tools"'`,
		`a."it's"`:                            `'a."it'\''s"'`,
		"a b":                                 "'a b'",
	} {
		if got := domain.ShellWord(in); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestARefusalNamesTheAllowCommandAsAShellTakesIt(t *testing.T) {
	m := domain.Manifest{Common: map[string]any{"extraKnownMarketplaces": map[string]any{"acme.tools": map[string]any{"source": "x"}}}}

	got := refusals(m)

	if want := `run nerve allow add 'extraKnownMarketplaces."acme.tools".source', or`; !strings.Contains(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}
}
