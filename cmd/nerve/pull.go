package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pullBrain fast-forwards the Brain's checkout from its upstream. It skips a
// checkout that is not a git repo, has no upstream, or has uncommitted
// changes, and fails when the checkout has diverged.
func pullBrain(brain string, r *report) error {
	if _, err := os.Stat(filepath.Join(brain, ".git")); err != nil {
		return nil
	}
	if _, err := git(brain, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		r.add(Event{Kind: evPullSkipped, Path: brain, Detail: "no upstream branch"})
		return nil
	}
	if out, err := git(brain, "status", "--porcelain"); err != nil || strings.TrimSpace(out) != "" {
		r.add(Event{Kind: evPullSkipped, Path: brain, Detail: "uncommitted changes"})
		return nil
	}
	before, _ := git(brain, "rev-parse", "HEAD")
	if _, err := git(brain, "pull", "-q", "--ff-only"); err != nil {
		return fmt.Errorf("pull %s: %w", brain, err)
	}
	if after, _ := git(brain, "rev-parse", "HEAD"); after != before {
		r.add(Event{Kind: evPulled, Path: brain, Target: strings.TrimSpace(after)})
	}
	return nil
}
