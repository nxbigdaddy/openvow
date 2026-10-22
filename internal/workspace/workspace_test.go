package workspace

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestOpenCopiesTree(t *testing.T) {
	ws, err := Open(newRepo(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer ws.Close()

	b, err := os.ReadFile(filepath.Join(ws.Root(), "src", "a.go"))
	if err != nil || string(b) != "package a" {
		t.Fatalf("copy failed: %v %q", err, b)
	}
}

func TestWriteAt_Ok(t *testing.T) {
	ws, err := Open(newRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	if err := ws.WriteAt("src/deep/b.go", strings.NewReader("package b"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(ws.Root(), "src", "deep", "b.go"))
	if err != nil || string(b) != "package b" {
		t.Fatalf("read back: %v %q", err, b)
	}
}

func TestWriteAt_Escapes(t *testing.T) {
	ws, err := Open(newRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	for _, p := range []string{"../evil.txt", "src/../../evil.txt", "/abs.txt"} {
		if err := ws.WriteAt(p, strings.NewReader("x"), 0o644); err == nil {
			t.Errorf("WriteAt(%q) should fail", p)
		}
	}
}

func TestWriteAt_SymlinkParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("创建符号链接需要管理员权限，Windows 上跳过")
	}
	outside := t.TempDir()
	ws, err := Open(newRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	link := filepath.Join(ws.Root(), "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := ws.WriteAt("link/x.txt", strings.NewReader("x"), 0o644); err == nil {
		t.Fatal("writing through symlink must fail")
	}
	if _, err := os.Stat(filepath.Join(outside, "x.txt")); err == nil {
		t.Fatal("file escaped the workspace")
	}
}

var _ = io.Discard
