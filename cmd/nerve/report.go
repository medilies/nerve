package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Event is one thing a command did or found. Kind says which; the other
// fields are set as that kind needs.
type Event struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"` // "" for common, where an event can be about either
	Path    string `json:"path,omitempty"`
	Target  string `json:"target,omitempty"`
	Item    string `json:"item,omitempty"` // kind and name, such as "skill grilling"
	Key     string `json:"key,omitempty"`
	Local   string `json:"local,omitempty"` // a settings value as JSON, "unset" when absent
	Brain   string `json:"brain,omitempty"` // likewise, as the Brain would write it
	Detail  string `json:"detail,omitempty"`
}

// Kinds of Event.
const (
	evLinked         = "linked"          // Path now links to Target
	evRemoved        = "removed"         // a link at Path that nerve made
	evWrote          = "wrote"           // Path
	evSetting        = "setting"         // Key at Path went from Local to Brain
	evDrift          = "drift"           // Key at Path changed outside the Brain, from Brain to Local, and was left as is
	evWroteBack      = "wrote-back"      // Item left Project in brain.toml, since its link was deleted
	evProblem        = "problem"         // Detail is wrong with Path (at Key)
	evOK             = "ok"              // Detail
	evDiff           = "diff"            // Detail is the upstream diff of Item, "" when it did not change
	evPinned         = "pinned"          // the skills from repo Detail moved to Target
	evAlreadyThere   = "already-pinned"  // repo Detail is already at Target
	evPulled         = "pulled"          // the Brain at Path fast-forwarded to Target
	evPullSkipped    = "pull-skipped"    // the Brain at Path was not pulled; Detail says why
	evVersion        = "version"         // Detail is the running nerve's version; Target the Brain's pin
	evUpgraded       = "upgraded"        // nerve at Path went from Detail to the pinned Target
	evUpgradeSkipped = "upgrade-skipped" // a development build (Detail) ran instead of the pinned Target
	evBrain          = "brain"           // Path is the machine's Brain checkout; Detail its state
	evAdded          = "added"           // Item joined Project
	evDropped        = "dropped"         // Item left Project
	evImported       = "imported"        // Key of Project's existing settings went where Detail says
	evOverlay        = "overlay"         // Key went to the local overlay at Path; Detail says why
	evProject        = "project"         // Project; Detail describes it
	evCheckout       = "checkout"        // Path belongs to Project; Detail its sync state
	evItem           = "item"            // Item of Project; Detail says where it comes from
	evValue          = "value"           // Key of Project is Brain; Detail says which layers give it
	evAutosync       = "autosync"        // autosync is Detail for Project, or for the Brain when Project is ""
	evUpload         = "upload"          // the zip at Path holds Item, new or changed: upload it to claude.ai account Project
	evUnload         = "unload"          // Item left claude.ai account Project: delete it there
	evCleaned        = "cleaned"         // nerve took its links and settings out of Path; Detail says why
	evSynced         = "synced"          // Project at Path is at Brain commit Target
	evUpToDate       = "up-to-date"      // Project at Path is at Brain commit Target, and nothing changed
	evUsage          = "usage"           // Detail is how to run nerve
)

func (e Event) String() string {
	switch e.Kind {
	case evLinked:
		return fmt.Sprintf("linked   %s -> %s", e.Path, e.Target)
	case evRemoved, evWrote:
		return fmt.Sprintf("%-8s %s", e.Kind, e.Path)
	case evSetting:
		switch {
		case e.Local == "unset":
			return fmt.Sprintf("set      %s = %s", e.Key, e.Brain)
		case e.Brain == "unset":
			return fmt.Sprintf("unset    %s (was %s)", e.Key, e.Local)
		}
		return fmt.Sprintf("set      %s = %s (was %s)", e.Key, e.Brain, e.Local)
	case evDrift:
		return fmt.Sprintf("drift    %s is %s here, %s in the Brain: left as is", e.Key, e.Local, e.Brain)
	case evWroteBack:
		return fmt.Sprintf("dropped  %s from project %s in %s, since its link was deleted: commit it", e.Item, e.Project, manifestFile)
	case evProblem:
		if e.Key != "" {
			return fmt.Sprintf("problem  %s: %s: %s", e.Path, e.Key, e.Detail)
		}
		return fmt.Sprintf("problem  %s: %s", e.Path, e.Detail)
	case evDiff:
		if e.Detail == "" {
			return "no diff  " + e.Item
		}
		return e.Detail
	case evPinned:
		return fmt.Sprintf("pinned   %s at %s: review the diff above, then commit %s and sync", e.Detail, e.Target, manifestFile)
	case evAlreadyThere:
		return fmt.Sprintf("%s is already pinned at %s", e.Detail, e.Target)
	case evPulled:
		return fmt.Sprintf("pulled   the Brain to %s", short(e.Target))
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
		if e.Detail != "" {
			return fmt.Sprintf("brain    %s (%s)", e.Path, e.Detail)
		}
		return "brain    " + e.Path
	case evAdded:
		return fmt.Sprintf("added    %s to project %s", e.Item, e.Project)
	case evDropped:
		if e.Project == "" {
			return fmt.Sprintf("dropped  %s from common", e.Item)
		}
		return fmt.Sprintf("dropped  %s from project %s", e.Item, e.Project)
	case evImported:
		return fmt.Sprintf("imported %s: %s", e.Key, e.Detail)
	case evOverlay:
		return fmt.Sprintf("overlay  %s set in %s: %s", e.Key, e.Path, e.Detail)
	case evProject:
		return fmt.Sprintf("project  %s: %s", e.Project, e.Detail)
	case evCheckout:
		return fmt.Sprintf("checkout %s (%s): %s", e.Path, e.Project, e.Detail)
	case evItem:
		return fmt.Sprintf("%-8s %s", e.Item, e.Detail)
	case evValue:
		return fmt.Sprintf("%s = %s  (%s)", e.Key, e.Brain, e.Detail)
	case evAutosync:
		if e.Project == "" {
			return fmt.Sprintf("autosync %s by default", e.Detail)
		}
		return fmt.Sprintf("autosync %s for %s", e.Detail, e.Project)
	case evUpload:
		return fmt.Sprintf("upload   %s to claude.ai account %s (%s)", e.Path, e.Project, e.Item)
	case evUnload:
		return fmt.Sprintf("delete   %s from claude.ai account %s", e.Item, e.Project)
	case evCleaned:
		return fmt.Sprintf("cleaned  %s: %s", e.Path, e.Detail)
	case evSynced:
		return fmt.Sprintf("synced   %s at %s", e.Project, short(e.Target))
	case evUpToDate:
		return fmt.Sprintf("%s is up to date at %s", e.Project, short(e.Target))
	}
	return e.Detail
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return "an uncommitted Brain"
	}
	return sha
}

// Ways a report is printed.
const (
	asText = iota // each event as a line, as it comes
	asJSON        // one JSON document of all events and the error
	asHook        // one Claude Code hook output: the events as a message to the user
)

// report prints events as text as they come, or gathers them for finish.
type report struct {
	w      io.Writer
	mode   int
	events []Event
	count  int
}

// newReport starts a report. A run that replaced an older nerve picks up the
// events that one gathered before it handed over.
func newReport(w io.Writer, mode int) *report {
	r := &report{w: w, mode: mode}
	if earlier := os.Getenv(earlierEventsEnv); mode != asText && earlier != "" {
		json.Unmarshal([]byte(earlier), &r.events)
	}
	return r
}

func (r *report) add(e Event) {
	r.count++
	if r.mode != asText {
		r.events = append(r.events, e)
		return
	}
	fmt.Fprintln(r.w, e)
}

// finish ends the report; when it gathered events, it prints them and the
// error, if any, and returns true.
func (r *report) finish(err error) bool {
	switch r.mode {
	case asJSON:
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
	case asHook:
		json.NewEncoder(r.w).Encode(hookOutput(r.events, err))
		return true
	}
	return false
}

// hookOutput is what autosync hands Claude Code: a message shown to the user
// and kept out of Claude's context, and a skill rescan when it linked a
// skill, so the skill is usable in this session.
func hookOutput(events []Event, err error) map[string]any {
	var lines []string
	reload := false
	for _, e := range events {
		if e.Kind == evLinked && filepath.Base(filepath.Dir(e.Path)) == "skills" {
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
