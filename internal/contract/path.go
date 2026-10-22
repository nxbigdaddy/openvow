// Package contract 定义任务契约的纯函数部分：路径规范化、glob 匹配、冻结比对。
//
// 这一层不做任何文件系统操作——它只回答「这个字符串能不能被接受」。
// 符号链接、目录穿越的实际解析由 workspace 层在落盘时再做一次。
package contract

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// ErrInvalidPath 表示提交上来的路径不合法（越界、绝对路径、含 .. 等）。
var ErrInvalidPath = errors.New("invalid path")

// NormalizePath 把路径规范化为「相对仓库根、以 / 分隔、不含 . 与 ..」的形式。
//
// 规则：
//   - 反斜杠统一成 /（终端在 Windows 上很常见）；
//   - 折叠连续的 / 与单独的 .；
//   - 拒绝绝对路径（/ 开头、盘符、UNC）；
//   - 拒绝任何 .. 段——不做"解析后再判断"，因为出现 .. 本身就是可疑信号；
//   - 拒绝空路径、控制字符、以 / 结尾的目录。
func NormalizePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidPath)
	}
	for _, r := range p {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: control character", ErrInvalidPath)
		}
	}

	q := strings.ReplaceAll(p, "\\", "/")

	if strings.HasPrefix(q, "/") {
		return "", fmt.Errorf("%w: absolute path %q", ErrInvalidPath, p)
	}
	// Windows 盘符：C:foo / C:/foo
	if len(q) >= 2 && q[1] == ':' {
		return "", fmt.Errorf("%w: drive letter in %q", ErrInvalidPath, p)
	}
	if strings.HasSuffix(q, "/") {
		return "", fmt.Errorf("%w: directory path %q", ErrInvalidPath, p)
	}

	segs := strings.Split(q, "/")
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		switch s {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("%w: parent segment in %q", ErrInvalidPath, p)
		}
		if err := validateSegment(s); err != nil {
			return "", err
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return "", fmt.Errorf("%w: empty after normalization", ErrInvalidPath)
	}
	return strings.Join(out, "/"), nil
}

// validateSegment 拒绝在 Windows 上非法、或在不同平台上语义不同的字符。
//
// 重点说两个：
//   - ':' 在 Windows 上是数据流分隔符（a.txt:hidden 指向 a.txt 的隐藏流），
//     既是绕过写集检查的手法，也会让 "0:" 这种段在二次规范化时被误判成盘符；
//   - 结尾的 '.' 与空格会被 Windows 静默裁掉，导致 a. 与 a 落到同一个文件。
//
// 代价是禁用少量在 Linux 上合法的文件名。对一个要在多平台合并产物的系统，
// 这种"收紧"比"某天在 Windows 上被绕过"划算。
func validateSegment(s string) error {
	for _, r := range s {
		switch r {
		case ':', '*', '?', '"', '<', '>', '|':
			return fmt.Errorf("%w: reserved character %q in segment %q", ErrInvalidPath, r, s)
		}
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") {
		return fmt.Errorf("%w: segment %q ends with '.' or space", ErrInvalidPath, s)
	}
	return nil
}

// ValidatePattern 校验写集 / 冻结声明里的 glob 模式。
// 允许 * ** ? 通配符，但不允许逃逸出仓库。
func ValidatePattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("%w: empty pattern", ErrInvalidPath)
	}
	if strings.ContainsRune(pattern, 0) {
		return fmt.Errorf("%w: control character in pattern", ErrInvalidPath)
	}
	if strings.Contains(pattern, "\\") {
		return fmt.Errorf("%w: pattern must use '/': %q", ErrInvalidPath, pattern)
	}
	if strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("%w: absolute pattern %q", ErrInvalidPath, pattern)
	}
	if len(pattern) >= 2 && pattern[1] == ':' {
		return fmt.Errorf("%w: drive letter in pattern %q", ErrInvalidPath, pattern)
	}
	for _, s := range strings.Split(pattern, "/") {
		if s == ".." {
			return fmt.Errorf("%w: parent segment in pattern %q", ErrInvalidPath, pattern)
		}
		if s == "" {
			return fmt.Errorf("%w: empty segment in pattern %q", ErrInvalidPath, pattern)
		}
	}
	return nil
}

// fold 用于比较：Windows 文件系统大小写不敏感，必须按不敏感比较，
// 否则 Src/Main.go 可以绕过 src/main.go 的冻结。Linux 上保持原样。
func fold(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
