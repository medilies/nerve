package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func repoURL(repo string) string { return "https://github.com/" + repo + ".git" }

// fetch returns a checkout of repo at sha, cached under the user cache dir.
func fetch(repo, sha string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "skills-sync", repo, sha)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	tmp := dir + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return "", err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", repoURL(repo), sha},
		{"-c", "advice.detachedHead=false", "checkout", "-q", "FETCH_HEAD"},
	} {
		if _, err := git(tmp, args...); err != nil {
			os.RemoveAll(tmp)
			return "", fmt.Errorf("fetch %s@%s: %w", repo, sha[:7], err)
		}
	}
	return dir, os.Rename(tmp, dir)
}

// remoteHead returns the commit at the tip of repo's default branch.
func remoteHead(repo string) (string, error) {
	out, err := git("", "ls-remote", repoURL(repo), "HEAD")
	if err != nil {
		return "", err
	}
	sha, _, _ := strings.Cut(out, "\t")
	if !shaPattern.MatchString(sha) {
		return "", fmt.Errorf("no HEAD for %s", repo)
	}
	return sha, nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", err
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}
