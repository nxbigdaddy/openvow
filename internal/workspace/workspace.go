// Package workspace 负责把仓库复制到临时目录，并把提交的文件安全地铺进去。
//
// 安全边界：所有写入必须落在临时目录内，路径上任何一级是符号链接都拒绝。
package workspace

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Workspace struct {
	root string
}

// Open 复制仓库到临时目录（跳过 .git），返回可写的副本。
func Open(repo string) (*Workspace, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("repo %q is not a directory", repo)
	}
	tmp, err := os.MkdirTemp("", "openvow-ws-*")
	if err != nil {
		return nil, err
	}
	if err := copyTree(abs, tmp); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	return &Workspace{root: tmp}, nil
}

func (w *Workspace) Root() string { return w.root }

func (w *Workspace) Close() error { return os.RemoveAll(w.root) }

// WriteAt 把 r 写到相对路径 rel。父目录按需创建，目标必须是普通文件。
func (w *Workspace) WriteAt(rel string, r io.Reader, perm fs.FileMode) error {
	full, err := safeJoin(w.root, rel)
	if err != nil {
		return err
	}
	if err := ensureNoSymlink(w.root, rel); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	// 目标已存在且是符号链接：拒绝，避免写到别处
	if fi, err := os.Lstat(full); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through symlink: %s", rel)
	}

	tmp, err := os.CreateTemp(filepath.Dir(full), ".openvow-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, full)
}

func safeJoin(root, rel string) (string, error) {
	// 先挡绝对路径：在 Windows 上 filepath.IsAbs(`\x`) 是 false，
	// 但 filepath.Join(root, `\x`) 会把它悄悄拼进 root，语义已经错了。
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return "", fmt.Errorf("absolute path %q", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return "", fmt.Errorf("absolute path %q", rel)
	}
	full := filepath.Join(root, clean)
	r, err := filepath.Rel(root, full)
	if err != nil {
		return "", err
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace: %q", rel)
	}
	return full, nil
}

// ensureNoSymlink 逐级检查路径上已存在的部分。
func ensureNoSymlink(root, rel string) error {
	cur := root
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil // 剩余部分都不存在，MkdirAll 会建普通目录
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in path: %q", rel)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // 不复制符号链接
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
