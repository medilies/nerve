package domain

import (
	"encoding/json"
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
)

// LocalSettings is the one settings file nerve writes in a project, relative
// to its working tree.
const LocalSettings = ".claude/settings.local.json"

// BrainPlaceholder in any settings string becomes the Brain's checkout path.
const BrainPlaceholder = "{{brain}}"

// Want is what a project's settings.local.json should hold: common settings,
// then the project's, then what nerve adds for its plugins, hooks and
// autosync, then the local overlay; {{brain}} filled in.
func Want(m Manifest, p *Project, autosyncCmd string, overlay map[string]any, brainDir string) map[string]any {
	want := Merge(nil, m.Common)
	want = Merge(want, p.Settings)
	want = Merge(want, Generated(m, p, autosyncCmd))
	want = Merge(want, overlay)
	return FillBrain(want, brainDir).(map[string]any)
}

// Generated is what a project's plugins, hooks and autosync add to its
// settings.
func Generated(m Manifest, p *Project, autosyncCmd string) map[string]any {
	g := map[string]any{}
	if len(p.Plugins) > 0 {
		enabled := map[string]any{}
		for _, id := range p.Plugins {
			enabled[id] = true
		}
		g["enabledPlugins"] = enabled
	}
	hooks := map[string]any{}
	add := func(event, matcher, command string) {
		entry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}
		if matcher != "" {
			entry["matcher"] = matcher
		}
		list, _ := hooks[event].([]any)
		hooks[event] = append(list, entry)
	}
	for _, name := range p.Hooks {
		if h, ok := m.Hooks[name]; ok {
			add(h.Event, h.Matcher, h.Command)
		}
	}
	if m.AutosyncFor(p) {
		add("SessionStart", "", autosyncCmd)
	}
	if len(hooks) > 0 {
		g["hooks"] = hooks
	}
	return g
}

// Merge lays src over dst: objects merge key by key, arrays are joined
// without repeats, anything else in src wins.
func Merge(dst, src map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		switch sv := v.(type) {
		case map[string]any:
			if dv, ok := out[k].(map[string]any); ok {
				out[k] = Merge(dv, sv)
				continue
			}
		case []any:
			if dv, ok := out[k].([]any); ok {
				joined := slices.Clone(dv)
				for _, item := range sv {
					if !slices.ContainsFunc(joined, func(x any) bool { return reflect.DeepEqual(x, item) }) {
						joined = append(joined, item)
					}
				}
				out[k] = joined
				continue
			}
		}
		out[k] = v
	}
	return out
}

// FillBrain replaces {{brain}} in every string with the Brain's path.
func FillBrain(v any, brain string) any {
	switch t := v.(type) {
	case string:
		return strings.ReplaceAll(t, BrainPlaceholder, brain)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = FillBrain(x, brain)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			out[k] = FillBrain(x, brain)
		}
		return out
	}
	return v
}

// Sep joins the keys of a leaf path. Keys may hold dots, as plugin names do,
// so a dot cannot.
const Sep = "\x1f"

// Dotted shows a leaf path the way a user writes it: keys joined by dots,
// and a key that is empty or holds a dot, a quote or = in double quotes,
// with \ before each quote or backslash in it.
func Dotted(key string) string {
	parts := strings.Split(key, Sep)
	for i, p := range parts {
		if p == "" || strings.ContainsAny(p, `."=`) {
			parts[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(p) + `"`
		}
	}
	return strings.Join(parts, ".")
}

// ShowKey shows a leaf path as Dotted does, with each key that is
// identifying or secret shown as Hidden.
func ShowKey(key string) string {
	parts := strings.Split(key, Sep)
	for i, p := range parts {
		if SensitiveText(p) != "" {
			parts[i] = Hidden
		}
	}
	return Dotted(strings.Join(parts, Sep))
}

// ErrKey is a settings key a user wrote that nerve cannot read.
var ErrKey = errors.New(`must be keys joined by dots, such as model or permissions.deny, with a key that holds a dot in double quotes, such as enabledPlugins."x@y.z"`)

// ParseKey reads a settings key the way a user writes it, as Dotted shows
// it, into its path of keys.
func ParseKey(s string) ([]string, error) {
	var path []string
	for i := 0; ; i++ {
		var seg string
		if i < len(s) && s[i] == '"' {
			var b strings.Builder
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
					i++
				}
				b.WriteByte(s[i])
			}
			if i == len(s) {
				return nil, ErrKey
			}
			seg, i = b.String(), i+1
		} else {
			j := strings.IndexAny(s[i:], `."`)
			if j < 0 {
				j = len(s) - i
			}
			if j == 0 {
				return nil, ErrKey
			}
			seg, i = s[i:i+j], i+j
		}
		if i < len(s) && (s[i] != '.' || i == len(s)-1) {
			return nil, ErrKey
		}
		path = append(path, seg)
		if i == len(s) {
			return path, nil
		}
	}
}

// CutItem splits key=value at the first = outside double quotes.
func CutItem(arg string) (key, value string, ok bool) {
	quoted := false
	for i := 0; i < len(arg); i++ {
		switch c := arg[i]; {
		case quoted && c == '\\':
			i++
		case c == '"':
			quoted = !quoted
		case c == '=' && !quoted:
			return arg[:i], arg[i+1:], true
		}
	}
	return arg, "", false
}

// LeafKey is the leaf path of a path of keys.
func LeafKey(path []string) string { return strings.Join(path, Sep) }

// Leaves flattens an object to the paths of its non-object values; an empty
// object is a value.
func Leaves(obj map[string]any) map[string]any { return leavesUpTo(obj, nil) }

// leavesUpTo flattens as Leaves does, but takes the value at each path in
// whole as a leaf.
func leavesUpTo(obj map[string]any, whole map[string]bool) map[string]any {
	out := map[string]any{}
	var walk func(map[string]any, string)
	walk = func(o map[string]any, prefix string) {
		for k, v := range o {
			if sub, ok := v.(map[string]any); ok && len(sub) > 0 && !whole[prefix+k] {
				walk(sub, prefix+k+Sep)
				continue
			}
			out[prefix+k] = v
		}
	}
	walk(obj, "")
	return out
}

// leafLayers flattens layers of settings so that they agree on where each
// leaf is. An empty object holds no setting, so it is left out. A key that
// one layer holds as a value and another as an object is taken whole, as
// one leaf.
func leafLayers(layers ...map[string]any) []map[string]any {
	flat := make([]map[string]any, len(layers))
	for i, l := range layers {
		flat[i] = settingLeaves(l, nil)
	}
	whole := map[string]bool{}
	under := inner(flat)
	for _, l := range flat {
		for k := range l {
			if under[k] {
				whole[k] = true
			}
		}
	}
	if len(whole) > 0 {
		for i, l := range layers {
			flat[i] = settingLeaves(l, whole)
		}
	}
	return flat
}

// settingLeaves flattens as leavesUpTo does, without empty objects, even
// inside a value taken whole.
func settingLeaves(obj map[string]any, whole map[string]bool) map[string]any {
	l := leavesUpTo(obj, whole)
	for k, v := range l {
		if o, ok := v.(map[string]any); ok {
			l[k] = withoutEmpty(o)
		}
	}
	maps.DeleteFunc(l, func(_ string, v any) bool { return emptyObject(v) })
	return l
}

// withoutEmpty is a copy of o without the empty objects it holds, at any
// depth.
func withoutEmpty(o map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range o {
		if sub, ok := v.(map[string]any); ok {
			if sub = withoutEmpty(sub); len(sub) == 0 {
				continue
			}
			v = sub
		}
		out[k] = v
	}
	return out
}

// inner lists the paths that some leaf of the layers sits under.
func inner(flat []map[string]any) map[string]bool {
	under := map[string]bool{}
	for _, l := range flat {
		for k := range l {
			for i := range len(k) {
				if k[i] == Sep[0] {
					under[k[:i]] = true
				}
			}
		}
	}
	return under
}

func emptyObject(v any) bool {
	o, ok := v.(map[string]any)
	return ok && len(o) == 0
}

// Unflatten is the inverse of Leaves. It copies the values, so the result
// shares nothing with l.
func Unflatten(l map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range SortedKeys(l) {
		parts := strings.Split(k, Sep)
		o := out
		for _, p := range parts[:len(parts)-1] {
			sub, ok := o[p].(map[string]any)
			if !ok {
				sub = map[string]any{}
				o[p] = sub
			}
			o = sub
		}
		o[parts[len(parts)-1]] = clone(l[k])
	}
	return out
}

// clone deep-copies decoded JSON.
func clone(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = clone(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = clone(x)
		}
		return out
	}
	return v
}

// Same reports whether a and b agree on key k, both lacking it included.
func Same(a, b map[string]any, k string) bool {
	av, aok := a[k]
	bv, bok := b[k]
	return aok == bok && reflect.DeepEqual(av, bv)
}

// Hidden stands for a sensitive value in what nerve reports, so a secret
// never reaches a report, or Claude through one.
const Hidden = "(hidden)"

// Show writes a leaf's value as JSON, "unset" when absent, and Hidden when
// it is sensitive.
func Show(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok {
		return "unset"
	}
	return ShowValue(k, v)
}

// ShowValue writes a value at leaf k as JSON, or Hidden when it is secret
// or identifying. A home path shows: it is this machine's own.
func ShowValue(k string, v any) string {
	if secret(k, v) != "" {
		return Hidden
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

func setOrDelete(dst map[string]any, k string, from map[string]any) {
	if v, ok := from[k]; ok {
		dst[k] = v
	} else {
		delete(dst, k)
	}
}

func unionKeys(layers ...map[string]any) []string {
	keys := map[string]bool{}
	for _, m := range layers {
		for k := range m {
			keys[k] = true
		}
	}
	return SortedKeys(keys)
}

func hasItem(list []any, x any) bool {
	return slices.ContainsFunc(list, func(y any) bool { return reflect.DeepEqual(x, y) })
}

// lists says whether k holds a list wherever it is set, and is set
// somewhere.
func lists(k string, layers ...map[string]any) bool {
	set := false
	for _, l := range layers {
		v, ok := l[k]
		if !ok {
			continue
		}
		if _, isList := v.([]any); !isList {
			return false
		}
		set = true
	}
	return set
}

// SortedKeys returns a map's keys in order.
func SortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

// Change is a settings key going from one value to another, each as JSON,
// "unset" or Hidden. For one item of a list, Item is set, and Before and
// After are that item, or "unset" on the side that lacks it.
type Change struct {
	Key    string // dotted
	Item   bool
	Before string
	After  string
}

// Reconciliation is what a settings file becomes.
type Reconciliation struct {
	Result   map[string]any // what the file holds
	Next     map[string]any // what nerve means it to hold: drift does not count
	Drift    []Change       // from what the Brain wants to what the file has, left as is
	Personal map[string]any // the file's settings and list items that neither the Brain nor nerve set, kept as they are
	Changes  []Change       // from what the file has to Result
	Write    bool           // whether the file must be written
}

// Reconcile works out what a settings file holding current should hold to
// match want, given what nerve meant it to hold when it last wrote it. A key
// that neither want nor last holds is personal: kept, discard or not. A key
// changed there since, that want does not agree with, is drift and kept as
// it is, unless discard is set. A list is reconciled item by item, so an
// item added or removed there is personal or drift on its own, and the
// Brain's other items still arrive. A file that is gone (current nil) holds
// nothing to keep: it is written anew.
func Reconcile(current, last, want map[string]any, discard bool) Reconciliation {
	if current == nil {
		last = nil
	}
	flat := leafLayers(current, last, want)
	have, prev, goal := flat[0], flat[1], flat[2]
	result, next, personal := map[string]any{}, map[string]any{}, map[string]any{}
	var rc Reconciliation
	for _, k := range unionKeys(have, prev, goal) {
		if lists(k, have, prev, goal) {
			rc.Drift = append(rc.Drift, reconcileList(k, have, prev, goal, discard, result, next, personal)...)
			continue
		}
		_, inP := prev[k]
		_, inG := goal[k]
		if !inP && !inG {
			result[k], personal[k] = have[k], have[k]
			continue
		}
		if discard || Same(have, prev, k) || Same(have, goal, k) {
			setOrDelete(result, k, goal)
			setOrDelete(next, k, goal)
			continue
		}
		setOrDelete(result, k, have)
		setOrDelete(next, k, prev)
		rc.Drift = append(rc.Drift, Change{Key: ShowKey(k), Before: Show(goal, k), After: Show(have, k)})
	}
	for _, k := range unionKeys(have, result) {
		if !Same(have, result, k) {
			rc.Changes = append(rc.Changes, Change{Key: ShowKey(k), Before: Show(have, k), After: Show(result, k)})
		}
	}
	rc.Result = Unflatten(result)
	rc.Write = len(rc.Changes) > 0 || (current == nil && len(result) > 0)
	if len(next) > 0 {
		rc.Next = Unflatten(next)
	}
	if len(personal) > 0 {
		rc.Personal = Unflatten(personal)
	}
	return rc
}

// reconcileList reconciles the list at k item by item, as Reconcile does
// keys, into result, next and personal. The result keeps the file's order, and adds
// the Brain's new items after; next takes the Brain's order, so it does not
// change with the file's.
func reconcileList(k string, have, prev, goal map[string]any, discard bool, result, next, personal map[string]any) []Change {
	h, _ := have[k].([]any)
	p, _ := prev[k].([]any)
	g, _ := goal[k].([]any)
	_, hasH := have[k]
	_, hasP := prev[k]
	_, hasG := goal[k]
	var items []any
	for _, x := range slices.Concat(h, g, p) {
		if !hasItem(items, x) {
			items = append(items, x)
		}
	}
	kept, means, mine := []any{}, []any{}, []any{}
	var drift []Change
	for _, x := range items {
		inH, inP, inG := hasItem(h, x), hasItem(p, x), hasItem(g, x)
		if inH && !inP && !inG {
			kept, mine = append(kept, x), append(mine, x)
			continue
		}
		if discard || inH == inP || inH == inG {
			if inG {
				kept, means = append(kept, x), append(means, x)
			}
			continue
		}
		c := Change{Key: ShowKey(k), Item: true, Before: "unset", After: "unset"}
		if inH {
			kept, c.After = append(kept, x), ShowValue(k, x)
		}
		if inP {
			means = append(means, x)
		}
		if inG {
			c.Before = ShowValue(k, x)
		}
		drift = append(drift, c)
	}
	meant := []any{}
	for _, x := range slices.Concat(g, p) {
		if hasItem(means, x) && !hasItem(meant, x) {
			meant = append(meant, x)
		}
	}
	// The key itself is there or not as Reconcile decides for any key, so
	// an empty list someone set stays.
	onlyFile := hasH && !hasP && !hasG // a list only the file has is personal, empty or not
	keep, mean := hasG, hasG
	switch {
	case onlyFile:
		keep = true
		personal[k] = h
	case !discard && hasH != hasP && hasH != hasG:
		keep, mean = hasH, hasP
	}
	if len(mine) > 0 && !onlyFile {
		personal[k] = mine
	}
	if len(kept) > 0 || keep {
		result[k] = kept
	}
	if len(meant) > 0 || mean {
		next[k] = meant
	}
	return drift
}

// PersonalKeys names each personal setting, and each personal item of a
// list, with the value or item as shown, sensitive ones hidden: a key or an
// item, and its shown value.
func PersonalKeys(personal map[string]any) (names []string, shown map[string]Change) {
	shown = map[string]Change{}
	l := Leaves(personal)
	for _, k := range SortedKeys(l) {
		items, isList := l[k].([]any)
		if !isList || len(items) == 0 {
			names = append(names, ShowKey(k))
			shown[ShowKey(k)] = Change{Key: ShowKey(k), After: ShowValue(k, l[k])}
			continue
		}
		for _, x := range items {
			name := ShowKey(k) + " " + ShowValue(k, x)
			names = append(names, name)
			shown[name] = Change{Key: ShowKey(k), Item: true, After: ShowValue(k, x)}
		}
	}
	return names, shown
}

// Strip takes out of current the keys nerve wrote (last) that were not
// changed since, and from each list the items nerve wrote. It returns what
// is left, and whether anything was taken.
func Strip(current, last map[string]any) (map[string]any, bool) {
	have, prev := Leaves(current), Leaves(last)
	taken := false
	for k, pv := range prev {
		hv, ok := have[k]
		if !ok {
			continue
		}
		hl, hIsList := hv.([]any)
		pl, pIsList := pv.([]any)
		switch {
		case hIsList && pIsList && len(hl) > 0:
			rest := slices.DeleteFunc(slices.Clone(hl), func(x any) bool { return hasItem(pl, x) })
			switch {
			case len(rest) == len(hl):
				continue
			case len(rest) == 0:
				delete(have, k)
			default:
				have[k] = rest
			}
			taken = true
		case reflect.DeepEqual(hv, pv):
			delete(have, k)
			taken = true
		}
	}
	return Unflatten(have), taken
}

// OverridesOfCommon lists a project's settings keys that only repeat common.
func OverridesOfCommon(p *Project, common map[string]any) []string {
	base, l := Leaves(common), Leaves(p.Settings)
	var same []string
	for _, k := range SortedKeys(l) {
		if v, ok := base[k]; ok && reflect.DeepEqual(v, l[k]) {
			same = append(same, Dotted(k))
		}
	}
	return same
}

// ParseValue reads a value as JSON, numbers as Numbers makes them, else
// as text.
func ParseValue(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	_ = d.Decode(&v)
	return Numbers(v)
}

// Numbers makes each number in decoded JSON, read as json.Number, one that
// compares equal to the same number written another way: an int64 when it
// is whole and fits, a float64 when it has a fraction or exponent, as
// brain.toml and Claude Code hold it, and else the json.Number itself, so a
// whole number beyond int64 keeps every digit. It changes objects and lists
// in place.
func Numbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if !strings.ContainsAny(string(t), ".eE") {
			return t
		}
		if f, err := t.Float64(); err == nil {
			return Numbers(f)
		}
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<63 {
			return int64(t)
		}
	case []any:
		for i, x := range t {
			t[i] = Numbers(x)
		}
	case map[string]any:
		for k, x := range t {
			t[k] = Numbers(x)
		}
	}
	return v
}

// HasNull says whether a value is or holds null, which brain.toml cannot
// hold.
func HasNull(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case []any:
		return slices.ContainsFunc(t, HasNull)
	case map[string]any:
		for _, x := range t {
			if HasNull(x) {
				return true
			}
		}
	}
	return false
}

// SetPath sets the value at a path of keys, making objects on the way, and
// refuses a path through something other than an object. A list is set
// without repeats.
func SetPath(obj map[string]any, path []string, v any) (map[string]any, error) {
	if obj == nil {
		obj = map[string]any{}
	}
	if len(path) == 1 {
		if list, ok := v.([]any); ok {
			v = distinct(list)
		}
		obj[path[0]] = v
		return obj, nil
	}
	sub, isObj := obj[path[0]].(map[string]any)
	if _, ok := obj[path[0]]; ok && !isObj {
		return obj, errors.New("is not an object")
	}
	sub, err := SetPath(sub, path[1:], v)
	if err != nil {
		return obj, err
	}
	obj[path[0]] = sub
	return obj, nil
}

// distinct is list without its repeats, in order.
func distinct(list []any) []any {
	var out []any
	for _, x := range list {
		if !hasItem(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// AddPath adds item to the list at a path of keys, making the list and the
// objects on the way. It reports whether it added it, and refuses a path
// that holds something other than a list.
func AddPath(obj map[string]any, path []string, item any) (map[string]any, bool, error) {
	if obj == nil {
		obj = map[string]any{}
	}
	v, ok := obj[path[0]]
	if len(path) > 1 {
		sub, isObj := v.(map[string]any)
		if ok && !isObj {
			return obj, false, errors.New("is not an object")
		}
		sub, added, err := AddPath(sub, path[1:], item)
		obj[path[0]] = sub
		return obj, added, err
	}
	list, isList := v.([]any)
	switch {
	case ok && !isList:
		return obj, false, errors.New("is not a list")
	case hasItem(list, item):
		return obj, false, nil
	}
	obj[path[0]] = append(slices.Clone(list), item)
	return obj, true, nil
}

// RemovePath removes the value at a path of keys, or only item from the list
// there, and the objects it leaves empty. It reports whether it removed
// anything.
func RemovePath(obj map[string]any, path []string, item any, hasItem bool) bool {
	v, ok := obj[path[0]]
	if !ok {
		return false
	}
	if len(path) > 1 {
		sub, isObj := v.(map[string]any)
		if !isObj || !RemovePath(sub, path[1:], item, hasItem) {
			return false
		}
		if len(sub) == 0 {
			delete(obj, path[0])
		}
		return true
	}
	if !hasItem {
		delete(obj, path[0])
		return true
	}
	list, isList := v.([]any)
	if !isList {
		return false
	}
	kept := slices.DeleteFunc(slices.Clone(list), func(x any) bool { return reflect.DeepEqual(x, item) })
	if len(kept) == len(list) {
		return false
	}
	if len(kept) == 0 {
		delete(obj, path[0])
	} else {
		obj[path[0]] = kept
	}
	return true
}
