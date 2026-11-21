package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/medilies/nerve/internal/brain/domain"
)

// onePlace refuses a use case told to act in two places at once: a named
// project, common, or common and every project.
func onePlace(project string, common, all bool) error {
	n := 0
	for _, set := range []bool{project != "", common, all} {
		if set {
			n++
		}
	}
	if n > 1 {
		return errors.New("--project, --common and --all each say where to act: pass one")
	}
	return nil
}

// settingKey reads a key the way a user writes it.
func settingKey(key string) ([]string, error) {
	path, err := domain.ParseKey(key)
	if err != nil {
		return nil, fmt.Errorf("setting %q %w", key, err)
	}
	return path, nil
}

// SetSetting sets a value in the target project's entry, or with common in
// common. A setting the Brain does not allow goes to the project's local
// overlay instead.
func (a App) SetSetting(ref ProjectRef, key string, v any, common bool, r Reporter) error {
	return a.changeSetting(ref, key, v, common, false, r, func(obj map[string]any, path []string) (map[string]any, error) {
		obj, err := domain.SetPath(obj, path, v)
		if err != nil {
			return nil, fmt.Errorf("%s %w", key, err)
		}
		return obj, nil
	})
}

// AddSetting adds one item to the list at key in the target project's entry,
// or with common in common. A setting the Brain does not allow goes to the
// project's local overlay instead.
func (a App) AddSetting(ref ProjectRef, key string, item any, common bool, r Reporter) error {
	return a.changeSetting(ref, key, item, common, true, r, func(obj map[string]any, path []string) (map[string]any, error) {
		obj, added, err := domain.AddPath(obj, path, item)
		switch {
		case err != nil:
			return nil, fmt.Errorf("%s %w", key, err)
		case !added:
			return nil, errAlready
		}
		return obj, nil
	})
}

// errAlready is a list that holds the item already: nothing to save.
var errAlready = errors.New("already there")

// changeSetting changes the settings of a project, its overlay, or common,
// as change does to one of them, and reports it; item says change adds to a
// list.
func (a App) changeSetting(ref ProjectRef, key string, v any, common, item bool, r Reporter, change func(map[string]any, []string) (map[string]any, error)) error {
	if err := onePlace(ref.Project, common, false); err != nil {
		return err
	}
	path, err := settingKey(key)
	if err != nil {
		return err
	}
	if domain.HasNull(v) {
		return fmt.Errorf("%s cannot hold null: to remove a setting, use setting rm", key)
	}
	changeAny := change
	change = func(obj map[string]any, path []string) (map[string]any, error) {
		was := domain.Misfits(obj)
		obj, err := changeAny(obj, path)
		if err != nil {
			return nil, err
		}
		for _, f := range domain.Misfits(obj) {
			switch {
			case slices.Contains(was, f): // a value that did not fit before stays for nerve check to report
			case f.Detail == domain.ListOfText:
				return nil, fmt.Errorf("%s %s: nerve setting add adds one item", f.Key, f.Detail)
			default:
				return nil, fmt.Errorf("%s %s", f.Key, f.Detail)
			}
		}
		return obj, nil
	}
	l, err := a.fresh(r)
	if err != nil {
		return err
	}
	k := domain.LeafKey(path)
	shown := domain.Dotted(k)
	message := func(where string) string {
		if item {
			return fmt.Sprintf("add an item to %s in %s", shown, where)
		}
		return fmt.Sprintf("set %s in %s", shown, where)
	}
	why := l.m.SettingRefusal(k, v)
	report := func(where string, before, after map[string]any, err error) error {
		if errors.Is(err, errAlready) {
			r.Add(Event{Kind: EvOK, Detail: fmt.Sprintf("%s already holds %s in %s", shown, domain.ShowValue(v), where)})
			return nil
		}
		if err != nil {
			return err
		}
		r.Add(Event{Kind: EvSetting, Path: where, Key: shown, Local: showAt(before, path), Brain: showAt(after, path)})
		return nil
	}
	if common {
		switch {
		case why == "":
		case domain.WrittenByNerve(k):
			return fmt.Errorf("%s is %s: %s, or set it per project, where it goes to the local overlay", key, why, domain.ShareNerves)
		default:
			return fmt.Errorf("%s is %s: run nerve allow add %s, or set it per project, where it goes to the local overlay", key, why, domain.ShellWord(domain.Dotted(k)))
		}
		// Every overlay the change drops a value from reads, before any
		// change is pushed.
		for _, project := range domain.SortedKeys(l.m.Projects) {
			if _, _, err := a.Machine.Overlay(project); err != nil {
				return err
			}
		}
		before := l.m.Common
		after, err := change(copySettings(before), path)
		if err == nil && showAt(before, path) == showAt(after, path) {
			err = errAlready
		}
		if err == nil {
			l.m.Common = after
			err = a.pushManifest(l.m, message("common"), r)
		}
		saved := err == nil
		if err := report(domain.ManifestFile+" (common)", before, after, err); err != nil || !saved {
			return err
		}
		for _, project := range domain.SortedKeys(l.m.Projects) {
			if err := a.dropOverlaid(project, path, item, r); err != nil {
				return err
			}
		}
		return nil
	}
	project, p, err := a.project(l, ref)
	if err != nil {
		return err
	}
	if why != "" {
		overlay, file, err := a.Machine.Overlay(project)
		if err != nil {
			return err
		}
		after, err := change(copySettings(overlay), path)
		if err == nil {
			file, err = a.Machine.SaveOverlay(project, after)
		}
		if err != nil {
			return report(file, nil, nil, err)
		}
		if domain.WrittenByNerve(k) {
			why += ": " + domain.ShareNerves
		} else {
			why += ": run nerve allow add " + domain.ShellWord(domain.Dotted(k)) + " to share it"
		}
		r.Add(Event{Kind: EvOverlay, Project: project, Key: shown, Path: file, Detail: why})
		return nil
	}
	if _, _, err := a.Machine.Overlay(project); err != nil { // read before any change is pushed
		return err
	}
	before := p.Settings
	after, err := change(copySettings(before), path)
	if err == nil && showAt(before, path) == showAt(after, path) {
		err = errAlready
	}
	if err == nil {
		p.Settings = after
		err = a.pushManifest(l.m, message("project "+project), r)
	}
	saved := err == nil
	if err := report(domain.ManifestFile+" (project "+project+")", before, after, err); err != nil || !saved {
		return err
	}
	return a.dropOverlaid(project, path, item, r)
}

// dropOverlaid takes out of a project's local overlay the value that would
// win over one just set in the Brain at path: a value at path or above it,
// except a list that a list item joins.
func (a App) dropOverlaid(project string, path []string, item bool, r Reporter) error {
	overlay, _, err := a.Machine.Overlay(project)
	if err != nil {
		return err
	}
	at := overlay
	for i, key := range path {
		v, ok := at[key]
		if !ok {
			return nil
		}
		sub, isObj := v.(map[string]any)
		_, isList := v.([]any)
		last := i == len(path)-1
		switch {
		case !last && isObj:
			at = sub
			continue
		case last && item && isList:
			return nil
		}
		domain.RemovePath(overlay, path[:i+1], nil, false)
		if len(overlay) == 0 {
			_, err = a.Machine.DropOverlay(project)
		} else {
			_, err = a.Machine.SaveOverlay(project, overlay)
		}
		if err != nil {
			return err
		}
		r.Add(Event{Kind: EvDropped, Project: project, Item: "setting " + domain.Dotted(domain.LeafKey(path[:i+1])), Detail: "local overlay"})
		return nil
	}
	return nil
}

// showAt shows the value at a path of keys in settings, an object included,
// as JSON, or "unset".
func showAt(settings map[string]any, path []string) string {
	var v any = settings
	for _, key := range path {
		o, _ := v.(map[string]any)
		x, ok := o[key]
		if !ok {
			return "unset"
		}
		v = x
	}
	return domain.ShowValue(v)
}

// copySettings deep-copies settings, so a change that fails leaves them
// as they were.
func copySettings(s map[string]any) map[string]any {
	if s == nil {
		return nil
	}
	return domain.Unflatten(domain.Leaves(s))
}

// RemoveSetting removes a key, or with key=value one item of a list, from
// the target project and its local overlay, from common, or from common,
// every project and their local overlays on this machine.
func (a App) RemoveSetting(ref ProjectRef, arg string, common, all bool, r Reporter) error {
	if err := onePlace(ref.Project, common, all); err != nil {
		return err
	}
	key, raw, hasValue := domain.CutItem(arg)
	path, err := settingKey(key)
	if err != nil {
		return err
	}
	var item any
	shown := domain.Dotted(domain.LeafKey(path))
	if hasValue {
		item = domain.ParseValue(raw)
		shown += "=" + raw
	}
	l, err := a.fresh(r)
	if err != nil {
		return err
	}
	type layer struct {
		project  string // "" for common
		overlay  bool
		settings *map[string]any
	}
	var layers []layer
	overlays := map[string]*map[string]any{}
	addProject := func(name string, p *domain.Project) error {
		layers = append(layers, layer{name, false, &p.Settings})
		o, _, err := a.Machine.Overlay(name)
		if err != nil {
			return err
		}
		overlays[name] = &o
		layers = append(layers, layer{name, true, &o})
		return nil
	}
	if all || common {
		layers = append(layers, layer{"", false, &l.m.Common})
	}
	if all {
		for _, name := range domain.SortedKeys(l.m.Projects) {
			if err := addProject(name, l.m.Projects[name]); err != nil {
				return err
			}
		}
	} else if !common {
		name, p, err := a.project(l, ref)
		if err != nil {
			return err
		}
		if err := addProject(name, p); err != nil {
			return err
		}
	}
	// Each change is reported once saved, so a save that fails leaves no
	// change unreported.
	var inBrain []Event
	inOverlay := map[string]Event{}
	for _, ly := range layers {
		if *ly.settings == nil || !domain.RemovePath(*ly.settings, path, item, hasValue) {
			continue
		}
		if len(*ly.settings) == 0 {
			*ly.settings = nil
		}
		e := Event{Kind: EvDropped, Project: ly.project, Item: "setting " + shown}
		if ly.overlay {
			e.Detail = "local overlay"
			inOverlay[ly.project] = e
		} else {
			inBrain = append(inBrain, e)
		}
	}
	if len(inBrain)+len(inOverlay) == 0 {
		where := "common"
		switch {
		case all:
			where = "project nor common"
		case !common:
			where = "project " + layers[0].project
		}
		return fmt.Errorf("no %s holds %s", where, shown)
	}
	if len(inBrain) > 0 {
		where := "common"
		switch {
		case all:
			where = "common and every project"
		case !common:
			where = "project " + layers[0].project
		}
		if err := a.pushManifest(l.m, fmt.Sprintf("remove %s from %s", shown, where), r); err != nil {
			return err
		}
		for _, e := range inBrain {
			r.Add(e)
		}
	}
	for _, name := range domain.SortedKeys(inOverlay) {
		o := *overlays[name]
		var err error
		if o == nil {
			_, err = a.Machine.DropOverlay(name)
		} else {
			_, err = a.Machine.SaveOverlay(name, o)
		}
		if err != nil {
			return err
		}
		r.Add(inOverlay[name])
	}
	return nil
}
