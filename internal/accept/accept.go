// Package accept 把 acceptance 契约翻译成「跑哪些命令、什么算通过」。
package accept

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"openvow/internal/runner"
)

const (
	KindSkeletonBuild = "skeleton_build"
	KindRedTests      = "red_tests"
	KindGreenTests    = "green_tests"
)

// 拒绝码，与协议文档一致。
const (
	CodeBuildFailed      = "build_failed"
	CodeTestsNotRed      = "tests_not_red"
	CodeAcceptanceFailed = "acceptance_failed"
	CodeTimeout          = "acceptance_timeout"
	CodeUnknownKind      = "acceptance_unknown_kind"
)

type Spec struct {
	Kind     string `json:"kind"`
	Build    string `json:"build"`
	Run      string `json:"run"`
	TimeoutS int    `json:"timeout_s"`
	// Expect 目前只用于 red_tests 的可读性，判定仍以退出码为准。
	Expect string `json:"expect"`
}

type Outcome struct {
	Accepted bool
	Code     string
	Detail   map[string]any
}

// Evaluate 按 kind 执行验收：
//
//	skeleton_build —— build 必须 0；
//	green_tests    —— build 0 且 run 0；
//	red_tests      —— build 0 且 run 非 0（非超时），否则说明测试是空的。
func Evaluate(ctx context.Context, dir string, raw json.RawMessage) Outcome {
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return Outcome{Code: CodeUnknownKind, Detail: map[string]any{"error": "acceptance is not valid JSON: " + err.Error()}}
	}
	return EvaluateSpec(ctx, dir, spec)
}

func EvaluateSpec(ctx context.Context, dir string, spec Spec) Outcome {
	timeout := time.Duration(spec.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	detail := map[string]any{"kind": spec.Kind}

	if spec.Build == "" && spec.Run == "" {
		return Outcome{Code: CodeUnknownKind, Detail: map[string]any{"error": "acceptance has no commands"}}
	}

	if spec.Build != "" {
		r := runner.Run(ctx, dir, spec.Build, timeout)
		detail["build_exit"] = r.ExitCode
		detail["build_output"] = r.Output
		if r.TimedOut {
			detail["timed_out"] = true
			return Outcome{Code: CodeTimeout, Detail: detail}
		}
		if r.Err != nil {
			detail["error"] = r.Err.Error()
			return Outcome{Code: CodeBuildFailed, Detail: detail}
		}
		if r.ExitCode != 0 {
			return Outcome{Code: CodeBuildFailed, Detail: detail}
		}
	}

	switch spec.Kind {
	case KindSkeletonBuild:
		return Outcome{Accepted: true, Detail: detail}

	case KindGreenTests:
		if spec.Run == "" {
			return Outcome{Code: CodeUnknownKind, Detail: merge(detail, "error", "green_tests requires run")}
		}
		r := runner.Run(ctx, dir, spec.Run, timeout)
		detail["run_exit"] = r.ExitCode
		detail["run_output"] = r.Output
		if r.TimedOut {
			detail["timed_out"] = true
			return Outcome{Code: CodeTimeout, Detail: detail}
		}
		if r.Err != nil {
			detail["error"] = r.Err.Error()
			return Outcome{Code: CodeAcceptanceFailed, Detail: detail}
		}
		if r.ExitCode != 0 {
			return Outcome{Code: CodeAcceptanceFailed, Detail: detail}
		}
		return Outcome{Accepted: true, Detail: detail}

	case KindRedTests:
		if spec.Run == "" {
			return Outcome{Code: CodeUnknownKind, Detail: merge(detail, "error", "red_tests requires run")}
		}
		r := runner.Run(ctx, dir, spec.Run, timeout)
		detail["run_exit"] = r.ExitCode
		detail["run_output"] = r.Output
		if r.TimedOut {
			detail["timed_out"] = true
			return Outcome{Code: CodeTimeout, Detail: detail}
		}
		if r.ExitCode == 0 {
			// 空测试在骨架上也会全绿——这正是要拦的情况
			return Outcome{Code: CodeTestsNotRed, Detail: merge(detail, "hint", "tests passed on the skeleton; they must fail")}
		}
		return Outcome{Accepted: true, Detail: detail}

	default:
		return Outcome{Code: CodeUnknownKind, Detail: merge(detail, "error", fmt.Sprintf("unknown acceptance kind %q", spec.Kind))}
	}
}

func merge(m map[string]any, k string, v any) map[string]any {
	m[k] = v
	return m
}
