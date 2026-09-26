package agent

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

var (
	validUnitRegex     = regexp.MustCompile(`^[a-zA-Z0-9_\.\@\-]+$`)
	validSinceRegex    = regexp.MustCompile(`^[0-9]+[smhd]$`)
	validPriorityRegex = regexp.MustCompile(`^(emerg|alert|crit|err|warning|notice|info|debug|[0-7])$`)
)

// QuerySystemLogs safely queries system logs on the local host using journalctl or syslog fallback.
func QuerySystemLogs(ctx context.Context, req protocol.LogQueryRequest) (*protocol.LogQueryResponse, error) {
	if runtime.GOOS != "linux" {
		return &protocol.LogQueryResponse{
			NodeUUID:   req.NodeUUID,
			Unit:       req.Unit,
			LinesCount: 1,
			Lines:      []string{"System log streaming is currently supported on Linux hosts."},
			QueriedAt:  time.Now().Unix(),
		}, nil
	}

	linesLimit := req.Lines
	if linesLimit <= 0 || linesLimit > 500 {
		linesLimit = 100
	}

	queryCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	// Try journalctl first
	if journalctlPath, err := exec.LookPath("journalctl"); err == nil && journalctlPath != "" {
		args := []string{"--no-pager", "-o", "short-iso", "-n", strconv.Itoa(linesLimit)}

		if req.Unit != "" {
			trimmedUnit := strings.TrimSpace(req.Unit)
			if !validUnitRegex.MatchString(trimmedUnit) {
				return nil, errors.New("invalid unit name")
			}
			args = append(args, "-u", trimmedUnit)
		}

		if req.Priority != "" {
			trimmedPri := strings.TrimSpace(req.Priority)
			if !validPriorityRegex.MatchString(trimmedPri) {
				return nil, errors.New("invalid priority parameter")
			}
			args = append(args, "-p", trimmedPri)
		}

		if req.Since != "" {
			trimmedSince := strings.TrimSpace(req.Since)
			if validSinceRegex.MatchString(trimmedSince) {
				args = append(args, "--since", "-"+trimmedSince)
			}
		}

		if req.Grep != "" {
			trimmedGrep := strings.TrimSpace(req.Grep)
			// Ensure grep doesn't contain null or newlines
			if !strings.ContainsAny(trimmedGrep, "\x00\r\n") && len(trimmedGrep) <= 128 {
				args = append(args, "-g", trimmedGrep)
			}
		}

		cmd := exec.CommandContext(queryCtx, "journalctl", args...)
		output, cmdErr := cmd.Output()
		if cmdErr == nil {
			rawLines := strings.Split(string(output), "\n")
			resultLines := make([]string, 0, len(rawLines))
			for _, l := range rawLines {
				l = strings.TrimRight(l, "\r\n")
				if l != "" {
					// Bound line length to 1024 chars for safety
					if len(l) > 1024 {
						l = l[:1024] + "..."
					}
					resultLines = append(resultLines, l)
				}
			}

			return &protocol.LogQueryResponse{
				NodeUUID:   req.NodeUUID,
				Unit:       req.Unit,
				LinesCount: len(resultLines),
				Lines:      resultLines,
				QueriedAt:  time.Now().Unix(),
			}, nil
		}
	}

	// Fallback to /var/log/syslog or /var/log/messages
	fallbackLines := readFilteredLogTail("/var/log/syslog", linesLimit, req.Grep)
	if len(fallbackLines) == 0 {
		fallbackLines = readFilteredLogTail("/var/log/messages", linesLimit, req.Grep)
	}

	return &protocol.LogQueryResponse{
		NodeUUID:   req.NodeUUID,
		Unit:       req.Unit,
		LinesCount: len(fallbackLines),
		Lines:      fallbackLines,
		QueriedAt:  time.Now().Unix(),
	}, nil
}

func readFilteredLogTail(path string, maxLines int, grep string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	grepLower := strings.ToLower(strings.TrimSpace(grep))
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := scanner.Text()
		if grepLower != "" && !strings.Contains(strings.ToLower(text), grepLower) {
			continue
		}
		if len(text) > 1024 {
			text = text[:1024] + "..."
		}
		lines = append(lines, text)
		if len(lines) > maxLines*2 {
			lines = lines[len(lines)-maxLines:]
		}
	}

	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}
