package contract

import "strings"

// Match 判断相对路径 path 是否匹配 glob pattern。
//
// 语义（刻意做窄，M0 不需要字符类）：
//   - `*`   匹配段内任意个字符（不含 /）
//   - `?`   匹配段内恰好一个字符
//   - `**`  作为完整段出现时，匹配零个或多个段
//   - 其它字符按字面匹配，大小写敏感（Windows 上的折叠在 frozen 比较里做）
//
// 例：
//
//	Match("src/**", "src/a.go")      == true
//	Match("src/**", "src/x/y.go")    == true
//	Match("src/*.go", "src/x/a.go")  == false
//	Match("a/**/b.go", "a/b.go")     == true   （** 匹配零段）
func Match(pattern, path string) bool {
	if pattern == "" || path == "" {
		return false
	}
	pat := strings.Split(strings.Trim(pattern, "/"), "/")
	name := strings.Split(strings.Trim(path, "/"), "/")
	return matchSegments(pat, name)
}

// MatchAny 判断 path 是否匹配任一 pattern；空列表返回 false。
func MatchAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if Match(p, path) {
			return true
		}
	}
	return false
}

func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		// ** 吃掉 0..n 段，逐个尝试
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if !matchSegment(pat[0], name[0]) {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}

// matchSegment 是段内通配，支持 * 与 ?，用双指针回溯实现。
func matchSegment(pat, s string) bool {
	p, i := 0, 0
	star, mark := -1, 0
	for i < len(s) {
		switch {
		case p < len(pat) && (pat[p] == '?' || pat[p] == s[i]):
			p++
			i++
		case p < len(pat) && pat[p] == '*':
			star = p
			mark = i
			p++
		case star >= 0:
			p = star + 1
			mark++
			i = mark
		default:
			return false
		}
	}
	for p < len(pat) && pat[p] == '*' {
		p++
	}
	return p == len(pat)
}
