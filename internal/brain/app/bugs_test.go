package app_test

import (
	"os"
	"testing"
)

// openBug skips a test of a bug not fixed yet, unless NERVE_OPEN_BUGS is
// set. Fixing the bug deletes the call.
func openBug(t *testing.T, bug string) {
	t.Helper()
	if os.Getenv("NERVE_OPEN_BUGS") == "" {
		t.Skip("open bug: " + bug)
	}
}
