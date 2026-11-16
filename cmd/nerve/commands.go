package main

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// runCheck validates the manifest, the settings, the instructions and the
// plugins.
func runCheck(root string, r *report) error {
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	mk, err := loadMarketplace(root)
	if err != nil {
		return err
	}
	problems, err := marketplaceProblems(root, mk)
	if err != nil {
		return err
	}
	problems = append(problems, settingsProblems(root, m, mk)...)
	problems = append(problems, instructionsProblems(root, m)...)
	for _, e := range problems {
		r.add(e)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problems", len(problems))
	}
	r.add(Event{Kind: evOK, Detail: "no problems"})
	return nil
}

// runUpdate prints how each plugin pinned in repo differs between its pin and
// sha (default: the upstream tip), then moves the pins.
func runUpdate(root, repo, sha string, r *report) error {
	mk, err := loadMarketplace(root)
	if err != nil {
		return err
	}
	repo = repoName(repo)
	var pinned []Plugin
	for _, p := range mk.Plugins {
		if p.Source.Path != "" || p.Source.repo() != repo {
			continue
		}
		if !shaPattern.MatchString(p.Source.SHA) {
			return fmt.Errorf("%s: %s pins no full commit sha", marketplaceFile, p.Name)
		}
		pinned = append(pinned, p)
	}
	if len(pinned) == 0 {
		return fmt.Errorf("no plugin in %s comes from %s", marketplaceFile, repo)
	}
	if sha == "" {
		if sha, err = remoteHead(repoURL(repo)); err != nil {
			return err
		}
	}
	if !shaPattern.MatchString(sha) {
		return errors.New("sha must be a full 40-character lowercase commit")
	}
	moved := map[string]int{} // old pin -> plugins at it
	for _, p := range pinned {
		if p.Source.SHA != sha {
			moved[p.Source.SHA]++
		}
	}
	if len(moved) == 0 {
		r.add(Event{Kind: evAlreadyThere, Detail: repo, Target: sha})
		return nil
	}
	olds := slices.Sorted(maps.Keys(moved))
	dir, err := fetchCommits(repoURL(repo), append(olds, sha)...)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, p := range pinned {
		if p.Source.SHA == sha {
			continue
		}
		folder := p.Source.folder()
		if !holds(dir, sha, folder) {
			return fmt.Errorf("%s: no %s at %s; drop the plugin from %s first", p.Name, folder, sha[:7], marketplaceFile)
		}
		args := []string{"diff", "--no-color", p.Source.SHA, sha, "--"}
		if folder != "" {
			args = append(args, folder)
		}
		d, err := git(dir, args...)
		if err != nil {
			return err
		}
		r.add(Event{Kind: evDiff, Plugin: p.Name, Detail: d})
	}
	path := filepath.Join(root, marketplaceFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, old := range olds {
		if bytes.Count(b, []byte(old)) != moved[old] {
			return fmt.Errorf("%s: pin %s must appear once per plugin pinned at it", marketplaceFile, old)
		}
		b = bytes.ReplaceAll(b, []byte(old), []byte(sha))
	}
	if err := writeFile(path, b); err != nil {
		return err
	}
	r.add(Event{Kind: evPinned, Detail: repo, Target: sha})
	return nil
}
