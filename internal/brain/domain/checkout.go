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

// Tree is a git working tree: where nerve writes (Top), the main checkout
// it belongs to (Main), which differs in a linked worktree, and the git
// folder its worktrees share (Repo), with their info/exclude.
type Tree struct {
	Top  string
	Main string
	Repo string
}

// TreeState is what a sync last did to one working tree, so nerve removes
// only what it made and can tell edits made outside the Brain.
type TreeState struct {
	Path     string
	Repo     string // the git folder whose info/exclude the tree shares
	Project  string
	Links    map[string]Link // link, relative to the tree -> what it carries and points to
	Folders  []string        // of TreeFolders, those nerve made
	Settings map[string]any  // settings.local.json as nerve last meant it
	Personal []string        // its personal settings already reported, as PersonalKeys names them
	Commit   string          // the Brain commit last synced
}

// TreeFolders are the folders nerve may make in a working tree, relative to
// it, deepest first.
var TreeFolders = []string{".claude/skills", ".claude/agents", ".claude"}

// Link is one link a working tree should hold: a skill folder or an agent
// file, and what it points to.
type Link struct {
	Kind   string // "skill" or "agent"
	Name   string
	Target string
}

// Rel is where the link goes in the tree.
func (l Link) Rel() string {
	if l.Kind == "agent" {
		return path.Join(".claude", "agents", l.Name+".md")
	}
	return path.Join(".claude", "skills", l.Name)
}

// Item is what a link carries, as people read it, such as "skill grilling".
func (l Link) Item() string { return l.Kind + " " + l.Name }
