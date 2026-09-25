package terminal

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"time"
)

const (
	maxOutputBytes     = 128 * 1024 // 128 KB max output buffer
	defaultExecTimeout = 30 * time.Second
	maxExecTimeout     = 120 * time.Second
)

// RunExec executes a command on the local node under strict limits and returns an Envelope.
func RunExec(ctx context.Context, command string, timeoutSec int) Envelope {
	start := time.Now()

	timeout := defaultExecTimeout
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
		if timeout > maxExecTimeout {
			timeout = maxExecTimeout
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(execCtx, "cmd.exe", "/c", command)
	} else {
		cmd = exec.CommandContext(execCtx, "/bin/sh", "-c", command)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdoutBuf, limit: maxOutputBytes}
	cmd.Stderr = &limitedWriter{w: &stderrBuf, limit: maxOutputBytes}

	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			exitCode = 124 // Standard timeout exit code
			if stderrBuf.Len() > 0 {
				stderrBuf.WriteString("\n")
			}
			stderrBuf.WriteString("command timed out after execution window")
		} else {
			exitCode = 1
			if stderrBuf.Len() > 0 {
				stderrBuf.WriteString("\n")
			}
			stderrBuf.WriteString(err.Error())
		}
	}

	return Envelope{
		Type:       TypeExecResult,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		ExitCode:   exitCode,
		DurationMS: duration,
	}
}

type limitedWriter struct {
	w     *bytes.Buffer
	limit int
	count int
}

func (lw *limitedWriter) Write(p []byte) (n int, err error) {
	if lw.count >= lw.limit {
		return len(p), nil // Silently discard once limit reached
	}
	remaining := lw.limit - lw.count
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err = lw.w.Write(p)
	lw.count += n
	return len(p), err
}
