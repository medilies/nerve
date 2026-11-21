package infra

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// BrainCheckout is a Brain checked out on disk, its manifest in brain.json.
type BrainCheckout struct {
	root string
}

// NewBrainCheckout is the Brain checked out at root.
func NewBrainCheckout(root string) BrainCheckout { return BrainCheckout{root: root} }

var _ app.Brain = BrainCheckout{}

func (b BrainCheckout) Root() string { return b.root }

// manifestFile is brain.json as written.
type manifestFile struct {
	Allowed  []string                `json:"allowedSettings,omitempty"`
	Sources  []sourceEntry           `json:"sources,omitempty"`
	Hooks    map[string]hookEntry    `json:"hooks,omitempty"`
	Common   *commonEntry            `json:"common,omitempty"`
	Projects map[string]projectEntry `json:"projects,omitempty"`
}

type sourceEntry struct {
	Repo   string   `json:"repo"`
	SHA    string   `json:"sha"`
	Path   string   `json:"path,omitempty"`
	Skills []string `json:"skills"`
}

type hookEntry struct {
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
}

type commonEntry struct {
	Settings map[string]any `json:"settings,omitempty"`
}

type projectEntry struct {
	Skills   []string       `json:"skills,omitempty"`
	Agents   []string       `json:"agents,omitempty"`
	Hooks    []string       `json:"hooks,omitempty"`
	Plugins  []string       `json:"plugins,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

func (b BrainCheckout) Load() (domain.Manifest, error) {
	var f manifestFile
	err := readStrict(filepath.Join(b.root, domain.ManifestFile), &f)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Manifest{}, fmt.Errorf("%s has no %s: a Brain is a git repo with %[2]s at its top; make one there, or run nerve init on the folder that holds yours", b.root, domain.ManifestFile)
	}
	if err != nil {
		return domain.Manifest{}, fmt.Errorf("%s: %w", domain.ManifestFile, err)
	}
	m := domain.Manifest{AllowedSettings: f.Allowed}
	for _, s := range f.Sources {
		m.Sources = append(m.Sources, domain.Source(s))
	}
	if len(f.Hooks) > 0 {
		m.Hooks = map[string]domain.Hook{}
		for name, h := range f.Hooks {
			m.Hooks[name] = domain.Hook(h)
		}
	}
	if f.Common != nil {
		m.Common = settings(f.Common.Settings)
	}
	m.Projects = map[string]*domain.Project{}
	for name, p := range f.Projects {
		m.Projects[name] = &domain.Project{
			Skills: p.Skills, Agents: p.Agents, Hooks: p.Hooks, Plugins: p.Plugins,
			Settings: settings(p.Settings),
		}
	}
	return m, m.Validate()
}

// settings is decoded settings with numbers as domain.Numbers makes them,
// nil when empty.
func settings(s map[string]any) map[string]any {
	if len(s) == 0 {
		return nil
	}
	domain.Numbers(s)
	return s
}

// Save writes brain.json in nerve's own layout, refusing what Load would.
func (b BrainCheckout) Save(m domain.Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	f := manifestFile{Allowed: m.AllowedSettings}
	for _, s := range m.Sources {
		f.Sources = append(f.Sources, sourceEntry(s))
	}
	if len(m.Hooks) > 0 {
		f.Hooks = map[string]hookEntry{}
		for name, h := range m.Hooks {
			f.Hooks[name] = hookEntry(h)
		}
	}
	if len(m.Common) > 0 {
		f.Common = &commonEntry{Settings: m.Common}
	}
	if len(m.Projects) > 0 {
		f.Projects = map[string]projectEntry{}
		for name, p := range m.Projects {
			f.Projects[name] = projectEntry{
				Skills: p.Skills, Agents: p.Agents, Hooks: p.Hooks, Plugins: p.Plugins,
				Settings: p.Settings,
			}
		}
	}
	path := filepath.Join(b.root, domain.ManifestFile)
	if err := writeJSON(path, f, anyone); err != nil {
		return fmt.Errorf("%s: %w", domain.ManifestFile, err)
	}
	return nil
}

// syncTimeout bounds the fetch of a sync.
var syncTimeout = 30 * time.Second

// isRepo says whether the Brain is a git repo of its own.
func (b BrainCheckout) isRepo() bool {
	_, err := os.Stat(filepath.Join(b.root, ".git"))
	return err == nil
}

// Why a Brain has no upstream to sync with.
var (
	errNoUpstream = errors.New("no upstream branch")
	errDetached   = errors.New("detached HEAD")
	errBranchGone = errors.New("its upstream branch is not on the remote")
)

// fetch brings the upstream's commits, and says why it cannot.
func (b BrainCheckout) fetch() error {
	// Pruned, so that a branch gone from the remote is gone here too.
	if _, err := gitWithin(syncTimeout, b.root, "fetch", "-q", "--prune"); err != nil {
		return errors.New("could not fetch: " + firstLine(err))
	}
	if _, err := git(b.root, "rev-parse", "-q", "--verify", "@{upstream}"); err != nil {
		return errBranchGone
	}
	return nil
}

// Sync makes the checkout equal to its upstream: it fetches, then resets the
// branch and its tracked files to the upstream, and says which local
// commits, changes and untracked files in the way it dropped. Other
// untracked files stay. It skips a checkout that is not a git repo, has a
// detached HEAD or no upstream, or whose remote it cannot reach or no longer
// has the branch, and says why.
func (b BrainCheckout) Sync(force bool, r app.Reporter) error {
	skip := func(why string) error {
		r.Add(app.Event{Kind: app.EvBrainSkipped, Path: b.root, Detail: why})
		return nil
	}
	if !b.isRepo() {
		return skip("it is not a git repo")
	}
	if _, _, err := b.upstream(); err != nil {
		return skip(err.Error())
	}
	if err := b.fetch(); err != nil {
		return skip(err.Error())
	}
	dropped, err := b.drops()
	if err != nil {
		return fmt.Errorf("sync %s: %w", b.root, err)
	}
	if dropped != "" && !force {
		return fmt.Errorf("the Brain at %s would drop %s: nerve push sends them, nerve sync -f drops them", b.root, dropped)
	}
	before := b.Head()
	if _, err := git(b.root, "reset", "-q", "--hard", "@{upstream}"); err != nil {
		return fmt.Errorf("sync %s: %w", b.root, err)
	}
	if after := b.Head(); after != before || dropped != "" {
		r.Add(app.Event{Kind: app.EvBrainSynced, Path: b.root, Target: after, Detail: dropped})
	}
	return nil
}

// drops says what a reset to the upstream would drop: local commits,
// changes to tracked files, and untracked files where the upstream has one.
func (b BrainCheckout) drops() (string, error) {
	var parts []string
	ours, err := b.commits("@{upstream}..HEAD")
	if err != nil {
		return "", err
	}
	if ours != "" {
		parts = append(parts, strings.Replace(ours, "commit", "local commit", 1))
	}
	out, err := git(b.root, "status", "--porcelain", "-z", "--untracked-files=no", "--no-renames")
	if err != nil {
		return "", err
	}
	var changed []string
	for _, entry := range strings.Split(out, "\x00") {
		if len(entry) > 3 {
			changed = append(changed, entry[3:])
		}
	}
	if len(changed) > 0 {
		parts = append(parts, "changes to "+named(changed, "file", ""))
	}
	inTheWay, err := b.untrackedInTheWay()
	if err != nil {
		return "", err
	}
	if len(inTheWay) > 0 {
		parts = append(parts, named(inTheWay, "untracked file", " the remote has"))
	}
	switch len(parts) {
	case 0:
		return "", nil
	case 1:
		return parts[0], nil
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1], nil
}

// untrackedInTheWay lists the untracked files, ignored ones included, that
// a reset to the upstream overwrites: one where the upstream has a file or
// a folder, or inside a folder where it has a file.
func (b BrainCheckout) untrackedInTheWay() ([]string, error) {
	out, err := git(b.root, "ls-files", "-z", "--others")
	if err != nil || out == "" {
		return nil, err
	}
	theirs, err := git(b.root, "ls-tree", "-r", "-z", "--name-only", "@{upstream}")
	if err != nil {
		return nil, err
	}
	files, folders := map[string]bool{}, map[string]bool{}
	for _, f := range strings.Split(theirs, "\x00") {
		files[f] = true
		for d := path.Dir(f); d != "."; d = path.Dir(d) {
			folders[d] = true
		}
	}
	var hit []string
	for _, f := range strings.Split(out, "\x00") {
		if f == "" {
			continue
		}
		in := files[f] || folders[f]
		for d := path.Dir(f); d != "." && !in; d = path.Dir(d) {
			in = files[d]
		}
		if in {
			hit = append(hit, f)
		}
	}
	return hit, nil
}

// named says how many things there are, then what, then names up to three.
func named(names []string, thing, what string) string {
	list := names
	more := ""
	if len(names) > 3 {
		list, more = names[:3], fmt.Sprintf(" and %d more", len(names)-3)
	}
	return count(fmt.Sprint(len(names)), thing) + what + " (" + strings.Join(list, ", ") + more + ")"
}

// Refresh brings the upstream's new commits by a fast-forward, so a
// command changes brain.json as the remote holds it. It refuses a checkout
// with changes to brain.json or commits its upstream lacks, which it would
// otherwise push or drop, showing them, and one whose upstream it cannot
// reach.
func (b BrainCheckout) Refresh(r app.Reporter) error {
	if !b.isRepo() {
		return fmt.Errorf("the Brain at %s is not a git repo: make it one, with a remote to push to", b.root)
	}
	if _, _, err := b.upstream(); err != nil {
		switch {
		case errors.Is(err, errNoUpstream):
			return fmt.Errorf("the Brain at %s has no upstream branch: give its branch one, with git push -u", b.root)
		case errors.Is(err, errDetached):
			return fmt.Errorf("the Brain at %s has a detached HEAD: check out its branch", b.root)
		}
		return fmt.Errorf("the Brain at %s: %w", b.root, err)
	}
	if err := b.fetch(); err != nil {
		if errors.Is(err, errBranchGone) {
			return fmt.Errorf("the Brain at %s: %w: nerve push makes it there", b.root, err)
		}
		return fmt.Errorf("the Brain at %s: %w: nothing changed", b.root, err)
	}
	theirs, err := b.theirs()
	if err != nil {
		return err
	}
	ours, err := b.ours()
	if err != nil {
		return err
	}
	if out, err := git(b.root, "diff", "HEAD", "--", domain.ManifestFile); err != nil && b.Head() != "" {
		return err
	} else if diff := trim(out); diff != "" || b.untracked(domain.ManifestFile) {
		if diff != "" {
			r.Add(app.Event{Kind: app.EvDiff, Item: domain.ManifestFile, Detail: diff})
		}
		return notPushed(fmt.Sprintf("%s in the Brain at %s has changes not pushed", domain.ManifestFile, b.root), ours, theirs)
	}
	if ours != "" {
		return notPushed(fmt.Sprintf("the Brain at %s has %s its remote lacks", b.root, ours), ours, theirs)
	}
	if err := b.fastForward(r); err != nil {
		return fmt.Errorf("the Brain cannot take its remote's commits, which change files changed here: %w", err)
	}
	return nil
}

// stagedMessage is a commit message for what is staged: it names a few
// files, or counts many and lists them below.
func (b BrainCheckout) stagedMessage() (string, error) {
	out, err := git(b.root, "diff", "--cached", "-M", "--name-status")
	if err != nil {
		return "", err
	}
	verbs := []string{"add", "change", "move", "remove"}
	done := map[string]string{"add": "added", "change": "changed", "move": "moved", "remove": "removed"}
	byVerb := map[string][]string{}
	var lines []string
	for _, l := range strings.Split(trim(out), "\n") {
		f := strings.Split(l, "\t")
		if len(f) < 2 {
			continue
		}
		verb, path, listed := "change", f[len(f)-1], f[len(f)-1]
		switch f[0][0] {
		case 'A', 'C':
			verb = "add"
		case 'D':
			verb = "remove"
		case 'R':
			verb, listed = "move", f[1]+" -> "+path
		}
		byVerb[verb] = append(byVerb[verb], path)
		lines = append(lines, verb+" "+listed)
	}
	var parts []string
	for _, v := range verbs {
		switch {
		case len(byVerb[v]) == 0:
		case len(lines) <= 3:
			parts = append(parts, v+" "+strings.Join(byVerb[v], ", "))
		default:
			parts = append(parts, fmt.Sprintf("%d %s", len(byVerb[v]), done[v]))
		}
	}
	if len(lines) <= 3 {
		return "nerve: " + strings.Join(parts, "; "), nil
	}
	return fmt.Sprintf("nerve: %d files: %s\n\n%s", len(lines), strings.Join(parts, ", "), strings.Join(lines, "\n")), nil
}

// notPushed refuses a Brain with work not pushed, said by what, and lists
// the commands that settle it: the remote's commits, theirs, leave a push
// that takes them first only while the Brain has none of its own, ours.
func notPushed(what, ours, theirs string) error {
	options := [][2]string{
		{"nerve push", "commit every change in the Brain, and push it"},
		{"nerve sync -f", "drop the Brain's changes and commits not pushed"},
	}
	if theirs != "" {
		what += ", and the remote has " + theirs + " this Brain lacks"
		options[0][1] = "take the remote's commits, then commit every change in the Brain and push it"
		options[1][1] = "take the remote's commits, and drop the Brain's not pushed"
	}
	if theirs != "" && ours != "" {
		options = [][2]string{options[1], {"nerve push -f", "overwrite the remote's commits with the Brain's"}}
	}
	var b strings.Builder
	b.WriteString(what + ".\nRun one of these, then this command again:")
	for _, o := range options {
		fmt.Fprintf(&b, "\n  %-29s %s", o[0], o[1])
	}
	return errors.New(b.String())
}

// fastForward takes the upstream's commits into a Brain that has none of
// its own, keeping its changes: git refuses one that would overwrite them.
func (b BrainCheckout) fastForward(r app.Reporter) error {
	if ours, err := b.ours(); err != nil || ours != "" {
		return fmt.Errorf("it has %s of its own", ours)
	}
	before := b.Head()
	if _, err := git(b.root, "merge", "-q", "--ff-only", "@{upstream}"); err != nil {
		return errors.New(gitReason(err))
	}
	if after := b.Head(); after != before {
		r.Add(app.Event{Kind: app.EvBrainSynced, Path: b.root, Target: after})
	}
	return nil
}

// gitReason is the line of git's output that says why it failed, a ref it
// refused or its error, with the files it lists under it; else its first
// line.
func gitReason(err error) string {
	lines := strings.Split(err.Error(), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		for _, mark := range []string{"! [", "error: ", "fatal: "} {
			if !strings.HasPrefix(line, mark) {
				continue
			}
			var files []string
			for _, f := range lines[i+1:] {
				if !strings.HasPrefix(f, "\t") {
					break
				}
				files = append(files, strings.TrimSpace(f))
			}
			if len(files) > 0 {
				line = strings.TrimSuffix(line, ":") + ": " + strings.Join(files, ", ")
			}
			return line
		}
	}
	return firstLine(err)
}

// OnRemote says whether the upstream holds rel, with no change to it here.
func (b BrainCheckout) OnRemote(rel string) bool {
	out, err := git(b.root, "ls-tree", "--name-only", "@{upstream}", "--", rel)
	if err != nil || trim(out) == "" {
		return false
	}
	out, err = git(b.root, "status", "--porcelain", "--", rel)
	return err == nil && trim(out) == ""
}

// untracked says whether git does not track rel, there or not.
func (b BrainCheckout) untracked(rel string) bool {
	out, err := git(b.root, "status", "--porcelain", "--", rel)
	return err == nil && strings.HasPrefix(out, "??")
}

// ours names the commits the Brain has that its upstream lacks, "" when
// none.
func (b BrainCheckout) ours() (string, error) { return b.commits("@{upstream}..HEAD") }

// theirs names the commits the upstream has that the Brain lacks, "" when
// none.
func (b BrainCheckout) theirs() (string, error) { return b.commits("HEAD..@{upstream}") }

// commits says how many commits a range holds, naming a few: "" when none.
// With no commits yet, HEAD has none of its own, and lacks all of another's.
func (b BrainCheckout) commits(between string) (string, error) {
	if b.Head() == "" {
		theirs, ok := strings.CutPrefix(between, "HEAD..")
		if !ok {
			return "", nil
		}
		between = theirs
	}
	out, err := git(b.root, "log", "--format=%h %s", between)
	if err != nil {
		return "", err
	}
	var subjects []string
	for _, line := range strings.Split(trim(out), "\n") {
		if line != "" {
			subjects = append(subjects, line)
		}
	}
	if len(subjects) == 0 {
		return "", nil
	}
	return named(subjects, "commit", ""), nil
}

// PushManifest commits brain.json alone with message, then pushes the
// branch to its upstream. Other changes stay as they are, uncommitted. When
// it cannot, brain.json and the branch go back to what the remote holds.
func (b BrainCheckout) PushManifest(message string, r app.Reporter) (err error) {
	remote, merge, err := b.upstream()
	if err != nil {
		return fmt.Errorf("cannot push %s: %w", b.root, err)
	}
	out, err := git(b.root, "status", "--porcelain", "--", domain.ManifestFile)
	if err != nil || trim(out) == "" {
		return err // brain.json as the remote has it: nothing to push
	}
	before := b.Head()
	defer func() {
		if err != nil && before != "" { // brain.json alone goes back: what the user staged stays
			_, _ = git(b.root, "reset", "-q", "--soft", before)
			_, _ = git(b.root, "reset", "-q", before, "--", domain.ManifestFile)
			_, _ = git(b.root, "checkout", "-q", before, "--", domain.ManifestFile)
		}
	}()
	if _, err := git(b.root, "add", "--", domain.ManifestFile); err != nil {
		return err
	}
	if _, err := git(b.root, "commit", "-q", "-m", message, "--", domain.ManifestFile); err != nil {
		return fmt.Errorf("cannot commit %s: %s", domain.ManifestFile, gitReason(err))
	}
	if _, err := git(b.root, "push", "-q", remote, "HEAD:"+merge); err != nil {
		return b.pushFailed(err)
	}
	r.Add(app.Event{Kind: app.EvPushed, Path: b.root, Target: b.Head(), Detail: message})
	return nil
}

// pushFailed says why a push failed: the remote moved since the Brain took
// its commits, or git's own reason.
func (b BrainCheckout) pushFailed(err error) error {
	if b.fetch() == nil {
		if theirs, terr := b.theirs(); terr == nil && theirs != "" {
			return fmt.Errorf("%w: it has %s", app.ErrRemoteMoved, theirs)
		}
	}
	return fmt.Errorf("cannot push: %s", gitReason(err))
}

// Push commits everything in the checkout with message, then pushes the
// branch to its upstream. It refuses when the remote has commits the Brain
// lacks, naming them, unless force says to overwrite them. With nothing to
// commit, it only pushes.
func (b BrainCheckout) Push(message string, force bool, r app.Reporter) error {
	if !b.isRepo() {
		return fmt.Errorf("%s is not a git repo: make it one, with a remote to push to", b.root)
	}
	remote, merge, err := b.upstream()
	if err != nil {
		return fmt.Errorf("cannot push %s: %w", b.root, err)
	}
	out, err := git(b.root, "status", "--porcelain")
	if err != nil {
		return err
	}
	changed := trim(out) != ""
	if !force {
		switch err := b.fetch(); {
		case errors.Is(err, errBranchGone): // a remote without the branch yet: nothing to overwrite
		case err != nil:
			return fmt.Errorf("cannot push %s: %w", b.root, err)
		default:
			theirs, err := b.theirs()
			if err != nil {
				return err
			}
			if theirs != "" {
				if err := b.fastForward(r); err != nil {
					return notPushed(fmt.Sprintf("the Brain at %s cannot take its remote's commits, as %v", b.root, err), "its own", theirs)
				}
			}
		}
	}
	committed := ""
	if changed {
		if _, err := git(b.root, "add", "-A"); err != nil {
			return err
		}
		if strings.TrimSpace(message) == "" {
			if message, err = b.stagedMessage(); err != nil {
				return err
			}
		}
		if _, err := git(b.root, "commit", "-q", "-m", message); err != nil {
			return fmt.Errorf("cannot commit the Brain: %s", gitReason(err))
		}
		committed, _, _ = strings.Cut(message, "\n")
	}
	args := []string{"push", "--porcelain", remote, "HEAD:" + merge}
	if force {
		args = append(args, "--force")
	}
	out, err = git(b.root, args...)
	if err != nil {
		if committed != "" {
			return fmt.Errorf("cannot push the Brain, as the remote may have moved: %s: your changes are committed here; nerve push sends them once the remote's are taken", gitReason(err))
		}
		return fmt.Errorf("cannot push the Brain, as the remote may have moved: %s", gitReason(err))
	}
	// A ref line starting with = is one the remote held already.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "=\t") {
			r.Add(app.Event{Kind: app.EvOK, Detail: fmt.Sprintf("the Brain at %s is its remote's already", app.Short(b.Head()))})
			return nil
		}
	}
	r.Add(app.Event{Kind: app.EvPushed, Path: b.root, Target: b.Head(), Detail: committed})
	return nil
}

// upstream is the remote and branch the checked-out branch tracks, or why
// there are none.
func (b BrainCheckout) upstream() (remote, merge string, err error) {
	// The full name: a tag of the same name would make the short one
	// heads/<branch>.
	branch, err := git(b.root, "symbolic-ref", "-q", "HEAD")
	switch {
	case exitCode(err) == 1: // 1: HEAD is not a branch
		return "", "", errDetached
	case err != nil:
		return "", "", fmt.Errorf("cannot read its git state: %s", firstLine(err))
	}
	// The branch's own config, not its tracking ref, which only a fetch
	// may make.
	name := strings.TrimPrefix(trim(branch), "refs/heads/")
	remote, rerr := git(b.root, "config", "branch."+name+".remote")
	merge, merr := git(b.root, "config", "branch."+name+".merge")
	for _, err := range []error{rerr, merr} {
		switch {
		case exitCode(err) == 1: // 1: the key is not set
			return "", "", errNoUpstream
		case err != nil:
			return "", "", fmt.Errorf("cannot read its git config: %s", firstLine(err))
		}
	}
	return trim(remote), trim(merge), nil
}

// count says n of a thing, as people do.
func count(n, thing string) string {
	if n == "1" {
		return "1 " + thing
	}
	return n + " " + thing + "s"
}

// Head is the Brain's commit, or "" when the Brain is no git repo of its
// own, even inside one.
func (b BrainCheckout) Head() string {
	if _, err := os.Stat(filepath.Join(b.root, ".git")); err != nil {
		return ""
	}
	out, err := git(b.root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return trim(out)
}

// State says what is not yet committed or pushed in the Brain, new files
// included: a new skill is one.
func (b BrainCheckout) State() string {
	if _, err := os.Stat(filepath.Join(b.root, ".git")); err != nil {
		return ""
	}
	out, err := git(b.root, "status", "--porcelain")
	if err != nil {
		return "cannot read its git state: " + firstLine(err)
	}
	var parts []string
	if trim(out) != "" {
		parts = append(parts, "uncommitted changes")
	}
	if b.Head() == "" {
		return strings.Join(append(parts, "no commits yet"), ", ")
	}
	// Against the upstream, else, on a branch with none, against every
	// remote branch. A Brain with no remote has nowhere to push.
	notPushed := []string{"@{upstream}..HEAD"}
	if _, err := git(b.root, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		notPushed = nil
		if remotes, err := git(b.root, "remote"); err == nil && trim(remotes) != "" {
			notPushed = []string{"HEAD", "--not", "--remotes"}
		}
	}
	if notPushed != nil {
		out, err := git(b.root, append([]string{"rev-list", "--count"}, notPushed...)...)
		switch n := trim(out); {
		case err != nil:
			parts = append(parts, "cannot count the commits not pushed: "+firstLine(err))
		case n == "1":
			parts = append(parts, "1 commit not pushed")
		case n != "0":
			parts = append(parts, n+" commits not pushed")
		}
	}
	return strings.Join(parts, ", ")
}

func (b BrainCheckout) OwnSkill(name string) (domain.Skill, bool, error) {
	dir := filepath.Join(b.root, "skills", name)
	if !fileExists(filepath.Join(dir, "SKILL.md")) {
		return domain.Skill{}, false, nil
	}
	s, err := readSkill(name, dir, "own")
	return s, true, err
}

// OwnSkills reads every folder in skills/, linked ones too, each with the
// error reading it.
func (b BrainCheckout) OwnSkills() ([]app.OwnSkill, error) {
	dir := filepath.Join(b.root, "skills")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var skills []app.OwnSkill
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		fi, err := os.Stat(path)
		switch {
		case err != nil && e.Type()&fs.ModeSymlink != 0:
			skills = append(skills, app.OwnSkill{Skill: domain.Skill{Name: e.Name(), Dir: path}, Err: errors.New("a link to a folder that is gone")})
			continue
		case err != nil || !fi.IsDir():
			continue
		}
		s, err := readSkill(e.Name(), path, "own")
		s.Name, s.Dir = e.Name(), path
		skills = append(skills, app.OwnSkill{Skill: s, Err: err})
	}
	return skills, nil
}

func (b BrainCheckout) AgentFile(name string) (string, bool) {
	path := filepath.Join(b.root, "agents", name+".md")
	return path, fileExists(path)
}

func (b BrainCheckout) Agents() ([]app.Agent, error) {
	entries, err := os.ReadDir(filepath.Join(b.root, "agents"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var agents []app.Agent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		f := filepath.Join(b.root, "agents", e.Name())
		rel, _ := filepath.Rel(b.root, f)
		ag := app.Agent{Name: strings.TrimSuffix(filepath.Base(f), ".md"), Rel: rel}
		ag.Meta, ag.Err = readFrontmatter(f)
		agents = append(agents, ag)
	}
	return agents, nil
}

// readSkill reads a skill folder's frontmatter.
func readSkill(name, dir, origin string) (domain.Skill, error) {
	meta, err := readFrontmatter(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return domain.Skill{}, fmt.Errorf("skill %s: %w", name, err)
	}
	return domain.Skill{Name: name, Dir: dir, Origin: origin, Meta: meta}, nil
}

func readFrontmatter(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return domain.ParseFrontmatter(filepath.Base(path), string(b))
}
