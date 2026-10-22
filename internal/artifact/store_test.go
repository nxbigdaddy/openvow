package artifact

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestPutAndGet(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sha, n, err := st.Put(strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("size = %d", n)
	}
	if len(sha) != 64 {
		t.Fatalf("sha = %q", sha)
	}

	// 重复写入同一内容：同一个 sha，不报错
	sha2, _, err := st.Put(strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if sha2 != sha {
		t.Fatalf("sha mismatch: %s vs %s", sha, sha2)
	}

	f, err := st.Open(sha)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	if string(b) != "hello" {
		t.Fatalf("content = %q", b)
	}

	size, err := st.Size(sha)
	if err != nil || size != 5 {
		t.Fatalf("size = %d, %v", size, err)
	}
}

func TestBadSHA(t *testing.T) {
	st, _ := New(t.TempDir())
	for _, bad := range []string{"", "abc", strings.Repeat("g", 64), "../etc/passwd"} {
		if _, err := st.Open(bad); err == nil {
			t.Errorf("Open(%q) should fail", bad)
		}
		if _, err := st.Size(bad); err == nil {
			t.Errorf("Size(%q) should fail", bad)
		}
	}
}

func TestOpenMissing(t *testing.T) {
	st, _ := New(t.TempDir())
	_, err := st.Open(strings.Repeat("a", 64))
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

var _ = os.Stat
