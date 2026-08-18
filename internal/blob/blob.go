package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ValidSHA(s string) bool { return shaRe.MatchString(s) }

type Store struct {
	objects string
	tmp     string
}

func New(root string) (*Store, error) {
	s := &Store{
		objects: filepath.Join(root, "objects"),
		tmp:     filepath.Join(root, "tmp"),
	}
	for _, dir := range []string{s.objects, s.tmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Put(r io.Reader) (sha string, size int64, err error) {
	tmp, err := os.CreateTemp(s.tmp, "put-*")
	if err != nil {
		return "", 0, err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	sha = hex.EncodeToString(h.Sum(nil))

	dst := s.Path(sha)
	if _, err := os.Stat(dst); err == nil {
		return sha, size, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", 0, err
	}
	return sha, size, nil
}

func (s *Store) PutFile(path string) (sha string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	return s.Put(f)
}

func (s *Store) Path(sha string) string {
	return filepath.Join(s.objects, sha[:2], sha[2:4], sha)
}

func (s *Store) Open(sha string) (*os.File, os.FileInfo, error) {
	if !ValidSHA(sha) {
		return nil, nil, errors.New("invalid hash")
	}
	f, err := os.Open(s.Path(sha))
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, st, nil
}

func (s *Store) TempFile(pattern string) (*os.File, error) {
	f, err := os.CreateTemp(s.tmp, pattern)
	if err != nil {
		return nil, fmt.Errorf("temp file: %w", err)
	}
	return f, nil
}

func (s *Store) Remove(sha string) error {
	if !ValidSHA(sha) {
		return errors.New("invalid hash")
	}
	if err := os.Remove(s.Path(sha)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
