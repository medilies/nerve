package app

import "github.com/medilies/nerve/internal/brain/domain"

// Brain is the Brain's checkout: its manifest, own skills and agents, and
// its git state.
type Brain interface {
	Root() string
	Load() (domain.Manifest, error)
	// Save writes the manifest in nerve's own layout.
	Save(domain.Manifest) error
	// MovePin replaces one pin in the manifest as written, touching nothing
	// else.
	MovePin(old, new string) error
	// Pull fast-forwards the checkout from its upstream, or says why not.
	Pull(Reporter) error
	// Head is the checked-out commit, "" outside git.
	Head() string
	// State says what the checkout holds that others cannot see yet.
	State() string
	OwnSkill(name string) (domain.Skill, bool, error)
	OwnSkills() ([]domain.Skill, error)
	AgentFile(name string) (string, bool)
	Agents() ([]Agent, error)
}

// Agent is an own agent file, as read.
type Agent struct {
	Name string
	Rel  string // relative to the Brain
	Meta map[string]string
	Err  error // reading its frontmatter
}

// Sources are third-party repos, fetched at a pin.
type Sources interface {
	// Skill finds a skill in a source at its pin.
	Skill(src domain.Source, name string) (domain.Skill, error)
	// Head is the tip of a repo's default branch.
	Head(repo string) (string, error)
	// Diff is how a skill differs between two commits of a repo.
	Diff(repo, name, from, to string) (string, error)
}

// Machine is what this machine keeps and the Brain never holds: the machine
// file, local overlays, and what nerve last did.
type Machine interface {
	Load() (domain.Machine, error)
	Save(domain.Machine) error
	// Create writes a new machine file, refusing to replace one, and
	// returns its path.
	Create(domain.Machine) (string, error)
	Overlay(project string) (settings map[string]any, path string, err error)
	SaveOverlay(project string, settings map[string]any) (path string, err error)
	// DropOverlay deletes a project's local overlay, returning its path, or
	// "" when it had none.
	DropOverlay(project string) (path string, err error)
	State(dir string) (domain.TreeState, error)
	SaveState(domain.TreeState) error
	DropState(dir string) error
	States() ([]domain.TreeState, error)
	// Built maps each claude.ai project to the hash of each skill zip last
	// built.
	Built() (map[string]map[string]string, error)
	SaveBuilt(map[string]map[string]string) error
}

// Trees are the working trees of checkouts on this machine, and git in
// them.
type Trees interface {
	// Locate finds the git working tree around dir.
	Locate(dir string) (domain.Tree, error)
	Exists(path string) bool  // follows links
	Present(path string) bool // a link counts, even a broken one
	// LinkTarget is where the link at path points, false when path is not
	// a link.
	LinkTarget(path string) (string, bool)
	// Link makes path a link to target, replacing a link already there, and
	// makes the folders on the way.
	Link(path, target string) error
	Remove(path string) error
	// RemoveEmptyFolder removes path if it is an empty folder, and says
	// whether it did.
	RemoveEmptyFolder(path string) (bool, error)
	Entries(dir string) []string
	// CheckIgnored refuses a tree whose settings.local.json git tracks or
	// does not ignore.
	CheckIgnored(dir string) error
	// SyncExclude keeps rels out of git through nerve's block in the
	// repo's info/exclude, which every worktree of the repo shares: rels
	// must cover them all. It refuses a block someone damaged.
	SyncExclude(dir string, rels []string) error
	ReadSettings(path string) (map[string]any, error)
	WriteSettings(path string, settings map[string]any) error
}

// Self is nerve itself: its own skill, how autosync runs it, and its
// version.
type Self interface {
	Skill() (domain.Skill, error)
	// Command is what autosync runs to sync the tree a session starts in.
	Command() (string, error)
	Version() string
}

// Zips are the skill zips built for claude.ai accounts.
type Zips interface {
	// Zip zips a skill folder. It refuses a link that leads to a folder,
	// to nothing, or out of the skill, which a zip cannot hold.
	Zip(domain.Skill) ([]byte, error)
	Write(project, name string, zip []byte) (path string, err error)
	Has(project, name string) bool
	Remove(project, name string)
}

// App carries a Brain through its ports.
type App struct {
	Brain   Brain
	Sources Sources
	Machine Machine
	Trees   Trees
	Self    Self
	Zips    Zips
}
