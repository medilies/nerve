package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// state is what sync last did to one working tree, kept in the machine's
// state folder, never in the project, so nerve removes only what it made and
// can tell edits made outside the Brain.
type state struct {
	Path     string            `json:"path"`
	Project  string            `json:"project,omitempty"`
	Links    map[string]string `json:"links,omitempty"`    // link, relative to the tree -> what it carries, as kind:name
	Settings map[string]any    `json:"settings,omitempty"` // settings.local.json as nerve last meant it
	Commit   string            `json:"commit,omitempty"`   // the Brain commit last synced
}

func statePath(name string) (string, error) {
	dir, err := machineDir()
	return filepath.Join(dir, "state", name+".json"), err
}

func treeStatePath(dir string) (string, error) {
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	return statePath(hex.EncodeToString(sum[:8]))
}

func loadState(dir string) (state, error) {
	st := state{Path: dir}
	path, err := treeStatePath(dir)
	if err != nil {
		return st, err
	}
	if err := readJSON(path, &st); err != nil {
		return st, err
	}
	if st.Links == nil {
		st.Links = map[string]string{}
	}
	return st, nil
}

func saveState(st state) error {
	path, err := treeStatePath(st.Path)
	if err != nil {
		return err
	}
	return writeJSON(path, st)
}

func dropState(dir string) error {
	path, err := treeStatePath(dir)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// readJSON reads path into v, leaving v alone when the file is missing.
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err == nil {
		err = json.Unmarshal(b, v)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFile(path, append(b, '\n'))
}
