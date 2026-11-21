package domain_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

const sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"

func TestValidateRefusesWhatAManifestCannotHold(t *testing.T) {
	for want, m := range map[string]domain.Manifest{
		"must be a release tag":            {Nerve: "latest"},
		"is not owner/name":                {Sources: []domain.Source{{Repo: "acme", SHA: sha, Skills: []string{"x"}}}},
		`"../x" is not owner/name`:         {Sources: []domain.Source{{Repo: "../x", SHA: sha, Skills: []string{"x"}}}},
		`"acme/." is not owner/name`:       {Sources: []domain.Source{{Repo: "acme/.", SHA: sha, Skills: []string{"x"}}}},
		"sha must be a full":               {Sources: []domain.Source{{Repo: "acme/up", SHA: "main", Skills: []string{"x"}}}},
		"acme/up: no skills":               {Sources: []domain.Source{{Repo: "acme/up", SHA: sha}}},
		`path "../x" must be a folder in`:  {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "../x", Skills: []string{"x"}}}},
		`path "/x" must be a folder in`:    {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "/x", Skills: []string{"x"}}}},
		`path "a/./b" must be a folder in`: {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Path: "a/./b", Skills: []string{"x"}}}},
		"needs an event and a command":     {Hooks: map[string]domain.Hook{"fmt": {Event: "Stop"}}},
		`hook "Fmt" must be lowercase`:     {Hooks: map[string]domain.Hook{"Fmt": {Event: "Stop", Command: "x"}}},
		`project "App" must be lowercase`:  {Projects: map[string]*domain.Project{"App": {}}},
		`skill "../x" must be lowercase`:   {Projects: map[string]*domain.Project{"app": {Skills: []string{"../x"}}}},
		`agent "a/b" must be lowercase`:    {Projects: map[string]*domain.Project{"app": {Agents: []string{"a/b"}}}},
		`hook "Fmt" must be lowercase w`:   {Projects: map[string]*domain.Project{"app": {Hooks: []string{"Fmt"}}}},
		`up: skill ".." must be`:           {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Skills: []string{".."}}}},
		`allowed-settings: "a..b" is not`:  {AllowedSettings: []string{"a..b"}},
		`acme/up is in two [[source]]`:     {Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Skills: []string{"x"}}, {Repo: "acme/up", SHA: sha, Skills: []string{"y"}}}},
		`"mcpServers.db.env" may hold s`:   {AllowedSettings: []string{"mcpServers.db.env"}},
		`"hooks.Stop" is written by nerve`: {AllowedSettings: []string{"hooks.Stop"}},
		`"enabledPlugins" is written by`:   {AllowedSettings: []string{"enabledPlugins"}},
	} {
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want %q", err, want)
		}
	}
	if err := (domain.Manifest{Nerve: "v1.2.3", Projects: map[string]*domain.Project{"app": {}}}).Validate(); err != nil {
		t.Errorf("a valid manifest is refused: %v", err)
	}
}

func TestAutosyncIsOnUnlessTheProjectOrTheBrainSaysOff(t *testing.T) {
	on, off := true, false
	for _, c := range []struct {
		brain, project *bool
		want           bool
	}{{nil, nil, true}, {&off, nil, false}, {&off, &on, true}, {nil, &off, false}} {
		m := domain.Manifest{Autosync: c.brain}
		if got := m.AutosyncFor(&domain.Project{Autosync: c.project}); got != c.want {
			t.Errorf("brain %v, project %v: autosync %v, want %v", c.brain, c.project, got, c.want)
		}
	}
}

func TestDescribeSumsUpAProject(t *testing.T) {
	m := domain.Manifest{}
	p := &domain.Project{Skills: []string{"a", "b"}, Hooks: []string{"h"}, Settings: map[string]any{"model": "opus"}}
	if got := m.Describe(p); got != "autosync on, 2 skills, 1 hook, 1 setting" {
		t.Errorf("described as %q", got)
	}
	off := false
	if got := m.Describe(&domain.Project{Autosync: &off}); got != "autosync off" {
		t.Errorf("a project with autosync off is described as %q", got)
	}
}

func TestDescribeCountsNoSettingInAnEmptyObject(t *testing.T) {
	p := &domain.Project{Settings: map[string]any{"x": map[string]any{}}}
	if got := (domain.Manifest{}).Describe(p); got != "autosync on" {
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

func TestRefusalsNameWhereEachIs(t *testing.T) {
	m := domain.Manifest{
		Common: map[string]any{"apiKeyHelper": "get-key", "statusLine": map[string]any{"command": "/home/someone/bin/line"}},
		Hooks:  map[string]domain.Hook{"notify": {Event: "Stop", Command: "notify me@example.com"}},
		Projects: map[string]*domain.Project{"app": {Settings: map[string]any{
			"permissions": map[string]any{"allow": []any{"WebFetch(domain:ghp_abcdefghijklmnopqrstuvwxyz0123)"}},
		}}},
	}
	var got []string
	for _, p := range m.Refusals() {
		got = append(got, p.Key+": "+p.Detail)
	}
	text := strings.Join(got, "\n")
	for _, want := range []string{
		"common.settings.apiKeyHelper: apiKeyHelper may hold secrets",
		"common.settings.statusLine.command: holds a home path",
		"hook.notify.command: holds an email address",
		"project.app.settings.permissions.allow: looks like a token",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}

func TestRefusalsLookEverywhereInTheManifest(t *testing.T) {
	m := domain.Manifest{
		Sources: []domain.Source{{Repo: "acme/up", SHA: sha, Skills: []string{"x"}}},
		Hooks:   map[string]domain.Hook{"guard": {Event: "PreToolUse", Matcher: "Bash(/home/bob/client-x/*)", Command: "x"}},
		Common:  map[string]any{"enabledPlugins": map[string]any{"tool@bob@example.com": true}},
		Projects: map[string]*domain.Project{"app": {
			Plugins: []string{"tool@ops.example.com"},
			Skills:  []string{"ok"},
		}},
	}
	var got []string
	for _, p := range m.Refusals() {
		got = append(got, p.Key+": "+p.Detail)
	}
	want := []string{
		"hook.guard.matcher: holds a home path: use {{brain}} or ~",
		"common.settings.enabledPlugins.(hidden): the key holds an email address: take it out of brain.toml, then nerve setting set it per project, which keeps it in the local overlay",
		"project.app.plugins: holds an email address",
	}
	if !slices.Equal(got, want) {
		t.Errorf("problems are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
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

	want := "common.settings.includeCoAuthoredBy: not an allowed setting: list it in allowed-settings, or run nerve setting rm --common on it, then nerve setting set per project, which keeps it in the local overlay\n" +
		"common.settings.statusLine.command: not an allowed setting: list it in allowed-settings, or run nerve setting rm --common on it, then nerve setting set per project, which keeps it in the local overlay\n" +
		"project.app.settings.permissions.additionalDirectories: not an allowed setting: list it in allowed-settings, or run nerve setting rm --project app on it, then nerve setting set --project app, which keeps it in the local overlay"
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

func TestSecretKeysAreRefusedAtAnyDepthEvenWhenAllowed(t *testing.T) {
	m := domain.Manifest{
		AllowedSettings: []string{"mcpServers", "pluginConfigs", "sandbox"},
		Common: map[string]any{
			"mcpServers":    map[string]any{"db": map[string]any{"env": map[string]any{"DB_PASSWORD": "x"}, "headers": map[string]any{"X-Api-Key": "y"}}},
			"pluginConfigs": map[string]any{"x": map[string]any{"options": map[string]any{"apiKey": "z", "dbPassword": "w"}}},
			"sandbox":       map[string]any{"enabled": true},
		},
	}

	got := refusals(m)
	for _, want := range []string{
		"common.settings.mcpServers.db.env.DB_PASSWORD: env may hold secrets",
		"common.settings.mcpServers.db.headers.X-Api-Key: headers may hold secrets",
		"common.settings.pluginConfigs.x.options.apiKey: apiKey may hold secrets",
		"common.settings.pluginConfigs.x.options.dbPassword: dbPassword may hold secrets",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "sandbox") {
		t.Errorf("refused an ordinary allowed setting:\n%s", got)
	}
}

func TestSettingRefusalLooksInsideAnObjectValue(t *testing.T) {
	m := domain.Manifest{AllowedSettings: []string{"mcpServers"}}
	v := map[string]any{"db": map[string]any{"env": map[string]any{"TOKEN": "x"}}}

	if why := m.SettingRefusal("mcpServers", v); why != "env may hold secrets" {
		t.Errorf("got %q", why)
	}
	if why := m.SettingRefusal("permissions", map[string]any{"allow": []any{"Read"}}); why != "" {
		t.Errorf("an object of allowed settings is refused: %s", why)
	}
}

func TestSensitiveGivesOneReasonWhateverTheMapOrder(t *testing.T) {
	v := map[string]any{"a": "me@example.com", "b": "ghp_abcdefghijklmnopqrstuvwxyz0123", "c": "/home/bob/x", "d": "/Users/bob/y"}
	want := domain.Sensitive("x", v)
	for range 50 {
		if got := domain.Sensitive("x", v); got != want {
			t.Fatalf("got %q, then %q", want, got)
		}
	}
}

func TestSensitiveFindsSecretKeysAtAnyDepth(t *testing.T) {
	if why := domain.Sensitive(domain.LeafKey([]string{"mcpServers", "db", "env", "HOST"}), "db.internal"); why != "env may hold secrets" {
		t.Errorf("got %q", why)
	}
}

func TestSensitiveLeavesOrdinaryValuesAlone(t *testing.T) {
	for _, v := range []any{"opus", "~/bin/line", "{{brain}}/hooks/x.sh", "gopls@claude-plugins-official", []any{"Bash(npm run test:*)"}} {
		if why := domain.Sensitive("x", v); why != "" {
			t.Errorf("%v flagged: %s", v, why)
		}
	}
}

func TestSensitiveLeavesAProjectFolderNamedHomeAlone(t *testing.T) {
	for _, v := range []string{"Edit(app/home/page.tsx)", "Read(./src/home/**)", "Read(src/Users/list.ts)", "Read(/root-tools/x)", "Bash(make bearer)"} {
		t.Run(v, func(t *testing.T) {
			if why := domain.Sensitive(domain.LeafKey([]string{"permissions", "allow"}), []any{v}); why != "" {
				t.Errorf("flagged: %s", why)
			}
		})
	}
}

func TestSensitiveFindsRootPathsKeysAndCredentials(t *testing.T) {
	for _, v := range []string{
		"Read(/root/.ssh/id_rsa)",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"-----BEGIN PRIVATE KEY-----",
		"postgres://u:pw@db:5432/app",
		"AIzaSyA1234567890abcdefghijklmnopqrstuv",
		"Bash(curl -H 'Authorization: Bearer abcDEF123456789')",
		"sk_live_4eC39HqLyjWDarjtT1zdp7dc",
		"hf_abcdefghijklmnopqrstuvwxyzABCDEF",
		"npm_abcdefghijklmnopqrstuvwxyz0123456789",
		"ya29.a0AfH6SMBabcdefghijklmnopqrstuvwxyz",
		"SG.abcdefghijklmnopqrstuv.abcdefghijklmnopqrstuvwxyz0123456789abcdefg",
		"x@/home/bob/market",
		"Read(/mnt/c/Users/bob/notes)",
		"Read(/var/home/bob/notes)",
	} {
		t.Run(v, func(t *testing.T) {
			if why := domain.Sensitive("x", v); why == "" {
				t.Errorf("not flagged")
			}
		})
	}
}

func TestSensitiveFindsSecretKeysInsideAValue(t *testing.T) {
	for _, v := range []any{
		map[string]any{"dbPassword": "hunter2"},
		[]any{map[string]any{"apiKey": "abc123"}},
		[]any{map[string]any{"cfg": map[string]any{"env": map[string]any{"X": "y"}}}},
	} {
		if why := domain.Sensitive("foo", v); why == "" {
			t.Errorf("%v not flagged", v)
		}
	}
}

func TestRefusalsLookInsideAListOfObjects(t *testing.T) {
	m := domain.Manifest{AllowedSettings: []string{"foo"}, Common: map[string]any{"foo": []any{map[string]any{"apiKey": "abc123"}}}}

	if got := refusals(m); !strings.Contains(got, "apiKey may hold secrets") {
		t.Errorf("not refused:\n%s", got)
	}
}

func TestSensitiveFindsLowercaseBearerTokensAndMoreSecretKeys(t *testing.T) {
	if why := domain.Sensitive("x", "Bash(curl -H 'authorization: bearer abcdefghijkl')"); why == "" {
		t.Errorf("a lowercase bearer token is not flagged")
	}
	for _, key := range []string{"privateKey", "accessKey", "sessionKey", "cookie", "authorization"} {
		if why := domain.Sensitive(key, "x"); why == "" {
			t.Errorf("key %s is not flagged", key)
		}
	}
}

func TestSensitiveFindsABasicAuthCredentialAndNotTheWord(t *testing.T) {
	if why := domain.Sensitive("x", "Bash(curl -H 'Authorization: Basic dXNlcjpwYXNz' x)"); why != "holds a credential" {
		t.Errorf("a basic auth credential: got %q", why)
	}
	for _, v := range []string{"Bash(make basic installation)", "Bash(echo basic step2)", "Basic usage"} {
		if why := domain.Sensitive("x", v); why != "" {
			t.Errorf("%q flagged: %s", v, why)
		}
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

func TestRefusalsRefuseANerveListHeldAsAnythingElse(t *testing.T) {
	m := domain.Manifest{
		Common:   map[string]any{"permissions": map[string]any{"deny": "Cron"}},
		Projects: map[string]*domain.Project{"app": {Settings: map[string]any{"permissions": map[string]any{"allow": map[string]any{"x": 1.0}}}}},
	}

	got := refusals(m)

	for _, want := range []string{
		`common.settings.permissions.deny: holds a list of text, such as ["Read"]: write it as one`,
		`project.app.settings.permissions.allow: holds a list of text, such as ["Read"]: write it as one`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}

func TestSensitiveFindsSecretsAndHomePathsInCommands(t *testing.T) {
	for _, v := range []string{
		"curl -d done https://hooks.slack.com/services/T01ABCDEF/B01ABCDEF/abcdefghijklmnopqrstuvwx",
		"curl https://discord.com/api/webhooks/123456789012345678/abcdefghijklmnop",
		"NTFY_TOKEN=tk_abc123 ./notify.sh",
		"./run.sh --password hunter2",
		"curl -H 'X-API-Key: abc123def456' https://x.io",
		"AWS_SECRET_ACCESS_KEY=abc ./x",
		"-----BEGIN PGP PRIVATE KEY BLOCK-----",
		"https://:pw@example.com",
		"echo done >/home/bob/log",
		"cc -I/home/bob/include x.c",
		"hi;/home/bob/x",
		"sh /root;",
	} {
		t.Run(v, func(t *testing.T) {
			if why := domain.Sensitive("x", v); why == "" {
				t.Errorf("not flagged")
			}
		})
	}
}

func TestSensitiveLeavesOrdinaryCommandsAlone(t *testing.T) {
	for _, v := range []string{
		"Bash(gh auth token:*)",
		"Bash(npm run test:*)",
		"NTFY_TOKEN=$NTFY_TOKEN ./notify.sh",
		`curl -H "Authorization: Bearer $GITHUB_TOKEN" https://x.io`,
		"{{brain}}/hooks/notify.sh --token-file ~/.config/x",
		"Co-Authored-By: Claude <noreply@anthropic.com>",
		"See Bearer authentication in the docs",
		"Bash(npm run secrets:check)",
		"Bash(make token:refresh)",
		"Edit(app/(marketing)/home/page.tsx)",
		"Edit(src/[id]/home/x.ts)",
	} {
		t.Run(v, func(t *testing.T) {
			if why := domain.Sensitive("x", v); why != "" {
				t.Errorf("flagged: %s", why)
			}
		})
	}
}

func TestSensitiveFindsSecretKeysWhateverTheirCase(t *testing.T) {
	for _, k := range []string{"Env", "HEADERS", "ApiKeyHelper"} {
		if why := domain.Sensitive(domain.LeafKey([]string{"mcpServers", "db", k, "X"}), "y"); why == "" {
			t.Errorf("%s not flagged", k)
		}
	}
}

func TestRefusalsAndOverridesOfCommonHideASensitiveKey(t *testing.T) {
	key := map[string]any{"ghp_abcdefghijklmnopqrstuvwxyz0123456789": 1.0}
	m := domain.Manifest{
		AllowedSettings: []string{"statusLine"},
		Common:          map[string]any{"statusLine": key},
	}

	got := refusals(m)
	same := domain.OverridesOfCommon(&domain.Project{Settings: map[string]any{"statusLine": key}}, m.Common)

	if strings.Contains(got, "ghp_") || !strings.Contains(got, "statusLine.(hidden): the key looks like a token: take it out of brain.toml") {
		t.Errorf("got:\n%s", got)
	}
	if !slices.Equal(same, []string{"statusLine.(hidden)"}) {
		t.Errorf("overrides of common are %v", same)
	}
}

func TestRefusalsSayHowToShareASettingNerveWrites(t *testing.T) {
	m := domain.Manifest{Common: map[string]any{"enabledPlugins": map[string]any{"x@y": true}}}

	if got := refusals(m); !strings.Contains(got, `common.settings.enabledPlugins.x@y: not an allowed setting: share a plugin with nerve plugin add, a hook with [hook.<name>]`) || strings.Contains(got, "allowed-settings") {
		t.Errorf("got:\n%s", got)
	}
}

func TestValidateRefusesOneRepoInTwoSourcesWhateverTheCase(t *testing.T) {
	m := domain.Manifest{Sources: []domain.Source{
		{Repo: "Acme/skills", SHA: sha, Skills: []string{"a"}},
		{Repo: "acme/skills", SHA: sha, Skills: []string{"b"}},
	}}

	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "is in two [[source]] tables") {
		t.Errorf("got %v", err)
	}
}
