package infra

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	dir, err := findSkill(tree, name)
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

func (SourceCache) Diff(repo, name, from, to string) (string, error) {
	oldTree, err := fetch(repo, from)
	if err != nil {
		return "", err
	}
	newTree, err := fetch(repo, to)
	if err != nil {
		return "", err
	}
	oldDir, err := findSkill(oldTree, name)
	if err != nil {
		return "", err
	}
	newDir, err := findSkill(newTree, name)
	if err != nil {
		return "", fmt.Errorf("at %s: %w; drop it from %s first", to[:7], err, domain.ManifestFile)
	}
	var out bytes.Buffer
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", oldDir, newDir)
	cmd.Stdout = &out
	var exit *exec.ExitError
	if err := cmd.Run(); err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) { // 1: they differ
		return "", err
	}
	return out.String(), nil
}

// fetch returns a checkout of repo at sha, cached under the user cache dir.
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
	// Each fetch in its own folder: two syncs may fetch the same pin at once.
	tmp, err := os.MkdirTemp(filepath.Dir(dir), sha+".tmp-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // a no-op once renamed
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", "--", repoURL(repo), sha},
		{"-c", "advice.detachedHead=false", "checkout", "-q", "FETCH_HEAD"},
	} {
		if _, err := git(tmp, args...); err != nil {
			return "", fmt.Errorf("fetch %s@%s: %w", repo, sha[:7], err)
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, serr := os.Stat(dir); serr == nil { // another fetch got there first
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

// findSkill finds a skill in a repo checkout: the whole repo when its root
// SKILL.md names it, else the one folder of that name holding a SKILL.md.
func findSkill(tree, name string) (string, error) {
	if meta, err := readFrontmatter(filepath.Join(tree, "SKILL.md")); err == nil && meta["name"] == name {
		return tree, nil
	}
	var found []string
	err := filepath.WalkDir(tree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() && d.Name() == name && fileExists(filepath.Join(path, "SKILL.md")) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no skill %q", name)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("skill %q is ambiguous: %v", name, found)
}
