package domain

import (
	"path"
	"path/filepath"
)

// Machine is what one machine knows and the Brain never does: where the
// Brain is checked out, and which project each checkout belongs to.
type Machine struct {
	Brain     string
	Checkouts []Checkout
}

// Checkout is one project's working tree on this machine.
type Checkout struct {
	Path    string
	Project string
}

// CheckoutAt returns the checkout at dir, if listed.
func (m Machine) CheckoutAt(dir string) (Checkout, bool) {
	for _, c := range m.Checkouts {
		if filepath.Clean(c.Path) == filepath.Clean(dir) {
			return c, true
		}
	}
	return Checkout{}, false
}

// Tree is a git working tree: where nerve writes (Top), and the main
// checkout it belongs to (Main), which differs in a linked worktree.
type Tree struct {
	Top  string
	Main string
}

// TreeState is what a sync last did to one working tree, so nerve removes
// only what it made and can tell edits made outside the Brain.
type TreeState struct {
	Path     string
	Project  string
	Links    map[string]string // link, relative to the tree -> what it carries, as kind:name
	Settings map[string]any    // settings.local.json as nerve last meant it
	Commit   string            // the Brain commit last synced
}

// Link is one link a working tree should hold: a skill folder or an agent
// file, and what it points to.
type Link struct {
	Kind   string // "skill" or "agent"
	Name   string
	Target string
}

// Rel is where the link goes in the tree, its name after prefix.
func (l Link) Rel(prefix string) string {
	if l.Kind == "agent" {
		return path.Join(".claude", "agents", prefix+l.Name+".md")
	}
	return path.Join(".claude", "skills", prefix+l.Name)
}

// Carries is how a tree's state records what a link carries.
func (l Link) Carries() string { return l.Kind + ":" + l.Name }
