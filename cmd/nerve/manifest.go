package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const manifestFile = "brain.toml"

// Manifest is a Brain's brain.toml: its profiles, and which third-party
// skills it uses at which commit.
type Manifest struct {
	Profiles []string `toml:"profiles"`
	Sources  []Source `toml:"source"`
}

// Source is one third-party repo, pinned to a commit.
type Source struct {
	Repo   string   `toml:"repo"`
	SHA    string   `toml:"sha"`
	Skills []string `toml:"skills"`
}

var (
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func loadManifest(root string) (Manifest, error) {
	var m Manifest
	path := filepath.Join(root, manifestFile)
	meta, err := toml.DecodeFile(path, &m)
	if err != nil {
		return m, fmt.Errorf("%s: %w", manifestFile, err)
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		return m, fmt.Errorf("%s: unknown keys %v", manifestFile, keys)
	}
	for i, p := range m.Profiles {
		if !namePattern.MatchString(p) {
			return m, fmt.Errorf("%s: profile %q must be lowercase words joined by hyphens", manifestFile, p)
		}
		if slices.Contains(m.Profiles[:i], p) {
			return m, fmt.Errorf("%s: profile %q is listed twice", manifestFile, p)
		}
	}
	for _, s := range m.Sources {
		if !repoPattern.MatchString(s.Repo) {
			return m, fmt.Errorf("%s: repo %q is not owner/name", manifestFile, s.Repo)
		}
		if !shaPattern.MatchString(s.SHA) {
			return m, fmt.Errorf("%s: %s: sha must be a full 40-character lowercase commit", manifestFile, s.Repo)
		}
		if len(s.Skills) == 0 {
			return m, fmt.Errorf("%s: %s: no skills", manifestFile, s.Repo)
		}
	}
	return m, nil
}

// expandHome expands a leading ~ in path.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}
