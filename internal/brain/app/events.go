// Package app holds nerve's use cases: carrying a Brain to the projects that
// list what they need, and changing what they list. It reaches files, git
// and the network only through its ports.
package app

import (
	"fmt"

	"github.com/medilies/nerve/internal/brain/domain"
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
}

// Kinds of Event.
const (
	EvLinked       = "linked"         // Path now links to Target
	EvRemoved      = "removed"        // a link or file at Path that nerve made
	EvWrote        = "wrote"          // Path
	EvKept         = "kept"           // Path, which nerve made, is left for the user; Detail says why
	EvSetting      = "setting"        // Key at Path went from Local to Brain
	EvDrift        = "drift"          // Key at Path changed outside the Brain, from Brain to Local, and was left as is; with Item, that item of the list
	EvPersonal     = "personal"       // Key at Path holds Local, or with Item that item of the list, which the Brain does not set: kept
	EvProblem      = "problem"        // Detail is wrong with Path (at Key)
	EvOK           = "ok"             // Detail
	EvDiff         = "diff"           // Detail is how Item changed in its source, "" when it did not
	EvPinned       = "pinned"         // the skills from repo Detail moved to Target
	EvAlreadyThere = "already-pinned" // repo Detail is already at Target
	EvPulled       = "pulled"         // the Brain at Path fast-forwarded to Target
	EvPullSkipped  = "pull-skipped"   // the Brain at Path was not pulled; Detail says why
	EvVersion      = "version"        // Detail is the running nerve's version; Target the Brain's pin
	EvBrain        = "brain"          // Path is the machine's Brain checkout; Detail its state
	EvAdded        = "added"          // Item joined Project
	EvDropped      = "dropped"        // Item left Project, or its local overlay when Detail says so
	EvOverlay      = "overlay"        // Key went to the local overlay at Path; Detail says why
	EvProject      = "project"        // Project; Detail describes it
	EvCheckout     = "checkout"       // Path belongs to Project; Detail its sync state
	EvItem         = "item"           // Item of Project; Detail says where it comes from
	EvValue        = "value"          // Key of Project is Brain; Detail says which layers give it
	EvAutosync     = "autosync"       // autosync is Detail for Project, or for the Brain when Project is ""
	EvCleaned      = "cleaned"        // nerve took its links and settings out of Path; Detail says why
	EvUnlisted     = "unlisted"       // this machine no longer lists Path, a checkout of Project: it is gone, or Detail says why
	EvSynced       = "synced"         // Project at Path is at Brain commit Target
	EvUpToDate     = "up-to-date"     // Project at Path is at Brain commit Target, and nothing changed
	EvUsage        = "usage"          // Detail is how to run nerve
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
			return fmt.Sprintf("set      %s = %s", e.Key, e.Brain)
		case e.Brain == "unset":
			return fmt.Sprintf("unset    %s (was %s)", e.Key, e.Local)
		}
		return fmt.Sprintf("set      %s = %s (was %s)", e.Key, e.Brain, e.Local)
	case EvDrift:
		switch {
		case e.Item != "" && e.Local != "unset":
			return fmt.Sprintf("drift    %s has %s here, not in the Brain: left as is", e.Key, e.Item)
		case e.Item != "":
			return fmt.Sprintf("drift    %s lacks %s here, which the Brain has: left as is", e.Key, e.Item)
		}
		return fmt.Sprintf("drift    %s is %s here, %s in the Brain: left as is", e.Key, e.Local, e.Brain)
	case EvPersonal:
		if e.Item != "" {
			return fmt.Sprintf("personal %s has %s: kept, the Brain does not set it", e.Key, e.Item)
		}
		return fmt.Sprintf("personal %s = %s: kept, the Brain does not set it", e.Key, e.Local)
	case EvProblem:
		if e.Key != "" {
			return fmt.Sprintf("problem  %s: %s: %s", e.Path, e.Key, e.Detail)
		}
		return fmt.Sprintf("problem  %s: %s", e.Path, e.Detail)
	case EvDiff:
		if e.Detail == "" {
			return "no diff  " + e.Item
		}
		return e.Detail
	case EvPinned:
		return fmt.Sprintf("pinned   %s at %s: review the diff above, then commit %s and sync", e.Detail, e.Target, domain.ManifestFile)
	case EvAlreadyThere:
		return fmt.Sprintf("%s is already pinned at %s", e.Detail, e.Target)
	case EvPulled:
		return fmt.Sprintf("pulled   the Brain to %s", Short(e.Target))
	case EvPullSkipped:
		return fmt.Sprintf("no pull  %s: %s", e.Path, e.Detail)
	case EvVersion:
		if e.Target != "" && e.Target != e.Detail {
			return fmt.Sprintf("nerve    %s (the Brain pins %s: install it)", e.Detail, e.Target)
		}
		return "nerve    " + e.Detail
	case EvBrain:
		if e.Detail != "" {
			return fmt.Sprintf("brain    %s (%s)", e.Path, e.Detail)
		}
		return "brain    " + e.Path
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
	case EvValue:
		return fmt.Sprintf("%s = %s  (%s)", e.Key, e.Brain, e.Detail)
	case EvAutosync:
		if e.Project == "" {
			return fmt.Sprintf("autosync %s by default", e.Detail)
		}
		return fmt.Sprintf("autosync %s for %s", e.Detail, e.Project)
	case EvCleaned:
		return fmt.Sprintf("cleaned  %s: %s", e.Path, e.Detail)
	case EvUnlisted:
		if e.Detail != "" {
			return fmt.Sprintf("unlisted %s, a checkout of %s: %s", e.Path, e.Project, e.Detail)
		}
		return fmt.Sprintf("unlisted %s, a checkout of %s that is gone", e.Path, e.Project)
	case EvSynced:
		return fmt.Sprintf("synced   %s at %s", e.Project, Short(e.Target))
	case EvUpToDate:
		return fmt.Sprintf("%s is up to date at %s", e.Project, Short(e.Target))
	}
	return e.Detail
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
