package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Event is one thing a command did or found. Kind says which; the other
// fields are set as that kind needs.
type Event struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Target string `json:"target,omitempty"`
	Plugin string `json:"plugin,omitempty"`
	Key    string `json:"key,omitempty"`
	Local  string `json:"local,omitempty"` // a settings value as JSON, "unset" when absent
	Brain  string `json:"brain,omitempty"` // likewise, as the Brain would write it
	Detail string `json:"detail,omitempty"`
}

// Kinds of Event.
const (
	evLinked         = "linked"  // Path now links to Target
	evRemoved        = "removed" // a link at Path that nerve made
	evWrote          = "wrote"   // Path
	evDrift          = "drift"   // settings Key at Path changed outside the Brain, from Brain to Local
	evProblem        = "problem" // Detail is wrong with Path
	evOK             = "ok"      // Detail
	evDiff           = "diff"    // Detail is the upstream diff of Plugin, "" when it did not change
	evPinned         = "pinned"  // the plugins from repo Detail moved to Target
	evAlreadyThere   = "already-pinned"
	evPulled         = "pulled"          // the Brain at Path fast-forwarded to Target
	evPullSkipped    = "pull-skipped"    // the Brain at Path was not pulled; Detail says why
	evVersion        = "version"         // Detail is the running nerve's version; Target the Brain's pin
	evUpgraded       = "upgraded"        // nerve at Path went from Detail to the pinned Target
	evUpgradeSkipped = "upgrade-skipped" // a development build (Detail) ran instead of the pinned Target
	evBrain          = "brain"           // Path is the machine's Brain checkout
	evConfig         = "config"          // Path is a Claude Code config; Detail its profile
	evUsage          = "usage"           // Detail is how to run nerve
)

func (e Event) String() string {
	switch e.Kind {
	case evLinked:
		return fmt.Sprintf("linked   %s -> %s", e.Path, e.Target)
	case evRemoved, evWrote:
		return fmt.Sprintf("%-8s %s", e.Kind, e.Path)
	case evDrift:
		return fmt.Sprintf("drift    %s: %s is %s here, %s in the Brain", e.Path, e.Key, e.Local, e.Brain)
	case evProblem:
		return fmt.Sprintf("problem  %s: %s", e.Path, e.Detail)
	case evDiff:
		if e.Detail == "" {
			return "no diff  " + e.Plugin
		}
		return e.Detail
	case evPinned:
		return fmt.Sprintf("pinned   %s at %s: review the diff above, then commit %s and sync", e.Detail, e.Target, marketplaceFile)
	case evPulled:
		return fmt.Sprintf("pulled   %s to %s", e.Path, e.Target)
	case evPullSkipped:
		return fmt.Sprintf("no pull  %s: %s", e.Path, e.Detail)
	case evVersion:
		if e.Target != "" && e.Target != e.Detail {
			return fmt.Sprintf("nerve    %s (the Brain pins %s: sync to upgrade)", e.Detail, e.Target)
		}
		return "nerve    " + e.Detail
	case evUpgraded:
		return fmt.Sprintf("upgraded %s from %s to %s", e.Path, e.Detail, e.Target)
	case evUpgradeSkipped:
		return fmt.Sprintf("kept     nerve %s, a development build, though the Brain pins %s", e.Detail, e.Target)
	case evBrain:
		return "brain    " + e.Path
	case evConfig:
		if e.Detail == "" {
			return "config   " + e.Path
		}
		return fmt.Sprintf("config   %s (profile %s)", e.Path, e.Detail)
	case evAlreadyThere:
		return fmt.Sprintf("%s is already pinned at %s", e.Detail, e.Target)
	}
	return e.Detail
}

// report prints events as text as they come, or gathers them into one JSON
// document printed by finish.
type report struct {
	w      io.Writer
	json   bool
	events []Event
}

// newReport starts a report. A --json run that replaced an older nerve picks
// up the events that one gathered before it handed over.
func newReport(w io.Writer, asJSON bool) *report {
	r := &report{w: w, json: asJSON}
	if earlier := os.Getenv(earlierEventsEnv); asJSON && earlier != "" {
		json.Unmarshal([]byte(earlier), &r.events)
	}
	return r
}

func (r *report) add(e Event) {
	if r.json {
		r.events = append(r.events, e)
		return
	}
	fmt.Fprintln(r.w, e)
}

// finish ends the report; in JSON mode it prints the events and the error,
// if any, and returns whether it did so.
func (r *report) finish(err error) bool {
	if !r.json {
		return false
	}
	doc := struct {
		Events []Event `json:"events"`
		Error  string  `json:"error,omitempty"`
	}{Events: r.events}
	if doc.Events == nil {
		doc.Events = []Event{}
	}
	if err != nil {
		doc.Error = err.Error()
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	enc.Encode(doc)
	return true
}
