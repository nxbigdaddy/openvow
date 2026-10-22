package contract

import "testing"

func TestNormalizePath_Valid(t *testing.T) {
	cases := []struct{ in, want string }{
		{"src/a.go", "src/a.go"},
		{"./src/a.go", "src/a.go"},
		{"src//a.go", "src/a.go"},
		{"src/./a.go", "src/a.go"},
		{"src/nested/../a.go", ""}, // 单独在 invalid 里测
		{`src\a.go`, "src/a.go"},
		{`src\nested\a.go`, "src/nested/a.go"},
		{"docs/设计 文档.md", "docs/设计 文档.md"},
		{"a", "a"},
		{"a/b/c/d.txt", "a/b/c/d.txt"},
	}
	for _, c := range cases {
		if c.want == "" {
			continue
		}
		got, err := NormalizePath(c.in)
		if err != nil {
			t.Errorf("NormalizePath(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePath_Invalid(t *testing.T) {
	cases := []string{
		"",
		".",
		"./",
		"..",
		"../etc/passwd",
		"src/../../etc/passwd",
		"src/nested/../a.go",
		"/etc/passwd",
		`\windows\system32`,
		`\\server\share\file`,
		"C:/Windows/system32",
		"c:relative",
		"src/",
		"src/a\x00.go",
		"src/a\x01.go",
		"./0:",       // 规范化后变成 "0:"，二次校验会被当成盘符（fuzz 发现）
		"a:b",        // Windows 数据流
		"a.txt:ads",  // 同上
		"a*b",        // 通配符不应出现在路径里
		"a?b",        // 同上
		"a.",         // Windows 会裁掉结尾的点
		"dir/a ",     // Windows 会裁掉结尾的空格
		`a<b`, `a>b`, `a|b`, `a"b`,
	}
	for _, in := range cases {
		if got, err := NormalizePath(in); err == nil {
			t.Errorf("NormalizePath(%q) = %q, want error", in, got)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	valid := []string{"src/**", "**", "src/*.go", "a/**/b.go", "src/**/*.go", "a.txt", "?/b"}
	for _, p := range valid {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("ValidatePattern(%q) unexpected error: %v", p, err)
		}
	}
	invalid := []string{"", "/abs", `src\**.go`, "../x", "a/../b", "a//b", "C:/x"}
	for _, p := range invalid {
		if err := ValidatePattern(p); err == nil {
			t.Errorf("ValidatePattern(%q) want error", p)
		}
	}
}
