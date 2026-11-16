package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const manifestFile = "skills.toml"

// Manifest is the desired state: which configs get skills, and which
// third-party skills are used at which commit.
type Manifest struct {
	Configs []string `toml:"configs"`
	Sources []Source `toml:"source"`
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
	if len(m.Configs) == 0 {
		return m, fmt.Errorf("%s: no configs", manifestFile)
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

// configDirs expands a leading ~ in each config path.
func (m Manifest) configDirs() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dirs := make([]string, len(m.Configs))
	for i, c := range m.Configs {
		if c == "~" || strings.HasPrefix(c, "~/") {
			c = filepath.Join(home, strings.TrimPrefix(c, "~"))
		}
		dirs[i] = c
	}
	return dirs, nil
}
