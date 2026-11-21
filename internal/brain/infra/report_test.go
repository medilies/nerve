package infra

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
)

func TestTextReportPrintsEachEventAsItComes(t *testing.T) {
	var out bytes.Buffer
	r := NewReport(&out, AsText)

	r.Add(app.Event{Kind: app.EvWrote, Path: "/x"})

	if out.String() != "wrote    /x\n" || r.Finish(errors.New("boom")) || r.gathered() != nil {
		t.Errorf("printed %q", out.String())
	}
}

func TestJSONReportIsOneDocumentOfEventsAndTheError(t *testing.T) {
	var out bytes.Buffer
	r := NewReport(&out, AsJSON)
	r.Add(app.Event{Kind: app.EvWrote, Path: "/x"})

	if !r.Finish(errors.New("boom")) {
		t.Fatal("a JSON report printed nothing")
	}

	var doc struct {
		Events []app.Event
		Error  string
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || len(doc.Events) != 1 || doc.Events[0] != (app.Event{Kind: app.EvWrote, Path: "/x"}) || doc.Error != "boom" {
		t.Errorf("document %s (%v)", out.String(), err)
	}
}

func hookDoc(t *testing.T, events []app.Event, err error) map[string]any {
	t.Helper()
	var out bytes.Buffer
	r := NewReport(&out, AsHook)
	for _, e := range events {
		r.Add(e)
	}
	r.Finish(err)
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("not JSON: %s", out.String())
	}
	return doc
}

func TestTextReportShowsNoControlCharacter(t *testing.T) {
	var out bytes.Buffer
	r := NewReport(&out, AsText)

	r.Add(app.Event{Kind: app.EvItem, Item: "skill evil\x1b[2Jx", Detail: "the team's"})

	if out.String() != "skill evil[2Jx the team's\n" {
		t.Errorf("printed %q", out.String())
	}
}

func TestHookReportSaysHowTheCheckoutStandsItsDriftAndProblems(t *testing.T) {
	doc := hookDoc(t, []app.Event{
		{Kind: app.EvBrain, Path: "/brain"},
		{Kind: app.EvProject, Project: "app", Detail: "1 skill"},
		{Kind: app.EvCheckout, Path: "/w/app", Project: "app", Detail: "synced at abcdef0; 1 change waits for nerve sync"},
		{Kind: app.EvItem, Item: "skill nerve", Detail: "linked as .claude/skills/nerve"},
		{Kind: app.EvDrift, Key: "model", Local: `"sonnet"`, Brain: `"opus"`},
	}, errors.New("boom"))

	want := "nerve: checkout /w/app (app): synced at abcdef0; 1 change waits for nerve sync\n" +
		"drift    model is \"sonnet\" here, \"opus\" in the Brain: left as is\nerror: boom"
	if len(doc) != 1 || doc["systemMessage"] != want {
		t.Errorf("doc is %v", doc)
	}
	if doc := hookDoc(t, nil, nil); len(doc) != 0 {
		t.Errorf("with nothing to say, the hook says %v", doc)
	}
}

func TestHookReportShowsNoControlCharacter(t *testing.T) {
	msg := hookOutput([]app.Event{{Kind: app.EvProblem, Path: "/w/evil\x1b[2J"}}, nil)["systemMessage"].(string)

	if strings.ContainsRune(msg, '\x1b') {
		t.Errorf("message is %q", msg)
	}
}
