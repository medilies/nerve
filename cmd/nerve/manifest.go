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

// Manifest is a Brain's brain.toml: the nerve release it is carried by (and,
// optionally, the SHA-256 of that release's checksums.txt) and its profiles.
type Manifest struct {
	Nerve          string   `toml:"nerve"`
	NerveChecksums string   `toml:"nerve-checksums"`
	Profiles       []string `toml:"profiles"`
}

var (
	shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
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
	if m.Nerve != "" && !tagPattern.MatchString(m.Nerve) {
		return m, fmt.Errorf("%s: nerve %q must be a release tag such as v1.2.3", manifestFile, m.Nerve)
	}
	if m.NerveChecksums != "" && (m.Nerve == "" || !sumPattern.MatchString(m.NerveChecksums)) {
		return m, fmt.Errorf("%s: nerve-checksums must be the lowercase SHA-256 of the pinned nerve release's checksums.txt", manifestFile)
	}
	for i, p := range m.Profiles {
		if !namePattern.MatchString(p) {
			return m, fmt.Errorf("%s: profile %q must be lowercase words joined by hyphens", manifestFile, p)
		}
		if slices.Contains(m.Profiles[:i], p) {
			return m, fmt.Errorf("%s: profile %q is listed twice", manifestFile, p)
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
