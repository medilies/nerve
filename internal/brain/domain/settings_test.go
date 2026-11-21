package domain_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

// obj reads a JSON object, as settings files hold them.
func obj(t *testing.T, s string) map[string]any {
	if t != nil {
		t.Helper()
	}
	if s == "" {
		return nil
	}
	o, ok := domain.ParseValue(s).(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object", s)
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
	last := obj(t, `{"model": "opus", "permissions": {"allow": ["Read"]}}`)
	want := obj(t, `{"model": "opus", "effortLevel": "high", "permissions": {"allow": ["Read"]}}`) // the Brain added a key

	rc := domain.Reconcile(current, last, want, false)

	if got := text(rc.Result); got != `{"effortLevel":"high","model":"sonnet","permissions":{"allow":["Bash(make)"]}}` {
		t.Errorf("result is %s, want the drift kept and the Brain's key written", got)
	}
	if got := text(rc.Next); got != `{"effortLevel":"high","model":"opus","permissions":{"allow":["Read"]}}` {
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

func TestReconcileKeepsPersonalSettingsAndSaysWhich(t *testing.T) {
	current := obj(t, `{"outputStyle": "terse", "permissions": {"allow": ["Read", "Bash(make)"]}}`)
	written := obj(t, `{"permissions": {"allow": ["Read"]}}`)

	for _, discard := range []bool{false, true} {
		rc := domain.Reconcile(current, written, written, discard)

		if len(rc.Drift) > 0 || rc.Write || text(rc.Next) != text(written) {
			t.Errorf("discard %v: drift %v, write %v, next %s: want the personal settings left out of both", discard, rc.Drift, rc.Write, text(rc.Next))
		}
		if got := text(rc.Personal); got != `{"outputStyle":"terse","permissions":{"allow":["Bash(make)"]}}` {
			t.Errorf("discard %v: personal is %s", discard, got)
		}
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

func TestReconcileTakesAnEmptyObjectForNoSetting(t *testing.T) {
	current := obj(t, `{"permissions": {}}`)

	rc := domain.Reconcile(current, nil, obj(t, `{"permissions": {"deny": ["X"]}}`), false)

	if len(rc.Drift) > 0 || text(rc.Result) != `{"permissions":{"deny":["X"]}}` {
		t.Errorf("drift %v, result %s", rc.Drift, text(rc.Result))
	}
	if text(current) != `{"permissions":{}}` {
		t.Errorf("Reconcile changed the file it read: %s", text(current))
	}
	if rc := domain.Reconcile(current, nil, nil, false); len(rc.Drift)+len(rc.Changes) > 0 || rc.Write {
		t.Errorf("an empty object the Brain lacks drifts: %+v", rc)
	}
}

// TestReconcileProperties checks Reconcile on every file, last write and
// Brain made of a few values, as a property: it is idempotent, discard
// writes what a fresh file gets plus the personal settings, and Strip
// leaves only drift and personal settings.
func TestReconcileProperties(t *testing.T) {
	checkReconcile(t,
		[]string{"", `1`, `2`, `["p"]`, `["p","q"]`, `{}`, `{"c":1}`},
		[]string{"", `["p"]`, `["q","p"]`})
}

// TestReconcilePropertiesWithNestedKeys checks the same properties on
// settings nested a level or two, as permissions and hooks are.
func TestReconcilePropertiesWithNestedKeys(t *testing.T) {
	checkReconcile(t,
		[]string{"", `1`, `{"c":1}`, `{"c":2}`, `{"c":["p"]}`, `{"c":["q","p"]}`, `{"c":{"d":1}}`, `{"c":{}}`, `{"c":1,"e":["p"]}`},
		[]string{"", `{"c":["p"]}`, `{"c":{"d":["p","q"]}}`})
}

func TestReconcileDiscardWritesNoEmptyObjectAFreshFileLacks(t *testing.T) {
	want := obj(t, `{"a": {"c": {}}}`)

	discard := domain.Reconcile(obj(t, `{"a": 1}`), obj(t, `{"a": {"c": 1}}`), want, true)

	if fresh := domain.Reconcile(nil, nil, want, false); text(discard.Result) != text(fresh.Result) {
		t.Errorf("discard wrote %s, a fresh file gets %s", text(discard.Result), text(fresh.Result))
	}
}

// checkReconcile checks the properties of TestReconcileProperties on every
// triple of layers that hold a, b, both or neither, with a and b taking each
// of their values.
func checkReconcile(t *testing.T, aValues, bValues []string) {
	t.Helper()
	var as, bs []any
	for _, v := range aValues {
		as = append(as, value(t, v))
	}
	for _, v := range bValues {
		bs = append(bs, value(t, v))
	}
	var layers []map[string]any
	for _, a := range as {
		for _, b := range bs {
			l := map[string]any{}
			if a != nil {
				l["a"] = a
			}
			if b != nil {
				l["b"] = b
			}
			layers = append(layers, l)
		}
	}
	layers = append(layers, nil) // no file at all
	for _, current := range layers {
		for _, last := range layers {
			for _, want := range layers {
				name := text(current) + " " + text(last) + " " + text(want)
				rc := domain.Reconcile(copyObj(current), last, want, false)

				written := current
				if rc.Write {
					written = rc.Result
				}
				again := domain.Reconcile(copyObj(written), rc.Next, want, false)
				if again.Write || len(again.Changes) > 0 || !reflect.DeepEqual(again.Drift, rc.Drift) || text(again.Next) != text(rc.Next) || text(again.Personal) != text(rc.Personal) {
					t.Fatalf("%s: not idempotent: %+v, then %+v", name, rc, again)
				}

				discard := domain.Reconcile(copyObj(current), last, want, true)
				fresh := domain.Reconcile(nil, nil, want, false)
				if wantDiscard := domain.Merge(fresh.Result, discard.Personal); len(discard.Drift) > 0 || text(unordered(discard.Result)) != text(unordered(wantDiscard)) {
					t.Fatalf("%s: discard wrote %s, want %s", name, text(discard.Result), text(wantDiscard))
				}

				rest, _ := domain.Strip(written, rc.Next)
				personal := domain.Leaves(rc.Personal)
				for k := range domain.Leaves(rest) {
					if _, mine := personal[k]; emptyLeaf(rest, k) || mine {
						continue
					}
					if !slices.ContainsFunc(rc.Drift, func(c domain.Change) bool {
						return c.Key == domain.Dotted(k) || strings.HasPrefix(domain.Dotted(k), c.Key+".")
					}) {
						t.Fatalf("%s: Strip left %s, which is not drift: %+v", name, domain.Dotted(k), rc.Drift)
					}
				}
			}
		}
	}
}

// value reads JSON as a settings file holds it, or nil for "".
func value(t *testing.T, s string) any {
	t.Helper()
	if s == "" {
		return nil
	}
	return domain.ParseValue(s)
}

// unordered sorts every list in v, since the order of a list's items is
// the file's.
func unordered(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			out[k] = unordered(x)
		}
		return out
	case []any:
		out := slices.Clone(t)
		slices.SortFunc(out, func(a, b any) int { return strings.Compare(text(a), text(b)) })
		return out
	}
	return v
}

func copyObj(o map[string]any) map[string]any {
	if o == nil {
		return nil
	}
	return obj(nil, text(o))
}

func emptyLeaf(o map[string]any, k string) bool {
	v, ok := domain.Leaves(o)[k].(map[string]any)
	return ok && len(v) == 0
}

func TestReconcileTakesAValueAndAnObjectAtOneKeyAsAWhole(t *testing.T) {
	rc := domain.Reconcile(obj(t, `{"a": 1}`), nil, obj(t, `{"a": {"b": 2}}`), false)

	if text(rc.Result) != `{"a":1}` || len(rc.Drift) != 1 || rc.Drift[0] != (domain.Change{Key: "a", Before: `{"b":2}`, After: "1"}) {
		t.Errorf("result %s, drift %+v, want the file's value kept as drift of a", text(rc.Result), rc.Drift)
	}
	if rc := domain.Reconcile(obj(t, `{"a": 1}`), nil, obj(t, `{"a": {"b": 2}}`), true); text(rc.Result) != `{"a":{"b":2}}` {
		t.Errorf("discard gave %s", text(rc.Result))
	}
	written := obj(t, `{"a": {"b": 2}}`)
	rc = domain.Reconcile(written, written, obj(t, `{"a": 1}`), false)
	if text(rc.Result) != `{"a":1}` || len(rc.Drift) > 0 || len(rc.Changes) != 1 || rc.Changes[0].Key != "a" {
		t.Errorf("result %s, drift %v, changes %+v", text(rc.Result), rc.Drift, rc.Changes)
	}
}

func TestReconcileHidesIdentifyingKeys(t *testing.T) {
	rc := domain.Reconcile(nil, nil, obj(t, `{"enabledPlugins": {"tool@bob@example.com": true}}`), false)

	if len(rc.Changes) != 1 || rc.Changes[0].Key != "enabledPlugins.(hidden)" || rc.Changes[0].After != domain.Hidden {
		t.Errorf("changes are %+v", rc.Changes)
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
	if got, err := domain.SetPath(nil, []string{"a", "b"}, 1.0); err != nil || text(got) != `{"a":{"b":1}}` {
		t.Errorf("set %s (%v)", text(got), err)
	}
}

func TestSetPathRefusesAPathThroughAValue(t *testing.T) {
	got, err := domain.SetPath(obj(t, `{"k": {"a": 2}}`), []string{"k", "a", "b"}, int64(2))

	if err == nil || text(got) != `{"k":{"a":2}}` {
		t.Errorf("set %s (%v), want k.a kept and refused, as AddPath does", text(got), err)
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
	for in, want := range map[string]string{`["a"]`: `["a"]`, `true`: `true`, `30`: `30`, `opus`: `"opus"`, `9007199254740993`: `9007199254740993`, `1.5`: `1.5`} {
		if got := text(domain.ParseValue(in)); got != want {
			t.Errorf("%s reads as %s, want %s", in, got, want)
		}
	}
	if !reflect.DeepEqual(domain.ParseValue("1.0"), domain.ParseValue("1")) {
		t.Errorf("1.0 and 1 differ")
	}
}

func TestShowValueKeepsWhatJSONNeedNotEscape(t *testing.T) {
	if got := domain.ShowValue("x", "a && b > c"); got != `"a && b > c"` {
		t.Errorf("shows %s", got)
	}
}

func TestShowValueShowsThePathsNerveFillsIn(t *testing.T) {
	m := domain.Manifest{Hooks: map[string]domain.Hook{"fmt": {Event: "PostToolUse", Command: "{{brain}}/hooks/fmt.sh"}}}
	p := &domain.Project{Hooks: []string{"fmt"}}

	want := domain.Leaves(domain.Want(m, p, "/home/u/.local/bin/nerve sync --here --hook", nil, "/home/u/brain"))

	for _, k := range domain.SortedKeys(want) {
		if got := domain.ShowValue(k, want[k]); got == domain.Hidden {
			t.Errorf("%s shows as %s", domain.Dotted(k), got)
		}
	}
}

func TestShowValueHidesTokensAndEmails(t *testing.T) {
	for _, v := range []string{"/home/u/bin/line --token ghp_abcdefghijklmnopqrstuvwxyz0123", "/home/u/bin/notify me@example.com"} {
		if got := domain.ShowValue(domain.LeafKey([]string{"statusLine", "command"}), v); got != domain.Hidden {
			t.Errorf("%s shows as %s", v, got)
		}
	}
}

func TestParseValueKeepsEveryDigitOfAWholeNumberBeyondInt64(t *testing.T) {
	if got := text(domain.ParseValue("12345678901234567890")); got != "12345678901234567890" {
		t.Errorf("reads back as %s", got)
	}
}

func TestParseKeyReadsWhatDottedWrites(t *testing.T) {
	for _, path := range [][]string{{"model"}, {"permissions", "deny"}, {"enabledPlugins", "x@y.z"}, {"a=b", "c"}, {`a"b`}, {""}, {"a", "", "b"}, {`a\b`}, {`x"y.z\`}} {
		shown := domain.Dotted(domain.LeafKey(path))
		got, err := domain.ParseKey(shown)
		if err != nil || !reflect.DeepEqual(got, path) {
			t.Errorf("%v shows as %s, which reads as %v (%v)", path, shown, got, err)
		}
	}
}

func TestParseKeyRefusesWhatIsNotAKey(t *testing.T) {
	for _, key := range []string{"", ".model", "model.", "a..b", `a."b`, `a"b".c`, `"a"b`, `"a\"`} {
		if path, err := domain.ParseKey(key); err == nil {
			t.Errorf("%q reads as %v", key, path)
		}
	}
}

func TestCutItemSplitsAtTheFirstEqualsOutsideQuotes(t *testing.T) {
	for arg, want := range map[string][3]any{
		"permissions.deny=Bash(x=1)": {"permissions.deny", "Bash(x=1)", true},
		`env."A=B"=1`:                {`env."A=B"`, "1", true},
		`env."A\"=B"=1`:              {`env."A\"=B"`, "1", true},
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
	wantDrift := []domain.Change{{Key: "permissions.deny", Item: true, Before: `"Cron"`, After: "unset"}}
	if !reflect.DeepEqual(rc.Drift, wantDrift) {
		t.Errorf("drift is %+v", rc.Drift)
	}
	if got := text(rc.Personal); got != `{"permissions":{"allow":["Bash(make)"]}}` {
		t.Errorf("personal is %s, want the approval", got)
	}
}

func TestReconcileDiscardDropsOnlyWhatDrifted(t *testing.T) {
	last := obj(t, `{"permissions": {"allow": ["Read"]}}`)
	current := obj(t, `{"permissions": {"allow": ["Bash(make)"]}}`) // Read removed, an approval added

	rc := domain.Reconcile(current, last, last, true)

	if text(rc.Result) != `{"permissions":{"allow":["Bash(make)","Read"]}}` || len(rc.Drift) > 0 {
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
	rc := domain.Reconcile(nil, nil, obj(t, `{"env": {"TOKEN": "sk-abc"}, "permissions": {"allow": ["Bash(notify bob@example.com)"]}}`), false)

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

func TestOverridesOfCommonLeaveAnEmptyObjectOut(t *testing.T) {
	p := &domain.Project{Settings: obj(t, `{"permissions": {}}`)}

	if got := domain.OverridesOfCommon(p, obj(t, `{"permissions": {}}`)); len(got) > 0 {
		t.Errorf("overrides are %v, want none: an empty object holds no setting", got)
	}
}
