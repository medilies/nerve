package infra

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// SourceCache fetches third-party repos from GitHub at a commit, into the
// user cache, once per commit.
type SourceCache struct{}

var _ app.Sources = SourceCache{}

func repoURL(repo string) string { return "https://github.com/" + repo + ".git" }

func (c SourceCache) Skill(src domain.Source, name string) (domain.Skill, error) {
	tree, err := fetch(src.Repo, src.SHA, false)
	if err != nil {
		return domain.Skill{}, err
	}
	dir, err := findSkill(tree, src.Path, name)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("%s@%s: %w", src.Repo, src.SHA[:7], err)
	}
	return readSkill(name, dir, src.Repo)
}

func (SourceCache) Tip(repo string) (string, error) {
	out, err := git("", "ls-remote", "--", repoURL(repo), "HEAD")
	if err != nil {
		return "", err
	}
	sha, _, _ := strings.Cut(out, "\t")
	if !domain.SHAPattern.MatchString(sha) {
		return "", fmt.Errorf("%s has no default branch", repo)
	}
	return sha, nil
}

func (SourceCache) Committed(repo, sha string) (time.Time, error) {
	dir, err := fetch(repo, sha, true)
	if err != nil {
		return time.Time{}, err
	}
	when, ok := readDate(dir + ".date")
	if !ok {
		return time.Time{}, fmt.Errorf("%s@%s: no date in the cache", repo, sha[:7])
	}
	return when, nil
}

// readDate reads the date file fetch writes beside a download.
func readDate(path string) (time.Time, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	when, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	return when, err == nil
}

func (SourceCache) Diff(src domain.Source, name, to string) (app.SkillDiff, error) {
	oldTree, err := fetch(src.Repo, src.SHA, true)
	if err != nil {
		return app.SkillDiff{}, err
	}
	newTree, err := fetch(src.Repo, to, true)
	if err != nil {
		return app.SkillDiff{}, err
	}
	oldDir, err := findSkill(oldTree, src.Path, name)
	if err != nil {
		return app.SkillDiff{}, fmt.Errorf("at its pin %s: %w; fix the pin in %s first", src.SHA[:7], err, domain.ManifestFile)
	}
	newDir, err := findSkill(newTree, src.Path, name)
	if err != nil {
		return app.SkillDiff{}, fmt.Errorf("at %s: %w; drop it from %s first", to[:7], err, domain.ManifestFile)
	}
	// The source's own text only: no diff tool or text conversion of the
	// user's, and shown as visible says.
	// Named from the cache folder both commits sit in, as <commit>/<path>:
	// the cache's own path tells the reader nothing.
	dir, before, after := "", oldDir, newDir
	if parent := filepath.Dir(oldTree); parent == filepath.Dir(newTree) {
		if a, err := filepath.Rel(parent, oldDir); err == nil {
			if b, err := filepath.Rel(parent, newDir); err == nil {
				dir, before, after = parent, a, b
			}
		}
	}
	diff := []string{"diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "--src-prefix=", "--dst-prefix="}
	stat, err := git(dir, append(diff, "--numstat", "-z", "--", before, after)...)
	if err != nil && exitCode(err) != 1 { // 1: they differ
		return app.SkillDiff{}, err
	}
	out, err := git(dir, append(diff, "--", before, after)...)
	if err != nil && exitCode(err) != 1 {
		return app.SkillDiff{}, err
	}
	d := app.SkillDiff{Files: numstat(stat, before, after), Dir: newDir}
	var b strings.Builder
	for _, r := range out {
		if b.Len() >= maxDiff {
			d.Text = b.String() + fmt.Sprintf("\n… the diff is cut at %d bytes: read the rest in %s\n", maxDiff, newDir)
			return d, nil
		}
		writeVisible(&b, r)
	}
	d.Text = b.String()
	return d, nil
}

// numstat reads git's --numstat -z output of a diff between the folders
// from and to: each file by its path in them, with its lines.
func numstat(out, from, to string) []app.FileChange {
	var files []app.FileChange
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		counts := strings.SplitN(fields[i], "\t", 3)
		if len(counts) < 3 {
			continue
		}
		path, in := counts[2], ""
		if path == "" && i+2 < len(fields) { // a pair: the old path, then the new
			path, in = fields[i+2], to
			if path == "/dev/null" { // removed
				path, in = fields[i+1], from
			}
			i += 2
		}
		if in != "" {
			path = strings.TrimPrefix(path, in+"/")
		} else if p, ok := strings.CutPrefix(path, from+"/"); ok {
			path = p
		} else {
			path = strings.TrimPrefix(path, to+"/")
		}
		path = strings.NewReplacer("\n", "<U+000A>", "\t", "<U+0009>").Replace(Visible(path)) // one line
		f := app.FileChange{Path: path, Binary: counts[0] == "-"}
		f.Added, _ = strconv.Atoi(counts[0])
		f.Removed, _ = strconv.Atoi(counts[1])
		files = append(files, f)
	}
	return files
}

// Visible is s with no control character that a terminal would act on, and
// each invisible one, such as a bidi override or a line separator, named:
// what a source's text or a folder name holds is shown as it is.
func Visible(s string) string {
	var b strings.Builder
	for _, r := range s {
		writeVisible(&b, r)
	}
	return b.String()
}

func writeVisible(b *strings.Builder, r rune) {
	switch {
	case r == '\n' || r == '\t':
		b.WriteRune(r)
	case unicode.IsControl(r):
	case unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp):
		fmt.Fprintf(b, "<U+%04X>", r)
	default:
		b.WriteRune(r)
	}
}

// maxDiff bounds the diff update shows, in bytes.
const maxDiff = 256 << 10

// fetch returns the files of repo at sha, cached under the user cache dir.
// dated asks for a download that knows when its commit was made: one from
// before nerve kept dates is downloaded again, and replaced once the new
// one is whole.
func fetch(repo, sha string, dated bool) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "nerve", "sources", repo, sha)
	date := dir + ".date" // when the commit was made, written once dir is in place
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	// One fetch of a commit at a time, so none reads a download another
	// replaces.
	unlock, err := lock(dir + ".lock")
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err := os.Stat(dir); err == nil && !dated {
		return dir, nil
	}
	if _, ok := readDate(date); ok && fileExists(dir) {
		return dir, nil
	}
	sweep(filepath.Dir(dir))
	// Each fetch in its own folder: two syncs may fetch the same pin at once.
	tmp, err := os.MkdirTemp(filepath.Dir(dir), sha+".tmp-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // a no-op once renamed
	for _, args := range [][]string{
		// No template: no hook of the user's lands in the repo.
		{"init", "-q", "--template="},
		{"fetch", "-q", "--depth", "1", "--", repoURL(repo), sha},
		// A link becomes a file holding its target: no link in a source
		// may reach out of it into the user's files.
		{"-c", "advice.detachedHead=false", "-c", "core.symlinks=false", "checkout", "-q", "FETCH_HEAD"},
	} {
		if args[0] == "-c" {
			// The files as committed: no filter of the user's, such as
			// git-lfs, runs on someone else's repo, and no line end changes.
			info := filepath.Join(tmp, ".git", "info")
			if err := os.MkdirAll(info, 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(info, "attributes"), []byte("* -filter -text\n"), 0o644); err != nil {
				return "", err
			}
		}
		// No hook of the user's runs on someone else's repo either.
		if _, err := git(tmp, append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...); err != nil {
			return "", fmt.Errorf("fetch %s@%s: %w", repo, sha[:7], err)
		}
	}
	when, err := git(tmp, "-c", "core.hooksPath=/dev/null", "show", "-s", "--format=%cI", "HEAD")
	if err != nil {
		return "", fmt.Errorf("fetch %s@%s: %w", repo, sha[:7], err)
	}
	// The files only: a skill that is the whole repo carries no .git.
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		return "", err
	}
	if _, ok := readDate(date); !ok {
		// A download with no date is set aside, as a killed fetch's
		// folder that sweep removes, so links into dir break only for
		// the moment the new one takes its place.
		old := tmp + "-old"
		if err := os.Rename(dir, old); err == nil {
			defer func() { _ = os.RemoveAll(old) }()
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		// A folder that stays there is a download of the same commit, by
		// a nerve that takes no lock, or one that could not be set aside.
		if _, serr := os.Stat(dir); serr != nil {
			return "", err
		}
	}
	// Whole or not at all: a reader never sees half a date.
	part := tmp + ".date"
	if err := os.WriteFile(part, []byte(strings.TrimSpace(when)+"\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(part, date); err != nil {
		_ = os.Remove(part)
		return "", err
	}
	return dir, nil
}

// lock holds the lock file at path until the returned func runs.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil // closing releases the lock
}

// sweep removes the folders and date files of fetches into dir that were
// killed: older than any fetch can run. dir is nerve's own, in its cache.
func sweep(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		tmp := filepath.Join(dir, e.Name())
		if fi, err := e.Info(); err == nil && strings.Contains(e.Name(), ".tmp-") && time.Since(fi.ModTime()) > time.Hour {
			_ = os.RemoveAll(tmp)
		}
	}
}

// findSkill finds a skill in a repo checkout, in the folder path names, or
// "" for the repo's root: that folder when its SKILL.md names the skill,
// else its folder of that name holding a SKILL.md. At the root, that folder
// may also be in skills/; a SKILL.md naming the skill wins over both, which
// must not both hold one.
func findSkill(tree, path, name string) (string, error) {
	base := filepath.Join(tree, filepath.FromSlash(path))
	meta, rootErr := readFrontmatter(filepath.Join(base, "SKILL.md"))
	if rootErr == nil && meta["name"] == name {
		return base, nil
	}
	unread := "" // a root SKILL.md that may be the skill
	if rootErr != nil && fileExists(filepath.Join(base, "SKILL.md")) {
		unread = "; its root SKILL.md cannot be read: " + rootErr.Error()
	}
	places := []string{name}
	if path == "" {
		places = []string{"skills/" + name, name}
	}
	var found []string
	for _, rel := range places {
		if fileExists(filepath.Join(base, filepath.FromSlash(rel), "SKILL.md")) {
			found = append(found, rel+"/")
		}
	}
	const fix = "name the folder that holds it with path in its entry of sources"
	switch {
	case len(found) == 1:
		return filepath.Join(base, filepath.FromSlash(found[0])), nil
	case len(found) > 1:
		return "", fmt.Errorf("skill %q is in both %s: %s", name, strings.Join(found, " and "), fix)
	case path != "":
		return "", fmt.Errorf("no skill %q in %s/%s/: %s%s", name, path, name, fix, unread)
	}
	return "", fmt.Errorf("no skill %q at the root, in skills/%s/ or in %s/: %s%s", name, name, name, fix, unread)
}
