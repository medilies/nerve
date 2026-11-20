package infra

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/medilies/nerve/internal/brain/app"
)

// Ways a report is printed.
const (
	AsText = iota // each event as a line, as it comes
	AsJSON        // one JSON document of all events and the error
	AsHook        // one Claude Code hook output: the events as a message to the user
)

// Report prints events as text as they come, or gathers them for Finish.
type Report struct {
	w      io.Writer
	mode   int
	events []app.Event
	count  int
}

var _ app.Reporter = (*Report)(nil)

// NewReport starts a report. A run that replaced an older nerve picks up
// the events that one gathered before it handed over.
func NewReport(w io.Writer, mode int) *Report {
	r := &Report{w: w, mode: mode}
	if earlier := os.Getenv(earlierEventsEnv); mode != AsText && earlier != "" {
		_ = json.Unmarshal([]byte(earlier), &r.events) // at worst, the earlier events are lost
	}
	return r
}

func (r *Report) Add(e app.Event) {
	r.count++
	if r.mode != AsText {
		r.events = append(r.events, e)
		return
	}
	_, _ = fmt.Fprintln(r.w, e)
}

func (r *Report) Count() int { return r.count }

// Gathered is the events gathered so far, nil when printed as they come.
func (r *Report) Gathered() []app.Event {
	if r.mode == AsText {
		return nil
	}
	if r.events == nil {
		return []app.Event{}
	}
	return r.events
}

// Finish ends the report; when it gathered events, it prints them and the
// error, if any, and returns true.
func (r *Report) Finish(err error) bool {
	switch r.mode {
	case AsJSON:
		doc := struct {
			Events []app.Event `json:"events"`
			Error  string      `json:"error,omitempty"`
		}{Events: r.Gathered()}
		if err != nil {
			doc.Error = err.Error()
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(doc)
		return true
	case AsHook:
		_ = json.NewEncoder(r.w).Encode(hookOutput(r.events, err))
		return true
	}
	return false
}

// hookOutput is what autosync hands Claude Code: a message shown to the
// user and kept out of Claude's context, and a skill rescan when it linked a
// skill, so the skill is usable in this session.
func hookOutput(events []app.Event, err error) map[string]any {
	var lines []string
	reload := false
	for _, e := range events {
		if e.Kind == app.EvLinked && filepath.Base(filepath.Dir(e.Path)) == "skills" {
			reload = true
		}
		lines = append(lines, e.String())
	}
	if err != nil {
		lines = append(lines, "error: "+err.Error())
	}
	out := map[string]any{"systemMessage": "nerve: " + strings.Join(lines, "\n")}
	if reload {
		out["hookSpecificOutput"] = map[string]any{"hookEventName": "SessionStart", "reloadSkills": true}
	}
	return out
}
