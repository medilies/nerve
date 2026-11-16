package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// Event is one thing a command did or found. Kind says which; the other
// fields are set as that kind needs.
type Event struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Target string `json:"target,omitempty"`
	Skill  string `json:"skill,omitempty"`
	Config string `json:"config,omitempty"`
	Key    string `json:"key,omitempty"`
	Local  string `json:"local,omitempty"` // a settings value as JSON, "unset" when absent
	Brain  string `json:"brain,omitempty"` // likewise, as the Brain would write it
	Detail string `json:"detail,omitempty"`
}

// Kinds of Event.
const (
	evLinked         = "linked"        // Path now links to Target
	evRemoved        = "removed"       // a link at Path that nerve made
	evWrote          = "wrote"         // Path
	evCodeOnly       = "code-only"     // Skill is not zipped for claude.ai; Detail says why
	evDrift          = "drift"         // settings Key at Path changed outside the Brain, from Brain to Local
	evUpload         = "upload"        // Skill is missing (Detail "missing") or stale on the claude.ai account of Config; Path is the zip
	evAIUpToDate     = "claude.ai-ok"  // the claude.ai account of Config holds every skill
	evAINotSynced    = "claude.ai-off" // Config has no synced claude.ai skills to compare
	evProblem        = "problem"       // Detail is wrong with Skill or Path
	evOK             = "ok"            // Detail
	evDiff           = "diff"          // Detail is the upstream diff of Skill
	evPinned         = "pinned"        // Detail is the source moved to Target
	evAlreadyThere   = "already-pinned"
	evPulled         = "pulled"          // the Brain at Path fast-forwarded to Target
	evPullSkipped    = "pull-skipped"    // the Brain at Path was not pulled; Detail says why
	evVersion        = "version"         // Detail is the running nerve's version; Target the Brain's pin
	evUpgraded       = "upgraded"        // nerve at Path went from Detail to the pinned Target
	evUpgradeSkipped = "upgrade-skipped" // a development build (Detail) ran instead of the pinned Target
	evBrain          = "brain"           // Path is the machine's Brain checkout
	evConfig         = "config"          // Path is a Claude Code config; Detail its profile
)

func (e Event) String() string {
	switch e.Kind {
	case evLinked:
		return fmt.Sprintf("linked   %s -> %s", e.Path, e.Target)
	case evRemoved, evWrote:
		return fmt.Sprintf("%-8s %s", e.Kind, e.Path)
	case evCodeOnly:
		return fmt.Sprintf("skipped  %s: Claude Code only (%s)", e.Skill, e.Detail)
	case evDrift:
		return fmt.Sprintf("drift    %s: %s is %s here, %s in the Brain", e.Path, e.Key, e.Local, e.Brain)
	case evUpload:
		return fmt.Sprintf("%-8s %s on claude.ai of %s: upload %s", e.Detail, e.Skill, e.Config, e.Path)
	case evAIUpToDate:
		return fmt.Sprintf("claude.ai of %s: up to date", e.Config)
	case evAINotSynced:
		return fmt.Sprintf("claude.ai of %s: no synced skills, skipped", e.Config)
	case evProblem:
		if e.Skill != "" {
			return fmt.Sprintf("problem  %s: %s", e.Skill, e.Detail)
		}
		return fmt.Sprintf("problem  %s: %s", e.Path, e.Detail)
	case evDiff:
		return e.Detail
	case evPinned:
		return fmt.Sprintf("pinned   %s at %s: review the diff above, then commit %s and sync", e.Detail, e.Target, manifestFile)
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

func newReport(w io.Writer, asJSON bool) *report { return &report{w: w, json: asJSON} }

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
