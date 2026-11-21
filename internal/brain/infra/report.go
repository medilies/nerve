package infra

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/medilies/nerve/internal/brain/app"
)

// Ways a report is printed.
const (
	AsText = iota // each event as a line, as it comes
	AsJSON        // one JSON document of all events and the error
	AsHook        // one Claude Code hook output: the session report, as a message to the user
)

// Report prints events as text as they come, or gathers them for Finish.
type Report struct {
	w      io.Writer
	mode   int
	events []app.Event
	err    error // the first write that failed
}

var _ app.Reporter = (*Report)(nil)

// NewReport starts a report.
func NewReport(w io.Writer, mode int) *Report { return &Report{w: w, mode: mode} }

func (r *Report) Add(e app.Event) {
	if r.mode != AsText {
		r.events = append(r.events, e)
		return
	}
	_, err := fmt.Fprintln(r.w, Visible(e.String()))
	r.failed(err)
}

func (r *Report) failed(err error) {
	if r.err == nil && err != nil {
		r.err = fmt.Errorf("cannot print the report: %w", err)
	}
}

// Err is the first write of the report that failed, if any: what it
// printed is cut short.
func (r *Report) Err() error { return r.err }

// gathered is the events gathered so far, nil when printed as they come.
func (r *Report) gathered() []app.Event {
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
		}{Events: r.gathered()}
		if err != nil {
			doc.Error = err.Error()
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		r.failed(enc.Encode(doc))
		return true
	case AsHook:
		r.failed(json.NewEncoder(r.w).Encode(hookOutput(r.events, err)))
		return true
	}
	return false
}

// hookOutput is the session report Claude Code shows the user, kept out of
// Claude's context: what the Brain has not pushed, how each checkout
// stands, its drift and problems. With
// nothing to say, it says nothing.
func hookOutput(events []app.Event, err error) map[string]any {
	var lines []string
	for _, e := range events {
		switch {
		case e.Kind == app.EvCheckout, e.Kind == app.EvDrift, e.Kind == app.EvProblem,
			e.Kind == app.EvBrain && e.Detail != "": // what the Brain has not pushed, which a sync refuses to drop
			lines = append(lines, e.String())
		}
	}
	if err != nil {
		lines = append(lines, "error: "+err.Error())
	}
	if len(lines) == 0 {
		return map[string]any{}
	}
	return map[string]any{"systemMessage": "nerve: " + Visible(strings.Join(lines, "\n"))}
}
