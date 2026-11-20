package domain

import (
	"encoding/json"
	"maps"
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

// Dotted shows a leaf path the way a user writes it.
func Dotted(key string) string { return strings.ReplaceAll(key, Sep, ".") }

// Undotted reads a key the way a user writes it.
func Undotted(key string) string { return strings.ReplaceAll(key, ".", Sep) }

// Leaves flattens an object to the paths of its non-object values; an empty
// object is a value.
func Leaves(obj map[string]any) map[string]any {
	out := map[string]any{}
	var walk func(map[string]any, string)
	walk = func(o map[string]any, prefix string) {
		for k, v := range o {
			if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
				walk(sub, prefix+k+Sep)
				continue
			}
			out[prefix+k] = v
		}
	}
	walk(obj, "")
	return out
}

// Unflatten is the inverse of Leaves.
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
		o[parts[len(parts)-1]] = l[k]
	}
	return out
}

// Same reports whether a and b agree on key k, both lacking it included.
func Same(a, b map[string]any, k string) bool {
	av, aok := a[k]
	bv, bok := b[k]
	return aok == bok && reflect.DeepEqual(av, bv)
}

// Show writes a leaf's value as JSON, "unset" when absent.
func Show(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok {
		return "unset"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func setOrDelete(dst map[string]any, k string, from map[string]any) {
	if v, ok := from[k]; ok {
		dst[k] = v
	} else {
		delete(dst, k)
	}
}

func unionKeys(a, b map[string]any) []string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return SortedKeys(keys)
}

// SortedKeys returns a map's keys in order.
func SortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

// Change is a settings key going from one value to another, each as JSON or
// "unset".
type Change struct {
	Key    string // dotted
	Before string
	After  string
}

// Reconciliation is what a settings file becomes.
type Reconciliation struct {
	Result  map[string]any // what the file holds
	Next    map[string]any // what nerve means it to hold: drift does not count
	Drift   []Change       // from what the Brain wants to what the file has, left as is
	Changes []Change       // from what the file has to Result
	Write   bool           // whether the file must be written
}

// Reconcile works out what a settings file holding current should hold to
// match want, given what nerve meant it to hold when it last wrote it. A key
// changed there since, that want does not agree with, is drift and kept as
// it is, unless discard is set.
func Reconcile(current, last, want map[string]any, discard bool) Reconciliation {
	have, prev, goal := Leaves(current), Leaves(last), Leaves(want)
	result, next := maps.Clone(goal), maps.Clone(goal)
	var rc Reconciliation
	for _, k := range unionKeys(have, prev) {
		if discard || Same(have, prev, k) || Same(have, goal, k) {
			continue
		}
		setOrDelete(result, k, have)
		setOrDelete(next, k, prev)
		rc.Drift = append(rc.Drift, Change{Key: Dotted(k), Before: Show(goal, k), After: Show(have, k)})
	}
	for _, k := range unionKeys(have, result) {
		if !Same(have, result, k) {
			rc.Changes = append(rc.Changes, Change{Key: Dotted(k), Before: Show(have, k), After: Show(result, k)})
		}
	}
	rc.Result = Unflatten(result)
	rc.Write = len(rc.Changes) > 0 || (current == nil && len(result) > 0)
	if len(next) > 0 {
		rc.Next = Unflatten(next)
	}
	return rc
}

// Strip takes out of current the keys nerve wrote (last) that were not
// changed since. It returns what is left, and whether anything was taken.
func Strip(current, last map[string]any) (map[string]any, bool) {
	have, prev := Leaves(current), Leaves(last)
	n := len(have)
	for k := range prev {
		if Same(have, prev, k) {
			delete(have, k)
		}
	}
	return Unflatten(have), len(have) != n
}

// Fate is where one key of a project's existing settings went when the
// project was added.
type Fate struct {
	Key  string // dotted
	What string
}

// ImportSettings sorts a project's existing settings against common ones:
// what the project's entry must record so the project behaves as before, and
// what goes to the local overlay because it is sensitive.
func ImportSettings(existing, common map[string]any) (entry, overlay map[string]any, fates []Fate) {
	have, base := Leaves(existing), Leaves(common)
	entryL, overlayL := map[string]any{}, map[string]any{}
	note := func(k, what string) { fates = append(fates, Fate{Key: Dotted(k), What: what}) }
	for _, k := range SortedKeys(have) {
		v := have[k]
		cv, inCommon := base[k]
		switch {
		case Sensitive(k, v) != "":
			overlayL[k] = v
			note(k, "kept in the local overlay: "+Sensitive(k, v))
		case !inCommon:
			entryL[k] = v
			note(k, "recorded in the project")
		case reflect.DeepEqual(v, cv):
			note(k, "already given by common")
		default:
			items, isList := v.([]any)
			commonItems, commonIsList := cv.([]any)
			if !isList || !commonIsList {
				entryL[k] = v
				note(k, "recorded as an override of common")
				continue
			}
			var extra []any
			for _, x := range items {
				if !slices.ContainsFunc(commonItems, func(y any) bool { return reflect.DeepEqual(x, y) }) {
					extra = append(extra, x)
				}
			}
			if len(extra) > 0 {
				entryL[k] = extra
			}
			note(k, "recorded as the items common lacks")
		}
	}
	for _, k := range SortedKeys(base) {
		if _, ok := have[k]; !ok {
			note(k, "added by common at the next sync")
		}
	}
	if len(entryL) > 0 {
		entry = Unflatten(entryL)
	}
	if len(overlayL) > 0 {
		overlay = Unflatten(overlayL)
	}
	return entry, overlay, fates
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

// ParseValue reads a value as JSON, else as text.
func ParseValue(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return v
}

// SetPath sets the value at a path of keys, making objects on the way.
func SetPath(obj map[string]any, path []string, v any) map[string]any {
	if obj == nil {
		obj = map[string]any{}
	}
	if len(path) == 1 {
		obj[path[0]] = v
		return obj
	}
	sub, _ := obj[path[0]].(map[string]any)
	obj[path[0]] = SetPath(sub, path[1:], v)
	return obj
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
