package contract

import "testing"

// 属性：规范化必须是幂等的——否则终端改一遍路径就能绕过校验。
func FuzzNormalizePath(f *testing.F) {
	for _, s := range []string{
		"src/a.go", "./a", "../x", "/abs", "a//b", "C:/x", `a\b`,
		"文档/说明.md", "", ".", "a/./b", "a/../b",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		got, err := NormalizePath(p)
		if err != nil {
			return
		}
		again, err2 := NormalizePath(got)
		if err2 != nil {
			t.Fatalf("normalized %q rejected on second pass: %v", got, err2)
		}
		if again != got {
			t.Fatalf("not idempotent: %q -> %q -> %q", p, got, again)
		}
	})
}

// 属性：匹配只要求不 panic、不死循环。
func FuzzMatch(f *testing.F) {
	f.Add("src/**", "src/a")
	f.Add("*", "a")
	f.Add("a/**/b", "a/b")
	f.Add("?", "x")
	f.Fuzz(func(t *testing.T, pattern, path string) {
		_ = Match(pattern, path)
	})
}
