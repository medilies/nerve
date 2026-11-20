package infra

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// release serves nerve's release tag holding bin, with its checksum (or
// sum), from a local server, and returns a nerve running version that
// upgrades from it, the stand-in binary it replaces, and its recorded
// re-runs with their environments.
func (e *testEnv) release(running, tag string, bin []byte, sum string) (Nerve, string, *[][]string) {
	e.t.Helper()
	asset := "nerve_" + runtime.GOOS + "_" + runtime.GOARCH
	if sum == "" {
		h := sha256.Sum256(bin)
		sum = hex.EncodeToString(h[:])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/" + tag + "/checksums.txt":
			_, _ = fmt.Fprintf(rw, "%s  %s\n", sum, asset)
		case "/" + tag + "/" + asset:
			_, _ = rw.Write(bin)
		default:
			http.NotFound(rw, req)
		}
	}))
	e.t.Cleanup(srv.Close)
	self := e.path("bin/nerve")
	e.write(self, "old binary")
	var reruns [][]string
	n := Nerve{Ver: running, Release: Release{
		URL:    srv.URL,
		Client: &http.Client{Timeout: time.Minute},
		Path:   func() (string, error) { return self, nil },
		Exec: func(path string, env []string) error {
			reruns = append(reruns, append([]string{path}, env...))
			return nil
		},
	}}
	return n, self, &reruns
}

func TestUpgradeReplacesNerveWithThePinAndRunsItInstead(t *testing.T) {
	e := newTestEnv(t)
	n, self, reruns := e.release("v0.1.0", "v0.2.0", []byte("new binary"), "")
	var r events

	if err := n.Upgrade(domain.Manifest{Nerve: "v0.2.0"}, &r); err != nil {
		t.Fatal(err)
	}

	if e.read(self) != "new binary" || len(*reruns) != 1 || (*reruns)[0][0] != self {
		t.Errorf("binary %q, re-runs %v", e.read(self), *reruns)
	}
	env := (*reruns)[0]
	if !slices.Contains(env, "NERVE_UPGRADED_TO=v0.2.0") || !slices.Contains(env, "NERVE_UPGRADED_FROM=v0.1.0") {
		t.Errorf("the re-run's environment lacks the versions: %v", env[len(env)-2:])
	}

	// The re-run is the pinned release, and says what it upgraded from.
	t.Setenv("NERVE_UPGRADED_FROM", "v0.1.0")
	t.Setenv("NERVE_UPGRADED_TO", "v0.2.0")
	n.Ver = "v0.2.0"
	if err := n.Upgrade(domain.Manifest{Nerve: "v0.2.0"}, &r); err != nil {
		t.Fatal(err)
	}
	if r.String() != "upgraded "+self+" from v0.1.0 to v0.2.0" || len(*reruns) != 1 || !Resumed() {
		t.Errorf("events %s, re-runs %d", r, len(*reruns))
	}
}

func TestUpgradeDoesNotNeedATempFileAnotherRunMayHold(t *testing.T) {
	e := newTestEnv(t)
	n, self, _ := e.release("v0.1.0", "v0.2.0", []byte("new binary"), "")
	e.write(self+".new", "another run's download")
	if err := os.Chmod(self+".new", 0o400); err != nil {
		t.Fatal(err)
	}

	if err := n.Upgrade(domain.Manifest{Nerve: "v0.2.0"}, &events{}); err != nil || e.read(self) != "new binary" {
		t.Errorf("got %v, binary %q", err, e.read(self))
	}
}

func TestUpgradeHandsTheEventsSoFarToTheReRun(t *testing.T) {
	e := newTestEnv(t)
	n, _, reruns := e.release("v0.1.0", "v0.2.0", []byte("new binary"), "")
	r := NewReport(&bytes.Buffer{}, AsJSON)
	r.Add(app.Event{Kind: app.EvPullSkipped, Detail: "no upstream branch"})

	if err := n.Upgrade(domain.Manifest{Nerve: "v0.2.0"}, r); err != nil {
		t.Fatal(err)
	}

	var earlier string
	for _, kv := range (*reruns)[0] {
		if v, ok := strings.CutPrefix(kv, "NERVE_EARLIER_EVENTS="); ok {
			earlier = v
		}
	}
	t.Setenv("NERVE_EARLIER_EVENTS", earlier)
	if got := NewReport(&bytes.Buffer{}, AsJSON).Gathered(); len(got) != 1 || got[0].Kind != app.EvPullSkipped {
		t.Errorf("the re-run's report starts with %v", got)
	}
}

func TestUpgradeRefusesADownloadThatFailsItsChecksums(t *testing.T) {
	for name, c := range map[string]struct {
		sum, pinned, want string
	}{
		"binary": {sum: strings.Repeat("0", 64), want: "does not match checksums.txt"},
		"pin":    {pinned: strings.Repeat("0", 64), want: "does not match nerve-checksums"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			n, self, reruns := e.release("v0.1.0", "v0.2.0", []byte("new binary"), c.sum)
			err := n.Upgrade(domain.Manifest{Nerve: "v0.2.0", NerveChecksums: c.pinned}, &events{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want %q", err, c.want)
			}
			if e.read(self) != "old binary" || len(*reruns) > 0 {
				t.Errorf("replaced or re-ran despite a bad checksum")
			}
		})
	}
}

func TestUpgradeAcceptsAReleaseMatchingThePinnedChecksums(t *testing.T) {
	e := newTestEnv(t)
	bin := []byte("new binary")
	n, self, _ := e.release("v0.1.0", "v0.2.0", bin, "")
	h := sha256.Sum256(bin)
	list := sha256.Sum256([]byte(hex.EncodeToString(h[:]) + "  nerve_" + runtime.GOOS + "_" + runtime.GOARCH + "\n"))

	if err := n.Upgrade(domain.Manifest{Nerve: "v0.2.0", NerveChecksums: hex.EncodeToString(list[:])}, &events{}); err != nil {
		t.Fatal(err)
	}
	if e.read(self) != "new binary" {
		t.Errorf("binary is %q", e.read(self))
	}
}

func TestUpgradeLeavesADevelopmentBuildAndAnUnpinnedBrainAlone(t *testing.T) {
	e := newTestEnv(t)
	n, self, reruns := e.release("(devel)", "v0.2.0", []byte("new binary"), "")
	var r events

	for _, m := range []domain.Manifest{{Nerve: "v0.2.0"}, {}} {
		if err := n.Upgrade(m, &r); err != nil {
			t.Fatal(err)
		}
	}
	if e.read(self) != "old binary" || len(*reruns) > 0 || r.String() != "kept     nerve (devel), a development build, though the Brain pins v0.2.0" {
		t.Errorf("events: %s", r)
	}
}

func TestUpgradeGivesUpOnAStalledDownload(t *testing.T) {
	e := newTestEnv(t)
	n, self, _ := e.release("v0.1.0", "v0.2.0", []byte("new binary"), "")
	stalled := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		<-req.Context().Done() // never answers
	}))
	t.Cleanup(stalled.Close)
	n.Release.URL, n.Release.Client = stalled.URL, &http.Client{Timeout: 100 * time.Millisecond}

	if err := n.Upgrade(domain.Manifest{Nerve: "v0.2.0"}, &events{}); err == nil || !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("got %v, want a timeout", err)
	}
	if e.read(self) != "old binary" {
		t.Errorf("binary is %q after a failed download", e.read(self))
	}
}

func TestUpgradeSaysHowToUpgradeABinaryItCannotReplace(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	e := newTestEnv(t)
	n, self, reruns := e.release("v0.1.0", "v0.2.0", []byte("new binary"), "")
	if err := os.Chmod(filepath.Dir(self), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(self), 0o755) })

	err := n.Upgrade(domain.Manifest{Nerve: "v0.2.0"}, &events{})

	if err == nil || !strings.Contains(err.Error(), "cannot replace "+self+" with nerve v0.2.0") || !strings.Contains(err.Error(), "install v0.2.0 by hand") {
		t.Fatalf("got %v", err)
	}
	if len(*reruns) > 0 {
		t.Errorf("re-ran without replacing the binary")
	}
}

func TestSkillInstallsTheEmbeddedSkillWhereLinksPointTo(t *testing.T) {
	e := newTestEnv(t)
	n := Nerve{SkillMD: []byte(skillMD("nerve", ""))}

	s, err := n.Skill()

	if err != nil || s.Name != "nerve" || s.Dir != e.path(".cache/nerve/skill/nerve") || e.read(filepath.Join(s.Dir, "SKILL.md")) != skillMD("nerve", "") {
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
	versions := []string{skillMD("nerve", "license: a\n"), skillMD("nerve", "license: b\n"+strings.Repeat("x: y\n", 2000))}
	path := e.path(".cache/nerve/skill/nerve/SKILL.md")
	if _, err := (Nerve{SkillMD: []byte(versions[0])}).Skill(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 2 { // two nerves, as during an upgrade
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
		"/home/u/go/bin/nerve": "/home/u/go/bin/nerve sync --here --hook",
		"/opt/my tools/nerve":  "'/opt/my tools/nerve' sync --here --hook",
	} {
		n := Nerve{Release: Release{Path: func() (string, error) { return path, nil }}}
		if got, _ := n.Command(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestVersionFallsBackToTheBuildInfo(t *testing.T) {
	if Version("v1.2.3") != "v1.2.3" || Version("") == "" {
		t.Errorf("versions %q, %q", Version("v1.2.3"), Version(""))
	}
}
