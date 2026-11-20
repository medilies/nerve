package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

// obj reads a JSON object, as settings files hold them.
func obj(t *testing.T, s string) map[string]any {
	t.Helper()
	if s == "" {
		return nil
	}
	var o map[string]any
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func text(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func keys(changes []domain.Change) []string {
	var k []string
	for _, c := range changes {
		k = append(k, c.Key)
	}
	return k
}

func TestReconcileKeepsDriftAndWritesTheRest(t *testing.T) {
	current := obj(t, `{"model": "sonnet", "permissions": {"allow": ["Bash(make)"]}}`) // changed here since
	last := obj(t, `{"model": "opus"}`)
	want := obj(t, `{"model": "opus", "effortLevel": "high"}`) // the Brain added a key

	rc := domain.Reconcile(current, last, want, false)

	if got := text(rc.Result); got != `{"effortLevel":"high","model":"sonnet","permissions":{"allow":["Bash(make)"]}}` {
		t.Errorf("result is %s, want the drift kept and the Brain's key written", got)
	}
	if got := text(rc.Next); got != `{"effortLevel":"high","model":"opus"}` {
		t.Errorf("next is %s, want what nerve means, so drift stays drift", got)
	}
	if got := keys(rc.Drift); !reflect.DeepEqual(got, []string{"model", "permissions.allow"}) {
		t.Errorf("drift is %v", got)
	}
	if d := rc.Drift[0]; d.Before != `"opus"` || d.After != `"sonnet"` {
		t.Errorf("model drifts from %s to %s, want from the Brain's to the file's", d.Before, d.After)
	}
	if got := rc.Changes; len(got) != 1 || got[0] != (domain.Change{Key: "effortLevel", Before: "unset", After: `"high"`}) || !rc.Write {
		t.Errorf("changes are %v (write %v), want effortLevel set", got, rc.Write)
	}
}

func TestReconcileDiscardOverwritesDrift(t *testing.T) {
	rc := domain.Reconcile(obj(t, `{"model": "sonnet"}`), obj(t, `{"model": "opus"}`), obj(t, `{"model": "opus"}`), true)

	if len(rc.Drift) > 0 || text(rc.Result) != `{"model":"opus"}` {
		t.Errorf("drift %v, result %s, want the Brain's value", rc.Drift, text(rc.Result))
	}
}

func TestReconcileRevokesWhatLeftTheBrain(t *testing.T) {
	written := obj(t, `{"model": "opus", "permissions": {"deny": ["Cron"]}}`)

	rc := domain.Reconcile(written, written, obj(t, `{"model": "opus"}`), false)

	if text(rc.Result) != `{"model":"opus"}` || !rc.Write || rc.Next == nil {
		t.Errorf("result is %s (write %v), want the deny list revoked", text(rc.Result), rc.Write)
	}
	if got := rc.Changes; len(got) != 1 || got[0].After != "unset" {
		t.Errorf("changes are %v, want permissions.deny unset", got)
	}
}

func TestReconcileLeavesAMatchingFileAlone(t *testing.T) {
	same := obj(t, `{"model": "opus"}`)
	if rc := domain.Reconcile(same, same, same, false); rc.Write || len(rc.Changes)+len(rc.Drift) > 0 {
		t.Errorf("a file that matches is rewritten: %+v", rc)
	}
	if rc := domain.Reconcile(nil, nil, map[string]any{}, false); rc.Write || rc.Next != nil {
		t.Errorf("an empty file is created: %+v", rc)
	}
	if rc := domain.Reconcile(nil, nil, same, false); !rc.Write {
		t.Errorf("a missing file is not written")
	}
}

func TestStripTakesOnlyWhatNerveWroteUnchanged(t *testing.T) {
	current := obj(t, `{"model": "sonnet", "effortLevel": "high", "outputStyle": "terse"}`)
	last := obj(t, `{"model": "opus", "effortLevel": "high"}`)

	rest, changed := domain.Strip(current, last)

	if text(rest) != `{"model":"sonnet","outputStyle":"terse"}` || !changed {
		t.Errorf("left %s (changed %v), want the user's own and the drifted key", text(rest), changed)
	}
	if _, changed := domain.Strip(obj(t, `{"a": 1}`), nil); changed {
		t.Errorf("Strip took keys nerve never wrote")
	}
}

func TestImportSettingsRecordsWhatTheProjectNeedsBeyondCommon(t *testing.T) {
	common := obj(t, `{"model": "opus", "effortLevel": "high", "syncClaudeAiSkills": false, "permissions": {"deny": ["NotebookEdit"]}}`)
	existing := obj(t, `{
		"model": "opus",
		"effortLevel": "low",
		"permissions": {"deny": ["NotebookEdit", "Cron"]},
		"outputStyle": "terse",
		"env": {"TOKEN": "abc"},
		"statusLine": {"command": "/home/someone/bin/line"}
	}`)

	entry, overlay, fates := domain.ImportSettings(existing, common)

	if got := text(entry); got != `{"effortLevel":"low","outputStyle":"terse","permissions":{"deny":["Cron"]}}` {
		t.Errorf("entry is %s", got)
	}
	if got := text(overlay); got != `{"env":{"TOKEN":"abc"},"statusLine":{"command":"/home/someone/bin/line"}}` {
		t.Errorf("overlay is %s", got)
	}
	want := map[string]string{
		"model":              "already given by common",
		"effortLevel":        "recorded as an override of common",
		"permissions.deny":   "recorded as the items common lacks",
		"outputStyle":        "recorded in the project",
		"env.TOKEN":          "kept in the local overlay: env may hold secrets",
		"statusLine.command": "kept in the local overlay: holds a home path: use {{brain}} or ~",
		"syncClaudeAiSkills": "added by common at the next sync",
	}
	got := map[string]string{}
	for _, f := range fates {
		got[f.Key] = f.What
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fates are %v\nwant %v", got, want)
	}
}

func TestMergeLaysObjectsJoinsArraysAndLetsLaterValuesWin(t *testing.T) {
	got := domain.Merge(
		obj(t, `{"model": "opus", "permissions": {"deny": ["A", "B"], "ask": ["X"]}}`),
		obj(t, `{"model": "sonnet", "permissions": {"deny": ["B", "C"]}}`),
	)
	if text(got) != `{"model":"sonnet","permissions":{"ask":["X"],"deny":["A","B","C"]}}` {
		t.Errorf("merged %s", text(got))
	}
}

func TestWantLaysCommonProjectNerveAndOverlay(t *testing.T) {
	off := false
	m := domain.Manifest{
		Common: obj(t, `{"model": "opus", "permissions": {"deny": ["NotebookEdit"]}}`),
		Hooks:  map[string]domain.Hook{"fmt": {Event: "PostToolUse", Matcher: "Edit", Command: "{{brain}}/hooks/fmt.sh"}},
	}
	p := &domain.Project{
		Settings: obj(t, `{"model": "sonnet", "permissions": {"deny": ["Cron"]}}`),
		Hooks:    []string{"fmt"},
		Plugins:  []string{"gopls@claude-plugins-official"},
	}

	got := domain.Want(m, p, "nerve sync --here --hook", obj(t, `{"model": "haiku"}`), "/b")

	want := `{"enabledPlugins":{"gopls@claude-plugins-official":true},` +
		`"hooks":{"PostToolUse":[{"hooks":[{"command":"/b/hooks/fmt.sh","type":"command"}],"matcher":"Edit"}],` +
		`"SessionStart":[{"hooks":[{"command":"nerve sync --here --hook","type":"command"}]}]},` +
		`"model":"haiku","permissions":{"deny":["NotebookEdit","Cron"]}}`
	if text(got) != want {
		t.Errorf("want is\n%s\nnot\n%s", text(got), want)
	}

	p.Autosync = &off
	if hooks := domain.Want(m, p, "x", nil, "/b")["hooks"].(map[string]any); hooks["SessionStart"] != nil {
		t.Errorf("autosync off still adds its hook")
	}
}

func TestRemovePathTakesAKeyOrAnItemAndEmptyObjects(t *testing.T) {
	s := obj(t, `{"model": "opus", "permissions": {"deny": ["A", "B"]}}`)

	if !domain.RemovePath(s, []string{"permissions", "deny"}, "A", true) || text(s) != `{"model":"opus","permissions":{"deny":["B"]}}` {
		t.Errorf("removing an item left %s", text(s))
	}
	if !domain.RemovePath(s, []string{"permissions", "deny"}, "B", true) || text(s) != `{"model":"opus"}` {
		t.Errorf("removing the last item left %s, want the emptied objects gone", text(s))
	}
	if domain.RemovePath(s, []string{"model"}, "x", true) || domain.RemovePath(s, []string{"nope"}, nil, false) {
		t.Errorf("RemovePath removed what is not there")
	}
	if !domain.RemovePath(s, []string{"model"}, nil, false) || len(s) != 0 {
		t.Errorf("removing a key left %s", text(s))
	}
}

func TestSetPathMakesObjectsOnTheWay(t *testing.T) {
	if got := domain.SetPath(nil, []string{"a", "b"}, 1.0); text(got) != `{"a":{"b":1}}` {
		t.Errorf("set %s", text(got))
	}
}

func TestLeafPathsKeepDottedKeysWhole(t *testing.T) {
	l := domain.Leaves(obj(t, `{"enabledPlugins": {"x.y@z": true}}`))
	for k := range l {
		if domain.Dotted(k) != `enabledPlugins."x.y@z"` || len(l) != 1 {
			t.Errorf("leaf %q", k)
		}
	}
	if got := domain.Unflatten(l); text(got) != `{"enabledPlugins":{"x.y@z":true}}` {
		t.Errorf("unflattened %s", text(got))
	}
}

func TestOverridesOfCommonFindsRepeatedValues(t *testing.T) {
	p := &domain.Project{Settings: obj(t, `{"model": "opus", "effortLevel": "low"}`)}
	if got := domain.OverridesOfCommon(p, obj(t, `{"model": "opus", "effortLevel": "high"}`)); !reflect.DeepEqual(got, []string{"model"}) {
		t.Errorf("overrides are %v", got)
	}
}

func TestParseValueReadsJSONElseText(t *testing.T) {
	for in, want := range map[string]string{`["a"]`: `["a"]`, `true`: `true`, `30`: `30`, `opus`: `"opus"`} {
		if got := text(domain.ParseValue(in)); got != want {
			t.Errorf("%s reads as %s, want %s", in, got, want)
		}
	}
}

func TestParseKeyReadsWhatDottedWrites(t *testing.T) {
	for _, path := range [][]string{{"model"}, {"permissions", "deny"}, {"enabledPlugins", "x@y.z"}, {"a=b", "c"}} {
		shown := domain.Dotted(domain.LeafKey(path))
		got, err := domain.ParseKey(shown)
		if err != nil || !reflect.DeepEqual(got, path) {
			t.Errorf("%v shows as %s, which reads as %v (%v)", path, shown, got, err)
		}
	}
}

func TestParseKeyRefusesWhatIsNotAKey(t *testing.T) {
	for _, key := range []string{"", ".model", "model.", "a..b", `a."b`, `a"b".c`, `"a"b`, `""`} {
		if path, err := domain.ParseKey(key); err == nil {
			t.Errorf("%q reads as %v", key, path)
		}
	}
}

func TestCutItemSplitsAtTheFirstEqualsOutsideQuotes(t *testing.T) {
	for arg, want := range map[string][3]any{
		"permissions.deny=Bash(x=1)": {"permissions.deny", "Bash(x=1)", true},
		`env."A=B"=1`:                {`env."A=B"`, "1", true},
		"model":                      {"model", "", false},
	} {
		key, value, ok := domain.CutItem(arg)
		if key != want[0] || value != want[1] || ok != want[2] {
			t.Errorf("%s cuts to %q, %q, %v", arg, key, value, ok)
		}
	}
}

func TestReconcileTakesAListItemByItem(t *testing.T) {
	last := obj(t, `{"permissions": {"allow": ["Read"], "deny": ["Cron", "Old"]}}`)
	current := obj(t, `{"permissions": {"allow": ["Read", "Bash(make)"], "deny": ["Old"]}}`) // an approval, and a deny removed
	want := obj(t, `{"permissions": {"allow": ["Read", "Edit"], "deny": ["Cron"]}}`)         // the Brain adds Edit and revokes Old

	rc := domain.Reconcile(current, last, want, false)

	if got := text(rc.Result); got != `{"permissions":{"allow":["Read","Bash(make)","Edit"],"deny":[]}}` {
		t.Errorf("result is %s, want the approval and the removal kept, Edit added and Old revoked", got)
	}
	if got := text(rc.Next); got != `{"permissions":{"allow":["Read","Edit"],"deny":["Cron"]}}` {
		t.Errorf("next is %s, want what nerve means, so drift stays drift", got)
	}
	wantDrift := []domain.Change{
		{Key: "permissions.allow", Item: true, Before: "unset", After: `"Bash(make)"`},
		{Key: "permissions.deny", Item: true, Before: `"Cron"`, After: "unset"},
	}
	if !reflect.DeepEqual(rc.Drift, wantDrift) {
		t.Errorf("drift is %+v", rc.Drift)
	}
}

func TestReconcileDiscardDropsOnlyWhatDrifted(t *testing.T) {
	last := obj(t, `{"permissions": {"allow": ["Read"]}}`)
	current := obj(t, `{"permissions": {"allow": ["Read", "Bash(make)"]}}`)

	rc := domain.Reconcile(current, last, last, true)

	if text(rc.Result) != `{"permissions":{"allow":["Read"]}}` || len(rc.Drift) > 0 {
		t.Errorf("result %s, drift %v", text(rc.Result), rc.Drift)
	}
}

func TestReconcileLeavesAListTheUserEmptiedOrDeleted(t *testing.T) {
	last := obj(t, `{"permissions": {"deny": ["Cron"]}}`)
	for current, want := range map[string]string{
		`{"permissions": {"deny": []}}`: `{"permissions":{"deny":[]}}`,
		`{}`:                            `{}`,
	} {
		rc := domain.Reconcile(obj(t, current), last, last, false)
		if text(rc.Result) != want || len(rc.Drift) != 1 || rc.Write {
			t.Errorf("%s became %s (write %v), drift %v", current, text(rc.Result), rc.Write, rc.Drift)
		}
	}
}

func TestReconcileHidesSensitiveValues(t *testing.T) {
	rc := domain.Reconcile(nil, nil, obj(t, `{"env": {"TOKEN": "sk-abc"}, "permissions": {"allow": ["Read(/home/bob/x)"]}}`), false)

	for _, c := range rc.Changes {
		if c.After != domain.Hidden {
			t.Errorf("%s shows as %s", c.Key, c.After)
		}
	}
}

func TestStripTakesNervesItemsFromAListTheUserAddedTo(t *testing.T) {
	current := obj(t, `{"permissions": {"allow": ["Read", "Bash(make)"], "deny": ["Cron"]}}`)
	last := obj(t, `{"permissions": {"allow": ["Read"], "deny": ["Cron"]}}`)

	rest, changed := domain.Strip(current, last)

	if text(rest) != `{"permissions":{"allow":["Bash(make)"]}}` || !changed {
		t.Errorf("left %s (changed %v), want only the user's item", text(rest), changed)
	}
}

func TestAddPathAddsOneItemToAList(t *testing.T) {
	s, added, err := domain.AddPath(nil, []string{"permissions", "allow"}, "Read")
	if err != nil || !added || text(s) != `{"permissions":{"allow":["Read"]}}` {
		t.Errorf("made %s (%v, %v)", text(s), added, err)
	}
	if s, added, _ = domain.AddPath(s, []string{"permissions", "allow"}, "Edit"); !added || text(s) != `{"permissions":{"allow":["Read","Edit"]}}` {
		t.Errorf("appended to %s", text(s))
	}
	if _, added, err = domain.AddPath(s, []string{"permissions", "allow"}, "Read"); added || err != nil {
		t.Errorf("added an item already there (%v)", err)
	}
	for _, path := range [][]string{{"model"}, {"model", "x"}} {
		if _, _, err := domain.AddPath(obj(t, `{"model": "opus"}`), path, "x"); err == nil {
			t.Errorf("added to %v, which is not a list", path)
		}
	}
}

func TestHasNullFindsNullAnywhere(t *testing.T) {
	for _, v := range []any{nil, []any{"a", nil}, map[string]any{"a": map[string]any{"b": nil}}} {
		if !domain.HasNull(v) {
			t.Errorf("%v has no null", v)
		}
	}
	if domain.HasNull(map[string]any{"a": []any{1.0, "x"}}) {
		t.Errorf("found a null in a value without one")
	}
}
