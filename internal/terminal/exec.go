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

	timeout := normalizeExecTimeout(timeoutSec)

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/c", command)
	} else {
		cmd = exec.Command("/bin/sh", "-c", command)
	}
	configureProcessGroup(cmd)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdoutBuf, limit: maxOutputBytes}
	cmd.Stderr = &limitedWriter{w: &stderrBuf, limit: maxOutputBytes}

	if err := cmd.Start(); err != nil {
		return Envelope{
			Type:       TypeExecResult,
			Stderr:     err.Error(),
			ExitCode:   1,
			DurationMS: time.Since(start).Milliseconds(),
		}
	}

	// CommandContext only kills the shell process.  A timed-out shell can
	// leave grandchildren (powershell, sleep, pipelines, etc.) running and
	// consuming the node's resources. Wait explicitly so the platform helper
	// can terminate the complete process tree before we return.
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitCh:
	case <-execCtx.Done():
		_ = terminateProcessTree(cmd)
		err = <-waitCh
	}
	duration := time.Since(start).Milliseconds()

	exitCode := 0
	if err != nil {
		// CommandContext terminates the process with an ExitError on most
		// platforms. Check the context first so callers can reliably
		// distinguish a timeout from an ordinary command failure.
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			exitCode = 124 // Standard timeout exit code
			if stderrBuf.Len() > 0 {
				stderrBuf.WriteString("\n")
			}
			stderrBuf.WriteString("command timed out after execution window")
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
				if stderrBuf.Len() > 0 {
					stderrBuf.WriteString("\n")
				}
				stderrBuf.WriteString(err.Error())
			}
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

func normalizeExecTimeout(timeoutSec int) time.Duration {
	if timeoutSec <= 0 {
		return defaultExecTimeout
	}
	maxSeconds := int(maxExecTimeout / time.Second)
	if timeoutSec >= maxSeconds {
		return maxExecTimeout
	}
	return time.Duration(timeoutSec) * time.Second
}

type limitedWriter struct {
	w     *bytes.Buffer
	limit int
	count int
}

func (lw *limitedWriter) Write(p []byte) (n int, err error) {
	inputLen := len(p)
	if lw.count >= lw.limit {
		return inputLen, nil // Silently discard once limit reached
	}
	remaining := lw.limit - lw.count
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, err = lw.w.Write(p)
	lw.count += len(p)
	// Report the complete input as consumed even when the tail is discarded;
	// returning a short write with nil error makes os/exec fail the command.
	return inputLen, err
}
