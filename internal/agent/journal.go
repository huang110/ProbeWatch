package agent

import (
	"bufio"
	"context"
	"errors"
	"io"
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
	validUnitRegex          = regexp.MustCompile(`^[a-zA-Z0-9_\.\@\-]+$`)
	validSinceRelativeRegex = regexp.MustCompile(`^-?[0-9]+[smhd]$`)
	validSinceISORegex      = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}([ T][0-9]{2}:[0-9]{2}:[0-9]{2})?$`)
	validPriorityRegex      = regexp.MustCompile(`^(emerg|alert|crit|err|warning|notice|info|debug|[0-7])$`)
)

// BuildJournalctlArgs validates and constructs structured argv arguments for journalctl.
func BuildJournalctlArgs(req protocol.LogQueryRequest) ([]string, error) {
	linesLimit := req.Lines
	if linesLimit <= 0 {
		linesLimit = 100
	} else if linesLimit > 1000 {
		linesLimit = 1000
	}

	if req.Unit != "" {
		trimmedUnit := strings.TrimSpace(req.Unit)
		if len(trimmedUnit) > 64 || !validUnitRegex.MatchString(trimmedUnit) {
			return nil, errors.New("invalid unit name")
		}
	}

	if req.Priority != "" {
		trimmedPri := strings.TrimSpace(req.Priority)
		if !validPriorityRegex.MatchString(trimmedPri) {
			return nil, errors.New("invalid priority parameter")
		}
	}

	if req.Since != "" {
		trimmedSince := strings.TrimSpace(req.Since)
		if len(trimmedSince) > 32 || (!validSinceRelativeRegex.MatchString(trimmedSince) && !validSinceISORegex.MatchString(trimmedSince)) {
			return nil, errors.New("invalid since parameter")
		}
	}

	if req.Grep != "" {
		trimmedGrep := strings.TrimSpace(req.Grep)
		if len(trimmedGrep) > 128 {
			return nil, errors.New("grep parameter exceeds maximum length 128")
		}
		if strings.ContainsAny(trimmedGrep, "\x00\r\n") {
			return nil, errors.New("grep contains disallowed characters")
		}
	}

	args := []string{"--no-pager", "-o", "short-iso", "-n", strconv.Itoa(linesLimit)}
	if req.Unit != "" {
		args = append(args, "-u", strings.TrimSpace(req.Unit))
	}
	if req.Priority != "" {
		args = append(args, "-p", strings.TrimSpace(req.Priority))
	}
	if req.Since != "" {
		trimmedSince := strings.TrimSpace(req.Since)
		if strings.HasPrefix(trimmedSince, "-") {
			args = append(args, "--since", trimmedSince)
		} else if validSinceRelativeRegex.MatchString(trimmedSince) {
			args = append(args, "--since", "-"+trimmedSince)
		} else {
			args = append(args, "--since", trimmedSince)
		}
	}
	if req.Grep != "" {
		args = append(args, "--grep="+strings.TrimSpace(req.Grep))
	}
	return args, nil
}

// QuerySystemLogs safely queries system logs on the local host using journalctl or syslog fallback.
func QuerySystemLogs(ctx context.Context, req protocol.LogQueryRequest) (*protocol.LogQueryResponse, error) {
	// Validate request parameters on every platform before the OS-specific
	// implementation. Otherwise Windows builds silently bypass the same input
	// safety checks enforced by journalctl on Linux.
	validatedArgs, err := BuildJournalctlArgs(req)
	if err != nil {
		return nil, err
	}
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
	if linesLimit <= 0 {
		linesLimit = 100
	} else if linesLimit > 1000 {
		linesLimit = 1000
	}

	args := validatedArgs

	queryCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	// Try journalctl first
	if journalctlPath, err := exec.LookPath("journalctl"); err == nil && journalctlPath != "" {
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
	isTruncated := false
	if info, err := file.Stat(); err == nil && info.Size() > 256*1024 {
		if _, err := file.Seek(-256*1024, io.SeekEnd); err == nil {
			isTruncated = true
		}
	}

	grepLower := strings.ToLower(strings.TrimSpace(grep))
	var lines []string
	scanner := bufio.NewScanner(file)
	if isTruncated {
		_ = scanner.Scan()
	}
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
