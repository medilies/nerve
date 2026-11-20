package infra

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"

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

// Zip zips a skill folder under its name, the same bytes for the same files.
func (ZipCache) Zip(s domain.Skill) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
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
	return buf.Bytes(), err
}

func (z ZipCache) Write(project, name string, b []byte) (string, error) {
	path := z.path(project, name)
	return path, writeFile(path, b)
}

func (z ZipCache) Has(project, name string) bool { return fileExists(z.path(project, name)) }

func (z ZipCache) Remove(project, name string) { _ = os.Remove(z.path(project, name)) }

func (ZipCache) FolderLink(dir string) string {
	found := ""
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipAll
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(path); err != nil || fi.IsDir() {
				found, _ = filepath.Rel(dir, path)
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}
