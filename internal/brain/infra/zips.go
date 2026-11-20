package infra

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/medilies/nerve/internal/brain/app"
	"github.com/medilies/nerve/internal/brain/domain"
)

// ZipCache keeps the skill zips built for claude.ai accounts in the user
// cache, never in the Brain.
type ZipCache struct {
	dir string
}

// NewZipCache keeps zips in the user cache.
func NewZipCache() (ZipCache, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return ZipCache{}, err
	}
	return ZipCache{dir: filepath.Join(cache, "nerve", "claude.ai")}, nil
}

var _ app.Zips = ZipCache{}

func (z ZipCache) path(project, name string) string {
	return filepath.Join(z.dir, project, name+".zip")
}

// Zip zips a skill folder under its name, the same bytes for the same
// files. A link to a file in the skill is zipped as that file; any other
// link is refused, so nothing outside the skill reaches the zip.
func (ZipCache) Zip(s domain.Skill) ([]byte, error) {
	root, err := filepath.EvalSymlinks(s.Dir)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err = filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(s.Dir, path)
		if d.Type()&fs.ModeSymlink != 0 {
			if why := linkProblem(root, path); why != "" {
				return fmt.Errorf("%s links %s, which a zip cannot hold", filepath.ToSlash(rel), why)
			}
		}
		w, err := zw.Create(s.Name + "/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	})
	if err == nil {
		err = zw.Close()
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// linkProblem says where the link at path leads that a zip of the skill at
// root cannot follow, or "".
func linkProblem(root, path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "to nothing"
	}
	if rel, err := filepath.Rel(root, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "out of the skill"
	}
	if fi, err := os.Stat(target); err != nil || fi.IsDir() {
		return "to a folder"
	}
	return ""
}

func (z ZipCache) Write(project, name string, b []byte) (string, error) {
	path := z.path(project, name)
	return path, writeFile(path, b)
}

func (z ZipCache) Has(project, name string) bool { return fileExists(z.path(project, name)) }

func (z ZipCache) Remove(project, name string) { _ = os.Remove(z.path(project, name)) }
