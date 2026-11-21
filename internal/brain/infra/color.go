package infra

import (
	"os"
	"strings"

	"github.com/medilies/nerve/internal/brain/app"
)

// ANSI colors, by what a line means.
const (
	made    = "32"   // green: nerve made or sent something
	takeOut = "35"   // magenta: nerve took something out
	look    = "33"   // yellow: the user may want to look
	wrong   = "1;31" // bold red: something is wrong
	info    = "36"   // cyan: how things stand
	idle    = "2"    // dim: nothing to do
	strong  = "1"    // bold
	minus   = "31"   // red: a line a diff takes out
)

// labels colors the label that starts an event's line.
var labels = map[string]string{
	app.EvLinked: made, app.EvWrote: made, app.EvAdded: made, app.EvPushed: made,
	app.EvBrainSynced: made, app.EvSynced: made, app.EvPinned: made, app.EvOK: made, app.EvAllowed: made,
	app.EvRemoved: takeOut, app.EvDropped: takeOut, app.EvCleaned: takeOut, app.EvDisallowed: takeOut,
	app.EvDrift: look, app.EvPersonal: look, app.EvKept: look, app.EvOverlay: look,
	app.EvUnlisted: look, app.EvBrainSkipped: look, app.EvChanged: look, app.EvNote: look,
	app.EvProblem: wrong,
	app.EvBrain:   info, app.EvProject: info, app.EvCheckout: info, app.EvItem: info, app.EvTip: info,
}

// Colors says whether to color text written to f: a terminal, unless
// NO_COLOR is set or TERM is dumb.
func Colors(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func paint(code, s string) string { return "\x1b[" + code + "m" + s + "\x1b[0m" }

// paintEvent colors line, an event's text with control characters hidden.
func paintEvent(e app.Event, line string) string {
	switch {
	case e.Kind == app.EvDiff:
		return paintDiff(line)
	case e.Kind == app.EvChanged && e.Detail != "":
		first, diff, _ := strings.Cut(line, "\n")
		return paintLabel(look, first) + "\n" + paintDiff(diff)
	case e.Kind == app.EvUpToDate, e.Kind == app.EvAlreadyThere, e.Kind == app.EvSame:
		return paint(idle, line)
	case e.Kind == app.EvUsage:
		return PaintCommands(line)
	case e.Kind == app.EvSetting && e.Brain == "unset":
		return paintLabel(takeOut, line)
	case e.Kind == app.EvSetting:
		return paintLabel(made, line)
	case labels[e.Kind] != "":
		return paintLabel(labels[e.Kind], line)
	}
	return line
}

// paintLabel colors the first word of line.
func paintLabel(code, line string) string {
	label, rest, _ := strings.Cut(line, " ")
	return paint(code, label) + " " + rest
}

func paintDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	header := true // until the first hunk: which files, and their modes
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "@@"):
			header = false
			lines[i] = paint(info, l)
		case header:
			lines[i] = paint(strong, l)
		case strings.HasPrefix(l, "+"):
			lines[i] = paint(made, l)
		case strings.HasPrefix(l, "-"):
			lines[i] = paint(minus, l)
		}
	}
	return strings.Join(lines, "\n")
}

// PaintCommands colors a usage, or an error that offers commands: each
// "usage:" in bold, and each command that starts a line in its column.
func PaintCommands(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "usage:"):
			lines[i] = paint(strong, "usage:") + strings.TrimPrefix(l, "usage:")
		case strings.HasPrefix(l, "  ") && len(l) > 2 && l[2] != ' ':
			if cmd, rest, ok := strings.Cut(l[2:], "  "); ok {
				lines[i] = "  " + paint(info, cmd) + "  " + rest
			}
		}
	}
	return strings.Join(lines, "\n")
}

// PaintError colors an error printed on its own: "error:" in red, and the
// commands it offers.
func PaintError(msg string) string { return paint(wrong, "error:") + " " + PaintCommands(msg) }
