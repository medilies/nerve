package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/medilies/nerve/internal/brain/domain"
)

// AllowSettings adds keys to the Brain's allowedSettings, so it may hold
// them, and pushes the change. A key nerve allows itself is refused: it
// needs no entry.
func (a App) AllowSettings(keys []string, r Reporter) error {
	l, err := a.fresh(r)
	if err != nil {
		return err
	}
	m := l.m
	var events []Event
	var added []string
	for _, key := range keys {
		path, err := domain.ParseKey(key)
		if err != nil {
			return fmt.Errorf("%s: allowedSettings: %q is not a settings key", domain.ManifestFile, key)
		}
		if domain.NerveAllows(path) {
			return fmt.Errorf("nerve allows %s itself: it needs no entry in allowedSettings", key)
		}
		if m.SettingRefusal(domain.LeafKey(path), "") == "" {
			events = append(events, Event{Kind: EvAllowed, Key: key, Detail: "already"})
			continue
		}
		// A key it covers needs no entry of its own any more.
		leaf := domain.LeafKey(path)
		m.AllowedSettings = slices.DeleteFunc(m.AllowedSettings, func(a string) bool {
			p, err := domain.ParseKey(a)
			if err != nil || !strings.HasPrefix(domain.LeafKey(p), leaf+domain.Sep) {
				return false
			}
			events = append(events, Event{Kind: EvDisallowed, Key: a})
			added = slices.DeleteFunc(added, func(x string) bool { return x == a })
			return true
		})
		m.AllowedSettings = append(m.AllowedSettings, key)
		if err := m.Validate(); err != nil {
			return err
		}
		added = append(added, key)
		events = append(events, Event{Kind: EvAllowed, Key: key})
	}
	if len(added) > 0 {
		if err := a.pushManifest(m, "allow "+strings.Join(added, ", "), r); err != nil {
			return err
		}
	}
	for _, e := range events {
		r.Add(e)
	}
	return nil
}

// RemoveAllowedSettings drops keys from the Brain's allowedSettings, and
// pushes the change. It refuses one whose settings the Brain still holds,
// and no other list allows.
func (a App) RemoveAllowedSettings(keys []string, r Reporter) error {
	l, err := a.fresh(r)
	if err != nil {
		return err
	}
	m := l.m
	m.AllowedSettings = slices.Clone(m.AllowedSettings)
	var events []Event
	for _, key := range keys {
		path, err := domain.ParseKey(key)
		if err != nil {
			return fmt.Errorf("%s: allowedSettings: %q is not a settings key", domain.ManifestFile, key)
		}
		same := func(a string) bool {
			p, err := domain.ParseKey(a)
			return err == nil && domain.LeafKey(p) == domain.LeafKey(path)
		}
		if !slices.ContainsFunc(m.AllowedSettings, same) {
			return fmt.Errorf("allowedSettings does not list %s", key)
		}
		m.AllowedSettings = slices.DeleteFunc(m.AllowedSettings, same)
		events = append(events, Event{Kind: EvDisallowed, Key: key})
	}
	if err := stillHeld(m); err != nil {
		return err
	}
	if err := a.pushManifest(m, "stop allowing "+strings.Join(keys, ", "), r); err != nil {
		return err
	}
	for _, e := range events {
		r.Add(e)
	}
	return nil
}

// stillHeld says which setting m holds that no list allows any more, and
// how to remove it first.
func stillHeld(m domain.Manifest) error {
	held := func(where, flag string, s map[string]any) error {
		l := domain.Leaves(s)
		for _, k := range domain.SortedKeys(l) {
			if m.SettingRefusal(k, l[k]) == domain.NotAllowed && !domain.WrittenByNerve(k) {
				return fmt.Errorf("%s%s holds it: nerve setting rm %s %s first", where, domain.Dotted(k), flag, domain.ShellWord(domain.Dotted(k)))
			}
		}
		return nil
	}
	if err := held("common.settings.", "--common", m.Common); err != nil {
		return err
	}
	for _, name := range domain.SortedKeys(m.Projects) {
		if err := held("projects."+name+".settings.", "--project "+name, m.Projects[name].Settings); err != nil {
			return err
		}
	}
	return nil
}
