package app

import (
	"fmt"
	"strings"

	"github.com/medilies/nerve/internal/brain/domain"
)

// SetSetting sets a value in the target project's entry, or with common in
// common. A sensitive value goes to the project's local overlay instead,
// never into the Brain.
func (a App) SetSetting(t Target, key string, v any, common bool, r Reporter) error {
	if key == "" || strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") {
		return fmt.Errorf("setting %q must be a key such as model or permissions.deny", key)
	}
	l, err := a.load()
	if err != nil {
		return err
	}
	k := domain.Undotted(key)
	path := strings.Split(k, domain.Sep)
	why := domain.Sensitive(k, v)
	if common {
		if why != "" {
			return fmt.Errorf("%s %s, so it cannot go in common: set it per project, where it goes to the local overlay", key, why)
		}
		old := domain.Show(domain.Leaves(l.m.Common), k)
		l.m.Common = domain.SetPath(l.m.Common, path, v)
		if err := a.Brain.Save(l.m); err != nil {
			return err
		}
		r.Add(Event{Kind: EvSetting, Path: domain.ManifestFile + " (common)", Key: key, Local: old, Brain: domain.Show(domain.Leaves(l.m.Common), k)})
		return nil
	}
	project, p, err := a.project(l, t)
	if err != nil {
		return err
	}
	if p.Surface == domain.ClaudeAI {
		return fmt.Errorf("project %s is a claude.ai account, which takes skills only", project)
	}
	if why != "" {
		overlay, _, err := a.Machine.Overlay(project)
		if err != nil {
			return err
		}
		file, err := a.Machine.SaveOverlay(project, domain.SetPath(overlay, path, v))
		if err != nil {
			return err
		}
		r.Add(Event{Kind: EvOverlay, Project: project, Key: key, Path: file, Detail: why})
		return nil
	}
	old := domain.Show(domain.Leaves(p.Settings), k)
	p.Settings = domain.SetPath(p.Settings, path, v)
	if err := a.Brain.Save(l.m); err != nil {
		return err
	}
	r.Add(Event{Kind: EvSetting, Path: domain.ManifestFile + " (project " + project + ")", Key: key, Local: old, Brain: domain.Show(domain.Leaves(p.Settings), k)})
	return nil
}

// RemoveSetting removes a key, or with key=value one item of a list, from
// the target project, from common, or from common and every project.
func (a App) RemoveSetting(t Target, arg string, common, all bool, r Reporter) error {
	l, err := a.load()
	if err != nil {
		return err
	}
	key, raw, hasValue := strings.Cut(arg, "=")
	var item any
	if hasValue {
		item = domain.ParseValue(raw)
	}
	path := strings.Split(domain.Undotted(key), domain.Sep)
	type target struct {
		name     string // "" for common
		settings *map[string]any
	}
	var targets []target
	if all || common {
		targets = append(targets, target{"", &l.m.Common})
	}
	if all {
		for _, name := range domain.SortedKeys(l.m.Projects) {
			targets = append(targets, target{name, &l.m.Projects[name].Settings})
		}
	} else if !common {
		name, p, err := a.project(l, t)
		if err != nil {
			return err
		}
		targets = append(targets, target{name, &p.Settings})
	}
	removed := 0
	for _, tg := range targets {
		if !domain.RemovePath(*tg.settings, path, item, hasValue) {
			continue
		}
		if len(*tg.settings) == 0 {
			*tg.settings = nil
		}
		removed++
		r.Add(Event{Kind: EvDropped, Project: tg.name, Item: "setting " + arg})
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
	return a.Brain.Save(l.m)
}

// ListSettings lists the target project's effective settings, each with the
// layers that give it.
func (a App) ListSettings(t Target, r Reporter) error {
	l, err := a.load()
	if err != nil {
		return err
	}
	project, p, err := a.project(l, t)
	if err != nil {
		return err
	}
	cmd, err := a.Self.Command()
	if err != nil {
		return err
	}
	overlay, _, err := a.Machine.Overlay(project)
	if err != nil {
		return err
	}
	layers := []struct {
		name string
		l    map[string]any
	}{
		{"common", domain.Leaves(l.m.Common)},
		{"project", domain.Leaves(p.Settings)},
		{"nerve: plugins, hooks, autosync", domain.Leaves(domain.Generated(l.m, p, cmd))},
		{"local overlay", domain.Leaves(overlay)},
	}
	final := domain.Leaves(domain.Want(l.m, p, cmd, overlay, a.Brain.Root()))
	for _, k := range domain.SortedKeys(final) {
		var from []string
		for _, layer := range layers {
			if _, ok := layer.l[k]; ok {
				from = append(from, layer.name)
			}
		}
		r.Add(Event{Kind: EvValue, Project: project, Key: domain.Dotted(k), Brain: domain.Show(final, k), Detail: strings.Join(from, " + ")})
	}
	if len(final) == 0 {
		r.Add(Event{Kind: EvOK, Detail: "project " + project + " has no settings"})
	}
	return nil
}
