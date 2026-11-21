// Package infra holds nerve's adapters: the TOML and JSON files it reads and
// writes, git, the source cache, links, zips, its own release, and how it
// prints reports.
package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/medilies/nerve/internal/brain/domain"
)

// writeFile replaces path's content all at once, so Claude Code never reads
// it half written. A path that is a link keeps the link and replaces what it
// points to, even a file that does not exist yet. An existing file keeps
// its mode, and a new one gets 0644 less the umask, as os.WriteFile gives.
func writeFile(path string, b []byte) error {
	path, err := linkEnd(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := newTemp(path)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }() // a no-op once renamed
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		if err := f.Chmod(fi.Mode().Perm()); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// newTemp creates a file of its own next to path, as os.CreateTemp does,
// but with the mode os.WriteFile gives, since it replaces path.
func newTemp(path string) (*os.File, error) {
	for range 100 {
		name := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+strconv.FormatUint(rand.Uint64(), 36))
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
	return nil, fmt.Errorf("%s: no free name for a temp file", path)
}

// linkEnd follows path while it is a link, to the file it ends at, which
// may not exist. A relative link is read from the folder it really is in,
// its own folder's links followed.
func linkEnd(path string) (string, error) {
	for range 40 {
		target, err := os.Readlink(path)
		if err != nil {
			return path, nil // not a link, or nothing there
		}
		if !filepath.IsAbs(target) {
			dir := filepath.Dir(path)
			if real, err := filepath.EvalSymlinks(dir); err == nil {
				dir = real
			}
			target = filepath.Join(dir, target)
		}
		path = target
	}
	return "", fmt.Errorf("%s: too many links", path)
}

// readObject reads a JSON object, or returns nil when the file is missing.
// People edit such files, so one that is blank, or starts with a byte order
// mark, reads too. Numbers are read as domain.Numbers makes them, so they
// keep every digit.
func readObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	var obj map[string]any
	if len(bytes.TrimSpace(b)) > 0 {
		if err := decodeJSON(b, &obj); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if obj == nil {
		obj = map[string]any{}
	}
	domain.Numbers(obj)
	return obj, nil
}

// decodeJSON reads one JSON value into v, numbers as json.Number.
func decodeJSON(b []byte, v any) error {
	if err := json.Unmarshal(b, new(json.RawMessage)); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

func writeObject(path string, obj map[string]any) error {
	if obj == nil {
		obj = map[string]any{}
	}
	if err := writeJSON(path, obj); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// readJSON reads path into v, leaving v alone when the file is missing.
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err == nil {
		err = decodeJSON(b, v)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeJSON writes v indented, with &, < and > as they are.
func writeJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return writeFile(path, buf.Bytes())
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// expandHome expands a leading ~ in path.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// shortenHome writes a path under the home folder as ~/…, so a file reads
// the same for anyone. A home folder that is a link counts under its own
// path too.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	homes := []string{home}
	if real, err := filepath.EvalSymlinks(home); err == nil && real != home {
		homes = append(homes, real)
	}
	for _, home := range homes {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "~"
			}
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return path
}

// gitTimeout bounds one git command, so a stalled network never holds a
// session start for long.
const gitTimeout = 2 * time.Minute

// deadline, when set, bounds every git command of this run, whatever its
// own timeout: autosync must finish before Claude Code stops waiting.
var deadline time.Time

// SetDeadline makes every git command of this run stop by t; the zero time
// lifts the bound.
func SetDeadline(t time.Time) { deadline = t }

// repoEnv are the variables that point git at one repo, as a git hook sets
// them; nerve names each repo by its folder instead.
var repoEnv = []string{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_DIR", "GIT_GRAFT_FILE", "GIT_IMPLICIT_WORK_TREE",
	"GIT_INDEX_FILE", "GIT_NAMESPACE", "GIT_NO_REPLACE_OBJECTS", "GIT_OBJECT_DIRECTORY", "GIT_PREFIX",
	"GIT_REPLACE_REF_BASE", "GIT_SHALLOW_FILE", "GIT_WORK_TREE",
}

// askEnv are the variables that let git, ssh or a credential helper ask
// for a password: a program to ask with, as an editor such as VS Code sets
// one, or a display to open a dialog on.
var askEnv = []string{"GIT_ASKPASS", "SSH_ASKPASS", "DISPLAY", "WAYLAND_DISPLAY"}

// gitError is a git command that failed, with its exit code.
type gitError struct {
	code int
	msg  string
}

func (e *gitError) Error() string { return e.msg }

// exitCode is the exit code of a git command that failed, or -1 when it
// did not run to its end.
func exitCode(err error) int {
	var ge *gitError
	if errors.As(err, &ge) {
		return ge.code
	}
	return -1
}

// git runs git in dir, and returns what it printed, even when it fails. It
// never asks for a password, since autosync runs it with no one to answer:
// it has no terminal or display, and runs no askpass helper, nor ssh's, so
// a remote that needs one fails. The user's own ssh command is kept. It
// stops after gitTimeout.
func git(dir string, args ...string) (string, error) { return gitWithin(gitTimeout, dir, args...) }

// gitWithin runs git as git does, stopping after timeout.
func gitWithin(timeout time.Duration, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	late := !deadline.IsZero() && deadline.Before(time.Now().Add(timeout))
	if late {
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.askPass="}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(repoEnv, name) || slices.Contains(askEnv, name)
	}), "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never", "GCM_INTERACTIVE=never")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) } // ssh too
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		switch {
		case ctx.Err() != nil && late:
			return stdout.String(), fmt.Errorf("git %s: out of time for this run", args[0])
		case ctx.Err() != nil:
			return stdout.String(), fmt.Errorf("git %s: no answer after %s", args[0], timeout)
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return stdout.String(), err
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = fmt.Sprintf("git %s: %v", args[0], err)
		}
		return stdout.String(), &gitError{code: exit.ExitCode(), msg: msg}
	}
	return stdout.String(), nil
}

// firstLine is the first line of an error, which for git says what failed.
func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

func trim(s string) string { return strings.TrimSpace(s) }
