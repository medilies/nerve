package infra

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
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

func TestZipRefusesALinkItCannotHold(t *testing.T) {
	e := newTestEnv(t)
	e.write(e.path("secret.txt"), "TOPSECRET")
	e.write(e.path("s/x/SKILL.md"), "x")
	e.write(e.path("s/x/file.md"), "x")
	e.link(e.path("s/x/to-file.md"), e.path("s/x/file.md"))
	z := ZipCache{}
	s := domain.Skill{Name: "x", Dir: e.path("s/x")}
	if _, err := z.Zip(s); err != nil {
		t.Errorf("a link to a file in the skill is refused: %v", err)
	}
	for link, want := range map[string]string{
		"notes.md": "notes.md links out of the skill",
		"refs":     "refs links to a folder",
		"gone.md":  "gone.md links to nothing",
	} {
		t.Run(link, func(t *testing.T) {
			e.link(e.path("s/x/"+link), map[string]string{"notes.md": "../../secret.txt", "refs": e.path("s/x"), "gone.md": "nowhere"}[link])
			defer func() { _ = os.Remove(e.path("s/x/" + link)) }()

			zip, err := z.Zip(s)

			if err == nil || !strings.Contains(err.Error(), want+", which a zip cannot hold") || bytes.Contains(zip, []byte("TOPSECRET")) {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestZipKeepsWhatAFileMayRun(t *testing.T) {
	openBug(t, "zips lose executable bits")
	e := newTestEnv(t)
	e.write(e.path("s/x/SKILL.md"), skillMD("x", ""))
	e.write(e.path("s/x/scripts/run.sh"), "#!/bin/sh\n")
	if err := os.Chmod(e.path("s/x/scripts/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	b, err := ZipCache{}.Zip(domain.Skill{Name: "x", Dir: e.path("s/x")})
	if err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "x/scripts/run.sh" && f.Mode()&0o111 == 0 {
			t.Errorf("%s is zipped as %s", f.Name, f.Mode())
		}
	}
}

func TestZipTakesASkillWhoseFolderIsALink(t *testing.T) {
	openBug(t, "a skill whose folder is a link is refused by the zip")
	e := newTestEnv(t)
	e.write(e.path("real/x/SKILL.md"), skillMD("x", ""))
	e.link(e.path("s/x"), e.path("real/x"))

	b, err := ZipCache{}.Zip(domain.Skill{Name: "x", Dir: e.path("s/x")})

	if err != nil || !bytes.Contains(b, []byte("x/SKILL.md")) {
		t.Errorf("zipped %d bytes (%v)", len(b), err)
	}
}
