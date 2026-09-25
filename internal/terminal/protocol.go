package terminal

// MessageType indicates the purpose of an envelope.
type MessageType string

const (
	TypeExec           MessageType = "exec"
	TypeExecResult     MessageType = "exec_result"
	TypeSessionStart   MessageType = "session_start"
	TypeSessionStarted MessageType = "session_started"
	TypeData           MessageType = "data"
	TypeResize         MessageType = "resize"
	TypeSessionEnd     MessageType = "session_end"
	TypePing           MessageType = "ping"
	TypePong           MessageType = "pong"
	TypeError          MessageType = "error"
)

// Envelope is the unified JSON message carrier for terminal and command execution.
type Envelope struct {
	Type       MessageType `json:"type"`
	ExecID     string      `json:"exec_id,omitempty"`
	SessionID  string      `json:"session_id,omitempty"`
	NodeUUID   string      `json:"node_uuid,omitempty"`
	Command    string      `json:"command,omitempty"`
	TimeoutSec int         `json:"timeout_sec,omitempty"`
	Stdout     string      `json:"stdout,omitempty"`
	Stderr     string      `json:"stderr,omitempty"`
	ExitCode   int         `json:"exit_code,omitempty"`
	DurationMS int64       `json:"duration_ms,omitempty"`
	Data       string      `json:"data,omitempty"`
	Cols       int         `json:"cols,omitempty"`
	Rows       int         `json:"rows,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Error      string      `json:"error,omitempty"`
	Shell      string      `json:"shell,omitempty"`
}
