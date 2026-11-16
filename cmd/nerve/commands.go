package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runCheck validates the manifest, that every pin resolves, and every skill.
func runCheck(root string, r *report) error {
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
			r.add(Event{Kind: evProblem, Skill: s.Name, Detail: p + " (" + s.Origin + ")"})
			bad++
		}
	}
	for _, e := range append(settingsProblems(root, m), instructionsProblems(root, m)...) {
		r.add(e)
		bad++
	}
	if bad > 0 {
		return fmt.Errorf("%d problems", bad)
	}
	r.add(Event{Kind: evOK, Detail: fmt.Sprintf("%d skills ok", len(skills))})
	return nil
}

// runUpdate prints how the source's pinned skills differ between the current
// pin and sha (default: the upstream tip), then moves the pin.
func runUpdate(root, repo, sha string, r *report) error {
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
		r.add(Event{Kind: evAlreadyThere, Detail: repo, Target: sha})
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
		d, err := diff(oldDir, newDir)
		if err != nil {
			return err
		}
		r.add(Event{Kind: evDiff, Skill: name, Detail: d})
	}
	path := filepath.Join(root, manifestFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Count(b, []byte(src.SHA)) != 1 {
		return fmt.Errorf("%s: pin %s must appear exactly once", manifestFile, src.SHA)
	}
	if err := writeFile(path, bytes.Replace(b, []byte(src.SHA), []byte(sha), 1)); err != nil {
		return err
	}
	r.add(Event{Kind: evPinned, Detail: repo, Target: sha})
	return nil
}

// diff returns a unified diff of two folders; git exits 1 when they differ.
func diff(a, b string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", a, b)
	cmd.Stdout, cmd.Stderr = &out, os.Stderr
	var exit *exec.ExitError
	if err := cmd.Run(); err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return "", err
	}
	return out.String(), nil
}
