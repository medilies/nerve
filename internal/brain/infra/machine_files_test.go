package infra

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

func TestMachineFileRoundTripsWithPathsUnderHome(t *testing.T) {
	e := newTestEnv(t)
	files, err := NewMachineFiles()
	if err != nil {
		t.Fatal(err)
	}
	m := domain.Machine{Brain: e.path("brain"), Checkouts: []domain.Checkout{{Path: e.path("dev/app"), Project: "app"}}}

	path, err := files.Create(m)
	if err != nil {
		t.Fatal(err)
	}

	if got := e.read(path); got != "brain = \"~/brain\"\n\n[[checkout]]\npath = \"~/dev/app\"\nproject = \"app\"\n" {
		t.Errorf("machine file is:\n%s", got)
	}
	if got, err := files.Load(); err != nil || got.Brain != m.Brain || got.Checkouts[0] != m.Checkouts[0] {
		t.Errorf("read back %+v (%v)", got, err)
	}
	if _, err := files.Create(m); err == nil || !strings.Contains(err.Error(), "exists: edit it instead") {
		t.Errorf("got %v", err)
	}
}

func TestMachineFileKeepsAPathAsItIsWritten(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("real-work/app/README"), "x")
	e.link(e.path("work"), e.path("real-work"))
	config := "brain = \"~/brain\"\n\n[[checkout]]\npath = \"~/work/app\"\nproject = \"app\"\n"
	e.write(e.path(".config/nerve/config.toml"), config)
	files, _ := NewMachineFiles()

	m, err := files.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.Checkouts = append(m.Checkouts, domain.Checkout{Path: e.path("other"), Project: "other"})
	if err := files.Save(m); err != nil {
		t.Fatal(err)
	}

	if got := e.read(e.path(".config/nerve/config.toml")); !strings.HasPrefix(got, config) {
		t.Errorf("machine file is:\n%s", got)
	}
}

func TestAStateFileNerveCannotReadStopsOnlyItsOwnTree(t *testing.T) {
	e := newTestEnv(t)
	files, _ := NewMachineFiles()
	if err := files.SaveState(domain.TreeState{Path: "/w/other", Project: "other"}); err != nil {
		t.Fatal(err)
	}
	if err := files.SaveState(domain.TreeState{Path: "/w/app", Project: "app"}); err != nil {
		t.Fatal(err)
	}
	e.write(files.statePath("/w/app"), "")

	if all, unreadable, err := files.States(); err != nil || len(all) != 1 || all[0].Path != "/w/other" || !reflect.DeepEqual(unreadable, []string{files.statePath("/w/app")}) {
		t.Errorf("states are %+v, unreadable %v (%v), want the other tree's, and the file named", all, unreadable, err)
	}
	if _, err := files.State("/w/app"); err == nil || !strings.Contains(err.Error(), "is not a state nerve can read: delete it, then sync again") {
		t.Errorf("got %v, want the fix named", err)
	}
}

func TestStatesReadOnlyFilesNamedLikeAState(t *testing.T) {
	e := newTestEnv(t)
	files, _ := NewMachineFiles()
	if err := files.SaveState(domain.TreeState{Path: "/w/app", Project: "app"}); err != nil {
		t.Fatal(err)
	}
	e.write(e.path(".config/nerve/state/stray.json"), "{}")

	if all, unreadable, err := files.States(); err != nil || len(all) != 1 || all[0].Path != "/w/app" || len(unreadable) > 0 {
		t.Errorf("states are %+v, unreadable %v (%v)", all, unreadable, err)
	}
}

func TestStatesAreReadUnderAConfigFolderNamedLikeAPattern(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("XDG_CONFIG_HOME", e.path("c[1]"))
	files, _ := NewMachineFiles()
	if err := files.SaveState(domain.TreeState{Path: "/w/app", Project: "app"}); err != nil {
		t.Fatal(err)
	}

	if all, _, err := files.States(); err != nil || len(all) != 1 {
		t.Errorf("states are %+v (%v)", all, err)
	}
}

func TestMachineFileWritesANewPathUnderALinkedHomeWithTilde(t *testing.T) {
	e := newTestEnv(t)
	real := e.path("real-home")
	e.write(filepath.Join(real, "brain/brain.toml"), "")
	e.link(e.path("home"), real)
	t.Setenv("HOME", e.path("home"))
	files, _ := NewMachineFiles()

	if err := files.Save(domain.Machine{Brain: filepath.Join(real, "brain")}); err != nil {
		t.Fatal(err)
	}

	if got := e.read(e.path(".config/nerve/config.toml")); got != "brain = \"~/brain\"\n" {
		t.Errorf("machine file is:\n%s", got)
	}
}

func TestMachineFileRefusesWhatItCannotHold(t *testing.T) {
	e := newTestEnv(t)
	files, _ := NewMachineFiles()
	if _, err := files.Load(); err == nil || !strings.Contains(err.Error(), "run nerve init") {
		t.Errorf("got %v without a machine file", err)
	}
	for _, c := range []struct{ content, want string }{
		{"brain = \"~/b\"\n[[config]]\ndir = \"~/.claude\"\n", "[[config]] is gone since nerve v0.3"},
		{"brain = \"b\"\n", `brain "b" must be absolute`},
		{"brain = \"~/b\"\nx = 1\n", "unknown keys [x]"},
		{"brain = \"~/b\"\n[[checkout]]\npath = \"~/a\"\nproject = \"a\"\n[[checkout]]\npath = \"~/a/\"\nproject = \"b\"\n", "is listed twice"},
	} {
		e.write(e.path(".config/nerve/config.toml"), c.content)
		if _, err := files.Load(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.content, err, c.want)
		}
	}
}

func TestStateAndOverlaysStayOnTheMachine(t *testing.T) {
	e := newTestEnv(t)
	files, _ := NewMachineFiles()

	st, err := files.State("/w/app")
	if err != nil || st.Path != "/w/app" || st.Links == nil {
		t.Fatalf("a new tree's state is %+v (%v)", st, err)
	}
	link := domain.Link{Kind: "skill", Name: "x", Target: "/brain/skills/x"}
	st.Project, st.Repo, st.Commit, st.Links[".claude/skills/x"] = "app", "/w/app/.git", "abc", link
	st.Before = map[string]any{"cleanupPeriodDays": int64(30)}
	if err := files.SaveState(st); err != nil {
		t.Fatal(err)
	}
	if got, _ := files.State("/w/app"); got.Project != "app" || got.Repo != "/w/app/.git" || got.Links[".claude/skills/x"] != link || !reflect.DeepEqual(got.Before, st.Before) {
		t.Errorf("read back %+v", got)
	}
	if all, _, _ := files.States(); len(all) != 1 || all[0].Path != "/w/app" {
		t.Errorf("states are %+v", all)
	}
	if err := files.SaveBuilt(map[string]map[string]string{"acct": {"x": "h"}}); err != nil {
		t.Fatal(err)
	}
	if all, unreadable, _ := files.States(); len(all) != 1 || len(unreadable) > 0 {
		t.Errorf("the claude.ai record counts as a tree: %+v, %v", all, unreadable)
	}
	if built, _ := files.Built(); built["acct"]["x"] != "h" {
		t.Errorf("built is %v", built)
	}
	if err := files.DropState("/w/app"); err != nil {
		t.Fatal(err)
	}
	if got, _ := files.State("/w/app"); got.Project != "" {
		t.Errorf("a dropped state reads %+v", got)
	}

	path, err := files.SaveOverlay("app", map[string]any{"env": map[string]any{"T": "1"}})
	if err != nil || path != e.path(".config/nerve/overlays/app.json") {
		t.Fatalf("overlay at %s (%v)", path, err)
	}
	if o, _, _ := files.Overlay("app"); o["env"].(map[string]any)["T"] != "1" {
		t.Errorf("overlay reads %v", o)
	}
	if o, _, err := files.Overlay("none"); o != nil || err != nil {
		t.Errorf("a missing overlay reads %v (%v)", o, err)
	}
	if dropped, err := files.DropOverlay("app"); err != nil || dropped != path || fileExists(path) {
		t.Errorf("dropped %q (%v)", dropped, err)
	}
	if dropped, err := files.DropOverlay("app"); err != nil || dropped != "" {
		t.Errorf("dropping a missing overlay gives %q (%v)", dropped, err)
	}
}
