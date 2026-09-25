package terminal

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
)

// AgentSession manages a running interactive shell on the agent node.
type AgentSession struct {
	sessionID string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	cancel    context.CancelFunc
	closed    bool
	mu        sync.Mutex
}

// StartAgentSession starts an interactive shell process and begins streaming its output.
func StartAgentSession(parentCtx context.Context, sessionID string, cols, rows int, onOutput func(data string), onEnd func(reason string)) (*AgentSession, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	var shell string
	var args []string

	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	} else {
		if _, err := os.Stat("/bin/bash"); err == nil {
			shell = "/bin/bash"
			args = []string{"-i"}
		} else {
			shell = "/bin/sh"
			args = []string{"-i"}
		}
	}

	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
	)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		cancel()
		return nil, err
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		cancel()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		cancel()
		return nil, err
	}

	session := &AgentSession{
		sessionID: sessionID,
		cmd:       cmd,
		stdin:     stdinPipe,
		cancel:    cancel,
	}

	// Stream stdout and stderr concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	streamPipe := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				onOutput(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}

	go streamPipe(stdoutPipe)
	go streamPipe(stderrPipe)

	go func() {
		_ = cmd.Wait()
		wg.Wait()
		session.mu.Lock()
		session.closed = true
		session.mu.Unlock()
		if onEnd != nil {
			onEnd("process_exit")
		}
	}()

	return session, nil
}

// Write writes data to the process stdin.
func (s *AgentSession) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.stdin == nil {
		return errors.New("session closed")
	}
	_, err := s.stdin.Write(data)
	return err
}

// Resize is a stub for platforms without PTY support.
func (s *AgentSession) Resize(cols, rows int) {
	// Terminal resize hook
}

// Close terminates the shell process and releases resources.
func (s *AgentSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	s.cancel()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}
