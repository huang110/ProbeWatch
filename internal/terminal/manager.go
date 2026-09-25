package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNodeOffline         = errors.New("agent terminal is offline or unreachable")
	ErrExecutionTimeout    = errors.New("command execution timed out")
	ErrSessionClosed       = errors.New("terminal session closed")
	ErrSessionNotFound     = errors.New("terminal session not found")
	ErrInvalidSessionParam = errors.New("invalid session parameters")
)

const (
	maxSessionIdle = 15 * time.Minute
)

// AgentTunnel represents an active reverse WebSocket connection from an agent.
type AgentTunnel struct {
	NodeUUID    string
	Conn        *Conn
	ConnectedAt time.Time
}

// ServerSession tracks an active interactive terminal session between a browser and an agent.
type ServerSession struct {
	SessionID string
	NodeUUID  string
	CreatedAt time.Time
	LastSeen  time.Time
	Cols      int
	Rows      int

	browserConn *Conn
	agentTunnel *AgentTunnel
	closeOnce   sync.Once
	closed      chan struct{}
	mu          sync.Mutex
}

// Manager coordinates all reverse tunnels and terminal sessions on the server.
type Manager struct {
	mu           sync.RWMutex
	tunnels      map[string]*AgentTunnel
	sessions     map[string]*ServerSession
	pendingExecs map[string]chan Envelope
}

// NewManager creates an initialized terminal Manager.
func NewManager() *Manager {
	return &Manager{
		tunnels:      make(map[string]*AgentTunnel),
		sessions:     make(map[string]*ServerSession),
		pendingExecs: make(map[string]chan Envelope),
	}
}

// RegisterTunnel registers an active agent tunnel connection.
func (m *Manager) RegisterTunnel(nodeUUID string, conn *Conn) *AgentTunnel {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If existing tunnel exists, close it
	if old, exists := m.tunnels[nodeUUID]; exists {
		_ = old.Conn.Close()
	}

	tunnel := &AgentTunnel{
		NodeUUID:    nodeUUID,
		Conn:        conn,
		ConnectedAt: time.Now(),
	}
	m.tunnels[nodeUUID] = tunnel
	return tunnel
}

// UnregisterTunnel removes an agent tunnel.
func (m *Manager) UnregisterTunnel(nodeUUID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.tunnels, nodeUUID)

	// Terminate any active sessions targeting this node
	for id, s := range m.sessions {
		if s.NodeUUID == nodeUUID {
			s.Close()
			delete(m.sessions, id)
		}
	}
}

// IsNodeConnected checks if the node has an active reverse tunnel.
func (m *Manager) IsNodeConnected(nodeUUID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.tunnels[nodeUUID]
	return ok
}

// ConnectedNodes returns a slice of all currently connected node UUIDs.
func (m *Manager) ConnectedNodes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	nodes := make([]string, 0, len(m.tunnels))
	for uuid := range m.tunnels {
		nodes = append(nodes, uuid)
	}
	return nodes
}

// ExecuteCommand sends a single command to an agent node and awaits the result.
func (m *Manager) ExecuteCommand(ctx context.Context, nodeUUID string, command string, timeoutSec int) (Envelope, error) {
	m.mu.RLock()
	tunnel, ok := m.tunnels[nodeUUID]
	m.mu.RUnlock()

	if !ok || tunnel == nil {
		return Envelope{}, ErrNodeOffline
	}

	execID, err := generateID()
	if err != nil {
		return Envelope{}, err
	}

	respCh := make(chan Envelope, 1)
	m.mu.Lock()
	m.pendingExecs[execID] = respCh
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.pendingExecs, execID)
		m.mu.Unlock()
	}()

	env := Envelope{
		Type:       TypeExec,
		ExecID:     execID,
		Command:    command,
		TimeoutSec: timeoutSec,
	}

	if err := tunnel.Conn.WriteJSON(env); err != nil {
		return Envelope{}, fmt.Errorf("dispatch command: %w", err)
	}

	timeoutDuration := time.Duration(timeoutSec+5) * time.Second
	if timeoutSec <= 0 {
		timeoutDuration = 35 * time.Second
	}

	select {
	case <-ctx.Done():
		return Envelope{}, ctx.Err()
	case <-time.After(timeoutDuration):
		return Envelope{}, ErrExecutionTimeout
	case result := <-respCh:
		return result, nil
	}
}

// HandleTunnelMessage processes an inbound message from an agent tunnel.
func (m *Manager) HandleTunnelMessage(nodeUUID string, env Envelope) {
	switch env.Type {
	case TypeExecResult:
		m.mu.RLock()
		ch, ok := m.pendingExecs[env.ExecID]
		m.mu.RUnlock()
		if ok && ch != nil {
			select {
			case ch <- env:
			default:
			}
		}

	case TypeData, TypeSessionStarted, TypeSessionEnd, TypeError:
		m.mu.RLock()
		session, ok := m.sessions[env.SessionID]
		m.mu.RUnlock()
		if ok && session != nil {
			session.handleAgentMessage(env)
		}
	}
}

// CreateSession creates an interactive terminal session linking a browser connection and an agent.
func (m *Manager) CreateSession(sessionID, nodeUUID string, cols, rows int, browserConn *Conn, onAuditClose func(durationSeconds int64)) (*ServerSession, error) {
	m.mu.RLock()
	tunnel, ok := m.tunnels[nodeUUID]
	m.mu.RUnlock()

	if !ok || tunnel == nil {
		return nil, ErrNodeOffline
	}

	if sessionID == "" {
		var err error
		sessionID, err = generateID()
		if err != nil {
			return nil, err
		}
	}

	session := &ServerSession{
		SessionID:   sessionID,
		NodeUUID:    nodeUUID,
		CreatedAt:   time.Now(),
		LastSeen:    time.Now(),
		Cols:        cols,
		Rows:        rows,
		browserConn: browserConn,
		agentTunnel: tunnel,
		closed:      make(chan struct{}),
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	// Notify agent to start interactive session
	startEnv := Envelope{
		Type:      TypeSessionStart,
		SessionID: sessionID,
		Cols:      cols,
		Rows:      rows,
	}
	if err := tunnel.Conn.WriteJSON(startEnv); err != nil {
		m.RemoveSession(sessionID)
		return nil, fmt.Errorf("failed to notify agent: %w", err)
	}

	// Browser read loop
	go func() {
		defer func() {
			session.Close()
			m.RemoveSession(sessionID)
			if onAuditClose != nil {
				onAuditClose(int64(time.Since(session.CreatedAt).Seconds()))
			}
		}()

		idleTicker := time.NewTicker(30 * time.Second)
		defer idleTicker.Stop()

		for {
			select {
			case <-session.closed:
				return
			default:
			}

			// Check idle timeout
			if time.Since(session.LastSeen) > maxSessionIdle {
				_ = browserConn.WriteJSON(Envelope{
					Type:  TypeError,
					Error: "Session closed due to inactivity",
				})
				return
			}

			var env Envelope
			if err := browserConn.ReadJSON(&env); err != nil {
				return
			}

			session.LastSeen = time.Now()

			switch env.Type {
			case TypeData:
				env.SessionID = sessionID
				_ = tunnel.Conn.WriteJSON(env)
			case TypeResize:
				env.SessionID = sessionID
				_ = tunnel.Conn.WriteJSON(env)
			case TypeSessionEnd:
				_ = tunnel.Conn.WriteJSON(Envelope{
					Type:      TypeSessionEnd,
					SessionID: sessionID,
				})
				return
			case TypePing:
				_ = browserConn.WriteJSON(Envelope{Type: TypePong})
			}
		}
	}()

	return session, nil
}

func (s *ServerSession) handleAgentMessage(env Envelope) {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case <-s.closed:
		return
	default:
	}

	s.LastSeen = time.Now()

	switch env.Type {
	case TypeData:
		_ = s.browserConn.WriteJSON(Envelope{
			Type: TypeData,
			Data: env.Data,
		})
	case TypeSessionStarted:
		_ = s.browserConn.WriteJSON(Envelope{
			Type:  TypeSessionStarted,
			Shell: env.Shell,
		})
	case TypeSessionEnd:
		_ = s.browserConn.WriteJSON(Envelope{
			Type:   TypeSessionEnd,
			Reason: env.Reason,
		})
		s.Close()
	case TypeError:
		_ = s.browserConn.WriteJSON(Envelope{
			Type:  TypeError,
			Error: env.Error,
		})
		s.Close()
	}
}

// Close closes the session.
func (s *ServerSession) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.browserConn.Close()
	})
}

// RemoveSession removes a session from tracking.
func (m *Manager) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
