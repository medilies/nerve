package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// secretKeys are settings that hold secrets or run credential helpers. They
// never enter the Brain; they belong in a local overlay.
var secretKeys = []string{"apiKeyHelper", "awsAuthRefresh", "awsCredentialExport", "env", "otelHeadersHelper"}

var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	homePattern  = regexp.MustCompile(`/home/[^/\s"']+|/Users/[^/\s"']+|[A-Za-z]:\\Users\\`)
	tokenPattern = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16})`)
)

// sensitive says why a setting must stay out of the Brain, or "". key is a
// leaf path.
func sensitive(key string, v any) string {
	top, _, _ := strings.Cut(key, sep)
	if slices.Contains(secretKeys, top) {
		return top + " may hold secrets"
	}
	return sensitiveText(v)
}

// sensitiveText says why a value is identifying or secret, or "".
func sensitiveText(v any) string {
	why := ""
	walkStrings(v, func(s string) {
		switch {
		case why != "":
		case tokenPattern.MatchString(s):
			why = "looks like a token"
		case emailPattern.MatchString(s):
			why = "holds an email address"
		case homePattern.MatchString(s):
			why = "holds a home path: use {{brain}} or ~"
		}
	})
	return why
}

func walkStrings(v any, fn func(string)) {
	switch t := v.(type) {
	case string:
		fn(t)
	case []any:
		for _, x := range t {
			walkStrings(x, fn)
		}
	case map[string]any:
		for k, x := range t {
			fn(k)
			walkStrings(x, fn)
		}
	}
}

// brainPlaceholder in any settings string becomes the Brain's checkout path.
const brainPlaceholder = "{{brain}}"

// readObject reads a JSON object, or returns nil when the file is missing.
func readObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, nil
}

func writeObject(path string, obj map[string]any) error {
	if obj == nil {
		obj = map[string]any{}
	}
	b, _ := json.MarshalIndent(obj, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFile(path, append(b, '\n'))
}

// overlayPath is a project's local overlay: settings that stay on this
// machine.
func overlayPath(project string) (string, error) {
	dir, err := machineDir()
	return filepath.Join(dir, "overlays", project+".json"), err
}

// wantSettings is what a project's settings.local.json should hold: common
// settings, then the project's, then what nerve adds for its plugins, hooks
// and autosync, then the local overlay.
func wantSettings(root string, m Manifest, name string, p *Project, autosyncCmd string) (map[string]any, error) {
	want := merge(nil, m.Common.Settings)
	want = merge(want, p.Settings)
	want = merge(want, generated(m, p, autosyncCmd))
	path, err := overlayPath(name)
	if err != nil {
		return nil, err
	}
	overlay, err := readObject(path)
	if err != nil {
		return nil, err
	}
	want = merge(want, overlay)
	return fillBrain(want, root).(map[string]any), nil
}

// generated is what a project's plugins, hooks and autosync add to its
// settings.
func generated(m Manifest, p *Project, autosyncCmd string) map[string]any {
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
	if m.autosync(p) {
		add("SessionStart", "", autosyncCmd)
	}
	if len(hooks) > 0 {
		g["hooks"] = hooks
	}
	return g
}

// merge lays src over dst: objects merge key by key, arrays are joined
// without repeats, anything else in src wins.
func merge(dst, src map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		switch sv := v.(type) {
		case map[string]any:
			if dv, ok := out[k].(map[string]any); ok {
				out[k] = merge(dv, sv)
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

func fillBrain(v any, brain string) any {
	switch t := v.(type) {
	case string:
		return strings.ReplaceAll(t, brainPlaceholder, brain)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = fillBrain(x, brain)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			out[k] = fillBrain(x, brain)
		}
		return out
	}
	return v
}

// sep joins the keys of a leaf path. Keys may hold dots, as plugin names do,
// so a dot cannot.
const sep = "\x1f"

// dotted shows a leaf path the way a user writes it.
func dotted(key string) string { return strings.ReplaceAll(key, sep, ".") }

// undotted reads a key the way a user writes it.
func undotted(key string) string { return strings.ReplaceAll(key, ".", sep) }

// leaves flattens an object to the paths of its non-object values; an empty
// object is a value.
func leaves(obj map[string]any) map[string]any {
	out := map[string]any{}
	var walk func(map[string]any, string)
	walk = func(o map[string]any, prefix string) {
		for k, v := range o {
			if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
				walk(sub, prefix+k+sep)
				continue
			}
			out[prefix+k] = v
		}
	}
	walk(obj, "")
	return out
}

// unflatten is the inverse of leaves.
func unflatten(l map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range sortedKeys(l) {
		parts := strings.Split(k, sep)
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

// same reports whether a and b agree on key k, both lacking it included.
func same(a, b map[string]any, k string) bool {
	av, aok := a[k]
	bv, bok := b[k]
	return aok == bok && reflect.DeepEqual(av, bv)
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
	return sortedKeys(keys)
}

func show(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok {
		return "unset"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// syncFile makes the settings file at path hold want, except for keys
// changed there since nerve last wrote last and that the Brain does not
// agree with: those are drift, reported and left as they are, unless discard
// is set. It reports each key it changes, updates last, and returns whether
// any key drifted. A dry run only reports drift.
func syncFile(path string, want map[string]any, last *map[string]any, discard, dry bool, r *report) (bool, error) {
	current, err := readObject(path)
	if err != nil {
		return false, err
	}
	have, prev, goal := leaves(current), leaves(*last), leaves(want)
	result, next := maps.Clone(goal), maps.Clone(goal)
	drifted := false
	for _, k := range unionKeys(have, prev) {
		if discard || same(have, prev, k) || same(have, goal, k) {
			continue
		}
		drifted = true
		setOrDelete(result, k, have)
		setOrDelete(next, k, prev)
		r.add(Event{Kind: evDrift, Path: path, Key: dotted(k), Local: show(have, k), Brain: show(goal, k)})
	}
	if dry {
		return drifted, nil
	}
	changed := false
	for _, k := range unionKeys(have, result) {
		if !same(have, result, k) {
			changed = true
			r.add(Event{Kind: evSetting, Path: path, Key: dotted(k), Local: show(have, k), Brain: show(result, k)})
		}
	}
	if changed || (current == nil && len(result) > 0) {
		if err := writeObject(path, unflatten(result)); err != nil {
			return drifted, err
		}
	}
	*last = unflatten(next)
	if len(next) == 0 {
		*last = nil
	}
	return drifted, nil
}

// importSettings sorts a project's existing settings against common ones:
// what the project entry must record, and what goes to the local overlay
// because it is sensitive. Each key's fate is reported.
func importSettings(project string, existing, common map[string]any, r *report) (entry, overlay map[string]any) {
	have, base := leaves(existing), leaves(common)
	entryL, overlayL := map[string]any{}, map[string]any{}
	note := func(k, what string) { r.add(Event{Kind: evImported, Project: project, Key: dotted(k), Detail: what}) }
	for _, k := range sortedKeys(have) {
		v := have[k]
		cv, inCommon := base[k]
		switch {
		case sensitive(k, v) != "":
			overlayL[k] = v
			note(k, "kept in the local overlay: "+sensitive(k, v))
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
	for _, k := range sortedKeys(base) {
		if _, ok := have[k]; !ok {
			note(k, "added by common at the next sync")
		}
	}
	if len(entryL) > 0 {
		entry = unflatten(entryL)
	}
	if len(overlayL) > 0 {
		overlay = unflatten(overlayL)
	}
	return entry, overlay
}

// manifestProblems lists sensitive values in the Brain's settings and hooks.
// Both check and sync refuse them, since a Brain may be public.
func manifestProblems(m Manifest) []Event {
	var p []Event
	scan := func(where string, settings map[string]any) {
		l := leaves(settings)
		for _, k := range sortedKeys(l) {
			if why := sensitive(k, l[k]); why != "" {
				p = append(p, Event{Kind: evProblem, Path: manifestFile, Key: where + dotted(k), Detail: why + ": move it to a local overlay"})
			}
		}
	}
	scan("common.settings.", m.Common.Settings)
	for _, name := range sortedKeys(m.Projects) {
		scan("project."+name+".settings.", m.Projects[name].Settings)
	}
	for _, name := range sortedKeys(m.Hooks) {
		if why := sensitiveText(m.Hooks[name].Command); why != "" {
			p = append(p, Event{Kind: evProblem, Path: manifestFile, Key: "hook." + name + ".command", Detail: why})
		}
	}
	return p
}

// writeFile replaces path's content all at once, so Claude Code never reads
// it half written. A path that is a link keeps the link and replaces what it
// points to, and an existing file keeps its mode.
func writeFile(path string, b []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // a no-op once renamed
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
