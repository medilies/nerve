package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// runCheck validates the manifest, that every pin resolves, and every skill.
func runCheck(root string, out io.Writer) error {
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	skills, err := loadSkills(root, m)
	if err != nil {
		return err
	}
	bad := 0
	for _, s := range skills {
		for _, p := range s.problems() {
			fmt.Fprintf(out, "%s (%s): %s\n", s.Name, s.Origin, p)
			bad++
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d problems", bad)
	}
	fmt.Fprintf(out, "%d skills ok\n", len(skills))
	return nil
}

// runUpdate prints how the source's pinned skills differ between the current
// pin and sha (default: the upstream tip), then moves the pin.
func runUpdate(root, repo, sha string, out io.Writer) error {
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	var src *Source
	for i := range m.Sources {
		if m.Sources[i].Repo == repo {
			src = &m.Sources[i]
		}
	}
	if src == nil {
		return fmt.Errorf("%s is not a source in %s", repo, manifestFile)
	}
	if sha == "" {
		if sha, err = remoteHead(repo); err != nil {
			return err
		}
	}
	if !shaPattern.MatchString(sha) {
		return errors.New("sha must be a full 40-character lowercase commit")
	}
	if sha == src.SHA {
		fmt.Fprintf(out, "%s is already pinned at %s\n", repo, sha)
		return nil
	}
	oldTree, err := fetch(repo, src.SHA)
	if err != nil {
		return err
	}
	newTree, err := fetch(repo, sha)
	if err != nil {
		return err
	}
	for _, name := range src.Skills {
		oldDir, err := findSkill(oldTree, name)
		if err != nil {
			return err
		}
		newDir, err := findSkill(newTree, name)
		if err != nil {
			return fmt.Errorf("at %s: %w; drop it from %s first", sha[:7], err, manifestFile)
		}
		if err := diff(oldDir, newDir, out); err != nil {
			return err
		}
	}
	path := filepath.Join(root, manifestFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Count(b, []byte(src.SHA)) != 1 {
		return fmt.Errorf("%s: pin %s must appear exactly once", manifestFile, src.SHA)
	}
	if err := os.WriteFile(path, bytes.Replace(b, []byte(src.SHA), []byte(sha), 1), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "pinned %s at %s: review the diff above, then commit %s and sync\n", repo, sha, manifestFile)
	return nil
}

// diff writes a unified diff of two folders; git exits 1 when they differ.
func diff(a, b string, out io.Writer) error {
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", a, b)
	cmd.Stdout, cmd.Stderr = out, os.Stderr
	var exit *exec.ExitError
	if err := cmd.Run(); err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return err
	}
	return nil
}
