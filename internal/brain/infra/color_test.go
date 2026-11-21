package infra

import (
	"bytes"
	"os"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
)

func TestAColorReportColorsEachLabelByWhatItMeans(t *testing.T) {
	for _, c := range []struct {
		e    app.Event
		want string
	}{
		{app.Event{Kind: app.EvLinked, Path: "/a", Target: "/b"}, "\x1b[32mlinked\x1b[0m   /a -> /b\n"},
		{app.Event{Kind: app.EvRemoved, Path: "/a"}, "\x1b[35mremoved\x1b[0m  /a\n"},
		{app.Event{Kind: app.EvSetting, Path: "/s", Key: "model", Local: "\"opus\"", Brain: "unset"}, "\x1b[35munset\x1b[0m    /s: model (was \"opus\")\n"},
		{app.Event{Kind: app.EvDrift, Path: "/s", Key: "model", Local: "a", Brain: "b"}, "\x1b[33mdrift\x1b[0m    /s: model is a here, b in the Brain: left as is\n"},
		{app.Event{Kind: app.EvProblem, Path: "/p", Detail: "bad"}, "\x1b[1;31mproblem\x1b[0m  /p: bad\n"},
		{app.Event{Kind: app.EvProject, Project: "app", Detail: "lists nothing yet"}, "\x1b[36mproject\x1b[0m  app: lists nothing yet\n"},
		{app.Event{Kind: app.EvUpToDate, Project: "app", Target: "aaaaaaaaaa", Path: "/w"}, "\x1b[2mapp is up to date at aaaaaaa in /w\x1b[0m\n"},
		{app.Event{Kind: app.EvDiff, Item: "x", Detail: "@@ -1 +1 @@\n-old\n+new"}, "\x1b[36m@@ -1 +1 @@\x1b[0m\n\x1b[31m-old\x1b[0m\n\x1b[32m+new\x1b[0m\n"},
		{app.Event{Kind: app.EvDiff, Item: "x", Detail: "diff --git a/x b/x\n--- a/x\n+++ b/x"}, "\x1b[1mdiff --git a/x b/x\x1b[0m\n\x1b[1m--- a/x\x1b[0m\n\x1b[1m+++ b/x\x1b[0m\n"},
		{app.Event{Kind: app.EvTip, Key: "a/b", From: "aaaaaaaa", FromDate: "d1", Target: "bbbbbbbb", TargetDate: "d2"}, "\x1b[36mtip\x1b[0m      a/b: pinned at aaaaaaa (d1), tip bbbbbbb (d2)\n"},
		{app.Event{Kind: app.EvAllowed, Key: "k"}, "\x1b[32mallowed\x1b[0m  k\n"},
		{app.Event{Kind: app.EvDisallowed, Key: "k"}, "\x1b[35mdropped\x1b[0m  k from allowedSettings\n"},
		{app.Event{Kind: app.EvNote, Path: "b", Detail: "d"}, "\x1b[33mnote\x1b[0m     b: d\n"},
		{app.Event{Kind: app.EvSame, Item: "skill x"}, "\x1b[2msame     skill x\x1b[0m\n"},
		{app.Event{Kind: app.EvChanged, Item: "skill x", Files: []app.FileChange{{Path: "f", Added: 1}}}, "\x1b[33mchanged\x1b[0m  skill x: f +1 -0\n"},
		{app.Event{Kind: app.EvChanged, Item: "skill x", Files: []app.FileChange{{Path: "f", Added: 1}}, Detail: "@@ -0 +1 @@\n+new"}, "\x1b[33mchanged\x1b[0m  skill x: f +1 -0\n\x1b[36m@@ -0 +1 @@\x1b[0m\n\x1b[32m+new\x1b[0m\n"},
	} {
		var out bytes.Buffer
		NewReport(&out, AsText).WithColor(true).Add(c.e)
		if out.String() != c.want {
			t.Errorf("%s printed %q, want %q", c.e.Kind, out.String(), c.want)
		}
	}
}

func TestAColorReportStillHidesControlCharactersFromTheEvent(t *testing.T) {
	var out bytes.Buffer
	NewReport(&out, AsText).WithColor(true).Add(app.Event{Kind: app.EvWrote, Path: "/a\x1b[2J"})
	if out.String() != "\x1b[32mwrote\x1b[0m    /a[2J\n" {
		t.Errorf("printed %q", out.String())
	}
}

func TestPaintCommandsColorsUsageAndTheCommandsAnErrorOffers(t *testing.T) {
	got := PaintCommands("usage:\n  init <brain>     tell nerve\n                   where\nRun one:\n  nerve sync -f    drop it")
	want := "\x1b[1musage:\x1b[0m\n  \x1b[36minit <brain>\x1b[0m     tell nerve\n                   where\nRun one:\n  \x1b[36mnerve sync -f\x1b[0m    drop it"
	if got != want {
		t.Errorf("painted %q, want %q", got, want)
	}
}

func TestColorsAreOffForWhatIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()
	if Colors(w) {
		t.Error("colors on for a pipe")
	}
}

func TestPaintErrorColorsTheWordErrorAndTheCommandsItOffers(t *testing.T) {
	got := PaintError("stuck.\nRun one:\n  nerve sync -f    drop it")
	want := "\x1b[1;31merror:\x1b[0m stuck.\nRun one:\n  \x1b[36mnerve sync -f\x1b[0m    drop it"
	if got != want {
		t.Errorf("painted %q, want %q", got, want)
	}
}
