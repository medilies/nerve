package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// marketplaceFile lists a Brain's plugins: its own, kept in the Brain, and
// third-party ones pinned to a commit.
const marketplaceFile = ".claude-plugin/marketplace.json"

// Marketplace is what nerve reads of marketplace.json; Claude Code reads and
// validates the rest.
type Marketplace struct {
	Name    string   `json:"name"`
	Plugins []Plugin `json:"plugins"`
}

type Plugin struct {
	Name   string       `json:"name"`
	Source PluginSource `json:"source"`
}

// PluginSource is a folder in the Brain (Path), or a git repo pinned to a
// commit (github, url or git-subdir).
type PluginSource struct {
	Path string `json:"-"`
	Repo string `json:"repo"` // github: owner/name
	URL  string `json:"url"`  // url and git-subdir: a git URL, or owner/name on GitHub
	Dir  string `json:"path"` // git-subdir: the plugin's folder in the repo
	SHA  string `json:"sha"`
}

// UnmarshalJSON reads a source written as a path or as an object.
func (s *PluginSource) UnmarshalJSON(b []byte) error {
	if json.Unmarshal(b, &s.Path) == nil {
		return nil
	}
	type fields PluginSource // without this method
	return json.Unmarshal(b, (*fields)(s))
}

// folder is the plugin's folder in its repo, "" for the whole repo.
func (s PluginSource) folder() string { return strings.Trim(path.Clean("/"+s.Dir), "/") }

// repo names the repo a pinned source comes from: owner/name on GitHub, else
// its URL.
func (s PluginSource) repo() string {
	if s.Repo != "" {
		return s.Repo
	}
	return repoName(s.URL)
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// repoName drops what spellings of one repo differ by: the GitHub host and a
// .git suffix.
func repoName(repo string) string {
	return strings.TrimSuffix(strings.TrimPrefix(repo, "https://github.com/"), ".git")
}

// repoURL is where git fetches a repo named by repoName.
func repoURL(repo string) string {
	if repoPattern.MatchString(repo) {
		return "https://github.com/" + repo + ".git"
	}
	return repo
}

// loadMarketplace reads the Brain's marketplace, or returns none when it has
// no plugins.
func loadMarketplace(root string) (Marketplace, error) {
	var mk Marketplace
	b, err := os.ReadFile(filepath.Join(root, marketplaceFile))
	if errors.Is(err, fs.ErrNotExist) {
		return mk, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &mk)
	}
	if err != nil {
		return mk, fmt.Errorf("%s: %w", marketplaceFile, err)
	}
	return mk, nil
}

// marketplaceProblems lists what keeps the Brain's skills from reaching a
// config as the Brain means: own skills outside a plugin, a third-party plugin
// not pinned to a commit that holds it, a missing own plugin, and own skills
// that break the skill rules.
func marketplaceProblems(root string, mk Marketplace) ([]Event, error) {
	var p []Event
	if _, err := os.Stat(filepath.Join(root, "skills")); err == nil {
		p = append(p, Event{Kind: evProblem, Path: "skills", Detail: "no config gets skills from here: move them into a plugin, such as plugins/brain/skills/"})
	}
	remote := map[string][]Plugin{} // repo -> its pinned plugins
	for _, pl := range mk.Plugins {
		src := pl.Source
		if src.Path == "" {
			if !shaPattern.MatchString(src.SHA) {
				p = append(p, Event{Kind: evProblem, Path: marketplaceFile,
					Detail: pl.Name + ": a plugin from outside the Brain must be a git source pinned to a full 40-character lowercase commit sha"})
				continue
			}
			remote[src.repo()] = append(remote[src.repo()], pl)
			continue
		}
		if _, err := os.Stat(filepath.Join(root, src.Path)); err != nil {
			p = append(p, Event{Kind: evProblem, Path: marketplaceFile, Detail: fmt.Sprintf("%s: no folder %s", pl.Name, src.Path)})
			continue
		}
		skills, err := ownSkills(filepath.Join(root, src.Path, "skills"))
		if err != nil {
			return nil, err
		}
		for _, s := range skills {
			rel, _ := filepath.Rel(root, s.Dir)
			for _, msg := range s.problems() {
				p = append(p, Event{Kind: evProblem, Path: rel, Detail: msg})
			}
		}
	}
	for _, repo := range slices.Sorted(maps.Keys(remote)) {
		p = append(p, pinProblems(repo, remote[repo])...)
	}
	return p, nil
}

// pinProblems lists the plugins from repo whose pin cannot be fetched or lacks
// their folder.
func pinProblems(repo string, plugins []Plugin) []Event {
	var shas []string
	for _, pl := range plugins {
		if !slices.Contains(shas, pl.Source.SHA) {
			shas = append(shas, pl.Source.SHA)
		}
	}
	dir, err := fetchCommits(repoURL(repo), shas...)
	if err != nil {
		return []Event{{Kind: evProblem, Path: marketplaceFile, Detail: err.Error()}}
	}
	defer os.RemoveAll(dir)
	var p []Event
	for _, pl := range plugins {
		if !holds(dir, pl.Source.SHA, pl.Source.folder()) {
			p = append(p, Event{Kind: evProblem, Path: marketplaceFile, Detail: fmt.Sprintf("%s: no %s at %s", pl.Name, pl.Source.folder(), pl.Source.SHA[:7])})
		}
	}
	return p
}

// holds reports whether commit sha, fetched into the repo at dir, has folder.
func holds(dir, sha, folder string) bool {
	_, err := git(dir, "cat-file", "-e", sha+":"+folder)
	return err == nil
}

// unlistedPlugins lists the plugins of the Brain's marketplace that a
// settings layer enables but the marketplace does not list.
func (mk Marketplace) unlistedPlugins(rel string, layer map[string]any) []Event {
	enabled, _ := layer["enabledPlugins"].(map[string]any)
	var p []Event
	for _, id := range slices.Sorted(maps.Keys(enabled)) {
		name, market, ok := strings.Cut(id, "@")
		if !ok || market != mk.Name || slices.ContainsFunc(mk.Plugins, func(pl Plugin) bool { return pl.Name == name }) {
			continue
		}
		p = append(p, Event{Kind: evProblem, Path: rel, Key: "enabledPlugins", Detail: fmt.Sprintf("enables %s, which %s does not list", id, marketplaceFile)})
	}
	return p
}
