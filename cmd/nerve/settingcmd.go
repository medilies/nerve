package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func runSetting(args []string, f flags, r *report) error {
	if len(args) == 0 {
		return errUsage
	}
	w, err := openWorkspace()
	if err != nil {
		return err
	}
	switch verb := args[0]; {
	case verb == "set" && len(args) == 3:
		return w.settingSet(args[1], parseValue(args[2]), f, r)
	case verb == "rm" && len(args) == 2:
		return w.settingRm(args[1], f, r)
	case verb == "ls" && len(args) == 1:
		return w.settingLs(f, r)
	}
	return errUsage
}

// parseValue reads a value as JSON, else as text.
func parseValue(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return v
}

// settingSet sets a value in the project's entry, or in common. A sensitive
// value goes to the project's local overlay instead, never into the Brain.
func (w workspace) settingSet(key string, v any, f flags, r *report) error {
	if key == "" || strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") {
		return fmt.Errorf("setting %q must be a key such as model or permissions.deny", key)
	}
	k := undotted(key)
	if f.common {
		if why := sensitive(k, v); why != "" {
			return fmt.Errorf("%s %s, so it cannot go in common: set it per project, where it goes to the local overlay", key, why)
		}
		old := show(leaves(w.m.Common.Settings), k)
		w.m.Common.Settings = setPath(w.m.Common.Settings, strings.Split(k, sep), v)
		if err := w.save(); err != nil {
			return err
		}
		r.add(Event{Kind: evSetting, Path: manifestFile + " (common)", Key: key, Local: old, Brain: show(leaves(w.m.Common.Settings), k)})
		return nil
	}
	project, p, err := w.target(f)
	if err != nil {
		return err
	}
	if p.Surface == claudeAI {
		return fmt.Errorf("project %s is a claude.ai account, which takes skills only", project)
	}
	if why := sensitive(k, v); why != "" {
		path, err := overlayPath(project)
		if err != nil {
			return err
		}
		overlay, err := readObject(path)
		if err != nil {
			return err
		}
		if err := writeObject(path, setPath(overlay, strings.Split(k, sep), v)); err != nil {
			return err
		}
		r.add(Event{Kind: evOverlay, Project: project, Key: key, Path: path, Detail: why})
		return nil
	}
	old := show(leaves(p.Settings), k)
	p.Settings = setPath(p.Settings, strings.Split(k, sep), v)
	if err := w.save(); err != nil {
		return err
	}
	r.add(Event{Kind: evSetting, Path: manifestFile + " (project " + project + ")", Key: key, Local: old, Brain: show(leaves(p.Settings), k)})
	return nil
}

// setPath sets the value at a path of keys, making objects on the way.
func setPath(obj map[string]any, path []string, v any) map[string]any {
	if obj == nil {
		obj = map[string]any{}
	}
	if len(path) == 1 {
		obj[path[0]] = v
		return obj
	}
	sub, _ := obj[path[0]].(map[string]any)
	obj[path[0]] = setPath(sub, path[1:], v)
	return obj
}

// settingRm removes a key, or with key=value one item of a list, from the
// project, from common with --common, or from common and every project with
// --all.
func (w workspace) settingRm(arg string, f flags, r *report) error {
	key, raw, hasValue := strings.Cut(arg, "=")
	var item any
	if hasValue {
		item = parseValue(raw)
	}
	path := strings.Split(undotted(key), sep)
	type target struct {
		name     string
		settings *map[string]any
	}
	var targets []target
	if f.all || f.common {
		targets = append(targets, target{"", &w.m.Common.Settings})
	}
	if f.all {
		for _, name := range sortedKeys(w.m.Projects) {
			targets = append(targets, target{name, &w.m.Projects[name].Settings})
		}
	} else if !f.common {
		name, p, err := w.target(f)
		if err != nil {
			return err
		}
		targets = append(targets, target{name, &p.Settings})
	}
	removed := 0
	for _, t := range targets {
		if !removePath(*t.settings, path, item, hasValue) {
			continue
		}
		if len(*t.settings) == 0 {
			*t.settings = nil
		}
		removed++
		r.add(Event{Kind: evDropped, Project: t.name, Item: "setting " + arg})
	}
	if removed == 0 {
		where := "common"
		switch {
		case len(targets) > 1:
			where = "project nor common"
		case targets[0].name != "":
			where = "project " + targets[0].name
		}
		return fmt.Errorf("no %s holds %s", where, arg)
	}
	return w.save()
}

// removePath removes the value at a path of keys, or only item from the list
// there, and the objects it leaves empty. It reports whether it removed
// anything.
func removePath(obj map[string]any, path []string, item any, hasItem bool) bool {
	v, ok := obj[path[0]]
	if !ok {
		return false
	}
	if len(path) > 1 {
		sub, isObj := v.(map[string]any)
		if !isObj || !removePath(sub, path[1:], item, hasItem) {
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

// settingLs lists the project's effective settings, each with the layers
// that give it.
func (w workspace) settingLs(f flags, r *report) error {
	project, p, err := w.target(f)
	if err != nil {
		return err
	}
	cmd, err := autosyncCommand()
	if err != nil {
		return err
	}
	final, err := wantSettings(w.root, w.m, project, p, cmd)
	if err != nil {
		return err
	}
	path, err := overlayPath(project)
	if err != nil {
		return err
	}
	overlay, err := readObject(path)
	if err != nil {
		return err
	}
	layers := []struct {
		name string
		l    map[string]any
	}{
		{"common", leaves(w.m.Common.Settings)},
		{"project", leaves(p.Settings)},
		{"nerve: plugins, hooks, autosync", leaves(generated(w.m, p, cmd))},
		{"local overlay", leaves(overlay)},
	}
	l := leaves(final)
	for _, k := range sortedKeys(l) {
		var from []string
		for _, layer := range layers {
			if _, ok := layer.l[k]; ok {
				from = append(from, layer.name)
			}
		}
		r.add(Event{Kind: evValue, Project: project, Key: dotted(k), Brain: show(l, k), Detail: strings.Join(from, " + ")})
	}
	if len(l) == 0 {
		r.add(Event{Kind: evOK, Detail: "project " + project + " has no settings"})
	}
	return nil
}
