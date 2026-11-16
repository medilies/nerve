package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// syncKeybindings links the config's keybindings.json to the Brain's, or
// removes the link it made once the Brain has none. It never replaces a real
// file.
func syncKeybindings(brain, cfg string, st *state, out io.Writer) error {
	src := filepath.Join(brain, "keybindings.json")
	dst := filepath.Join(cfg, "keybindings.json")
	_, err := os.Stat(src)
	want := err == nil
	fi, err := os.Lstat(dst)
	isLink := err == nil && fi.Mode()&fs.ModeSymlink != 0
	switch {
	case err == nil && !isLink && want:
		return fmt.Errorf("%s is a real file, not a link: move what you need into the Brain, delete it, then sync again", dst)
	case !want:
		if isLink && st.Keybindings {
			if err := os.Remove(dst); err != nil {
				return err
			}
			fmt.Fprintf(out, "removed  %s\n", dst)
		}
		st.Keybindings = false
		return nil
	}
	st.Keybindings = true
	if target, _ := os.Readlink(dst); isLink && target == src {
		return nil
	}
	if isLink {
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		return err
	}
	if err := os.Symlink(src, dst); err != nil {
		return err
	}
	fmt.Fprintf(out, "linked   %s -> %s\n", dst, src)
	return nil
}
