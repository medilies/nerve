package infra

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

func TestZipHoldsTheSkillUnderItsNameTheSameEachTime(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("s/research/SKILL.md"), skillMD("research", ""))
	e.write(e.path("s/research/refs/a.md"), "a")
	e.write(e.path("s/research/.git/HEAD"), "x")
	z, err := NewZipCache()
	if err != nil {
		t.Fatal(err)
	}
	s := domain.Skill{Name: "research", Dir: e.path("s/research")}

	one, err := z.Zip(s)
	if err != nil {
		t.Fatal(err)
	}
	two, _ := z.Zip(s)

	if !bytes.Equal(one, two) {
		t.Errorf("the same skill zips differently")
	}
	zr, err := zip.NewReader(bytes.NewReader(one), int64(len(one)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if len(names) != 2 || names[0] != "research/SKILL.md" || names[1] != "research/refs/a.md" {
		t.Errorf("zip holds %v", names)
	}
	path, err := z.Write("acct", "research", one)
	if err != nil || path != e.path(".cache/nerve/claude.ai/acct/research.zip") || !z.Has("acct", "research") {
		t.Errorf("written at %s (%v)", path, err)
	}
	z.Remove("acct", "research")
	if z.Has("acct", "research") {
		t.Errorf("a removed zip is still there")
	}
}

func TestFolderLinkFindsWhatAZipCannotHold(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("s/x/SKILL.md"), "x")
	e.write(e.path("s/x/file.md"), "x")
	if err := os.Symlink(e.path("s/x/file.md"), e.path("s/x/to-file.md")); err != nil {
		t.Fatal(err)
	}
	z := ZipCache{}
	if got := z.FolderLink(e.path("s/x")); got != "" {
		t.Errorf("a link to a file is refused: %s", got)
	}
	if err := os.Symlink(e.path("s"), e.path("s/x/refs")); err != nil {
		t.Fatal(err)
	}
	if got := z.FolderLink(e.path("s/x")); got != "refs" {
		t.Errorf("found %q, want refs", got)
	}
}
