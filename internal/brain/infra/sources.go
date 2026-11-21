package infra

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	tree, err := fetch(src.Repo, src.SHA)
	if err != nil {
		return domain.Skill{}, err
	}
	dir, err := findSkill(tree, src.Path, name)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("%s@%s: %w", src.Repo, src.SHA[:7], err)
	}
	return readSkill(name, dir, src.Repo)
}

func (SourceCache) Head(repo string) (string, error) {
	out, err := git("", "ls-remote", "--", repoURL(repo), "HEAD")
	if err != nil {
		return "", err
	}
	sha, _, _ := strings.Cut(out, "\t")
	if !domain.SHAPattern.MatchString(sha) {
		return "", fmt.Errorf("no HEAD for %s", repo)
	}
	return sha, nil
}

func (SourceCache) Diff(src domain.Source, name, to string) (string, error) {
	oldTree, err := fetch(src.Repo, src.SHA)
	if err != nil {
		return "", err
	}
	newTree, err := fetch(src.Repo, to)
	if err != nil {
		return "", err
	}
	oldDir, err := findSkill(oldTree, src.Path, name)
	if err != nil {
		return "", err
	}
	newDir, err := findSkill(newTree, src.Path, name)
	if err != nil {
		return "", fmt.Errorf("at %s: %w; drop it from %s first", to[:7], err, domain.ManifestFile)
	}
	// The source's own text only: no diff tool or text conversion of the
	// user's, and shown as visible says.
	out, err := git("", "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "--", oldDir, newDir)
	if err != nil && exitCode(err) != 1 { // 1: they differ
		return "", err
	}
	var b strings.Builder
	for _, r := range out {
		if b.Len() >= maxDiff {
			return b.String() + fmt.Sprintf("\n… the diff is cut at %d bytes: read the rest in %s\n", maxDiff, newDir), nil
		}
		writeVisible(&b, r)
	}
	return b.String(), nil
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

// maxDiff bounds the diff skill update shows, in bytes.
const maxDiff = 256 << 10

// fetch returns the files of repo at sha, cached under the user cache dir.
func fetch(repo, sha string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "nerve", "sources", repo, sha)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
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
	// The files only: a skill that is the whole repo carries no .git.
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, serr := os.Stat(dir); serr == nil { // another fetch got there first
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

// sweep removes the folders of fetches into dir that were killed: older
// than any fetch can run. dir is nerve's own, in its cache.
func sweep(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		tmp := filepath.Join(dir, e.Name())
		if fi, err := e.Info(); err == nil && e.IsDir() && strings.Contains(e.Name(), ".tmp-") && time.Since(fi.ModTime()) > time.Hour {
			_ = os.RemoveAll(tmp)
		}
	}
}

// findSkill finds a skill in a repo checkout, in the folder path names, or
// "" for the repo's root: that folder when its SKILL.md names the skill,
// else its folder of that name holding a SKILL.md. At the root, a skill may
// also be in skills/; it must be in one place only.
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
	const fix = "name the folder that holds it with path in its [[source]]"
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
