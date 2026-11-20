package infra

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/medilies/nerve/internal/brain/app"
)

func TestTextReportPrintsEachEventAsItComes(t *testing.T) {
	var out bytes.Buffer
	r := NewReport(&out, AsText)

	r.Add(app.Event{Kind: app.EvWrote, Path: "/x"})

	if out.String() != "wrote    /x\n" || r.Finish(errors.New("boom")) || r.Gathered() != nil || r.Count() != 1 {
		t.Errorf("printed %q", out.String())
	}
}

func TestJSONReportIsOneDocumentOfEventsAndTheError(t *testing.T) {
	var out bytes.Buffer
	r := NewReport(&out, AsJSON)

	if !r.Finish(errors.New("boom")) {
		t.Fatal("a JSON report printed nothing")
	}

	var doc struct {
		Events []app.Event
		Error  string
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.Events == nil || doc.Error != "boom" {
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

func TestHookReportIsAMessageToTheUserAndRescansLinkedSkills(t *testing.T) {
	doc := hookDoc(t, []app.Event{
		{Kind: app.EvLinked, Path: "/w/app/.claude/skills/go-stack", Target: "/brain/skills/go-stack"},
		{Kind: app.EvSynced, Project: "app", Target: "abcdef0123"},
	}, nil)

	if doc["systemMessage"] != "nerve: linked   /w/app/.claude/skills/go-stack -> /brain/skills/go-stack\nsynced   app at abcdef0" {
		t.Errorf("message is %q", doc["systemMessage"])
	}
	if out, _ := json.Marshal(doc["hookSpecificOutput"]); string(out) != `{"hookEventName":"SessionStart","reloadSkills":true}` {
		t.Errorf("hookSpecificOutput is %s", out)
	}

	doc = hookDoc(t, []app.Event{{Kind: app.EvLinked, Path: "/w/app/.claude/agents/r.md"}}, errors.New("boom"))
	if doc["hookSpecificOutput"] != nil || doc["systemMessage"] != "nerve: linked   /w/app/.claude/agents/r.md -> \nerror: boom" {
		t.Errorf("doc is %v", doc)
	}
}

func TestHookReportIsOneLineWhenNothingChanged(t *testing.T) {
	upToDate := app.Event{Kind: app.EvUpToDate, Project: "app", Target: "abcdef0123"}
	for want, events := range map[string][]app.Event{
		"nerve: app is up to date at abcdef0": {upToDate},
		"nerve: app is up to date at abcdef0 (no pull: no upstream branch; kept development build (devel) over v0.3.0)": {
			{Kind: app.EvPullSkipped, Path: "/brain", Detail: "no upstream branch"},
			{Kind: app.EvUpgradeSkipped, Detail: "(devel)", Target: "v0.3.0"},
			upToDate,
		},
		"nerve: no pull  /brain: uncommitted changes\ndrift    model is \"sonnet\" here, \"opus\" in the Brain: left as is\napp is up to date at abcdef0": {
			{Kind: app.EvPullSkipped, Path: "/brain", Detail: "uncommitted changes"},
			{Kind: app.EvDrift, Key: "model", Local: `"sonnet"`, Brain: `"opus"`},
			upToDate,
		},
	} {
		if got := hookDoc(t, events, nil)["systemMessage"]; got != want {
			t.Errorf("message is\n%s\nwant\n%s", got, want)
		}
	}
}
