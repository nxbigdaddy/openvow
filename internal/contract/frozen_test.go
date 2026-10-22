package contract

import (
	"runtime"
	"testing"
)

func codes(vs []Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Code + ":" + v.Path
	}
	return out
}

func TestCheck_OK(t *testing.T) {
	writes := []string{"src/parser/**"}
	frozen := []Frozen{{Path: "src/parser/api.rs", SHA256: "aaa"}}
	files := []SubmittedFile{
		{Path: "src/parser/impl.rs", SHA256: "bbb"},
		{Path: "./src/parser/lexer.rs", SHA256: "ccc"},
	}
	if got := Check(writes, frozen, files); len(got) != 0 {
		t.Fatalf("unexpected violations: %v", codes(got))
	}
}

func TestCheck_OutsideWrites(t *testing.T) {
	writes := []string{"src/parser/**"}
	files := []SubmittedFile{{Path: "src/main.rs", SHA256: "x"}}
	got := Check(writes, nil, files)
	if len(got) != 1 || got[0].Code != CodeOutsideWrites {
		t.Fatalf("want %s, got %v", CodeOutsideWrites, codes(got))
	}
}

func TestCheck_FrozenSubmitted(t *testing.T) {
	writes := []string{"src/**"}
	frozen := []Frozen{{Path: "src/parser/api.rs", SHA256: "aaa"}}
	// 即使哈希一致也算违规：冻结文件不该出现在提交里
	files := []SubmittedFile{{Path: "src/parser/api.rs", SHA256: "aaa"}}
	got := Check(writes, frozen, files)
	if len(got) != 1 || got[0].Code != CodeFrozenFile {
		t.Fatalf("want %s, got %v", CodeFrozenFile, codes(got))
	}
}

func TestCheck_DuplicateAfterNormalize(t *testing.T) {
	writes := []string{"src/**"}
	files := []SubmittedFile{
		{Path: "src/a.go", SHA256: "1"},
		{Path: "./src/a.go", SHA256: "2"},
	}
	got := Check(writes, nil, files)
	if len(got) != 1 || got[0].Code != CodeDuplicatePath {
		t.Fatalf("want %s, got %v", CodeDuplicatePath, codes(got))
	}
}

func TestCheck_InvalidPath(t *testing.T) {
	writes := []string{"src/**"}
	files := []SubmittedFile{{Path: "../etc/passwd", SHA256: "x"}}
	got := Check(writes, nil, files)
	if len(got) != 1 || got[0].Code != CodePathInvalid {
		t.Fatalf("want %s, got %v", CodePathInvalid, codes(got))
	}
}

func TestCheck_MultipleViolationsSorted(t *testing.T) {
	writes := []string{"src/**"}
	frozen := []Frozen{{Path: "src/b.rs", SHA256: "bbb"}}
	files := []SubmittedFile{
		{Path: "src/b.rs", SHA256: "bbb"},
		{Path: "lib/c.rs", SHA256: "c"},
		{Path: "../x", SHA256: "x"},
	}
	got := Check(writes, frozen, files)
	if len(got) != 3 {
		t.Fatalf("want 3 violations, got %v", codes(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Path > got[i].Path {
			t.Errorf("violations not sorted: %v", codes(got))
		}
	}
}

func TestCheck_WindowsCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only meaningful on case-insensitive filesystems")
	}
	writes := []string{"src/**"}
	frozen := []Frozen{{Path: "src/api.rs", SHA256: "aaa"}}
	files := []SubmittedFile{{Path: "SRC/Api.rs", SHA256: "aaa"}}
	got := Check(writes, frozen, files)
	if len(got) != 1 || got[0].Code != CodeFrozenFile {
		t.Fatalf("case-variant must hit frozen file, got %v", codes(got))
	}
}
