// Package app holds nerve's use cases: carrying a Brain to the projects that
// list what they need, and changing what they list. It reaches files, git
// and the network only through its ports.
package app

import (
	"fmt"
	"strings"
)

// Event is one thing a use case did or found. Kind says which; the other
// fields are set as that kind needs.
type Event struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"` // "" for common, where an event can be about either
	Path    string `json:"path,omitempty"`
	Target  string `json:"target,omitempty"`
	Item    string `json:"item,omitempty"` // kind and name, such as "skill grilling"; for drift in a list, the item as JSON
	Key     string `json:"key,omitempty"`
	Local   string `json:"local,omitempty"` // a settings value as JSON, "unset" when absent
	Brain   string `json:"brain,omitempty"` // likewise, as the Brain would write it
	Detail  string `json:"detail,omitempty"`
	// For a source's tip: the pin's commit, and when each commit was made.
	From       string       `json:"from,omitempty"`
	FromDate   string       `json:"fromDate,omitempty"`
	TargetDate string       `json:"targetDate,omitempty"`
	Files      []FileChange `json:"files,omitempty"` // for a changed skill
}

// Kinds of Event.
const (
	EvLinked       = "linked"           // Path now links to Target
	EvRemoved      = "removed"          // a link or file at Path that nerve made
	EvWrote        = "wrote"            // Path
	EvKept         = "kept"             // Path, which nerve made, is left for the user; Detail says why
	EvSetting      = "setting"          // Key at Path went from Local to Brain
	EvDrift        = "drift"            // Key at Path changed outside the Brain, from Brain to Local, and was left as is; with Item, that item of the list; Detail "local overlay" when the overlay gives it
	EvPersonal     = "personal"         // Key at Path holds Local, or with Item that item of the list, which the Brain does not set: kept
	EvProblem      = "problem"          // Detail is wrong with Path (at Key)
	EvOK           = "ok"               // Detail
	EvDiff         = "diff"             // Detail is how Item, brain.json, changed in the Brain and was not pushed
	EvTip          = "tip"              // source Key, pinned at From of FromDate, has its tip at Target of TargetDate
	EvChanged      = "changed"          // Item from source Key changed in Files since the pin, and is at Path at the tip; Detail is the diff, when asked
	EvSame         = "same"             // Item from source Key did not change since the pin
	EvPinned       = "pinned"           // the skills from repo Detail moved to Target
	EvAlreadyThere = "already-pinned"   // repo Detail is already at Target
	EvBrainSynced  = "brain-synced"     // the Brain at Path is now its upstream's Target; Detail what it dropped, if anything
	EvBrainSkipped = "brain-not-synced" // the Brain at Path was not made its upstream's; Detail says why
	EvPushed       = "pushed"           // the Brain at Path pushed Target to its upstream; Detail is the commit's message, if it made one
	EvBrain        = "brain"            // Path is the machine's Brain checkout; Detail its state
	EvAdded        = "added"            // Item joined Project
	EvDropped      = "dropped"          // Item left Project, or its local overlay when Detail says so
	EvOverlay      = "overlay"          // Key went to the local overlay at Path; Detail says why
	EvProject      = "project"          // Project; Detail describes it
	EvCheckout     = "checkout"         // Path belongs to Project; Detail its sync state
	EvItem         = "item"             // Item of Project; Detail says where it comes from
	EvCleaned      = "cleaned"          // nerve took its links and settings out of Path; Detail says why
	EvUnlisted     = "unlisted"         // this machine no longer lists Path, a checkout of Project: it is gone, or Detail says why
	EvSynced       = "synced"           // Project at Path is at Brain commit Target
	EvUpToDate     = "up-to-date"       // Project at Path is at Brain commit Target, and nothing changed
	EvUsage        = "usage"            // Detail is how to run nerve
	EvAllowed      = "allowed"          // allowedSettings lists Key now, or Detail "already"
	EvDisallowed   = "disallowed"       // allowedSettings no longer lists Key
	EvNote         = "note"             // Detail is worth knowing about Path, but not wrong
	EvVersion      = "version"          // Detail is nerve's version
)

func (e Event) String() string {
	switch e.Kind {
	case EvLinked:
		return fmt.Sprintf("linked   %s -> %s", e.Path, e.Target)
	case EvRemoved, EvWrote:
		return fmt.Sprintf("%-8s %s", e.Kind, e.Path)
	case EvKept:
		return fmt.Sprintf("kept     %s: %s", e.Path, e.Detail)
	case EvSetting:
		switch {
		case e.Local == "unset":
			return fmt.Sprintf("set      %s%s = %s", at(e.Path), e.Key, e.Brain)
		case e.Brain == "unset":
			return fmt.Sprintf("unset    %s%s (was %s)", at(e.Path), e.Key, e.Local)
		}
		return fmt.Sprintf("set      %s%s = %s (was %s)", at(e.Path), e.Key, e.Brain, e.Local)
	case EvDrift:
		from := "the Brain"
		if e.Detail != "" {
			from = "the " + e.Detail
		}
		switch {
		case e.Item != "" && e.Local != "unset":
			return fmt.Sprintf("drift    %s%s has %s here, not in %s: left as is", at(e.Path), e.Key, e.Item, from)
		case e.Item != "":
			return fmt.Sprintf("drift    %s%s lacks %s here, which %s has: left as is", at(e.Path), e.Key, e.Item, from)
		}
		return fmt.Sprintf("drift    %s%s is %s here, %s in %s: left as is", at(e.Path), e.Key, e.Local, e.Brain, from)
	case EvPersonal:
		if e.Item != "" {
			return fmt.Sprintf("personal %s%s has %s: kept, the Brain does not set it", at(e.Path), e.Key, e.Item)
		}
		return fmt.Sprintf("personal %s%s = %s: kept, the Brain does not set it", at(e.Path), e.Key, e.Local)
	case EvProblem:
		if e.Key != "" {
			return fmt.Sprintf("problem  %s: %s: %s", e.Path, e.Key, e.Detail)
		}
		return fmt.Sprintf("problem  %s: %s", e.Path, e.Detail)
	case EvDiff:
		return e.Detail
	case EvTip:
		return fmt.Sprintf("tip      %s: pinned at %s (%s), tip %s (%s)", e.Key, Short(e.From), e.FromDate, Short(e.Target), e.TargetDate)
	case EvChanged:
		files := make([]string, len(e.Files))
		for i, f := range e.Files {
			files[i] = fmt.Sprintf("%s +%d -%d", f.Path, f.Added, f.Removed)
			if f.Binary {
				files[i] = f.Path + " binary"
			}
		}
		line := fmt.Sprintf("changed  %s: %s", e.Item, strings.Join(files, ", "))
		if e.Detail != "" {
			return line + "\n" + e.Detail
		}
		return line
	case EvSame:
		return "same     " + e.Item
	case EvPinned:
		return fmt.Sprintf("pinned   %s at %s", e.Detail, Short(e.Target))
	case EvAlreadyThere:
		return fmt.Sprintf("%s is already pinned at %s", e.Detail, e.Target)
	case EvBrainSynced:
		if e.Detail != "" {
			return fmt.Sprintf("synced   the Brain to %s, dropping %s", Short(e.Target), e.Detail)
		}
		return fmt.Sprintf("synced   the Brain to %s", Short(e.Target))
	case EvPushed:
		if e.Detail != "" {
			return fmt.Sprintf("pushed   the Brain at %s: %s", Short(e.Target), e.Detail)
		}
		return fmt.Sprintf("pushed   the Brain at %s", Short(e.Target))
	case EvBrainSkipped:
		return fmt.Sprintf("no sync  the Brain at %s: %s", e.Path, e.Detail)
	case EvBrain:
		if e.Detail != "" {
			return fmt.Sprintf("brain    %s (%s)", e.Path, e.Detail)
		}
		return "brain    " + e.Path
	case EvAllowed:
		if e.Detail != "" {
			return fmt.Sprintf("allowed  %s %s", e.Key, e.Detail)
		}
		return "allowed  " + e.Key
	case EvDisallowed:
		return fmt.Sprintf("dropped  %s from allowedSettings", e.Key)
	case EvNote:
		return fmt.Sprintf("note     %s: %s", e.Path, e.Detail)
	case EvAdded:
		return fmt.Sprintf("added    %s to project %s", e.Item, e.Project)
	case EvDropped:
		switch {
		case e.Detail != "":
			return fmt.Sprintf("dropped  %s from the %s of project %s", e.Item, e.Detail, e.Project)
		case e.Project == "":
			return fmt.Sprintf("dropped  %s from common", e.Item)
		}
		return fmt.Sprintf("dropped  %s from project %s", e.Item, e.Project)
	case EvOverlay:
		return fmt.Sprintf("overlay  %s set in %s: %s", e.Key, e.Path, e.Detail)
	case EvProject:
		return fmt.Sprintf("project  %s: %s", e.Project, e.Detail)
	case EvCheckout:
		return fmt.Sprintf("checkout %s (%s): %s", e.Path, e.Project, e.Detail)
	case EvItem:
		return fmt.Sprintf("%-8s %s", e.Item, e.Detail)
	case EvCleaned:
		return fmt.Sprintf("cleaned  %s: %s", e.Path, e.Detail)
	case EvUnlisted:
		if e.Detail != "" {
			return fmt.Sprintf("unlisted %s, a checkout of %s: %s", e.Path, e.Project, e.Detail)
		}
		return fmt.Sprintf("unlisted %s, a checkout of %s that is gone", e.Path, e.Project)
	case EvSynced:
		return fmt.Sprintf("synced   %s at %s in %s", e.Project, Short(e.Target), e.Path)
	case EvUpToDate:
		return fmt.Sprintf("%s is up to date at %s in %s", e.Project, Short(e.Target), e.Path)
	}
	return e.Detail
}

// at is where a setting is, as a prefix: "" when the event names no place.
func at(path string) string {
	if path == "" {
		return ""
	}
	return path + ": "
}

// Short is a commit as people read it.
func Short(sha string) string {
	switch {
	case len(sha) > 7:
		return sha[:7]
	case sha == "":
		return "an uncommitted Brain"
	}
	return sha
}

// Reporter takes the events of a use case as they happen.
type Reporter interface {
	Add(Event)
}
