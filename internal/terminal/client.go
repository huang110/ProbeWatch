package terminal

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client handles the agent-side reverse tunnel to the ProbeWatch server.
type Client struct {
	endpoint  string
	nodeUUID  string
	nodeToken string

	mu       sync.Mutex
	conn     *Conn
	sessions map[string]*AgentSession
}

// NewClient creates a new terminal tunnel client.
func NewClient(endpoint, nodeUUID, nodeToken string) *Client {
	return &Client{
		endpoint:  endpoint,
		nodeUUID:  nodeUUID,
		nodeToken: nodeToken,
		sessions:  make(map[string]*AgentSession),
	}
}

// Run runs the client connection loop, reconnecting on disconnection until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	wsURL := c.buildWebSocketURL()

	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			c.closeAll()
			return
		default:
		}

		err := c.connectAndServe(ctx, wsURL)
		if ctx.Err() != nil {
			return
		}

		if err != nil {
			// Backoff on connection error
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
		} else {
			backoff = 1 * time.Second
		}
	}
}

func (c *Client) buildWebSocketURL() string {
	ep := strings.TrimRight(c.endpoint, "/")
	if strings.HasPrefix(ep, "https://") {
		return "wss://" + strings.TrimPrefix(ep, "https://") + "/api/agent/v1/terminal/tunnel"
	}
	if strings.HasPrefix(ep, "http://") {
		return "ws://" + strings.TrimPrefix(ep, "http://") + "/api/agent/v1/terminal/tunnel"
	}
	return "ws://" + ep + "/api/agent/v1/terminal/tunnel"
}

func (c *Client) connectAndServe(ctx context.Context, wsURL string) error {
	header := make(http.Header)
	header.Set("X-Node-UUID", c.nodeUUID)
	header.Set("X-Node-Token", c.nodeToken)

	conn, err := Dial(ctx, wsURL, header)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		_ = conn.Close()
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		c.closeAll()
	}()

	// Ping ticker for NAT keepalive
	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()

	errCh := make(chan error, 1)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pingTicker.C:
				if err := conn.WriteJSON(Envelope{Type: TypePing}); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	go func() {
		for {
			var env Envelope
			if err := conn.ReadJSON(&env); err != nil {
				errCh <- err
				return
			}
			c.handleMessage(ctx, env)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (c *Client) handleMessage(ctx context.Context, env Envelope) {
	switch env.Type {
	case TypePing:
		c.send(Envelope{Type: TypePong})

	case TypeExec:
		go func() {
			result := RunExec(ctx, env.Command, env.TimeoutSec)
			result.ExecID = env.ExecID
			c.send(result)
		}()

	case TypeSessionStart:
		c.startSession(ctx, env)

	case TypeData:
		c.mu.Lock()
		session, ok := c.sessions[env.SessionID]
		c.mu.Unlock()
		if ok && session != nil {
			_ = session.Write([]byte(env.Data))
		}

	case TypeResize:
		c.mu.Lock()
		session, ok := c.sessions[env.SessionID]
		c.mu.Unlock()
		if ok && session != nil {
			session.Resize(env.Cols, env.Rows)
		}

	case TypeSessionEnd:
		c.closeSession(env.SessionID)
	}
}

func (c *Client) startSession(ctx context.Context, env Envelope) {
	sessionID := env.SessionID
	cols := env.Cols
	if cols <= 0 {
		cols = 80
	}
	rows := env.Rows
	if rows <= 0 {
		rows = 24
	}

	onOutput := func(data string) {
		c.send(Envelope{
			Type:      TypeData,
			SessionID: sessionID,
			Data:      data,
		})
	}

	onEnd := func(reason string) {
		c.send(Envelope{
			Type:      TypeSessionEnd,
			SessionID: sessionID,
			Reason:    reason,
		})
		c.closeSession(sessionID)
	}

	session, err := StartAgentSession(ctx, sessionID, cols, rows, onOutput, onEnd)
	if err != nil {
		c.send(Envelope{
			Type:      TypeError,
			SessionID: sessionID,
			Error:     fmt.Sprintf("start session failed: %v", err),
		})
		return
	}

	c.mu.Lock()
	c.sessions[sessionID] = session
	c.mu.Unlock()

	c.send(Envelope{
		Type:      TypeSessionStarted,
		SessionID: sessionID,
	})
}

func (c *Client) closeSession(sessionID string) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	delete(c.sessions, sessionID)
	c.mu.Unlock()

	if ok && session != nil {
		_ = session.Close()
	}
}

func (c *Client) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, s := range c.sessions {
		_ = s.Close()
		delete(c.sessions, id)
	}
}

func (c *Client) send(env Envelope) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn != nil {
		_ = conn.WriteJSON(env)
	}
}
