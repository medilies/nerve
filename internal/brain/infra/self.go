package infra

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// Release is where nerve's releases come from, and how it replaces and
// re-runs itself.
type Release struct {
	URL    string
	Client *http.Client
	// Path is the running binary as it was run, through any link to it.
	Path func() (string, error)
	// Exec runs the binary at path in place of this process.
	Exec func(path string, env []string) error
}

// GitHubRelease is nerve's releases on GitHub, replacing the running binary.
func GitHubRelease() Release {
	return Release{
		URL:    "https://github.com/medilies/nerve/releases/download",
		Client: &http.Client{Timeout: 5 * time.Minute},
		Path:   ranAs,
		Exec:   func(path string, env []string) error { return syscall.Exec(path, os.Args, env) },
	}
}

// ranAs is the running binary as it was run, such as bin/nerve linking
// v1/nerve, so a hook calling it follows the link to the next version. It
// is the binary itself when the name it was run by leads elsewhere.
func ranAs() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	name := os.Args[0]
	if !strings.ContainsRune(name, filepath.Separator) {
		if name, err = exec.LookPath(name); err != nil {
			return exe, nil
		}
	}
	name, err = filepath.Abs(name)
	if err != nil {
		return exe, nil
	}
	ran, err := os.Stat(name)
	if err != nil {
		return exe, nil
	}
	if self, err := os.Stat(exe); err != nil || !os.SameFile(ran, self) {
		return exe, nil
	}
	return name, nil
}

// binary is the file the running binary really is, which an upgrade
// replaces.
func (r Release) binary() (string, error) {
	path, err := r.Path()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

// A run started by an upgrade gets both versions, so it reports the upgrade
// itself and a release that reports the wrong version cannot loop.
const (
	upgradedEnv     = "NERVE_UPGRADED_TO"
	upgradedFromEnv = "NERVE_UPGRADED_FROM"
)

// earlierEventsEnv carries a gathering report's events into the run that
// replaces it, so its one document still holds them.
const earlierEventsEnv = "NERVE_EARLIER_EVENTS"

// Resumed reports whether this run replaced an older nerve, which did the
// pull already.
func Resumed() bool { return os.Getenv(upgradedEnv) != "" }

// Version is the version a release set, else what the Go toolchain recorded,
// such as a go install tag or "(devel)".
func Version(set string) string {
	if set != "" {
		return set
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(devel)"
}

// Nerve is the running nerve: its embedded skill, its version and its
// release.
type Nerve struct {
	SkillMD []byte
	Ver     string
	Release Release
}

var _ app.Self = Nerve{}

func (n Nerve) Version() string { return n.Ver }

// Skill writes the binary's skill into the user cache, where sync links it
// from, so the two always match.
func (n Nerve) Skill() (domain.Skill, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return domain.Skill{}, err
	}
	dir := filepath.Join(cache, "nerve", "skill", "nerve")
	path := filepath.Join(dir, "SKILL.md")
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, n.SkillMD) {
		if err := writeFile(path, n.SkillMD); err != nil { // at once: another session may read it
			return domain.Skill{}, err
		}
	}
	return readSkill("nerve", dir, "nerve")
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./~+-]+$`)

func (n Nerve) Command() (string, error) {
	path, err := n.Release.Path()
	if err != nil {
		return "", err
	}
	if !shellSafe.MatchString(path) {
		path = "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	}
	return path + " sync --here --hook", nil
}

// Upgrade replaces the running binary with the Brain's pinned release and
// runs it in its place. It does nothing when there is no pin or the pin
// already runs, and leaves development builds alone. When the Brain also
// pins the release's checksums.txt, the download must match it.
func (n Nerve) Upgrade(m domain.Manifest, r app.Reporter) error {
	pin := m.Nerve
	running := n.Ver
	if pin == "" || running == pin {
		if from := os.Getenv(upgradedFromEnv); pin != "" && os.Getenv(upgradedEnv) == pin && from != "" {
			path, _ := n.Release.binary()
			r.Add(app.Event{Kind: app.EvUpgraded, Path: path, Detail: from, Target: pin})
		}
		return nil
	}
	if !domain.TagPattern.MatchString(running) {
		r.Add(app.Event{Kind: app.EvUpgradeSkipped, Detail: running, Target: pin})
		return nil
	}
	if os.Getenv(upgradedEnv) == pin {
		return fmt.Errorf("upgraded to %s, but the new binary reports %s", pin, running)
	}
	path, err := n.Release.binary()
	if err != nil {
		return err
	}
	// A file of its own: another session may be upgrading too.
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".new-*")
	if err != nil {
		return fmt.Errorf("cannot replace %s with nerve %s, the Brain's pin: %w; make its folder writable, or install %s by hand", path, pin, err, pin)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // a no-op once renamed
	bin, err := n.fetchRelease(pin, m.NerveChecksums)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(bin); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(0o755); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	env := os.Environ()
	if g, ok := r.(interface{ Gathered() []app.Event }); ok && g.Gathered() != nil {
		// The re-run starts a new report: hand it the events so far.
		b, _ := json.Marshal(g.Gathered())
		env = append(env, earlierEventsEnv+"="+string(b))
	}
	return n.Release.Exec(path, append(env, upgradedFromEnv+"="+running, upgradedEnv+"="+pin))
}

// fetchRelease downloads this platform's binary of release tag and checks
// it against the release's checksums.txt, and that file against sums when
// the Brain pins it.
func (n Nerve) fetchRelease(tag, sums string) ([]byte, error) {
	asset := fmt.Sprintf("nerve_%s_%s", runtime.GOOS, runtime.GOARCH)
	list, err := n.download(n.Release.URL + "/" + tag + "/checksums.txt")
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(list); sums != "" && hex.EncodeToString(got[:]) != sums {
		return nil, fmt.Errorf("nerve %s: checksums.txt does not match nerve-checksums in %s", tag, domain.ManifestFile)
	}
	want, err := checksumOf(list, asset)
	if err != nil {
		return nil, fmt.Errorf("nerve %s: %w", tag, err)
	}
	bin, err := n.download(n.Release.URL + "/" + tag + "/" + asset)
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(bin); hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("nerve %s: %s does not match checksums.txt", tag, asset)
	}
	return bin, nil
}

func (n Nerve) download(url string) ([]byte, error) {
	resp, err := n.Release.Client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// checksumOf finds a file's SHA-256 in sha256sum output.
func checksumOf(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no %s in checksums.txt", name)
}
