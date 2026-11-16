package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Swappable in tests.
var (
	releaseURL = "https://github.com/medilies/nerve/releases/download"
	selfPath   = func() (string, error) {
		p, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(p)
	}
	reexec     = func(path string, env []string) error { return syscall.Exec(path, os.Args, env) }
	httpClient = &http.Client{Timeout: 5 * time.Minute}
)

// A run started by an upgrade gets both versions, so it reports the upgrade
// itself and a release that reports the wrong version cannot loop.
const (
	upgradedEnv     = "NERVE_UPGRADED_TO"
	upgradedFromEnv = "NERVE_UPGRADED_FROM"
)

// upgradeToPin replaces the running binary with the Brain's pinned release
// and runs it in its place. It does nothing when there is no pin or the pin
// already runs, and leaves development builds alone. When the Brain also pins
// the release's checksums.txt, the download must match it.
func upgradeToPin(m Manifest, r *report) error {
	pin := m.Nerve
	running := currentVersion()
	if pin == "" || running == pin {
		if from := os.Getenv(upgradedFromEnv); pin != "" && os.Getenv(upgradedEnv) == pin && from != "" {
			path, _ := selfPath()
			r.add(Event{Kind: evUpgraded, Path: path, Detail: from, Target: pin})
		}
		return nil
	}
	if !tagPattern.MatchString(running) {
		r.add(Event{Kind: evUpgradeSkipped, Detail: running, Target: pin})
		return nil
	}
	if os.Getenv(upgradedEnv) == pin {
		return fmt.Errorf("upgraded to %s, but the new binary reports %s", pin, running)
	}
	path, err := selfPath()
	if err != nil {
		return err
	}
	tmp := path + ".new"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot replace %s with nerve %s, the Brain's pin: %w; make its folder writable, or install %s by hand", path, pin, err, pin)
	}
	defer os.Remove(tmp) // a no-op once renamed
	bin, err := fetchRelease(pin, m.NerveChecksums)
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(bin); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return reexec(path, append(os.Environ(), upgradedFromEnv+"="+running, upgradedEnv+"="+pin))
}

// fetchRelease downloads this platform's binary of release tag and checks it
// against the release's checksums.txt, and that file against sums when the
// Brain pins it.
func fetchRelease(tag, sums string) ([]byte, error) {
	asset := fmt.Sprintf("nerve_%s_%s", runtime.GOOS, runtime.GOARCH)
	list, err := download(releaseURL + "/" + tag + "/checksums.txt")
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(list); sums != "" && hex.EncodeToString(got[:]) != sums {
		return nil, fmt.Errorf("nerve %s: checksums.txt does not match nerve-checksums in %s", tag, manifestFile)
	}
	want, err := checksumOf(list, asset)
	if err != nil {
		return nil, fmt.Errorf("nerve %s: %w", tag, err)
	}
	bin, err := download(releaseURL + "/" + tag + "/" + asset)
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(bin); hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("nerve %s: %s does not match checksums.txt", tag, asset)
	}
	return bin, nil
}

func download(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
