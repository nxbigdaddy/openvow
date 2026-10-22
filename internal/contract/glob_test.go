package contract

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"src/**", "src/a.go", true},
		{"src/**", "src/x/y.go", true},
		{"src/**", "lib/a.go", false},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/x/a.go", false},
		{"**/*.go", "a.go", true},
		{"**/*.go", "src/a.go", true},
		{"a/**/b.go", "a/b.go", true},
		{"a/**/b.go", "a/x/y/b.go", true},
		{"a/**/b.go", "a/x/y/c.go", false},
		{"src/?", "src/a", true},
		{"src/?", "src/ab", false},
		{"src/a?.go", "src/ab.go", true},
		{"src/a?.go", "src/a.go", false},
		{"src/a.go", "src/a.go", true},
		{"src/a.go", "src/A.go", false}, // 大小写敏感（Windows 折叠在 Check 里做）
		{"**", "a/b/c", true},
		{"**", "a", true},
		{"src/**/x", "src/x", true},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestMatchAny(t *testing.T) {
	writes := []string{"src/parser/**", "src/lexer/**"}
	if !MatchAny(writes, "src/parser/mod.rs") {
		t.Error("expected match in parser")
	}
	if MatchAny(writes, "src/main.rs") {
		t.Error("unexpected match for src/main.rs")
	}
	if MatchAny(nil, "src/parser/mod.rs") {
		t.Error("empty write set must match nothing")
	}
}
