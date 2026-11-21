package infra

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestSkillInstallsTheEmbeddedSkillWhereLinksPointTo(t *testing.T) {
	e := newTestEnv(t)
	n := Nerve{SkillMD: []byte(skillMD("nerve", ""))}

	s, err := n.Skill()

	if err != nil || s.Name != "nerve" || s.Dir != e.cache("skill/nerve") || e.read(filepath.Join(s.Dir, "SKILL.md")) != skillMD("nerve", "") {
		t.Errorf("got %+v (%v)", s, err)
	}
}

func TestSkillReplacesTheCachedCopyAtOnce(t *testing.T) {
	e := newTestEnv(t)
	older, newer := skillMD("nerve", "license: old\n"), skillMD("nerve", "license: new\n")
	s, err := Nerve{SkillMD: []byte(older)}.Skill()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(s.Dir, "SKILL.md")) // a session reading the skill
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if _, err := (Nerve{SkillMD: []byte(newer)}).Skill(); err != nil {
		t.Fatal(err)
	}

	if b, _ := io.ReadAll(f); string(b) != older {
		t.Errorf("a reader of the old copy saw %q, want it whole", b)
	}
	if got := e.read(filepath.Join(s.Dir, "SKILL.md")); got != newer {
		t.Errorf("the cached copy is %q", got)
	}
}

func TestSessionsStartingAtOnceReadAWholeSkill(t *testing.T) {
	e := newTestEnv(t)
	versions := []string{skillMD("nerve", "license: a\n"), skillMD("nerve", "license: b\nx: y\n"+strings.Repeat("  more\n", 2000))}
	path := e.cache("skill/nerve/SKILL.md")
	if _, err := (Nerve{SkillMD: []byte(versions[0])}).Skill(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 2 { // two nerves of different versions, as after an install
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := (Nerve{SkillMD: []byte(versions[(i+j)%2])}).Skill(); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	go func() { wg.Wait(); close(done) }()
	for {
		select {
		case <-done:
			return
		default:
		}
		if b, err := os.ReadFile(path); err != nil || !slices.Contains(versions, string(b)) {
			t.Fatalf("a session read %d bytes (%v), not a whole skill", len(b), err)
		}
	}
}

func TestCommandQuotesAPathTheShellWouldSplit(t *testing.T) {
	for path, want := range map[string]string{
		"/home/u/go/bin/nerve": "/home/u/go/bin/nerve status --hook",
		"/opt/my tools/nerve":  "'/opt/my tools/nerve' status --hook",
	} {
		n := Nerve{Path: func() (string, error) { return path, nil }}
		if got, _ := n.Command(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// printCommandEnv makes TestCommandKeepsTheLinkNerveRunsThrough, run again
// through a link, print the command and stop.
const printCommandEnv = "NERVE_TEST_PRINT_COMMAND"

func TestCommandKeepsTheLinkNerveRunsThrough(t *testing.T) {
	if os.Getenv(printCommandEnv) != "" {
		command, err := Nerve{Path: RanAs}.Command()
		fmt.Printf("command: %s (%v)\n", command, err)
		return
	}
	e := newTestEnv(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := e.path("bin/nerve") // as bin/nerve -> v1/nerve
	e.link(link, self)
	run := exec.Command(link, "-test.run=^TestCommandKeepsTheLinkNerveRunsThrough$")
	run.Env = append(os.Environ(), printCommandEnv+"=1")

	out, err := run.Output()

	if want := "command: " + link + " status --hook (<nil>)\n"; err != nil || !strings.Contains(string(out), want) {
		t.Errorf("got %q (%v), want %q", out, err, want)
	}
}
