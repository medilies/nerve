package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// state is what sync last did to one config, kept in <config>/.nerve.json,
// so it removes only its own links and can tell edits made outside the Brain.
type state struct {
	Links       []string       `json:"links"`                 // skill links it made
	Settings    map[string]any `json:"settings,omitempty"`    // settings.json as last written
	Keybindings bool           `json:"keybindings,omitempty"` // whether it linked keybindings.json
}

const (
	stateFile       = ".nerve.json"
	legacyStateFile = "skills/.skills-sync.json" // links only, before .nerve.json
)

func loadState(cfg string) (state, error) {
	var st state
	path := filepath.Join(cfg, stateFile)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		path = filepath.Join(cfg, legacyStateFile)
		b, err = os.ReadFile(path)
		if os.IsNotExist(err) {
			return st, nil
		}
		if err == nil {
			err = json.Unmarshal(b, &st.Links)
		}
	} else if err == nil {
		err = json.Unmarshal(b, &st)
	}
	if err != nil {
		return st, fmt.Errorf("%s: %w", path, err)
	}
	return st, nil
}

func saveState(cfg string, st state) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(cfg, stateFile), append(b, '\n')); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(cfg, legacyStateFile)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
