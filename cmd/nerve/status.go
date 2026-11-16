package main

import "runtime/debug"

// version is set at release time; builds without it fall back to what the
// Go toolchain recorded, such as a go install tag or "(devel)".
var version string

func currentVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(devel)"
}

// runStatus says where this machine's Brain is, which profile each config
// follows, and which nerve is running.
func runStatus(r *report) error {
	m, err := loadMachine()
	if err != nil {
		r.add(Event{Kind: evVersion, Detail: currentVersion()})
		return err
	}
	b, err := loadManifest(m.Brain)
	r.add(Event{Kind: evVersion, Detail: currentVersion(), Target: b.Nerve})
	if err != nil {
		return err
	}
	r.add(Event{Kind: evBrain, Path: m.Brain})
	for _, c := range m.Configs {
		r.add(Event{Kind: evConfig, Path: c.Dir, Detail: c.Profile})
	}
	return nil
}
