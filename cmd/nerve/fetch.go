package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// fetchCommits returns a bare repo, in a new temp folder the caller removes,
// that holds the given commits of the repo at url.
func fetchCommits(url string, shas ...string) (string, error) {
	dir, err := os.MkdirTemp("", "nerve-")
	if err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"init", "-q", "--bare"},
		append([]string{"fetch", "-q", "--depth", "1", "--", url}, shas...),
	} {
		if _, err := git(dir, args...); err != nil {
			os.RemoveAll(dir)
			return "", fmt.Errorf("fetch %s: %w", url, err)
		}
	}
	return dir, nil
}

// remoteHead returns the commit at the tip of the default branch of the repo
// at url.
func remoteHead(url string) (string, error) {
	out, err := git("", "ls-remote", "--", url, "HEAD")
	if err != nil {
		return "", err
	}
	sha, _, _ := strings.Cut(out, "\t")
	if !shaPattern.MatchString(sha) {
		return "", fmt.Errorf("no HEAD for %s", url)
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
