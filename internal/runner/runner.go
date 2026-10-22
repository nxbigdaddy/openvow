// Package runner 在工作目录里执行一条命令，带超时与输出截断。
package runner

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const maxOutput = 64 << 10 // 64 KB

type Result struct {
	ExitCode int
	Output   string
	TimedOut bool
	Err      error
}

// Run 执行 command（按平台用 cmd /c 或 sh -c 包一层）。
func Run(ctx context.Context, dir, command string, timeout time.Duration) Result {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = dir

	buf := &limitedBuffer{limit: maxOutput}
	cmd.Stdout = buf
	cmd.Stderr = buf

	err := cmd.Run()
	res := Result{Output: buf.String()}

	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = -1
		return res
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res
		}
		res.Err = err
		res.ExitCode = -1
		return res
	}
	return res
}

// limitedBuffer 只保留前 limit 字节，并记录被截断。
type limitedBuffer struct {
	buf       strings.Builder
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.buf.Len() < b.limit {
		room := b.limit - b.buf.Len()
		if len(p) > room {
			b.buf.Write(p[:room])
			b.truncated = true
		} else {
			b.buf.Write(p)
		}
	} else {
		b.truncated = true
	}
	return n, nil
}

func (b *limitedBuffer) String() string {
	s := b.buf.String()
	if b.truncated {
		s += "\n…（输出已截断）"
	}
	return s
}
