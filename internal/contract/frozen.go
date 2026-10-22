package contract

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// 违规码。与协议里的错误码一一对应，终端据此决定怎么改。
const (
	CodePathInvalid   = "path_invalid"
	CodeDuplicatePath = "duplicate_path"
	CodeOutsideWrites = "path_outside_writes"
	CodeFrozenFile    = "frozen_file_modified"
)

// Frozen 是一条冻结声明：路径 + 平台认可的哈希。
// 只要终端提交了这个路径，无论哈希是否相同都算违规——
// 冻结文件的正文由平台用这里的版本覆盖，终端改动能不生效，但必须被告知。
type Frozen struct {
	Path   string
	SHA256 string
}

// SubmittedFile 是终端提交的一条文件记录。
type SubmittedFile struct {
	Path   string
	SHA256 string
}

// Violation 描述一条不通过项。
type Violation struct {
	Code    string
	Path    string
	Message string
}

func (v Violation) Error() string {
	return fmt.Sprintf("%s: %s (%s)", v.Code, v.Path, v.Message)
}

// Check 做 L0/L1 的结构性校验，返回按路径排序的全部违规；空切片表示通过。
//
// 它只回答四个问题：
//  1. 路径本身合法吗（越界、绝对路径、控制字符）；
//  2. 同一路径被提交了两次吗；
//  3. 每个路径都落在写集里吗；
//  4. 有没有动冻结文件。
//
// 哈希重算、必填文件齐全、大小上限由调用方在做完 I/O 之后补上。
func Check(writes []string, frozen []Frozen, files []SubmittedFile) []Violation {
	var out []Violation

	frozenSet := make(map[string]Frozen, len(frozen))
	for _, f := range frozen {
		norm, err := NormalizePath(f.Path)
		if err != nil {
			continue // 冻结声明本身不合法属于建任务时的图校验，不在这里报
		}
		frozenSet[fold(norm)] = f
	}

	seen := make(map[string]string, len(files))
	for _, f := range files {
		norm, err := NormalizePath(f.Path)
		if err != nil {
			out = append(out, Violation{Code: CodePathInvalid, Path: f.Path, Message: err.Error()})
			continue
		}
		key := fold(norm)
		if prev, ok := seen[key]; ok {
			out = append(out, Violation{Code: CodeDuplicatePath, Path: norm,
				Message: fmt.Sprintf("duplicate of %q", prev)})
			continue
		}
		seen[key] = norm

		if !matchForFS(writes, norm) {
			out = append(out, Violation{Code: CodeOutsideWrites, Path: norm,
				Message: "not covered by any writes pattern"})
			continue
		}
		if fr, ok := frozenSet[key]; ok {
			out = append(out, Violation{Code: CodeFrozenFile, Path: norm,
				Message: fmt.Sprintf("frozen file must not be submitted (expected sha256 %s)", fr.SHA256)})
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// matchForFS 在 Windows 上做大小写不敏感匹配，与文件系统语义保持一致；
// 否则 src/** 拒掉 SRC/a.go 虽然安全，但会让终端莫名其妙。
func matchForFS(patterns []string, path string) bool {
	if runtime.GOOS != "windows" {
		return MatchAny(patterns, path)
	}
	lp := strings.ToLower(path)
	for _, p := range patterns {
		if Match(strings.ToLower(p), lp) {
			return true
		}
	}
	return false
}
