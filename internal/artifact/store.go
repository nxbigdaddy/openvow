// Package artifact 是内容寻址的产物库：sha256 决定路径，只写一次，可重复读。
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrNotFound = errors.New("artifact not found")

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) Root() string { return s.root }

// path 只接受 64 位小写十六进制，避免路径注入。
func (s *Store) path(sha string) (string, error) {
	if len(sha) != 64 {
		return "", fmt.Errorf("invalid sha256 length %d", len(sha))
	}
	for _, r := range sha {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return "", fmt.Errorf("invalid sha256 character %q", r)
		}
	}
	return filepath.Join(s.root, sha[:2], sha), nil
}

// Put 写入内容并返回 sha256 与字节数。已存在则直接复用。
func (s *Store) Put(r io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(s.root, "upload-*")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), r)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", 0, err
	}

	sha := hex.EncodeToString(h.Sum(nil))
	dst, err := s.path(sha)
	if err != nil {
		return "", 0, err
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		return sha, n, nil // 已存在，幂等
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", 0, err
	}
	return sha, n, nil
}

func (s *Store) Open(sha string) (*os.File, error) {
	p, err := s.path(sha)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return f, err
}

func (s *Store) Size(sha string) (int64, error) {
	p, err := s.path(sha)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(p)
	if os.IsNotExist(err) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
